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
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/version"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/gravwell_http_ingester.conf`
)

var (
	confLoc        = flag.String("config-file", defaultConfigLoc, "Location for configuration file")
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
	} else if *verbose {
		v = true
	}
	if *confLoc == `` {
		dlog.Fatal("Invalid log location")
	}
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing
	if *stderrOverride != `` {
		if oldstderr, err := syscall.Dup(int(os.Stderr.Fd())); err != nil {
			lg.Fatal("Failed to dup stderr: %v\n", err)
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
			//file created, dup it
			if err := syscall.Dup2(int(fout.Fd()), int(os.Stderr.Fd())); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to dup2 stderr: %v\n", err)
				fout.Close()
			}
		}
	}
}

func main() {
	var lgr *log.Logger
	cfg, err := GetConfig(*confLoc)
	if err != nil {
		lg.Fatal("Failed to load config file \"%s\": %v", *confLoc, err)
	}

	//logging is a bit whacky here, we are creating a logger for fatal errors that goes to
	//stderr and then creating another logger that goes to the logging file
	// this is so that we can log fatal errors to both stderr and the log file
	// but ONLY log errors to the webserver to the file
	if lgr, err = cfg.GetLogger(); err != nil {
		lg.Fatal("Failed to get logger: %v", err)
	} else if err = lg.AddWriter(lgr); err != nil {
		lg.Fatal("Failed to add log file writer to standard logger")
	}
	defer lgr.Close()
	maxBody = cfg.MaxBody()

	debugout("Handling %d listeners", len(cfg.Listener))
	tags, err := cfg.Tags()
	if err != nil {
		lg.Fatal("Failed to load tags: %v", err)
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.Fatal("Failed to get backend targets from configuration: %v", err)
	}

	debugout("Loaded %d tags", len(tags))
	id, ok := cfg.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID\n")
	}
	igCfg := ingest.UniformMuxerConfig{
		Destinations:    conns,
		Tags:            tags,
		Auth:            cfg.Secret(),
		LogLevel:        cfg.LogLevel(),
		VerifyCert:      !cfg.InsecureSkipTLSVerification(),
		Logger:          lg,
		IngesterName:    "httppost",
		IngesterVersion: version.GetVersion(),
		IngesterUUID:    id.String(),
		CacheDepth:      cfg.Cache_Depth,
		CachePath:       cfg.Ingest_Cache_Path,
		CacheSize:       cfg.Max_Ingest_Cache,
		CacheMode:       cfg.Cache_Mode,
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		lg.Fatal("Failed to create new uniform muxer: %v ", err)
	}
	debugout("Started ingester muxer\n")
	if err := igst.Start(); err != nil {
		lg.Fatal("Failed start our ingest system: %v", err)
	}
	debugout("Waiting for connections to indexers\n")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.Fatal("Timedout waiting for backend connections: %v", err)
	}
	debugout("Successfully connected to ingesters\n")
	hnd := &handler{
		mp:   map[string]handlerConfig{},
		auth: map[string]authHandler{},
		igst: igst,
		lgr:  lgr,
	}
	for _, v := range cfg.Listener {
		var hcfg handlerConfig
		if hcfg.tag, err = igst.GetTag(v.Tag_Name); err != nil {
			lg.Fatal("Failed to pull tag %v: %v", v.Tag_Name, err)
		}
		if v.Ignore_Timestamps {
			hcfg.ignoreTs = true
		} else {
			tcfg := timegrinder.Config{
				EnableLeftMostSeed: true,
				FormatOverride:     v.Timestamp_Format_Override,
			}
			if hcfg.tg, err = timegrinder.NewTimeGrinder(tcfg); err != nil {
				lg.Fatal("Failed to generate new timegrinder: %v", err)
			}
			if v.Assume_Local_Timezone {
				hcfg.tg.SetLocalTime()
			}
			if v.Timezone_Override != `` {
				if err = hcfg.tg.SetTimezone(v.Timezone_Override); err != nil {
					lg.Fatal("Failed to override timezone: %v", err)
				}
			}
		}
		if hcfg.method = v.Method; hcfg.method == `` {
			hcfg.method = defaultMethod
		}

		hcfg.pproc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor)
		if err != nil {
			lg.Fatal("Preprocessor construction error: %v", err)
		}
		//check if authentication is enabled for this URL
		if pth, ah, err := v.NewAuthHandler(lgr); err != nil {
			lg.Fatal("Failed to get a new authentication handler: %v", err)
		} else if hnd != nil {
			if pth != `` {
				hnd.auth[pth] = ah
			}
			hcfg.auth = ah
		}
		hnd.mp[v.URL] = hcfg
	}
	srv := &http.Server{
		Addr:         cfg.Bind,
		Handler:      hnd,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		ErrorLog:     dlog.New(lgr, ``, dlog.Lshortfile|dlog.LUTC|dlog.LstdFlags),
	}
	if cfg.TLSEnabled() {
		c := cfg.TLS_Certificate_File
		k := cfg.TLS_Key_File
		if err := srv.ListenAndServeTLS(c, k); err != nil {
			lg.Error("Failed to serve HTTPS server: %v", err)
		}
	} else {
		if err := srv.ListenAndServe(); err != nil {
			lg.Error("Failed to serve HTTP server: %v", err)
		}
	}
	for k, v := range hnd.mp {
		if v.pproc != nil {
			if err := v.pproc.Close(); err != nil {
				lg.Error("Failed to close preprocessors for handler %v: %v", k, err)
			}
		}
	}
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("Failed to sync muxer on close: %v", err)
	}
	if err := igst.Close(); err != nil {
		lg.Error("Failed to close muxer: %v", err)
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
