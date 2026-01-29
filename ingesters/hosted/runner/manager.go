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
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/hosted"
)

const (
	stopTimeout  = 10 * time.Second // how long we will wait for ingesters to exit gracefully
	restartDelay = time.Minute      // on failures wait 1min to restart
)

type wrappedRunner struct {
	hosted.Runner
	lastStart time.Time // when was the last time a start was attempted
}

type runtimeManager struct {
	ctx  context.Context
	cf   context.CancelFunc
	igst *ingest.IngestMuxer
	sh   *hosted.StateHandler
	lgr  *log.Logger
	mp   map[uuid.UUID]wrappedRunner
}

func newRuntimeManager(igst *ingest.IngestMuxer, sh *hosted.StateHandler, lg *log.Logger) (r *runtimeManager, err error) {
	if sh == nil {
		err = fmt.Errorf("missing state handler")
		return
	} else if igst == nil {
		err = fmt.Errorf("missing ingest muxer")
		return
	}
	ctx, cf := context.WithCancel(context.Background())
	r = &runtimeManager{
		ctx:  ctx,
		cf:   cf,
		igst: igst,
		lgr:  lg,
		sh:   sh,
		mp:   make(map[uuid.UUID]wrappedRunner),
	}
	return
}

func (rm *runtimeManager) stop() (err error) {
	rm.cf()
	for _, v := range rm.mp {
		if lerr := v.Close(); lerr != nil {
			err = stackCloseErrors(err, lerr, v.Name(), v.UUID())
		}
	}
	return
}

// createNativeRuntime creates a basic runtime that has handles on loggers, bucket writer, and the context
func (rm *runtimeManager) createNativeRuntime(kind, name string, ingesterUUID uuid.UUID) (rt hosted.Runtime, err error) {
	// grab a new native runtime based on the kind, name, and UUID
	ingesterID := fmt.Sprintf("%s/%s/%s", kind, name, ingesterUUID.String())
	var bw *hosted.BucketWriter
	// get a bucket writer for this specific ingester to maintain state
	if bw, err = rm.sh.GetBucketWriter(ingesterID); err != nil {
		err = fmt.Errorf("failed to get bucket writer for hosted ingester %s: %w", ingesterID, err)
		return
	}
	// create a new logger that gets line numbers and appname right for native ingesters
	var lgr *log.KVLogger
	if lgr, err = hosted.NewNativeLogger(rm.lgr, kind, name); err != nil {
		err = fmt.Errorf("failed to create native logger for hosted ingester %s: %w", ingesterID, err)
		return
	}
	// create the native runtime
	rt, err = hosted.NewNativeRuntime(rm.ctx, ingesterID, bw, rm.igst, lgr)
	return
}

// createIngesters loads up all of the ingesters specified in the config and then goes after any ingesters that may
// have been pulled
func (rm *runtimeManager) createIngesters(cfg *cfgType, ib base.IngesterBase) (err error) {
	// load up the native ingesters
	err = cfg.forEachIngester(rm.igst, rm.createNativeRuntime, func(name, id string, runner hosted.Runner) error {
		ingesterUUID := runner.UUID()
		if existing, ok := rm.mp[ingesterUUID]; ok {
			ib.Logger.Error("hosted ingester UUID collision",
				log.KV("existing-uuid", existing.UUID()),
				log.KV("colliding-type", existing.ID()),
				log.KV("colliding-name", name),
				log.KV("colliding-uuid", ingesterUUID))
			return nil // just skip it
		}

		wr := wrappedRunner{
			Runner: runner,
		}
		rm.mp[ingesterUUID] = wr
		return nil
	})

	return
}

// startIngesters goes through the map of ingesters and starts any that are not running
func (rm *runtimeManager) startIngesters() (err error) {
	//actually fire up the ingesters
	for k, v := range rm.mp {
		// check if ingester is running
		if !v.Running() {
			// check if we have attempted to restart within the restartDelay
			if time.Since(v.lastStart) < restartDelay {
				// too soon since last start attempt
				continue
			}
			v.lastStart = time.Now()
			rm.mp[k] = v // update the map with the new lastStart time
			if lerr := v.Start(); lerr != nil {
				rm.lgr.Error("failed to start hosted ingester",
					log.KV("ingester-name", v.Name()),
					log.KV("ingester-uuid", v.UUID()),
					log.KV("error", lerr))
			}
		}
	}
	return
}
