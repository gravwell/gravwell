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
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/utils/caps"
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
	debugOn bool
	lg      *log.Logger
)

func main() {
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
	//check capabilities so we can scream and throw a potential warning upstream
	if !caps.Has(caps.NET_BIND_SERVICE) {
		lg.Warn("missing capability", log.KV("capability", "NET_BIND_SERVICE"), log.KV("warning", "may not be able to bind to service ports"))
		debugout("missing capability NET_BIND_SERVICE, may not be able to bind to service ports")
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
	ib.AnnounceShutdown()
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
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("failed to sync", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		lg.Error("failed to close", log.KVErr(err))
	}
}

func debugout(format string, args ...interface{}) {
	if debugOn {
		fmt.Printf(format, args...)
	}
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
