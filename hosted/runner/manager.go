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
	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/hosted/storage"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/base"
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
	sh   *storage.BoltHandler
	lgr  *log.Logger
	mp   map[uuid.UUID]wrappedRunner
}

func newRuntimeManager(igst *ingest.IngestMuxer, sh *storage.BoltHandler, lg *log.Logger) (r *runtimeManager, err error) {
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

func (rm *runtimeManager) createRunners(c *cfgType, ib base.IngesterBase) (err error) {
	if c == nil {
		return fmt.Errorf("nil config, can't create runners")
	}
	for name, builder := range c.Builders() {
		var ig hosted.Ingester
		if ig, err = builder.Build(rm.igst); err != nil {
			return fmt.Errorf("failed to build %s plugin %s: %w", builder.Kind(), name, err)
		}
		rt, err := rm.createNativeRuntime(builder.Kind(), name, builder.UUID())
		if err != nil {
			return fmt.Errorf("failed to create runtime for %s plugin %s: %w", builder.Kind(), name, err)
		}
		runner, err := hosted.NewNativeRunner(builder.ID(), name, builder.Version(), builder.UUID(), ig, rt)
		if err != nil {
			return fmt.Errorf("failed to create runner for %s plugin %s: %w", builder.Kind(), name, err)
		}
		if existing, exists := rm.mp[builder.UUID()]; exists {
			ib.Logger.Error("hosted runner UUID collision",
				log.KV("existing-uuid", existing.UUID()),
				log.KV("colliding-type", existing.ID()),
				log.KV("colliding-name", name),
				log.KV("colliding-uuid", builder.UUID()))
			continue // just skip it
		}
		rm.mp[builder.UUID()] = wrappedRunner{Runner: runner}
		// TODO(2073): Register individual plugins as child ingesters
		//rm.igst.RegisterChild(builder.UUID().String(), ingest.IngesterState{
		//	Configuration:
		//})
	}
	return nil
}

// createNativeRuntime creates a basic runtime that has handles on loggers, bucket writer, and the context
func (rm *runtimeManager) createNativeRuntime(kind, name string, ingesterUUID uuid.UUID) (rt hosted.Runtime, err error) {
	// grab a new native runtime based on the kind, name, and UUID
	ingesterID := fmt.Sprintf("%s/%s/%s", kind, name, ingesterUUID.String())
	var bw *storage.BucketWriter
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
