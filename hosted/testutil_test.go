package hosted

import (
	"context"
	"sync"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// testRuntime is a full in-memory implementation of Runtime used across all
// hosted package tests. The sleepFunc field can be overridden to control
// sleep behaviour without real wall-clock delays. aliveFunc can be overridden
// to control Alive() independently of context cancellation.
type testRuntime struct {
	mu             sync.Mutex
	ctx            context.Context
	cancel         context.CancelFunc
	store          map[string][]byte
	syncCalls      int
	syncErr        error
	entries        []entry.Entry
	tags           map[string]entry.EntryTag
	nextTag        entry.EntryTag
	sleepFunc      func(time.Duration) bool // nil = real sleep
	aliveFunc      func() bool              // nil = use context
	sleepDurations []time.Duration          // all recorded sleep calls
	warnCalls      int
	infoCalls      int
}

func newTestRuntime(ctx context.Context, cancel context.CancelFunc) *testRuntime {
	return &testRuntime{
		ctx:    ctx,
		cancel: cancel,
		store:  make(map[string][]byte),
		tags:   make(map[string]entry.EntryTag),
	}
}

// Runtime
func (r *testRuntime) Alive() bool {
	if r.aliveFunc != nil {
		return r.aliveFunc()
	}
	return r.ctx.Err() == nil
}
func (r *testRuntime) Context() context.Context { return r.ctx }
func (r *testRuntime) Sleep(d time.Duration) bool {
	r.mu.Lock()
	r.sleepDurations = append(r.sleepDurations, d)
	r.mu.Unlock()
	if r.sleepFunc != nil {
		return r.sleepFunc(d)
	}
	select {
	case <-r.ctx.Done():
		return true
	case <-time.After(d):
		return false
	}
}

// Storage
func (r *testRuntime) Get(key string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.store[key]
	if !ok {
		return nil, storage.ErrStorageNotFound
	}
	return append([]byte(nil), v...), nil
}
func (r *testRuntime) Put(key string, value []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store[key] = append([]byte(nil), value...)
	return nil
}
func (r *testRuntime) GetString(key string) (string, error) {
	v, err := r.Get(key)
	return string(v), err
}
func (r *testRuntime) PutString(key, value string) error {
	return r.Put(key, []byte(value))
}
func (r *testRuntime) GetInt64(_ string) (int64, error) {
	return 0, storage.ErrStorageNotFound
}
func (r *testRuntime) PutInt64(_ string, _ int64) error { return nil }
func (r *testRuntime) GetTime(key string) (time.Time, error) {
	v, err := r.GetString(key)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, v)
}
func (r *testRuntime) PutTime(key string, value time.Time) error {
	return r.PutString(key, value.Format(time.RFC3339Nano))
}
func (r *testRuntime) Sync() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.syncCalls++
	return r.syncErr
}
func (r *testRuntime) syncCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.syncCalls
}
func (r *testRuntime) warnCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.warnCalls
}
func (r *testRuntime) infoCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.infoCalls
}
func (r *testRuntime) recordedSleeps() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.sleepDurations...)
}

// Logger
func (r *testRuntime) Debug(_ string, _ ...rfc5424.SDParam) {}
func (r *testRuntime) Info(_ string, _ ...rfc5424.SDParam) {
	r.mu.Lock()
	r.infoCalls++
	r.mu.Unlock()
}
func (r *testRuntime) Warn(_ string, _ ...rfc5424.SDParam) {
	r.mu.Lock()
	r.warnCalls++
	r.mu.Unlock()
}
func (r *testRuntime) Error(_ string, _ ...rfc5424.SDParam)    {}
func (r *testRuntime) Critical(_ string, _ ...rfc5424.SDParam) {}

// Writer
func (r *testRuntime) Write(e entry.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
	return nil
}
func (r *testRuntime) NegotiateTag(name string) (entry.EntryTag, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tags[name]; ok {
		return t, nil
	}
	r.nextTag++
	r.tags[name] = r.nextTag
	return r.nextTag, nil
}

// testJob is a scripted Job implementation. Each call to Handle returns the
// next result from results. If calls exceed results, Stop() is returned.
type testJob struct {
	mu      sync.Mutex
	calls   int
	results []handleResult
	onCall  func(n int, rt Runtime) // optional side effect, called before returning
}

type handleResult struct {
	cont *Continuation
	err  error
}

func (j *testJob) Handle(ctx context.Context, rt Runtime) (*Continuation, error) {
	j.mu.Lock()
	n := j.calls
	j.calls++
	j.mu.Unlock()

	if j.onCall != nil {
		j.onCall(n, rt)
	}

	if n < len(j.results) {
		return j.results[n].cont, j.results[n].err
	}
	return Stop(), nil
}

func (j *testJob) callCount() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.calls
}
