/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// LLM proxy ingester. Acts as an HTTP proxy that forwards LLM traffic to the
// upstream provider while ingesting structured events (user prompts, assistant
// replies, tool calls, token usage) into Gravwell. Provider support is
// pluggable via the protocol package; the built-in protocols are "openai-chat"
// (OpenAI /v1/chat/completions) and "anthropic-messages" (Anthropic /v1/messages).
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	_ "time/tzdata"

	"github.com/gravwell/gravwell/v4/debug"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/base"
	"github.com/gravwell/gravwell/v4/ingesters/llm_ingester/protocol"
	_ "github.com/gravwell/gravwell/v4/ingesters/llm_ingester/protocol/anthropic"
	_ "github.com/gravwell/gravwell/v4/ingesters/llm_ingester/protocol/openai"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/gravwell_llm_ingester.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/gravwell_llm_ingester.conf.d`
	appName           = `llmingester`

	httpServerReadHeaderTimeout = 10 * time.Second
	httpServerIdleConnTimeout   = 30 * time.Second
	shutdownTimeout             = 30 * time.Second
)

func main() {
	go debug.HandleDebugSignals(appName)

	var cfg *cfgType
	ibc := base.IngesterBaseConfig{
		IngesterName:                 appName,
		AppName:                      appName,
		DefaultConfigLocation:        defaultConfigLoc,
		DefaultConfigOverlayLocation: defaultConfigDLoc,
		GetConfigFunc:                GetConfig,
	}
	ib, err := base.Init(ibc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	if err = ib.AssignConfig(&cfg); err != nil || cfg == nil {
		fmt.Fprintf(os.Stderr, "failed to assign config: %v\n", err)
		os.Exit(1)
	}
	lg := ib.Logger

	utils.StartProfile()
	defer utils.StopProfile()

	igst, err := ib.GetMuxer()
	if err != nil {
		lg.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()
	ib.AnnounceStartup()

	sessions, err := newSessionStore(longestTTL(cfg), cfg.Session_Match_Window, cfg.State_Store_Location)
	if err != nil {
		lg.FatalCode(0, "failed to init session store", log.KVErr(err))
		return
	}

	lg.Info("registered protocols", log.KV("protocols", strings.Join(protocol.Names(), ", ")))

	type listenerSrv struct {
		name string
		cfg  *listener
		srv  *http.Server
	}
	var servers []listenerSrv
	for name, lcfg := range cfg.Listener {
		proto, err := protocol.Lookup(lcfg.Protocol)
		if err != nil {
			lg.FatalCode(0, "unknown protocol", log.KV("listener", name), log.KVErr(err))
			return
		}
		tag, err := igst.NegotiateTag(lcfg.Tag_Name)
		if err != nil {
			lg.FatalCode(0, "failed to negotiate tag",
				log.KV("listener", name), log.KV("tag", lcfg.Tag_Name), log.KVErr(err))
			return
		}
		pproc, err := cfg.Preprocessor.ProcessorSet(igst, lcfg.Preprocessor)
		if err != nil {
			lg.FatalCode(0, "failed to build preprocessor set",
				log.KV("listener", name), log.KVErr(err))
			return
		}
		ph, err := newProxyHandler(name, lcfg, proto, tag, pproc, sessions, lg)
		if err != nil {
			lg.FatalCode(0, "failed to build proxy handler",
				log.KV("listener", name), log.KVErr(err))
			return
		}
		mux := http.NewServeMux()
		for _, p := range proto.Paths() {
			mux.Handle(p, ph)
		}
		for _, p := range proto.PassthroughPaths() {
			mux.Handle(p, ph)
		}
		srv := &http.Server{
			Addr:              lcfg.Bind,
			Handler:           mux,
			ReadHeaderTimeout: httpServerReadHeaderTimeout,
			IdleTimeout:       httpServerIdleConnTimeout,
			ErrorLog:          lg.StandardLogger(),
		}
		servers = append(servers, listenerSrv{name: name, cfg: lcfg, srv: srv})
	}

	var wg sync.WaitGroup
	for _, ls := range servers {
		wg.Add(1)
		go func(ls listenerSrv) {
			defer wg.Done()
			lg.Info("starting listener",
				log.KV("listener", ls.name),
				log.KV("bind", ls.cfg.Bind),
				log.KV("upstream", ls.cfg.Upstream_URL))
			var serr error
			if ls.cfg.TLSEnabled() {
				serr = ls.srv.ListenAndServeTLS(ls.cfg.TLS_Certificate_File, ls.cfg.TLS_Key_File)
			} else {
				serr = ls.srv.ListenAndServe()
			}
			if serr != nil && serr != http.ErrServerClosed {
				lg.Error("listener exited",
					log.KV("listener", ls.name), log.KVErr(serr))
			}
		}(ls)
	}

	utils.WaitForQuit()
	lg.Info("shutdown signal received")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	for _, ls := range servers {
		if err := ls.srv.Shutdown(ctx); err != nil {
			lg.Warn("listener shutdown error",
				log.KV("listener", ls.name), log.KVErr(err))
		}
	}
	wg.Wait()

	if err := sessions.Flush(); err != nil {
		lg.Warn("failed to flush session store", log.KVErr(err))
	}
	if err := igst.Sync(utils.ExitSyncTimeout); err != nil {
		lg.Error("failed to sync muxer on close", log.KVErr(err))
	}
	ib.AnnounceShutdown()
}

// longestTTL picks the largest Session-TTL across all listeners so the shared
// session store keeps entries around long enough for every listener.
func longestTTL(cfg *cfgType) time.Duration {
	var ttl time.Duration
	for _, l := range cfg.Listener {
		if l.SessionTTL() > ttl {
			ttl = l.SessionTTL()
		}
	}
	if ttl == 0 {
		ttl = defaultSessionTTL
	}
	return ttl
}
