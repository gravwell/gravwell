/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package hosted

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest/log"
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
	Type         string
	ingesterUUID uuid.UUID
}

// NewRunner creates a new NativeRunner that has validated some basic parameters and is ready to Run
func NewRunner(tp string, ingesterUUID uuid.UUID, ig Ingester) (r *NativeRunner, err error) {
	if tp == `` {
		err = errors.New("missing type")
		return
	} else if ig == nil {
		err = errors.New("nil ingester interface")
		return
	}
	if ingesterUUID == uuid.Nil {
		ingesterUUID = uuid.New()
	}
	r = &NativeRunner{
		Type:         tp,
		Ingester:     ig,
		ingesterUUID: ingesterUUID,
	}
	return
}

// Run wraps the Ingester.Run with some more tests and a recoverable runner loop so we can recover
func (nr *NativeRunner) Run(rt Runtime) (err error) {
	if nr == nil || nr.Ingester == nil {
		err = errors.New("native runner not ready")
		return
	} else if rt == nil {
		err = errors.New("nil runtime")
		return
	}
	var lastRun time.Time
	for rt.Context().Err() == nil {
		if d := time.Since(lastRun); d < restartDelay {
			if rt.Sleep(d) {
				break
			}
		}
		lastRun = time.Now()
		var stack string
		if stack, err = nr.recoverableRun(rt); err != nil {
			rt.Error("native ingester failed",
				log.KV("type", nr.Type),
				log.KV("name", nr.Name()),
				log.KV("uuid", nr.ingesterUUID),
				log.KVErr(err),
				log.KV("stack", stack))
		}
	}
	return
}

// recoverableRun is just the underlying Ingster.Run wrapped in a defer recover so that if an ingester
// implementation fails we don't take down the entire hosted ingester stack.
func (nr *NativeRunner) recoverableRun(rt Runtime) (stack string, err error) {
	defer func(rerr *error) {
		if r := recover(); r != nil {
			// check our parameter and that the caller didn't already set it somehow...
			if rerr != nil && *rerr != nil {
				*rerr = errors.New("ingester panic")
			}
			stack = fmt.Sprintf("%v", r)
		}
	}(&err)
	err = nr.Ingester.Run(rt)
	return
}
