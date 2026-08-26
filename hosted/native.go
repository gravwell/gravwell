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
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/version"
	"golang.org/x/sync/errgroup"
)

const (
	restartDelay = time.Minute // on failures wait 1min to restart
)

var (
	ErrNotReady       = errors.New("not ready")
	ErrAlreadyStarted = errors.New("already started")
	ErrIngesterPanic  = errors.New("ingester panic")
)

type NativeConfig struct {
	Ingester_UUID uuid.UUID
}

// NativeRunner represents a specific instantiation of a native hosted ingester.
// A native runner is just a wrapper around the implementation of the hosted.Ingester interface that runs
// in a regular old go routine.  We don't FULLY trust these, so we wrap them in a recover.
type NativeRunner struct {
	Ingester
	mtx          *sync.RWMutex
	eg           *errgroup.Group
	id           string
	name         string
	version      version.Canonical
	ingesterUUID uuid.UUID
	rt           Runtime // scoped runtime handed to the ingester
	cfg          any     // the native config for the ingester
	ctx          context.Context
	cf           context.CancelFunc
	running      bool      // is the ingester currently running
	started      time.Time // when the ingester was first started, used to report child uptime
	err          error     // error from go routine runner
}

// NewNativeRunner creates a new NativeRunner that has validated some basic parameters and is ready to Run
func NewNativeRunner(id, name, verstr string, ingesterUUID uuid.UUID, cfg any, ig Ingester, rt Runtime) (r *NativeRunner, err error) {
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
	} else if cfg == nil {
		err = errors.New("nil config")
		return
	}
	if ingesterUUID == uuid.Nil {
		ingesterUUID = uuid.New()
	}
	r = &NativeRunner{
		id:           id,
		mtx:          &sync.RWMutex{},
		eg:           &errgroup.Group{},
		name:         name,
		version:      ver,
		Ingester:     ig,
		ingesterUUID: ingesterUUID,
		cfg:          cfg,
	}
	r.ctx, r.cf = context.WithCancel(rt.Context())
	r.rt = &scopedRuntime{Runtime: rt, ctx: r.ctx}
	return
}

// scopedRuntime narrows a Runtime down to a single runner's lifetime.
// The runtime handed to NewNativeRunner is shared by every ingester in the process, so its
// context only ends when the whole process is going down. An ingester that waits on that
// context can never be stopped on its own, which means Close blocks forever and a config
// reload can't cycle just one ingester. Scoping Context and Sleep to the runner's own
// context is what makes an individual stop actually work.
type scopedRuntime struct {
	Runtime
	ctx context.Context
}

func (sr *scopedRuntime) Context() context.Context { return sr.ctx }

// StateProvider implements StateUnwrapper so that the runner can reach the ingest counters of
// the runtime we wrap, embedding the Runtime interface hides them from a type assertion.
func (sr *scopedRuntime) StateProvider() StateProvider {
	return runtimeStateProvider(sr.Runtime)
}

func (sr *scopedRuntime) Sleep(d time.Duration) (r bool) {
	tmr := time.NewTimer(d)
	defer tmr.Stop()
	select {
	case <-tmr.C:
	case <-sr.ctx.Done():
		r = true
	}
	return
}

// runtimeStateProvider digs the StateProvider out of a runtime, whether the runtime implements it
// directly or wraps something that does.  A runtime that tracks no state gets a stub, so callers
// always have something to ask.
func runtimeStateProvider(rt Runtime) StateProvider {
	if sp, ok := rt.(StateProvider); ok {
		return sp
	}
	if u, ok := rt.(StateUnwrapper); ok {
		if sp := u.StateProvider(); sp != nil {
			return sp
		}
	}
	return emptyState{}
}

// emptyState stands in for a runtime that tracks nothing at all
type emptyState struct{}

func (emptyState) State() State { return State{} }

// Start initializes and starts the ingester routine
func (nr *NativeRunner) Start() error {
	if nr == nil || nr.Ingester == nil || nr.rt == nil {
		return ErrNotReady
	}
	nr.mtx.Lock()
	defer nr.mtx.Unlock()
	if nr.running {
		return ErrAlreadyStarted
	}
	nr.running = true
	if nr.started.IsZero() {
		nr.started = time.Now()
	}

	nr.eg.Go(func() error {
		rerr := nr.run()
		nr.mtx.Lock()
		nr.running = false
		nr.mtx.Unlock()
		return rerr
	})
	return nil
}

