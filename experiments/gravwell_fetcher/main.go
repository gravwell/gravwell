/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
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

	"github.com/gravwell/gravwell/v3/debug"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
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

	lg = ib.Logger

	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()
	ib.AnnounceStartup()

	// set state tracker
	fetcherTracker, err := NewObjectTracker(cfg.Global.State_Store_Location)
	if err != nil {
		ib.Logger.FatalCode(0, "failed to create state tracker", log.KVErr(err))
		return
	}

	// fire up the fetcher handlers for each type
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	var src net.IP
	buildAsanaHandlerConfig(cfg, src, fetcherTracker, lg, igst, ib, ctx, &wg)
	buildDuoHandlerConfig(cfg, src, fetcherTracker, lg, igst, ib, ctx, &wg)
	buildThinkstHandlerConfig(cfg, src, fetcherTracker, lg, igst, ib, ctx, &wg)
	buildOktaHandlerConfig(cfg, src, fetcherTracker, lg, igst, ib, ctx, &wg)
	buildShodanHandlerConfig(cfg, src, fetcherTracker, lg, igst, ib, ctx, &wg)

	// listen for signals so we can close gracefully
	utils.WaitForQuit()
	ib.AnnounceShutdown()

	cancel()
	wg.Wait()

	id, ok := cfg.Global.IngesterUUID()
	if !ok {
		ib.Logger.FatalCode(0, "could not read ingester UUID")
	}
	lg.Info("Fetcher ingester exiting", log.KV("ingesteruuid", id))
	if err := igst.Sync(utils.ExitSyncTimeout); err != nil {
		lg.Error("failed to sync", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		lg.Error("failed to close", log.KVErr(err))
	}
}
