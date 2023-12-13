/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"fmt"
	"io"
	dlog "log"
	"net"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/utils/caps"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/gravwell_http_ingester.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/gravwell_http_ingester.conf.d`
	appName           = `httpingester`
)

var (
	lg      *log.Logger
	debugOn bool
	maxBody int

	exitCtx, exitFn = context.WithCancel(context.Background())
)

func main() {
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
		fmt.Fprintf(os.Stderr, "failed to get configuration %v\n", err)
		return
	} else if err = ib.AssignConfig(&cfg); err != nil || cfg == nil {
		fmt.Fprintf(os.Stderr, "failed to assign configuration %v %v\n", err, cfg == nil)
		return
	}
	debugOn = ib.Verbose
	lg = ib.Logger
	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()
	ib.AnnounceStartup()

	debugout("Started ingester muxer\n")
	maxBody = cfg.MaxBody()
	debugout("Handling %d listeners\n", len(cfg.Listener))

	//check capabilities so we can scream and throw a potential warning upstream
	if !caps.Has(caps.NET_BIND_SERVICE) {
		lg.Warn("missing capability", log.KV("capability", "NET_BIND_SERVICE"), log.KV("warning", "may not be able to bind to service ports"))
		debugout("missing capability NET_BIND_SERVICE, may not be able to bind to service ports")
	}

	hnd, err := newHandler(igst, lg)
	if err != nil {
		lg.FatalCode(0, "Failed to create new handler")
	}

	if hcurl, ok := cfg.HealthCheck(); ok {
		hnd.healthCheckURL = path.Clean(hcurl)
	}
	for _, v := range cfg.Listener {
		hcfg := routeHandler{
			handler:       handleSingle,
			paramAttacher: getAttacher(v.Attach_URL_Parameter),
		}
		if v.Multiline {
			hcfg.handler = handleMulti
		}
		if hcfg.tag, err = igst.GetTag(v.Tag_Name); err != nil {
			lg.Fatal("failed to pull tag", log.KV("tag", v.Tag_Name), log.KVErr(err))
		}
		if v.Ignore_Timestamps {
			hcfg.ignoreTs = true
		} else {
			tcfg := timegrinder.Config{
				EnableLeftMostSeed: true,
			}
			if hcfg.tg, err = timegrinder.NewTimeGrinder(tcfg); err != nil {
				lg.Fatal("failed to generate new timegrinder", log.KVErr(err))
			} else if err = cfg.TimeFormat.LoadFormats(hcfg.tg); err != nil {
				lg.Fatal("failed to load custom time formats", log.KVErr(err))
			}
			if v.Timestamp_Format_Override != `` {
				if err = hcfg.tg.SetFormatOverride(v.Timestamp_Format_Override); err != nil {
					lg.Fatal("Failed to set override timestamp", log.KVErr(err))
				}
			}
			if v.Assume_Local_Timezone {
				hcfg.tg.SetLocalTime()
			}
			if v.Timezone_Override != `` {
				if err = hcfg.tg.SetTimezone(v.Timezone_Override); err != nil {
					lg.Fatal("failed to override timezone", log.KVErr(err))
				}
			}
		}
		if v.Method == `` {
			v.Method = defaultMethod
		}

		hcfg.pproc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor)
		if err != nil {
			lg.Fatal("preprocessor construction error", log.KVErr(err))
		}
		//check if authentication is enabled for this URL
		if pth, ah, err := v.NewAuthHandler(lg); err != nil {
			lg.Fatal("failed to get a new authentication handler", log.KVErr(err))
		} else if hnd != nil {
			if pth != `` {
				if err = hnd.addAuthHandler(http.MethodPost, pth, ah); err != nil {
					lg.Fatal("failed to add auth handler", log.KV("url", pth), log.KVErr(err))
				}
			}
			hcfg.auth = ah
		}
		if err = hnd.addHandler(v.Method, v.URL, hcfg); err != nil {
			lg.Fatal("failed to add handler", log.KV("url", v.URL), log.KVErr(err))
		}
		debugout("URL %s handling %s\n", v.URL, v.Tag_Name)
	}

	if err = includeHecListeners(hnd, igst, cfg, lg); err != nil {
		lg.Fatal("failed to include HEC Listeners", log.KVErr(err))
	}
	if err = includeKDSListeners(hnd, igst, cfg, lg); err != nil {
		lg.Fatal("failed to include KDS Listeners", log.KVErr(err))
	}

	srv := &http.Server{
		Addr:              cfg.Bind,
		Handler:           hnd,
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          dlog.New(lg, ``, dlog.Lshortfile|dlog.LUTC|dlog.LstdFlags),
	}
	done := make(chan error, 1)
	if cfg.TLSEnabled() {
		c := cfg.TLS_Certificate_File
		k := cfg.TLS_Key_File
		debugout("Binding to %v with TLS enabled using %s %s\n", cfg.Bind, cfg.TLS_Certificate_File, cfg.TLS_Key_File)
		go func(dc chan error) {
			defer close(dc)
			if err := srv.ListenAndServeTLS(c, k); err != nil {
				lg.Error("failed to serve HTTPS server", log.KVErr(err))
			}
		}(done)
	} else {
		debugout("Binding to %v in cleartext mode\n", cfg.Bind)
		go func(dc chan error) {
			defer close(dc)
			if err := srv.ListenAndServe(); err != nil {
				lg.Error("failed to serve HTTP server", log.KVErr(err))
			}
		}(done)
	}
	qc := utils.GetQuitChannel()
	defer close(qc)
	select {
	case <-done:
	case <-qc:
		if err := srv.Close(); err != nil {
			lg.Error("failed to serve HTTP server", log.KVErr(err))
		}
	}
	debugout("Server is exiting\n")
	ib.AnnounceShutdown()

	exitFn()

	for k, v := range hnd.mp {
		if v.pproc != nil {
			if err := v.pproc.Close(); err != nil {
				lg.Error("failed to close preprocessors for handler", log.KV("preprocessor", k), log.KVErr(err))
			}
		}
	}
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("failed to sync muxer on close", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		lg.Error("failed to close muxer", log.KVErr(err))
	}
}

func debugout(format string, args ...interface{}) {
	if debugOn {
		fmt.Printf(format, args...)
	}
}

func getRemoteAddr(r *http.Request) (host string) {
	xfflist, ok := r.Header[`X-Forwarded-For`]
	if !ok || len(xfflist) == 0 {
		host, _, _ = net.SplitHostPort(r.RemoteAddr)
	} else {
		host = xfflist[0]
	}
	return

}

func getRemoteIP(r *http.Request) (ip net.IP) {
	if host := getRemoteAddr(r); host != `` {
		if ip = net.ParseIP(host); ip != nil {
			return
		}
	}
	ip = net.ParseIP(`127.0.0.1`)
	return
}

func readAll(r io.Reader, buff []byte) (offset int, err error) {
	var n int
	for offset < len(buff) {
		if n, err = r.Read(buff[offset:]); err != nil {
			if err == io.EOF {
				err = nil
				offset += n
			}
			return
		} else if n == 0 {
			return
		}
		offset += n
	}
	return
}
