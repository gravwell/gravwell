/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/gravwell/gravwell/v4/debug"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/base"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/netflow_capture.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/netflow_capture.conf.d`
	ingesterName      = `flow`
	appName           = `netflow`
	batchSize         = 512
)

var (
	debugOn bool
	lg      *log.Logger

	exitCtx, exitFn = context.WithCancel(context.Background())
)

func main() {
	go debug.HandleDebugSignals(ingesterName)
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
		return
	} else if err = ib.AssignConfig(&cfg); err != nil || cfg == nil {
		fmt.Fprintf(os.Stderr, "failed to assign configuration %v %v\n", err, cfg == nil)
		return
	}
	debugOn = ib.Verbose
	lg = ib.Logger
	id, ok := cfg.IngesterUUID()
	if !ok {
		ib.Logger.FatalCode(0, "could not read ingester UUID")
	}

	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()
	ib.AnnounceStartup()

	debugout("Started ingester muxer\n")

	connClosers = make(map[int]closer, 1)
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
	ib.AnnounceShutdown()
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

	exitFn()

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
					if err := igst.WriteBatchContext(exitCtx, ents); err != nil {
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
				if err := igst.WriteBatchContext(exitCtx, ents); err != nil {
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
				if err := igst.WriteBatchContext(exitCtx, ents); err != nil {
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
	if debugOn {
		fmt.Printf(format, args...)
	}
}
