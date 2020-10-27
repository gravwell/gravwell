/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

const (
	defaultConfigLoc     = `/opt/gravwell/etc/simple_relay.conf`
	ingesterName         = `simplerelay`
	batchSize            = 512
	maxDataSize      int = 8 * 1024 * 1024
	initDataSize     int = 512 * 1024
)

var (
	cpuprofile     = flag.String("cpuprofile", "", "write cpu profile to file")
	confLoc        = flag.String("config-file", defaultConfigLoc, "Location for configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	ver            = flag.Bool("version", false, "Print the version information and exit")

	v  bool
	lg *log.Logger
)

func mainInit() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	var fp string
	var err error
	if *stderrOverride != `` {
		fp = filepath.Join(`/dev/shm/`, *stderrOverride)
	}
	cb := func(w io.Writer) {
		version.PrintVersion(w)
		ingest.PrintVersion(w)
	}
	if lg, err = log.NewStderrLoggerEx(fp, cb); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get stderr logger: %v\n", err)
		os.Exit(-1)
	}

	v = *verbose
	connClosers = make(map[int]closer, 1)
	validate.ValidateConfig(GetConfig, *confLoc)
}

func main() {
	mainInit()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			lg.Fatal("Failed to open %s for profile file: %v\n", *cpuprofile, err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	cfg, err := GetConfig(*confLoc)
	if err != nil {
		lg.FatalCode(0, "Failed to get configuration: %v\n", err)
		return
	}

	if len(cfg.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "Failed to open log file %s: %v", cfg.Log_File, err)
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("Failed to add a writer: %v", err)
		}
		if len(cfg.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Log_Level); err != nil {
				lg.FatalCode(0, "Invalid Log Level \"%s\": %v", cfg.Log_Level, err)
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "Failed to get tags from configuration: %v\n", err)
		return
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.FatalCode(0, "Failed to get backend targets from configuration: %v\n", err)
		return
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	lmt, err := cfg.RateLimit()
	if err != nil {
		lg.FatalCode(0, "Failed to get rate limit from configuration: %v\n", err)
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	debugout("INSECURE skip TLS certificate verification: %v\n", cfg.InsecureSkipTLSVerification())
	id, ok := cfg.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID\n")
	}
	igCfg := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
		LogLevel:           cfg.LogLevel(),
		VerifyCert:         !cfg.InsecureSkipTLSVerification(),
		IngesterName:       ingesterName,
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		RateLimitBps:       lmt,
		Logger:             lg,
		CacheDepth:         cfg.Cache_Depth,
		CachePath:          cfg.Ingest_Cache_Path,
		CacheSize:          cfg.Max_Ingest_Cache,
		CacheMode:          cfg.Cache_Mode,
		LogSourceOverride:  net.ParseIP(cfg.Log_Source_Override),
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		lg.Fatal("Failed build our ingest system: %v\n", err)
		return
	}

	defer igst.Close()
	debugout("Started ingester muxer\n")
	if err := igst.Start(); err != nil {
		lg.Fatal("Failed start our ingest system: %v\n", err)
		return
	}
	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "Timedout waiting for backend connections: %v\n", err)
		return
	}
	debugout("Successfully connected to ingesters\n")
	wg := &sync.WaitGroup{}

	var flshr flusher

	ctx, cancel := context.WithCancel(context.Background())

	//fire off our simple listeners
	if err := startSimpleListeners(cfg, igst, wg, &flshr, ctx); err != nil {
		lg.FatalCode(-1, "Failed to start simple listeners: %v\n", err)
		return
	}
	//fire off our json listeners
	if err := startJSONListeners(cfg, igst, wg, &flshr, ctx); err != nil {
		lg.FatalCode(-1, "Failed to start json listeners: %v\n", err)
		return
	}

	debugout("Running\n")

	//listen for signals so we can close gracefully
	utils.WaitForQuit()
	debugout("Closing %d connections\n", connCount())

	go func() {
		time.Sleep(time.Second)
		cancel()
	}()

	mtx.Lock()
	for _, v := range connClosers {
		v.Close()
	}
	mtx.Unlock() //must unlock so they can delete their connections

	//wait for everyone to exit with a timeout
	wch := make(chan bool, 1)

	go func() {
		wg.Wait()
		wch <- true
	}()
	select {
	case <-wch:
	case <-time.After(1 * time.Second):
		lg.Error("Failed to wait for all connections to close.  %d active\n", connCount())
	}
	if err := flshr.Close(); err != nil {
		lg.Error("Failed to close preprocessors: %v", err)
	}
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("Failed to sync: %v\n", err)
	}
	if err := igst.Close(); err != nil {
		lg.Error("Failed to close: %v\n", err)
	}
}

func debugout(format string, args ...interface{}) {
	if !v {
		return
	}
	fmt.Printf(format, args...)
}

type flusher struct {
	sync.Mutex
	set []io.Closer
}

func (f *flusher) Add(c io.Closer) {
	if c == nil {
		return
	}
	f.Lock()
	f.set = append(f.set, c)
	f.Unlock()
}

func (f *flusher) Close() (err error) {
	f.Lock()
	for _, v := range f.set {
		if lerr := v.Close(); lerr != nil {
			err = addError(lerr, err)
		}
	}
	f.Unlock()
	return
}

func addError(nerr, err error) error {
	if nerr == nil {
		return err
	} else if err == nil {
		return nerr
	}
	return fmt.Errorf("%v : %v", err, nerr)
}
