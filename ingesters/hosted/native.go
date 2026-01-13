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
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

const (
	restartDelay = time.Minute // on failures wait 1min to restart
)

type NativeConfig struct {
	Ingester_UUID uuid.UUID //
}

// NativeRunner represents a specific instantiation of a native hosted ingester.
// A native runner is just a wrapper around the implemenation of the hosted.Ingester interface that runs
// in a regular old go routine.  We don't FULLY trust these, so we wrap them in a recover.
type NativeRunner struct {
	Ingester
	mtx          *sync.Mutex
	wg           *sync.WaitGroup
	id           string
	name         string
	version      version.Canonical
	ingesterUUID uuid.UUID
	rt           Runtime
	ctx          context.Context
	cf           context.CancelFunc
	running      bool  // is the ingester currently running
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
		mtx:          &sync.Mutex{},
		wg:           &sync.WaitGroup{},
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
	nr.mtx.Lock()
	if !nr.running {
		nr.running = true
		nr.wg.Add(1)
		go nr.run()
	} else {
		err = errors.New("already started")
	}
	nr.mtx.Unlock()
	return
}

// Close stops the running routine, collects the error and returns
func (nr *NativeRunner) Close() (err error) {
	if nr == nil || nr.rt == nil {
		return errors.New("not ready")
	}
	nr.cf()
	// wait for routine to exit TODO FIXME - add a timeout on this wait
	// WaitGroup probably isn't the right tool here
	nr.wg.Done()
	if err = nr.err; err == context.Canceled {
		err = nil
	}
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

// Running returns whether the ingester is currently running
func (nr *NativeRunner) Running() bool {
	return nr.running
}

// LastError returns the last error encountered by the ingester
func (nr *NativeRunner) LastError() error {
	if nr != nil {
		return nr.err
	}
	return errors.New("native runner not initialized")
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
			if nr.rt.Sleep(restartDelay - d) {
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

func NewNativeLogger(lgr *log.Logger, appname string) (r Logger, err error) {
	if lgr == nil {
		return nil, errors.New("missing logger")
	}
	if r, err = lgr.Clone(``, appname); err != nil {
		r, err = nil, fmt.Errorf("failed to clone logger: %w", err)
	}
	return
}

// NativeRuntime implements a hosted.Runtime for native ingesters that don't need any special handling
type NativeRuntime struct {
	*BucketWriter
	Logger
	igst *ingest.IngestMuxer
	ctx  context.Context
	id   string
}

// NewNativeRuntime creates a basic runtime that has handles on loggers, bucket writer, and the context and is designed to run
// natively compiled/included ingesters.
func NewNativeRuntime(ctx context.Context, id string, bw *BucketWriter, igst *ingest.IngestMuxer, lgr Logger) (r *NativeRuntime, err error) {
	if bw == nil {
		err = fmt.Errorf("missing bucket writer")
		return
	} else if lgr == nil {
		err = fmt.Errorf("missing logger")
		return
	} else if igst == nil {
		err = fmt.Errorf("missing ingest muxer")
		return
	} else if ctx == nil {
		err = fmt.Errorf("missing context")
		return
	} else if id == `` {
		err = fmt.Errorf("missing runtime ID")
		return
	}
	r = &NativeRuntime{
		BucketWriter: bw,
		Logger:       lgr,
		igst:         igst,
		ctx:          ctx,
		id:           id,
	}
	return
}

// Alive returns true if the runtime is considered alive, this simply means that the upstream ingest muxer is not blocked
func (nr *NativeRuntime) Alive() bool {
	return !nr.igst.WillBlock() // if the ingest muxer is blocked, we are not alive, that means we keep trucking if cache is alive and well
}

// Sleep sleeps for the given duration or until the context is done, returning true if the context was done
func (nr *NativeRuntime) Sleep(d time.Duration) (r bool) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-nr.ctx.Done():
		r = true
	}
	return
}

// Context returns the runtime context
func (nr *NativeRuntime) Context() context.Context {
	return nr.ctx
}

// ID returns the runtime ID
func (nr *NativeRuntime) ID() string {
	return nr.id
}

// NegotiateTag negotiates a tag with the ingest muxer natively
func (nr *NativeRuntime) NegotiateTag(s string) (t entry.EntryTag, err error) {
	if nr == nil || nr.igst == nil {
		err = fmt.Errorf("ingest writer not available")
		return
	}
	return nr.igst.NegotiateTag(s)
}

func (nr *NativeRuntime) Write(ent entry.Entry) (err error) {
	if nr == nil || nr.igst == nil {
		err = fmt.Errorf("ingest writer not available")
		return
	}
	// we cannot trust the ingesters to not modify the entry or re-use buffers, perform a deep copy on the entry
	localEnt := ent.DeepCopy()
	err = nr.igst.WriteEntry(&localEnt)
	return
}
