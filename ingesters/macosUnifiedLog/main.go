/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// macOS unified log ingester. Runs `log stream` for each configured stream and
// ingests every event it emits as a single-line JSON entry.
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"sync"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/gravwell/gravwell/v3/debug"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/macos_unified_log.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/macos_unified_log.conf.d`
	ingesterName      = `macOS Unified Log`
	appName           = `macosunifiedlog`
)

func main() {
	go debug.HandleDebugSignals(appName)

	var cfg *cfgType
	ibc := base.IngesterBaseConfig{
		IngesterName:                 ingesterName,
		AppName:                      appName,
		DefaultConfigLocation:        defaultConfigLoc,
		DefaultConfigOverlayLocation: defaultConfigDLoc,
		GetConfigFunc:                GetConfig,
	}
	ib, err := base.Init(ibc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get configuration %v\n", err)
		os.Exit(1)
	} else if err = ib.AssignConfig(&cfg); err != nil || cfg == nil {
		fmt.Fprintf(os.Stderr, "failed to assign configuration %v %v\n", err, cfg == nil)
		os.Exit(1)
	}
	lg := ib.Logger

	// configuration validation works anywhere, but actually streaming requires macOS
	if runtime.GOOS != `darwin` {
		lg.FatalCode(0, "the macOS unified log ingester can only run on macOS", log.KV("os", runtime.GOOS))
	} else if _, err = os.Stat(logCommand); err != nil {
		lg.FatalCode(0, "unified logging command is unavailable", log.KV("path", logCommand), log.KVErr(err))
	}

	var globalSrc net.IP
	if cfg.Global.Source_Override != `` {
		// already validated by the config Verify
		globalSrc = net.ParseIP(cfg.Global.Source_Override)
	}

	igst, err := ib.GetMuxer()
	if err != nil {
		lg.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	ib.AnnounceStartup()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var procs []*processors.ProcessorSet
	for name, sc := range cfg.Stream {
		tag, err := igst.GetTag(sc.Tag_Name)
		if err != nil {
			lg.FatalCode(0, "failed to resolve tag", log.KV("stream", name), log.KV("tag", sc.Tag_Name), log.KVErr(err))
		}
		proc, err := cfg.Preprocessor.ProcessorSet(igst, sc.Preprocessor)
		if err != nil {
			lg.FatalCode(0, "preprocessor construction error", log.KV("stream", name), log.KVErr(err))
		}
		procs = append(procs, proc)

		src := sc.src
		if src == nil {
			src = globalSrc
		}
		s := newStreamer(name, sc, tag, src, proc, lg)
		wg.Go(func() { s.run(ctx) })
	}

	// listen for signals so we can close gracefully
	utils.WaitForQuit()
	ib.AnnounceShutdown()

	cancel()
	wg.Wait()

	for _, proc := range procs {
		if err := proc.Close(); err != nil {
			lg.Error("failed to close preprocessors", log.KVErr(err))
		}
	}
	if err := igst.Sync(utils.ExitSyncTimeout); err != nil {
		lg.Error("failed to sync", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		lg.Error("failed to close", log.KVErr(err))
	}
}
