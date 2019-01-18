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

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/ingest/log"
	"github.com/gravwell/ingesters/version"
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
	if *stderrOverride != `` {
		fp := path.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			//file created, dup it
			if err := syscall.Dup2(int(fout.Fd()), int(os.Stderr.Fd())); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to dup2 stderr: %v\n", err)
				fout.Close()
			}
		}
		version.PrintVersion(fout)
		ingest.PrintVersion(fout)
	}
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing
}

type handler struct {
	mp   map[string]entry.EntryTag
	igst *ingest.IngestMuxer
}

func main() {
	cfg, err := GetConfig(*confLoc)
	if err != nil {
		lg.Fatal("Failed to load config file \"%s\": %v", *confLoc, err)
	}
	if cfg.LogLoc() != `` {
		fout, err := os.OpenFile(cfg.LogLoc(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			dlog.Fatal(err)
		}
		if err = lg.AddWriter(fout); err != nil {
			dlog.Fatalf("Failed to add a writer: %v", err)
		}
		defer fout.Close()
	}
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
	igCfg := ingest.UniformMuxerConfig{
		Destinations: conns,
		Tags:         tags,
		Auth:         cfg.Secret(),
		LogLevel:     cfg.LogLevel(),
		IngesterName: "httppost",
		VerifyCert:   !cfg.InsecureSkipTLSVerification(),
		Logger:       lg,
	}
	if cfg.EnableCache() {
		igCfg.EnableCache = true
		igCfg.CacheConfig.FileBackingLocation = cfg.LocalFileCachePath()
		igCfg.CacheConfig.MaxCacheSize = cfg.MaxCachedData()
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
		mp:   map[string]entry.EntryTag{},
		igst: igst,
	}
	for _, v := range cfg.Listener {
		tg, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("Failed to pull tag %v: %v", v.Tag_Name, err)
		}
		hnd.mp[v.URL] = tg
	}
	srv := &http.Server{
		Addr:         cfg.Bind,
		Handler:      hnd,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	defer igst.Sync(time.Second)
	defer igst.Close()
	if err := srv.ListenAndServe(); err != nil {
		lg.Error("Failed to serve HTTP server: %v", err)
	}

}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	if r.Method != http.MethodPost {
		lg.Info("bad request Method: %s != %s", r.Method, http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	tg, ok := h.mp[r.URL.Path]
	if !ok {
		lg.Info("bad request URL %v", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	b := make([]byte, maxBody)
	n, err := readAll(r.Body, b)
	if err != nil && err != io.EOF {
		lg.Error("Got bad request: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	} else if n == maxBody {
		lg.Error("Request too large, 4MB max")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	b = b[0:n]
	if len(b) == 0 {
		lg.Warn("Got an empty post from", r.RemoteAddr)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	e := entry.Entry{
		TS:   entry.Now(),
		SRC:  getRemoteIP(r),
		Tag:  tg,
		Data: b,
	}
	if err = h.igst.WriteEntry(&e); err != nil {
		lg.Error("Failed to send entry: %v", err)
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