// Close stops the running routine, collects the error and returns
func (nr *NativeRunner) Close() (err error) {
	if nr == nil || nr.rt == nil {
		return ErrNotReady
	}
	nr.cf()
	err = nr.eg.Wait()
	if errors.Is(err, context.Canceled) {
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

func (nr *NativeRunner) Version() (r string) {
	if nr != nil {
		r = nr.version.String()
	}
	return
}

func (nr *NativeRunner) Config() (r any) {
	if nr != nil {
		r = nr.cfg
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

// ChildState builds up the state of this single hosted ingester so that it can be registered
// as a child on the shared ingest muxer.  Ingest counters come from the runtime we were handed,
// a runtime that does not track them just yields empty counters.
func (nr *NativeRunner) ChildState() (s ingest.IngesterState) {
	if nr == nil {
		return
	}
	s = ingest.IngesterState{
		UUID:    nr.ingesterUUID.String(),
		Name:    nr.id,
		Label:   nr.name,
		Version: nr.version.String(),
	}
	nr.mtx.RLock()
	running, started := nr.running, nr.started
	nr.mtx.RUnlock()
	// a stopped ingester has no uptime to speak of, leave it at zero
	if running && !started.IsZero() {
		s.Uptime = time.Since(started)
	}
	// a config is only reported if the plugin opted in by implementing ConfigSanitizer,
	// we do not report a config we were not explicitly handed a safe version of
	if cs, ok := nr.cfg.(ConfigSanitizer); ok {
		if cfg := cs.SanitizedConfig(); cfg != nil {
			if bts, err := json.Marshal(cfg); err != nil {
				nr.rt.Error("failed to encode hosted ingester config",
					log.KV("id", nr.id), log.KV("name", nr.name), log.KVErr(err))
			} else {
				s.Configuration = json.RawMessage(bts)
			}
		}
	}
	st := runtimeStateProvider(nr.rt).State()
	s.Entries, s.Size, s.Tags = st.Entries, st.Size, st.Tags
	// an error or warning is only carried when there actually is one, the metadata block
	// is dropped from a child state that grows too large to ship
	if md, err := childMetadata(st); err != nil {
		nr.rt.Error("failed to encode hosted ingester status",
			log.KV("id", nr.id), log.KV("name", nr.name), log.KVErr(err))
	} else {
		s.Metadata = md
	}
	return
}

// ChildStatus is the reporting block attached to a hosted ingester's child state.  A plugin that
// is degraded but still running has no other way to surface that condition to the indexer.
type ChildStatus struct {
	Error string `json:",omitempty"`
	Warn  string `json:",omitempty"`
}

// childMetadata encodes the error and warning conditions of a state, it returns nil when the
// ingester is healthy so that a clean child carries no metadata at all.
func childMetadata(st State) (r json.RawMessage, err error) {
	if st.Error == nil && st.Warn == `` {
		return
	}
	cs := ChildStatus{Warn: st.Warn}
	if st.Error != nil {
		cs.Error = st.Error.Error()
	}
	var bts []byte
	if bts, err = json.Marshal(cs); err != nil {
		return
	}
	r = json.RawMessage(bts)
	return
}

// run wraps the Ingester.Run with some more tests and a recoverable runner loop so we can recover
func (nr *NativeRunner) run() error {
	if nr == nil || nr.Ingester == nil || nr.rt == nil {
		return ErrNotReady
	}

	var lastRun time.Time
	var lastErr error
	for nr.ctx.Err() == nil {
		if d := time.Since(lastRun); d < restartDelay {
			if nr.rt.Sleep(restartDelay - d) {
				break
			}
		}
		lastRun = time.Now()
		// a fresh attempt starts clean, whatever went wrong last time is stale.  Job based
		// plugins manage this per poll, this covers panics and ingesters that run their own loop
		nr.rt.ClearError()
		if stack, err := nr.recoverableRun(); err != nil {
			lastErr = err
			nr.rt.SetError(err)
			nr.rt.Error("native ingester failed",
				log.KV("id", nr.id),
				log.KV("name", nr.name),
				log.KV("uuid", nr.ingesterUUID),
				log.KVErr(err),
				log.KV("stack", stack))
		}
	}

	return lastErr
}

// recoverableRun is just the underlying Ingster.Run wrapped in a defer recover so that if an ingester
// implementation fails we don't take down the entire hosted ingester stack.
func (nr *NativeRunner) recoverableRun() (stack string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = ErrIngesterPanic
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
	StatusTracker // SetError/SetWarn and friends for the hosted ingester
	Logger        *log.KVLogger
	igst          *ingest.IngestMuxer
	ctx           context.Context
	id            string
	entries       atomic.Uint64
	size          atomic.Uint64
	tagMtx        sync.RWMutex
	tags          map[entry.EntryTag]string // tags this ingester has used, names resolved lazily
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
		tags:         map[entry.EntryTag]string{},
	}
	return
}

// Alive returns true if the runtime is considered alive, this simply means that the upstream ingest muxer is not blocked
func (nr *NativeRuntime) Alive() bool {
	return !nr.igst.WillBlock() // if the ingest muxer is blocked, we are not alive, that means we keep trucking if cache is alive and well
}

// Sleep sleeps for the given duration or until the context is done, returning true if the context was done
func (nr *NativeRuntime) Sleep(d time.Duration) (r bool) {
	select {
	case <-time.After(d):
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
		err = fmt.Errorf("%w: runtime or ingester not initialized", ErrNotReady)
		return
	}
	if t, err = nr.igst.NegotiateTag(s); err == nil {
		nr.tagMtx.Lock()
		nr.tags[t] = s
		nr.tagMtx.Unlock()
	}
	return
}

func (nr *NativeRuntime) Write(ent entry.Entry) (err error) {
	if nr == nil || nr.igst == nil {
		err = fmt.Errorf("%w, ingest writer not available", ErrNotReady)
		return
	}
	// we cannot trust the ingesters to not modify the entry or re-use buffers, perform a deep copy on the entry
	localEnt := ent.DeepCopy()
	if err = nr.igst.WriteEntry(&localEnt); err == nil {
		nr.entries.Add(1)
		// account for the whole entry, header and enumerated values included, not just the data
		nr.size.Add(localEnt.Size())
		nr.trackTag(localEnt.Tag)
	}
	return
}

// State implements the StateProvider interface and hands back the ingest counters and any
// error or warning condition for this single hosted ingester.
func (nr *NativeRuntime) State() (s State) {
	if nr == nil {
		return
	}
	s.Entries = nr.entries.Load()
	s.Size = nr.size.Load()
	s.Tags = nr.tagNames()
	s.Error, s.Warn = nr.Status()
	return
}

// trackTag notes that an entry went out under the given tag.  We only record the ID here,
// names are resolved in tagNames because plugins are free to negotiate tags directly against
// the muxer at build time, in which case a name never passes through the runtime at all.
func (nr *NativeRuntime) trackTag(tag entry.EntryTag) {
	nr.tagMtx.RLock()
	_, ok := nr.tags[tag]
	nr.tagMtx.RUnlock()
	if ok {
		return
	}
	nr.tagMtx.Lock()
	if _, ok = nr.tags[tag]; !ok {
		nr.tags[tag] = ``
	}
	nr.tagMtx.Unlock()
}

// tagNames returns the sorted set of tag names this ingester has written to, resolving any
// tag that was negotiated outside of the runtime.  A tag the muxer cannot resolve is skipped
// and retried on the next call.
func (nr *NativeRuntime) tagNames() (tags []string) {
	nr.tagMtx.Lock()
	defer nr.tagMtx.Unlock()
	tags = make([]string, 0, len(nr.tags))
	for tag, name := range nr.tags {
		if name == `` {
			var ok bool
			if name, ok = nr.igst.LookupTag(tag); !ok {
				continue
			}
			nr.tags[tag] = name
		}
		tags = append(tags, name)
	}
	sort.Strings(tags)
	return
}

// The log methods need to be wrapped so we don't return errors to callers.
// This should eventually be handled and potentially surface through an Alive check failure.

func (nr *NativeRuntime) Debug(msg string, sds ...rfc5424.SDParam) {
	nr.Logger.DebugWithDepth(log.DEFAULT_DEPTH+1, msg, sds...)
}
func (nr *NativeRuntime) Info(msg string, sds ...rfc5424.SDParam) {
	nr.Logger.InfoWithDepth(log.DEFAULT_DEPTH+1, msg, sds...)
}
func (nr *NativeRuntime) Warn(msg string, sds ...rfc5424.SDParam) {
	nr.Logger.WarnWithDepth(log.DEFAULT_DEPTH+1, msg, sds...)
}
func (nr *NativeRuntime) Error(msg string, sds ...rfc5424.SDParam) {
	nr.Logger.ErrorWithDepth(log.DEFAULT_DEPTH+1, msg, sds...)
}
func (nr *NativeRuntime) Critical(msg string, sds ...rfc5424.SDParam) {
	nr.Logger.CriticalWithDepth(log.DEFAULT_DEPTH+1, msg, sds...)
}
