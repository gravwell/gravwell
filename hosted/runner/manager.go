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
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/hosted/plugins"
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
	kind      string    // plugin kind (okta, wiz, ...), the runner only knows its own ID
	childKey  string    // key this ingester is registered under on the ingest muxer
}

// childState grabs the current state of the ingester and stamps on the identifiers that only
// the manager knows about.  Name is the plugin kind and Label is the configured instance name,
// which is how a child shows up alongside every other ingester attached to the indexer.
func (wr wrappedRunner) childState() (s ingest.IngesterState) {
	s = wr.ChildState()
	s.Name = wr.kind
	s.Label = wr.Name()
	return
}

// childRegistry is the part of the ingest muxer used to report hosted ingesters as children.
// It is an interface so the registration bookkeeping can be exercised without standing up a muxer.
type childRegistry interface {
	RegisterChild(string, ingest.IngesterState)
	UnregisterChild(string)
}

type runtimeManager struct {
	ctx      context.Context
	cf       context.CancelFunc
	igst     *ingest.IngestMuxer
	reg      childRegistry
	sh       *storage.BoltHandler
	lgr      *log.Logger
	mp       map[uuid.UUID]wrappedRunner
	children map[string]struct{} // child keys currently registered on the ingest muxer
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
		ctx:      ctx,
		cf:       cf,
		igst:     igst,
		reg:      igst,
		lgr:      lg,
		sh:       sh,
		mp:       make(map[uuid.UUID]wrappedRunner),
		children: make(map[string]struct{}),
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
	// unregister all children, this won't happen in prod in a way that matters, but could be used in some tests
	for k := range rm.children {
		rm.reg.UnregisterChild(k)
		delete(rm.children, k)
	}

	return
}

func (rm *runtimeManager) createRunners(c *cfgType, ib base.IngesterBase) (err error) {
	if c == nil {
		return fmt.Errorf("nil config, can't create runners")
	}
	for name, builder := range c.Builders() {
		if err = rm.createRunner(name, builder); err != nil {
			if errors.Is(err, errExists) {
				continue // just skip it
			}
			return // this is an actual error
		}
	}
	return nil
}

var errExists = errors.New("hosted runner UUID already exists")

func (rm *runtimeManager) createRunner(name string, builder plugins.IngesterBuilder) error {
	rt, bw, err := rm.createNativeRuntime(builder.Kind(), name, builder.UUID())
	if err != nil {
		return fmt.Errorf("failed to create runtime for %s plugin %s: %w", builder.Kind(), name, err)
	}
	var ig hosted.Ingester
	if ig, err = builder.Build(rm.igst, bw.Sync); err != nil {
		return fmt.Errorf("failed to build %s plugin %s: %w", builder.Kind(), name, err)
	}
	runner, err := hosted.NewNativeRunner(builder.ID(), name, builder.Version(), builder.UUID(), builder.Config(), ig, rt)
	if err != nil {
		return fmt.Errorf("failed to create runner for %s plugin %s: %w", builder.Kind(), name, err)
	}
	if existing, exists := rm.mp[builder.UUID()]; exists {
		rm.lgr.Error("hosted runner UUID collision",
			log.KV("existing-uuid", existing.UUID()),
			log.KV("colliding-type", existing.ID()),
			log.KV("colliding-name", name),
			log.KV("colliding-uuid", builder.UUID()),
		)
		return errExists
	}
	rm.mp[builder.UUID()] = wrappedRunner{
		Runner:   runner,
		kind:     builder.Kind(),
		childKey: childKey(builder.Kind(), name),
	}
	return nil
}

// childKey builds the key a hosted ingester is registered under on the ingest muxer.
// Plugin configs are maps keyed by name within a kind, so kind and name together are unique.
func childKey(kind, name string) string {
	return fmt.Sprintf("%s/%s", kind, name)
}

// syncChildren resets the child registrations on the ingest muxer so that it reports exactly
// the set of hosted ingesters we are currently running.  Registration is a straight overwrite,
// so this doubles as the refresh that keeps each child's entry counts and uptime current.
// It runs on startup, after every config reload, and on the periodic check.
func (rm *runtimeManager) syncChildren() {
	active := make(map[string]struct{}, len(rm.mp))
	for _, wr := range rm.mp {
		active[wr.childKey] = struct{}{}
		rm.reg.RegisterChild(wr.childKey, wr.childState())
	}
	// anything we had registered that is no longer configured goes away
	for k := range rm.children {
		if _, ok := active[k]; !ok {
			rm.reg.UnregisterChild(k)
		}
	}
	rm.children = active
}

