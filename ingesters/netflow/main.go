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
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/netflow_capture.conf`
	ingesterName     = `flow`
	appName          = `netflow`
	batchSize        = 512
)

var (
	cpuprofile     = flag.String("cpuprofile", "", "write cpu profile to file")
	confLoc        = flag.String("config-file", defaultConfigLoc, "Location for configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver            = flag.Bool("version", false, "Print the version information and exit")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	v              bool
	lg             *log.Logger
)

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
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
			if err := syscall.Dup2(int(fout.Fd()), int(os.Stderr.Fd())); err != nil {
				fout.Close()
				lg.FatalCode(0, "failed to dup2 stderr", log.KVErr(err))
			}
		}
	}

	v = *verbose
	connClosers = make(map[int]closer, 1)
	validate.ValidateConfig(GetConfig, *confLoc, ``)
}

func main() {
	debug.SetTraceback("all")
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			lg.FatalCode(0, "failed to open profile file", log.KV("path", *cpuprofile), log.KVErr(err))
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	cfg, err := GetConfig(*confLoc)
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
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.FatalCode(0, "failed to get backend targets from configuration", log.KVErr(err))
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	lmt, err := cfg.RateLimit()
	if err != nil {
		lg.FatalCode(0, "failed to get rate limit from configuration", log.KVErr(err))
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	debugout("INSECURE skipping TLS verification: %v\n", cfg.InsecureSkipTLSVerification())
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
		Logger:             lg,
		CacheDepth:         cfg.Cache_Depth,
		CachePath:          cfg.Ingest_Cache_Path,
		CacheSize:          cfg.Max_Ingest_Cache,
		CacheMode:          cfg.Cache_Mode,
		LogSourceOverride:  net.ParseIP(cfg.Log_Source_Override),
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		lg.Fatal("failed build our ingest system", log.KVErr(err))
	}

	defer igst.Close()
	debugout("Started ingester muxer\n")
	if cfg.SelfIngest() {
		lg.AddRelay(igst)
	}
	if err := igst.Start(); err != nil {
		lg.FatalCode(0, "failed start our ingest system", log.KVErr(err))
	}
	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "timeout waiting for backend connections", log.KV("timeout", cfg.Timeout()), log.KVErr(err))
	}
	debugout("Successfully connected to ingesters\n")

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		lg.FatalCode(0, "failed to set configuration for ingester state messages", log.KVErr(err))
	}

	wg := sync.WaitGroup{}
	ch := make(chan *entry.Entry, 2048)
	bc := bindConfig{
		ch:   ch,
		wg:   &wg,
		igst: igst,
	}

	var src net.IP
	if cfg.Source_Override != `` {
		// global override
		src = net.ParseIP(cfg.Source_Override)
		if src == nil {
			lg.FatalCode(0, "Global Source-Override is invalid")
		}
	}

	//fire up our backends
	for k, v := range cfg.Collector {
		//get the tag for this listener
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.FatalCode(0, "failed to resolve tag", log.KV("tag", v.Tag_Name), log.KV("collector", k), log.KVErr(err))
		}
		ft, err := translateFlowType(v.Flow_Type)
		if err != nil {
			lg.FatalCode(0, "invalid flow type", log.KV("flowtype", v.Flow_Type), log.KV("collector", k), log.KVErr(err))
		}
		bc.tag = tag
		bc.ignoreTS = v.Ignore_Timestamps
		bc.localTZ = v.Assume_Local_Timezone
		bc.sessionDumpEnabled = v.Session_Dump_Enabled
		bc.lastInfoDump = time.Now()
		var bh BindHandler
		switch ft {
		case nfv5Type:
			if bh, err = NewNetflowV5Handler(bc); err != nil {
				lg.FatalCode(0, "NewNetflowV5Handlerfailed", log.KVErr(err))
				return
			}
		case ipfixType:
			if bh, err = NewIpfixHandler(bc); err != nil {
				lg.FatalCode(0, "NewIpfixHandler failed", log.KVErr(err))
				return
			}
		default:
			lg.FatalCode(0, "invalid flow type", log.KV("flowtype", ft))
			return
		}
		if err = bh.Listen(v.Bind_String); err != nil {
			lg.FatalCode(0, "failed to listen", log.KV("bindstring", bh.String()), log.KVErr(err))
		}
		id := addConn(bh)
		if err := bh.Start(id); err != nil {
			lg.FatalCode(0, "start error", log.KV("collector", bh.String()), log.KVErr(err))
		}
		wg.Add(1)
	}
	debugout("Started %d handlers\n", len(cfg.Collector))
	//fire off our relay
	doneChan := make(chan bool)
	go relay(ch, doneChan, src, igst)

	debugout("Running\n")

	//listen for signals so we can close gracefully
	utils.WaitForQuit()
	debugout("Closing %d connections\n", connCount())
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
		//close our output channel
		close(ch)
		//wait for our ingest relay to exit
		<-doneChan
	case <-time.After(1 * time.Second):
		lg.Error("failed to wait for all connections to close", log.KV("active", connCount()))
	}
	lg.Info("netflow ingester exiting", log.KV("ingesteruuid", id))
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("failed to sync", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		lg.Error("failed to close", log.KVErr(err))
	}
}

func relay(ch chan *entry.Entry, done chan bool, srcOverride net.IP, igst *ingest.IngestMuxer) {
	var ents []*entry.Entry

	tckr := time.NewTicker(time.Second)
	defer tckr.Stop()
mainLoop:
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				if len(ents) > 0 {
					if err := igst.WriteBatch(ents); err != nil {
						if err != ingest.ErrNotRunning {
							lg.Error("failed to WriteBatch", log.KVErr(err))
						}
					}
				}
				ents = nil
				break mainLoop
			}
			if e != nil {
				if srcOverride != nil {
					e.SRC = srcOverride
				}
				ents = append(ents, e)
			}
			if len(ents) >= batchSize {
				if err := igst.WriteBatch(ents); err != nil {
					if err != ingest.ErrNotRunning {
						lg.Error("failed to WriteBatch", log.KVErr(err))
					} else {
						break mainLoop
					}
				}
				ents = nil
			}
		case _ = <-tckr.C:
			if len(ents) > 0 {
				if err := igst.WriteBatch(ents); err != nil {
					if err != ingest.ErrNotRunning {
						lg.Error("failed to WriteBatch", log.KVErr(err))
					} else {
						break mainLoop
					}
				}
				ents = nil
			}
		}
	}
	close(done)
}

func debugout(format string, args ...interface{}) {
	if !v {
		return
	}
	fmt.Printf(format, args...)
}
