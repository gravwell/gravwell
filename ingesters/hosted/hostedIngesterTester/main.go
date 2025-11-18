/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// This is a simple hosted ingester tester for use in developing hosted ingesters.
// This test utility does not contain all the isolation and complete runtimes of the
// full hosted ingester system.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/hosted"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

const (
	defaultConfigLoc  = `/tmp/hosted_ingester_tester.conf`
	defaultConfigDLoc = ``
	ingesterName      = `hostedtest`
	appName           = `hostedtest`

	exitSyncTimeout = time.Minute
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

	lg := ib.Logger
	_, ok := cfg.IngesterUUID()
	if !ok {
		ib.Logger.FatalCode(0, "could not read ingester UUID")
	}

	// check that we have configured ingesters
	if c := cfg.IngesterCount(); c <= 0 {
		ib.Logger.FatalCode(0, "no hosted ingesters configured")
		return
	} else {
		ib.Logger.Info("starting", log.KV("hosted-count", c))
	}

	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()
	ib.AnnounceStartup()
	lg.Info("Ingester running")

	mp := make(map[uuid.UUID]hosted.Runner, cfg.IngesterCount())
	ctx, cf := context.WithCancel(context.Background())

	// Fire up native hosted ingesters first
	if err = startNativeIngesters(ctx, cfg, ib, igst, mp); err != nil {
		ib.Logger.Error("failed to start native ingesters", log.KVErr(err))
	}

	//listen for signals so we can close gracefully
	utils.WaitForQuit()
	cf()

	if err = stopIngesters(mp); err != nil {
		ib.Logger.Error("failed to close ingesters", log.KVErr(err))
	}

	// go shutdown everything
	ib.AnnounceShutdown()
	if err = igst.Sync(exitSyncTimeout); err != nil {
		ib.Logger.Error("failed to sync ingest muxer", log.KVErr(err))
	} else if err = igst.Close(); err != nil {
		ib.Logger.Error("failed to close ingest muxer", log.KVErr(err))
	}

}

func startNativeIngesters(ctx context.Context, cfg *cfgType, ib base.IngesterBase, igst *ingest.IngestMuxer, mp map[uuid.UUID]hosted.Runner) (err error) {
	return
}

func stopIngesters(mp map[uuid.UUID]hosted.Runner) (err error) {
	return
}
