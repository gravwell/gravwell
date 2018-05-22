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
	"log"
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
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/netflow_capture.conf`
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
			fmt.Fprintf(os.Stderr, "Failed to open %s for profile file: %v\n", *cpuprofile, err)
			os.Exit(-1)
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

	tags, err := cfg.Tags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get tags from configuration: %v\n", err)
		return
	}
	conns, err := cfg.Targets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get backend targets from configuration: %v\n", err)
		return
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
		fmt.Fprintf(os.Stderr, "Failed build our ingest system: %v\n", err)
		return
	}

	defer igst.Close()
	debugout("Started ingester muxer\n")
	if err := igst.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed start our ingest system: %v\n", err)
		return
	}
	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		fmt.Fprintf(os.Stderr, "Timedout waiting for backend connections: %v\n", err)
		return
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
			log.Fatal("Global Source-Override is invalid")
		}
	}

	//fire up our backends
	for k, v := range cfg.Collector {
		//get the tag for this listener
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to resolve tag \"%s\" for %s: %v\n", v.Tag_Name, k, err)
			return
		}
		ft, err := translateFlowType(v.Flow_Type)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid flow type \"%s\": %v\n", v.Flow_Type, err)
			return
		}
		bc.tag = tag
		bc.ignoreTS = v.Ignore_Timestamp
		bc.localTZ = v.Assume_Local_Timezone
		var bh BindHandler
		switch ft {
		case nfv5Type:
			if bh, err = NewNetflowV5Handler(bc); err != nil {
				fmt.Fprintf(os.Stderr, "NewNetflowV5Handler error: %v\n", err)
				return
			}
		default:
			fmt.Fprintf(os.Stderr, "Invalid flow type %v\n", ft)
			return
		}
		if err = bh.Listen(v.Bind_String); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to listen on %s handler: %v\n", bh.String(), err)
		}
		id := addConn(bh)
		if err := bh.Start(id); err != nil {
			fmt.Fprintf(os.Stderr, "%s.Start() error: %v\n", bh.String(), err)
			return
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
		fmt.Fprintf(os.Stderr, "Failed to wait for all connections to close.  %d active\n", connCount())
	}
	if err := igst.Sync(time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to sync: %v\n", err)
	}
	if err := igst.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to close: %v\n", err)
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
							fmt.Fprintf(os.Stderr, "Failed to throw batch: %v\n", err)
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
						fmt.Fprintf(os.Stderr, "Failed to throw batch: %v\n", err)
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
						fmt.Fprintf(os.Stderr, "Failed to throw batch: %v\n", err)
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
