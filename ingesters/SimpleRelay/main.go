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
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/utils/caps"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

const (
	defaultConfigLoc      = `/opt/gravwell/etc/simple_relay.conf`
	defaultConfigDLoc     = `/opt/gravwell/etc/simple_relay.conf.d`
	ingesterName          = `simplerelay`
	appName               = `simplerelay`
	batchSize             = 512
	maxDataSize       int = 8 * 1024 * 1024
	initDataSize      int = 512 * 1024
)

var (
	confLoc        = flag.String("config-file", defaultConfigLoc, "Location for configuration file")
	confdLoc       = flag.String("config-overlays", defaultConfigDLoc, "Location for configuration overlay files")
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
	validate.ValidateConfig(GetConfig, *confLoc, *confdLoc)
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing
	lg.SetAppname(appName)
	if *stderrOverride != `` {
		if oldstderr, err := syscall.Dup(int(os.Stderr.Fd())); err != nil {
			lg.Fatal("failed to dup stderr", log.KVErr(err))
		} else {
			lg.AddWriter(os.NewFile(uintptr(oldstderr), "oldstderr"))
		}

		fp := filepath.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			version.PrintVersion(fout)
			ingest.PrintVersion(fout)
			log.PrintOSInfo(fout)
			//file created, dup it
			if err := syscall.Dup3(int(fout.Fd()), int(os.Stderr.Fd()), 0); err != nil {
				fout.Close()
				lg.FatalCode(0, "failed to dup2 stderr", log.KVErr(err))
			}
		}
	}

	v = *verbose
	connClosers = make(map[int]closer, 1)
}

func main() {
	debug.SetTraceback("all")
	mainInit()
	cfg, err := GetConfig(*confLoc, *confdLoc)
	if err != nil {
		lg.FatalCode(0, "failed to get configuration", log.KVErr(err))
		return
	}

	if len(cfg.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "failed to open log file", log.KV("path", cfg.Log_File), log.KVErr(err))
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("failed to add a writer", log.KVErr(err))
		}
		if len(cfg.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Log_Level); err != nil {
				lg.FatalCode(0, "invalid Log Level", log.KV("loglevel", cfg.Log_Level), log.KVErr(err))
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "failed to get tags from configuration", log.KVErr(err))
		return
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.FatalCode(0, "failed to get backend targets from configuration", log.KVErr(err))
		return
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	lmt, err := cfg.RateLimit()
	if err != nil {
		lg.FatalCode(0, "failed to get rate limit from configuration", log.KVErr(err))
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	debugout("INSECURE skip TLS certificate verification: %v\n", cfg.InsecureSkipTLSVerification())
	id, ok := cfg.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID")
	}
	igCfg := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
		VerifyCert:         !cfg.InsecureSkipTLSVerification(),
		IngesterName:       ingesterName,
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		IngesterLabel:      cfg.Label,
		RateLimitBps:       lmt,
		CacheDepth:         cfg.Cache_Depth,
		CachePath:          cfg.Ingest_Cache_Path,
		CacheSize:          cfg.Max_Ingest_Cache,
		CacheMode:          cfg.Cache_Mode,
		Logger:             lg,
		LogSourceOverride:  net.ParseIP(cfg.Log_Source_Override),
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		lg.Fatal("failed build our ingest system", log.KVErr(err))
		return
	}
	defer igst.Close()
	debugout("Started ingester muxer\n")
	// Henceforth, logs will also go out via the muxer to the gravwell tag
	if cfg.SelfIngest() {
		lg.AddRelay(igst)
	}
	if err := igst.Start(); err != nil {
		lg.Fatal("failed start our ingest system", log.KVErr(err))
		return
	}

	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "timeout waiting for backend connections", log.KV("timeout", cfg.Timeout()), log.KVErr(err))
		return
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
		lg.FatalCode(0, "failed to set configuration for ingester state messages", log.KV("ingesteruuid", id), log.KVErr(err))
	}

	wg := &sync.WaitGroup{}

	var flshr flusher

	ctx, cancel := context.WithCancel(context.Background())

	//fire off our simple listeners
	if err := startSimpleListeners(cfg, igst, wg, &flshr, ctx); err != nil {
		lg.FatalCode(0, "Failed to start simple listeners", log.KV("ingesteruuid", id), log.KVErr(err))
		return
	}
	// fire off our regex listeners
	if err := startRegexListeners(cfg, igst, wg, &flshr, ctx); err != nil {
		lg.FatalCode(0, "Failed to start regex listeners", log.KV("ingesteruuid", id), log.KVErr(err))
		return
	}
	//fire off our json listeners
	if err := startJSONListeners(cfg, igst, wg, &flshr, ctx); err != nil {
		lg.FatalCode(0, "Failed to start json listeners", log.KV("ingesteruuid", id), log.KVErr(err))
		return
	}

	lg.Info("Ingester running")

	//listen for signals so we can close gracefully
	utils.WaitForQuit()
	debugout("Closing %d connections\n", connCount())
	lg.Info("Closing active connections", log.KV("ingesteruuid", id), log.KV("active", connCount()))

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
		lg.Error("Failed to wait for all connections to close", log.KV("timeout", time.Second), log.KV("active", connCount()))
	}
	if err := flshr.Close(); err != nil {
		lg.Error("failed to close preprocessors", log.KVErr(err))
	}
	lg.Info("Ingester exiting", log.KV("ingesteruuid", id))
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("failed to sync", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		lg.Error("failed to close", log.KVErr(err))
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