// createNativeRuntime creates a basic runtime that has handles on loggers, bucket writer, and the context
func (rm *runtimeManager) createNativeRuntime(kind, name string, ingesterUUID uuid.UUID) (rt hosted.Runtime, bw *storage.BucketWriter, err error) {
	ingesterID := fmt.Sprintf("%s/%s/%s", kind, name, ingesterUUID.String())
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
	rm.syncChildren()
	return
}

// reloadIngesters will walk the incoming config and compare it against the running config to determine an action
// to take on each plugin/ingester.  Actions can be:
//  1. start a whole new ingester
//  2. stop/remove an ingester that is no longer configured
//  3. detect a config change on an existing ingester and restart it
func (rm *runtimeManager) reloadIngesters(nc *cfgType) (err error) {
	if nc == nil {
		return errors.New("new config is empty")
	}

	newSet := map[string]struct{}{} // just a membership struct
	var es struct{}                 // empt value to assign into the membership struct

	// step 1 is check if there are any new ingesters that we can easily just start or restart
	for name, builder := range nc.Builders() {
		guid := builder.UUID()
		newSet[guid.String()] = es

		if existing, ok := rm.mp[guid]; !ok {
			// just fire it up
			if err = rm.createRunner(name, builder); err != nil {
				rm.lgr.Error("failed to create new ingester on reload",
					log.KV("kind", builder.Kind()),
					log.KV("name", name),
					log.KV("uuid", guid),
					log.KVErr(err),
				)
				// just continue
				continue
			}
			rm.lgr.Info("created new ingester on reload",
				log.KV("kind", builder.Kind()),
				log.KV("name", name),
				log.KV("uuid", guid),
			)
		} else if existing.configChanged(name, builder) {
			if lerr := existing.Close(); lerr != nil {
				rm.lgr.Error("failed to stop ingester on reload",
					log.KV("kind", builder.Kind()),
					log.KV("name", name),
					log.KV("uuid", guid),
					log.KVErr(lerr),
				)
				// does not restart, we don't want to corrupt the state
				continue
			}
			// ok fire the new one up
			delete(rm.mp, guid)
			if lerr := rm.createRunner(name, builder); lerr != nil {
				rm.lgr.Error("failed to create ingester on config change",
					log.KV("kind", builder.Kind()),
					log.KV("name", name),
					log.KV("uuid", guid),
					log.KVErr(lerr),
				)
				continue
			}
			rm.lgr.Info("restarted ingester on reload",
				log.KV("kind", builder.Kind()),
				log.KV("name", name),
				log.KV("uuid", guid),
			)
		}
	}

	// step 2 is check if any ingesters have been removed and shut them down
	for guid, wr := range rm.mp {
		if _, ok := newSet[guid.String()]; !ok {
			// ingester was removed, just kill it and delete it
			//grab the name and ID before we attempt to close it, just in case
			id, name := wr.ID(), wr.Name()
			if lerr := wr.Close(); lerr != nil {
				rm.lgr.Error("failed to shutdown ingester on reload",
					log.KV("id", id),
					log.KV("name", name),
					log.KV("uuid", guid),
					log.KVErr(lerr),
				)
				// not much to do here other than complain about it... :/
				continue
			}
			delete(rm.mp, guid) // ingester is closed
			rm.lgr.Info("shutdown ingester on reload",
				log.KV("id", id),
				log.KV("name", name),
				log.KV("uuid", guid),
			)
		}
	}

	// last step is to fire up all the ingesters that might be down
	return rm.startIngesters()
}

func (wr wrappedRunner) configChanged(name string, builder plugins.IngesterBuilder) bool {
	// if the name, UUID, kind (id), or version  changed, return true
	if wr.Name() != name || wr.UUID() != builder.UUID() {
		return true
	} else if wr.ID() != builder.ID() || wr.Version() != builder.Version() {
		return true
	}
	// now shunt off into the runner to see if it thinks its config changed
	if internalConfig := wr.Config(); internalConfig != nil {
		if compareConfig, ok := internalConfig.(hosted.Config); ok {
			return !compareConfig.Equal(builder.Config())
		}
	}
	return true // we can't compare so this gets restarted
}
