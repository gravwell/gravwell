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
	"github.com/gravwell/gravwell/v3/ingesters/hosted/okta"
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

	// okta
	for k, v := range cfg.Okta {
		// this shouldn't happen, but scream about it anyway
		if v == nil {
			ib.Logger.Error("nil okta ingester config", log.KV("name", k))
			continue
		}
		if existing, ok := mp[v.UUID()]; ok {
			ib.Logger.Error("hosted ingester UUID collision",
				log.KV("existing-name", existing.Name()),
				log.KV("existing-uuid", existing.UUID()),
				log.KV("colliding-type", `okta`),
				log.KV("colliding-name", k),
				log.KV("colliding-uuid", v.Ingester_UUID))
			continue // just skip it
		}
		// get a new ingester
		var ig *okta.OktaIngester
		var runner *hosted.NativeRunner
		if ig, err = okta.NewOktaIngester(*v, igst); err != nil {
			ib.Logger.Error("failed to create new okta ingester", log.KVErr(err))
			continue
		}

		//TODO FIXME - create a new runtime
		var rt hosted.Runtime

		// create a new hosted native runner
		if runner, err = hosted.NewNativeRunner(`okta`, v.UUID(), ig, rt); err != nil {
			ib.Logger.Error("failed to create new native runner",
				log.KV("type", `okta`),
				log.KV("name", k),
				log.KV("uuid", v.Ingester_UUID),
				log.KVErr(err))
			continue
		}
		mp[v.UUID()] = runner

	}
	return
}

func stopIngesters(mp map[uuid.UUID]hosted.Runner) (err error) {
	for _, v := range mp {
		if lerr := v.Close(); lerr != nil {
			err = stackCloseErrors(err, lerr, v.Name(), v.UUID())
		}
	}
	return
}

func stackCloseErrors(curr, next error, name string, guid uuid.UUID) error {
	if curr == nil {
		return fmt.Errorf("failed to close %s (%v) %w", name, guid, next)
	}
	return fmt.Errorf("%w\nfailed to close %s (%v) %w", curr, name, guid, next)
}
