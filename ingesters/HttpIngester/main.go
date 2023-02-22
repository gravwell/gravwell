/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"flag"
	"fmt"
	"io"
	dlog "log"
	"net"
	"net/http"
	"os"
	"path"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils/caps"
	"github.com/gravwell/gravwell/v3/ingesters/version"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/gravwell_http_ingester.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/gravwell_http_ingester.conf.d`
	appName           = `httpingester`
)

var (
	confLoc        = flag.String("config-file", defaultConfigLoc, "Location for configuration file")
	confdLoc       = flag.String("config-overlays", defaultConfigDLoc, "Location for configuration overlay files")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver            = flag.Bool("version", false, "Print the version information and exit")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	lg             *log.Logger
	v              bool
	maxBody        int
)

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	validate.ValidateConfig(GetConfig, *confLoc, *confdLoc)
	if *verbose {
		v = true
	}
	if *confLoc == `` {
		dlog.Fatal("Invalid log location")
	}
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing
	lg.SetAppname(appName)
	if *stderrOverride != `` {
		if oldstderr, err := syscall.Dup(int(os.Stderr.Fd())); err != nil {
			lg.Fatal("Failed to dup stderr", log.KVErr(err))
		} else {
			lg.AddWriter(os.NewFile(uintptr(oldstderr), "oldstderr"))
		}

		fp := path.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			version.PrintVersion(fout)
			ingest.PrintVersion(fout)
			log.PrintOSInfo(fout)
			//file created, dup it
			if err := syscall.Dup3(int(fout.Fd()), int(os.Stderr.Fd()), 0); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to dup2 stderr: %v\n", err)
				fout.Close()
			}
		}
	}
}

func main() {
	debug.SetTraceback("all")
	var lgr *log.Logger
	cfg, err := GetConfig(*confLoc, *confdLoc)
	if err != nil {
		lg.Fatal("failed to load config file", log.KV("file", *confLoc), log.KVErr(err))
	}

	// lgr points only to the log file defined in the config.
	// We add it as a writer to lg, then just use lg everywhere.
	if lgr, err = cfg.GetLogger(); err != nil {
		lg.Fatal("failed to get logger", log.KVErr(err))
	} else if err = lg.AddWriter(lgr); err != nil {
		lg.Fatal("failed to add log file writer to standard logger")
	}
	defer lgr.Close()
	maxBody = cfg.MaxBody()

	debugout("Handling %d listeners\n", len(cfg.Listener))
	tags, err := cfg.Tags()
	if err != nil {
		lg.Fatal("failed to load tags", log.KVErr(err))
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.Fatal("failed to get backend targets from configuration", log.KVErr(err))
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	lmt, err := cfg.RateLimit()
	if err != nil {
		lg.FatalCode(0, "failed to get rate limit from configuration", log.KVErr(err))
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	debugout("Loaded %d tags\n", len(tags))
	id, ok := cfg.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "could not read ingester UUID")
	}
	igCfg := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
		VerifyCert:         !cfg.InsecureSkipTLSVerification(),
		Logger:             lg,
		IngesterName:       appName,
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		IngesterLabel:      cfg.Label,
		RateLimitBps:       lmt,
		CacheDepth:         cfg.Cache_Depth,
		CachePath:          cfg.Ingest_Cache_Path,
		CacheSize:          cfg.Max_Ingest_Cache,
		CacheMode:          cfg.Cache_Mode,
		LogSourceOverride:  net.ParseIP(cfg.Log_Source_Override),
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		lg.Fatal("failed to create new uniform muxer", log.KVErr(err))
	}
	debugout("Started ingester muxer\n")
	if cfg.SelfIngest() {
		lg.AddRelay(igst)
	}
	if err := igst.Start(); err != nil {
		lg.Fatal("failed start our ingest system", log.KVErr(err))
	}
	debugout("Waiting for connections to indexers\n")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.Fatal("Timedout waiting for backend connections", log.KVErr(err))
	}
	debugout("Successfully connected to ingesters\n")

	//check capabilities so we can scream and throw a potential warning upstream
	if !caps.Has(caps.NET_BIND_SERVICE) {
		lg.Warn("missing capability", log.KV("capability", "NET_BIND_SERVICE"), log.KV("warning", "may not be able to bind to service ports"))
		debugout("missing capability NET_BIND_SERVICE, may not be able to bind to service ports")
	}

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		lg.FatalCode(0, "Failed to set configuration for ingester state messages")
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
			handler: handleSingle,
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
		Addr:         cfg.Bind,
		Handler:      hnd,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		ErrorLog:     dlog.New(lg, ``, dlog.Lshortfile|dlog.LUTC|dlog.LstdFlags),
	}
	if cfg.TLSEnabled() {
		c := cfg.TLS_Certificate_File
		k := cfg.TLS_Key_File
		debugout("Binding to %v with TLS enabled using %s %s\n", cfg.Bind, cfg.TLS_Certificate_File, cfg.TLS_Key_File)
		if err := srv.ListenAndServeTLS(c, k); err != nil {
			lg.Error("failed to serve HTTPS server", log.KVErr(err))
		}
	} else {
		debugout("Binding to %v in cleartext mode\n", cfg.Bind)
		if err := srv.ListenAndServe(); err != nil {
			lg.Error("failed to serve HTTP server", log.KVErr(err))
		}
	}
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
	if !v {
		return
	}
	fmt.Printf(format, args...)
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
