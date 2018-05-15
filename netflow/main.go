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
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/ingest/log"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/flow.conf`
	ingesterName     = `flow`
	batchSize        = 512
)

var (
	cpuprofile     = flag.String("cpuprofile", "", "write cpu profile to file")
	configOverride = flag.String("config-file-override", "", "Override location for configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	confLoc        string
	v              bool
	lg             *log.Logger
)

func init() {
	flag.Parse()

	if *stderrOverride != `` {
		fp := filepath.Join(`/dev/shm/`, *stderrOverride)
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
	}
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing

	if *configOverride == "" {
		confLoc = defaultConfigLoc
	} else {
		confLoc = *configOverride
	}
	v = *verbose
	connClosers = make(map[int]closer, 1)
}

func main() {
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			lg.Fatal("Failed to open %s for profile file: %v\n", *cpuprofile, err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	cfg, err := GetConfig(confLoc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get configuration: %v\n", err)
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
		lg.Fatal("Failed to get tags from configuration: %v\n", err)
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.Fatal("Failed to get backend targets from configuration: %v\n", err)
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	//fire up the ingesters
	debugout("INSECURE skipping TLS verification: %v\n", cfg.InsecureSkipTLSVerification())
	igCfg := ingest.UniformMuxerConfig{
		Destinations: conns,
		Tags:         tags,
		Auth:         cfg.Secret(),
		LogLevel:     cfg.LogLevel(),
		VerifyCert:   !cfg.InsecureSkipTLSVerification(),
		IngesterName: ingesterName,
	}
	if cfg.EnableCache() {
		igCfg.EnableCache = true
		igCfg.CacheConfig.FileBackingLocation = cfg.LocalFileCachePath()
		igCfg.CacheConfig.MaxCacheSize = cfg.MaxCachedData()
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		lg.Fatal("Failed build our ingest system: %v\n", err)
	}

	defer igst.Close()
	debugout("Started ingester muxer\n")
	if err := igst.Start(); err != nil {
		lg.Fatal("Failed start our ingest system: %v\n", err)
	}
	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "Timedout waiting for backend connections: %v\n", err)
	}
	debugout("Successfully connected to ingesters\n")
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
			lg.FatalCode(0, "Failed to resolve tag \"%s\" for %s: %v\n", v.Tag_Name, k, err)
		}
		ft, err := translateFlowType(v.Flow_Type)
		if err != nil {
			lg.FatalCode(0, "Invalid flow type \"%s\": %v\n", v.Flow_Type, err)
		}
		bc.tag = tag
		bc.ignoreTS = v.Ignore_Timestamp
		bc.localTZ = v.Assume_Local_Timezone
		var bh BindHandler
		switch ft {
		case nfv5Type:
			if bh, err = NewNetflowV5Handler(bc); err != nil {
				lg.FatalCode(0, "NewNetflowV5Handler error: %v\n", err)
				return
			}
		default:
			lg.FatalCode(0, "Invalid flow type %v\n", ft)
			return
		}
		if err = bh.Listen(v.Bind_String); err != nil {
			lg.FatalCode(0, "Failed to listen on %s handler: %v\n", bh.String(), err)
		}
		id := addConn(bh)
		if err := bh.Start(id); err != nil {
			lg.FatalCode(0, "%s.Start() error: %v\n", bh.String(), err)
		}
		wg.Add(1)
	}
	debugout("Started %d handlers\n", len(cfg.Collector))
	//fire off our relay
	doneChan := make(chan bool)
	go relay(ch, doneChan, src, igst)

	debugout("Running\n")

	//listen for signals so we can close gracefully
	sch := make(chan os.Signal, 1)
	signal.Notify(sch, os.Interrupt)
	<-sch
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
		lg.Error("Failed to wait for all connections to close.  %d active\n", connCount())
	}
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("Failed to sync: %v\n", err)
	}
	if err := igst.Close(); err != nil {
		lg.Error("Failed to close: %v\n", err)
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
							lg.Error("Failed to WriteBatch: %v\n", err)
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
						lg.Error("Failed to WriteBatch: %v\n", err)
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
						lg.Error("Failed to WriteBatch: %v\n", err)
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
