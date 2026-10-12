package slack

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"golang.org/x/time/rate"
)

type testRuntime struct {
	hosted.StatusTracker
	mu sync.Mutex

	ctx   context.Context
	store map[string][]byte
	tags  map[string]entry.EntryTag
	next  entry.EntryTag

	entries           []entry.Entry
	ackCalls          int
	ackFailAt         int
	ackErr            error
	failStateAfterAck bool
	putCalls          map[string]int
	putSizes          map[string][]int
	putTotal          int
	failPutFrom       int
	failPutKey        string
	failPutAt         int
	failPutErr        error
	warnings          []string
}

func newTestRuntime() *testRuntime {
	return &testRuntime{
		ctx:      context.Background(),
		store:    make(map[string][]byte),
		tags:     make(map[string]entry.EntryTag),
		putCalls: make(map[string]int),
		putSizes: make(map[string][]int),
	}
}

func (r *testRuntime) Alive() bool              { return r.ctx.Err() == nil }
func (r *testRuntime) Context() context.Context { return r.ctx }
func (r *testRuntime) Sleep(time.Duration) bool { return r.ctx.Err() != nil }

func (r *testRuntime) Get(key string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.store[key]
	if !ok {
		return nil, storage.ErrStorageNotFound
	}
	return append([]byte(nil), value...), nil
}

func (r *testRuntime) Put(key string, value []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.putCalls[key]++
	r.putSizes[key] = append(r.putSizes[key], len(value))
	r.putTotal++
	if r.failPutFrom > 0 && r.putTotal >= r.failPutFrom {
		return errors.New("synthetic persistent state failure")
	}
	if key == r.failPutKey && r.putCalls[key] == r.failPutAt {
		if r.failPutErr != nil {
			return r.failPutErr
		}
		return errors.New("synthetic state failure")
	}
	r.store[key] = append([]byte(nil), value...)
	return nil
}

func (r *testRuntime) GetString(key string) (string, error) {
	value, err := r.Get(key)
	return string(value), err
}

func (r *testRuntime) PutString(key, value string) error { return r.Put(key, []byte(value)) }

func (r *testRuntime) GetInt64(key string) (int64, error) {
	value, err := r.GetString(key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(value, 10, 64)
}

func (r *testRuntime) PutInt64(key string, value int64) error {
	return r.PutString(key, strconv.FormatInt(value, 10))
}

func (r *testRuntime) GetTime(key string) (time.Time, error) {
	value, err := r.GetString(key)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, value)
}

func (r *testRuntime) PutTime(key string, value time.Time) error {
	return r.PutString(key, value.Format(time.RFC3339Nano))
}

func (r *testRuntime) Write(ent entry.Entry) error {
	r.mu.Lock()
	r.entries = append(r.entries, ent.DeepCopy())
	r.mu.Unlock()
	return nil
}

func (r *testRuntime) WriteAcknowledged(ctx context.Context, ent entry.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ackCalls++
	if r.ackFailAt > 0 && r.ackCalls == r.ackFailAt {
		if r.ackErr != nil {
			return r.ackErr
		}
		return errors.New("synthetic acknowledgement failure")
	}
	if r.failStateAfterAck && r.failPutFrom == 0 {
		// Allow the receipt slot write, then fail its head update and every
		// later state write.
		r.failPutFrom = r.putTotal + 2
	}
	r.entries = append(r.entries, ent.DeepCopy())
	return nil
}

func (r *testRuntime) NegotiateTag(name string) (entry.EntryTag, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tag, ok := r.tags[name]; ok {
		return tag, nil
	}
	r.next++
	r.tags[name] = r.next
	return r.next, nil
}

func (*testRuntime) Debug(string, ...rfc5424.SDParam) {}
func (*testRuntime) Info(string, ...rfc5424.SDParam)  {}
func (r *testRuntime) Warn(message string, _ ...rfc5424.SDParam) {
	r.mu.Lock()
	r.warnings = append(r.warnings, message)
	r.mu.Unlock()
}
func (*testRuntime) Error(string, ...rfc5424.SDParam)    {}
func (*testRuntime) Critical(string, ...rfc5424.SDParam) {}

func validConfig(host string) *Config {
	return &Config{
		BaseConfig: hosted.BaseConfig{Ingester_UUID: "5cbb0f50-8de8-4f62-9daf-f1d0f7063030"},
		Host:       host,
		API_Token:  "synthetic-token",
	}
}

func newTestPlugin(t testingT, conf *Config, now time.Time, hc *http.Client) *Plugin {
	t.Helper()
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	plugin, err := New(conf)
	if err != nil {
		t.Fatal(err)
	}
	plugin.now = func() time.Time { return now }
	plugin.api.now = func() time.Time { return now }
	plugin.api.http = hc
	plugin.api.limiter.SetLimit(rate.Inf)
	return plugin
}

type testingT interface {
	Helper()
	Fatal(...any)
}
