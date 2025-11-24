/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package hosted

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

const (
	restartDelay = 30 * time.Second // on failures wait 30s to restart
)

type NativeConfig struct {
	Ingester_UUID uuid.UUID //
}

// NativeRunner represents a specific instantiation of a native hosted ingester.
// A native runner is just a wrapper around the implemenation of the hosted.Ingester interface that runs
// in a regular old go routine.  We don't FULLY trust these, so we wrap them in a recover.
type NativeRunner struct {
	Ingester
	id           string
	name         string
	version      version.Canonical
	ingesterUUID uuid.UUID
	rt           Runtime
	ctx          context.Context
	cf           context.CancelFunc
	err          error // error from go routine runner
}

// NewNativeRunner creates a new NativeRunner that has validated some basic parameters and is ready to Run
func NewNativeRunner(id, name, verstr string, ingesterUUID uuid.UUID, ig Ingester, rt Runtime) (r *NativeRunner, err error) {
	var ver version.Canonical
	if id == `` {
		err = errors.New("missing ingester ID")
		return
	} else if name == `` {
		err = errors.New("missing ingester name")
		return
	} else if verstr == `` {
		err = errors.New("missing ingester version")
		return
	} else if ig == nil {
		err = errors.New("nil ingester interface")
		return
	} else if ver, err = version.Parse(verstr); err != nil {
		return
	}
	if ingesterUUID == uuid.Nil {
		ingesterUUID = uuid.New()
	}
	r = &NativeRunner{
		id:           id,
		name:         name,
		version:      ver,
		Ingester:     ig,
		ingesterUUID: ingesterUUID,
		rt:           rt,
	}
	r.ctx, r.cf = context.WithCancel(rt.Context())
	return
}

// Start initializes and starts the ingester routine
func (nr *NativeRunner) Start() (err error) {
	if nr == nil || nr.Ingester == nil || nr.rt == nil {
		return errors.New("not ready")
	}
	//TODO check if we are already started
	return
}

// Close stops the running routine, collects the error and returns
func (nr *NativeRunner) Close() (err error) {
	if nr == nil || nr.rt == nil {
		return errors.New("not ready")
	}
	nr.cf()
	//TODO wait for routine to exit
	err = nr.err
	return
}

// ID returns the ID to implement the interface
func (nr *NativeRunner) ID() (id string) {
	if nr != nil {
		id = nr.id
	}
	return
}

// Name returns the name to implement the interface
func (nr *NativeRunner) Name() (name string) {
	if nr != nil {
		name = nr.name
	}
	return
}

// UUID returns the name to implement the interface
func (nr *NativeRunner) UUID() (r uuid.UUID) {
	if nr != nil {
		r = nr.ingesterUUID
	}
	return
}

// run wraps the Ingester.Run with some more tests and a recoverable runner loop so we can recover
func (nr *NativeRunner) run() {
	if nr == nil || nr.Ingester == nil || nr.rt == nil {
		nr.err = errors.New("native runner not ready")
		return
	}
	var lastRun time.Time
	for nr.ctx.Err() == nil {
		if d := time.Since(lastRun); d < restartDelay {
			if nr.rt.Sleep(d) {
				break
			}
		}
		lastRun = time.Now()
		var stack string
		if stack, nr.err = nr.recoverableRun(); nr.err != nil {
			nr.rt.Error("native ingester failed",
				log.KV("id", nr.id),
				log.KV("name", nr.name),
				log.KV("uuid", nr.ingesterUUID),
				log.KVErr(nr.err),
				log.KV("stack", stack))
		}
	}
}

// recoverableRun is just the underlying Ingster.Run wrapped in a defer recover so that if an ingester
// implementation fails we don't take down the entire hosted ingester stack.
func (nr *NativeRunner) recoverableRun() (stack string, err error) {
	defer func(rerr *error) {
		if r := recover(); r != nil {
			// check our parameter and that the caller didn't already set it somehow...
			if rerr != nil && *rerr != nil {
				*rerr = errors.New("ingester panic")
			}
			stack = fmt.Sprintf("%v", r)
		}
	}(&err)
	err = nr.Ingester.Run(nr.ctx, nr.rt)
	return
}
