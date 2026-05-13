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
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/hosted/storage"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/base"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/hosted_runner.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/hosted_runner.conf.d`
	ingesterName      = `hosted-runner`
	appName           = `hosted-runner`

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
		fmt.Fprintf(os.Stderr, "failed to assign configuration %v\n", err)
		return
	}

	lg := ib.Logger
	_, ok := cfg.Global.IngesterUUID()
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

	// get the state manager up and rolling
	sh, err := storage.OpenBoltHandler(cfg.State.Path, cfg.State.Sync)
	if err != nil {
		ib.Logger.FatalCode(0, "failed to open state handler", log.KVErr(err))
		return
	}

	// get the ingest connection
	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()

	ib.AnnounceStartup()
	lg.Info("Ingester running")

	rm, err := newRuntimeManager(igst, sh, lg)
	if err != nil {
		sh.Close() // ignore return, but no writes should have occurred
		ib.Logger.FatalCode(0, "failed to create runtime manager", log.KVErr(err))
	}

	// Fire up native hosted ingesters first
	if err = rm.createRunners(cfg, ib); err != nil {
		rm.stop()  // best effort close
		sh.Close() // ignore return, but no writes should have occurred
		ib.Logger.FatalCode(1, "failed to create ingesters", log.KVErr(err))
	}

	// ingesters exist, fire them up
	if err = rm.startIngesters(); err != nil {
		rm.stop()  // best effort close
		sh.Close() // ignore return, but no writes should have occurred
		ib.Logger.FatalCode(2, "failed to start ingesters", log.KVErr(err))
	}

	//listen for signals so we can close gracefully
	sig := utils.GetQuitChannel()
	tckr := time.NewTicker(time.Minute)
	defer tckr.Stop()

exitLoop:
	for {
		select {
		case <-sig:
			lg.Info("ingester shutting down")
			break exitLoop
		case <-tckr.C:
			// go check on all ingesters and see if we should try to restart one that has died
			rm.startIngesters()
		}
	}

	if err = rm.stop(); err != nil {
		ib.Logger.Error("failed to close ingesters", log.KVErr(err))
	} else if err = sh.Close(); err != nil {
		ib.Logger.Error("failed to close state handler", log.KVErr(err))
	}

	// go shutdown everything
	ib.AnnounceShutdown()
	if err = igst.Sync(exitSyncTimeout); err != nil {
		ib.Logger.Error("failed to sync ingest muxer", log.KVErr(err))
	} else if err = igst.Close(); err != nil {
		ib.Logger.Error("failed to close ingest muxer", log.KVErr(err))
	}
}

func stackCloseErrors(curr, next error, name string, guid uuid.UUID) error {
	next = fmt.Errorf("failed to close %s (%v) %w", name, guid, next)
	if curr == nil {
		return next
	}
	return errors.Join(curr, next)
}
