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

	"github.com/crewjam/rfc5424"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

const (
	restartDelay = time.Minute // on failures wait 1min to restart
)

type NativeConfig struct {
	Ingester_UUID uuid.UUID
}

// NativeRunner represents a specific instantiation of a native hosted ingester.
// A native runner is just a wrapper around the implemenation of the hosted.Ingester interface that runs
// in a regular old go routine.  We don't FULLY trust these, so we wrap them in a recover.
type NativeRunner struct {
	Ingester
	mtx          *sync.RWMutex
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
		mtx:          &sync.RWMutex{},
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
	defer nr.mtx.Unlock()
	if nr.running {
		return errors.New("already started")
	}
	nr.running = true

	nr.wg.Add(1)
	go func() {
		nr.run()
		nr.wg.Done()
		nr.mtx.Lock()
		nr.running = false
		nr.mtx.Unlock()
	}()
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
	nr.wg.Wait()
	if err = nr.LastError(); errors.Is(err, context.Canceled) {
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
	if nr != nil {
		nr.mtx.RLock()
		defer nr.mtx.RUnlock()
		return nr.running
	}
	return false
}

// LastError returns the last error encountered by the ingester
func (nr *NativeRunner) LastError() error {
	if nr != nil {
		nr.mtx.RLock()
		defer nr.mtx.RUnlock()
		return nr.err
	}
	return errors.New("native runner not initialized")
}

func (nr *NativeRunner) setError(err error) {
	if nr != nil {
		nr.mtx.Lock()
		defer nr.mtx.Unlock()
		nr.err = err
	}
}

// run wraps the Ingester.Run with some more tests and a recoverable runner loop so we can recover
func (nr *NativeRunner) run() {
	if nr == nil {
		return // not much else we can do
	}

	if nr.Ingester == nil || nr.rt == nil {
		nr.setError(errors.New("native runner not ready"))
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
		if stack, err := nr.recoverableRun(); err != nil {
			nr.setError(err)
			nr.rt.Error("native ingester failed",
				log.KV("id", nr.id),
				log.KV("name", nr.name),
				log.KV("uuid", nr.ingesterUUID),
				log.KVErr(err),
				log.KV("stack", stack))
		}
	}
}

// recoverableRun is just the underlying Ingster.Run wrapped in a defer recover so that if an ingester
// implementation fails we don't take down the entire hosted ingester stack.
func (nr *NativeRunner) recoverableRun() (stack string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("ingester panic")
			stack = fmt.Sprintf("%v", r)
		}
	}()
	err = nr.Ingester.Run(nr.ctx, nr.rt)
	return
}

func NewNativeLogger(lgr *log.Logger, appname, instance string) (*log.KVLogger, error) {
	if lgr == nil {
		return nil, errors.New("missing logger")
	}
	l, err := lgr.Clone(``, appname)
	if err != nil {
		return nil, fmt.Errorf("failed to clone logger: %w", err)
	}
	return log.NewLoggerWithKV(l, log.KV("instance", instance)), nil
}

// NativeRuntime implements a hosted.Runtime for native ingesters that don't need any special handling
type NativeRuntime struct {
	*storage.BucketWriter
	Logger *log.KVLogger
	igst   *ingest.IngestMuxer
	ctx    context.Context
	id     string
}

// NewNativeRuntime creates a basic runtime that has handles on loggers, bucket writer, and the context and is designed to run
// natively compiled/included ingesters.
func NewNativeRuntime(ctx context.Context, id string, bw *storage.BucketWriter, igst *ingest.IngestMuxer, lgr *log.KVLogger) (r *NativeRuntime, err error) {
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
		err = fmt.Errorf("runtime or ingester not initialized")
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

// The log methods need to be wrapped so we don't return errors to callers.
// This should eventually be handled and potentially surface through an Alive check failure.

func (nr *NativeRuntime) Debug(msg string, sds ...rfc5424.SDParam) {
	nr.Logger.Debug(msg, sds...)
}
func (nr *NativeRuntime) Info(msg string, sds ...rfc5424.SDParam) {
	nr.Logger.Info(msg, sds...)
}
func (nr *NativeRuntime) Warn(msg string, sds ...rfc5424.SDParam) {
	nr.Logger.Warn(msg, sds...)
}
func (nr *NativeRuntime) Error(msg string, sds ...rfc5424.SDParam) {
	nr.Logger.Error(msg, sds...)
}
func (nr *NativeRuntime) Critical(msg string, sds ...rfc5424.SDParam) {
	nr.Logger.Critical(msg, sds...)
}
