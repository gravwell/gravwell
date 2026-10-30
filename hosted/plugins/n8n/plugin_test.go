/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package n8n

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// mockRuntime mirrors the current accepted plugin-test pattern. The exact
// upstream tree does not export a hosted.NewMock helper.
type mockRuntime struct {
	hosted.StatusTracker
	mu           sync.Mutex
	ctx          context.Context
	entries      []entry.Entry
	store        map[string][]byte
	tags         map[string]entry.EntryTag
	nextTag      entry.EntryTag
	WriteErrs    int
	NegotiateErr error
}

func newMockRuntime(ctx context.Context) *mockRuntime {
	return &mockRuntime{ctx: ctx, store: make(map[string][]byte), tags: make(map[string]entry.EntryTag)}
}

type persistedProbeRuntime struct {
	hosted.Runtime
	bucket *storage.BucketWriter
}

func (r *persistedProbeRuntime) Get(key string) ([]byte, error)     { return r.bucket.Get(key) }
func (r *persistedProbeRuntime) Put(key string, value []byte) error { return r.bucket.Put(key, value) }

type dropAfterPutRuntime struct {
	hosted.Runtime
	putCount int
	dropAt   int
	dead     bool
}

func (r *dropAfterPutRuntime) Put(key string, value []byte) error {
	r.putCount++
	if r.dead || r.putCount >= r.dropAt {
		r.dead = true
		return nil
	}
	return r.Runtime.Put(key, value)
}

type failAfterFirstWaitLimiter struct {
	calls atomic.Int32
	err   error
}

func (l *failAfterFirstWaitLimiter) Wait(context.Context) error {
	if l.calls.Add(1) > 1 {
		return l.err
	}
	return nil
}

func TestPluginReusesRateLimiterAcrossFailedHandleCalls(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "synthetic failure", http.StatusInternalServerError)
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	limitErr := errors.New("shared limiter rejected immediate retry")
	limiter := &failAfterFirstWaitLimiter{err: limitErr}
	plugin.limiter = limiter
	rt := newMockRuntime(t.Context())

	if _, err := plugin.Handle(t.Context(), rt); err == nil || errors.Is(err, limitErr) {
		t.Fatalf("first failed poll error=%v, want the server failure", err)
	}
	if _, err := plugin.Handle(t.Context(), rt); !errors.Is(err, limitErr) {
		t.Fatalf("second failed poll error=%v, want the shared limiter failure", err)
	}
	if got := limiter.calls.Load(); got != 2 {
		t.Fatalf("limiter calls=%d want 2", got)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("server requests=%d want 1; the second Handle bypassed the shared limiter", got)
	}
}

func TestPluginRetainsRetryAfterAcrossFailedHandleCalls(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "120")
			http.Error(w, "synthetic throttle", http.StatusTooManyRequests)
			return
		}
		http.Error(w, "synthetic failure", http.StatusInternalServerError)
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	gate := newRequestLimiter(10000).(*requestGate)
	now := time.Now()
	gate.now = func() time.Time { return now }
	var waited []time.Duration
	gate.wait = func(_ context.Context, delay time.Duration) error {
		waited = append(waited, delay)
		now = now.Add(delay)
		return nil
	}
	plugin.limiter = gate
	rt := newMockRuntime(t.Context())

	if _, err := plugin.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("first failed poll error=%v want HTTP 429", err)
	}
	if _, err := plugin.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("second failed poll error=%v want HTTP 500", err)
	}
	if len(waited) != 1 || waited[0] < 119*time.Second || waited[0] > 121*time.Second {
		t.Fatalf("retry waits=%v want one approximately 120-second wait", waited)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("server requests=%d want 2", got)
	}
}

func TestCompletedManifestCommitsBeforeInflightStateIsCleared(t *testing.T) {
	var phase atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "next" {
			if phase.Load() == 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"two"}],"nextCursor":null}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"one"}],"nextCursor":"next"}`)
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "users")
	rt := newMockRuntime(t.Context())

	if _, err := New(conf).Handle(t.Context(), rt); err == nil {
		t.Fatal("initial partial poll unexpectedly succeeded")
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("partial poll wrote %d entries, want 1", got)
	}

	phase.Store(1)
	crash := &dropAfterPutRuntime{Runtime: rt, dropAt: 2}
	if _, err := New(conf).Handle(t.Context(), crash); err != nil {
		t.Fatal(err)
	}
	if !crash.dead {
		t.Fatal("synthetic abrupt termination point was not reached")
	}
	if _, err := New(conf).Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 2 {
		t.Fatalf("restart wrote %d total entries for 2 identities; completed manifest was not durable before in-flight cleanup", got)
	}
}

func TestInflightEvictionSuppressesPairsEncounteredLaterInPoll(t *testing.T) {
	var phase atomic.Int32
	var mu sync.Mutex
	second := []string(nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if phase.Load() == 0 {
			if r.URL.Query().Get("cursor") == "next" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"a"},{"id":"b"},{"id":"c"},{"id":"d"},{"id":"e"}],"nextCursor":"next"}`)
			return
		}
		mu.Lock()
		body := `{"data":[` + strings.Join(second, ",") + `],"nextCursor":null}`
		mu.Unlock()
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	plugin.maxPending = 5
	rt := newMockRuntime(t.Context())
	if _, err := plugin.Handle(t.Context(), rt); err == nil {
		t.Fatal("initial partial poll unexpectedly succeeded")
	}
	if got := len(rt.Entries()); got != 5 {
		t.Fatalf("partial poll wrote %d entries, want 5", got)
	}
	pendingBytes, err := rt.Get(conf.stateKey(datasetCatalog["users"]) + "/inflight")
	if err != nil {
		t.Fatal(err)
	}
	saved, ok := parseRecordState(pendingBytes[sha256.Size:])
	if !ok || saved.len() != 5 {
		t.Fatalf("saved in-flight state is invalid or has %d pairs", saved.len())
	}
	stateToID := make(map[string]string)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		record := normalizeTestRecord(t, `{"id":"`+id+`"}`, false)
		var state recordState
		copy(state[:], record.identity[:])
		copy(state[sha256.Size:], record.fingerprint[:])
		stateToID[string(state[:])] = id
	}
	mu.Lock()
	second = append(second, `{"id":"f"}`)
	for i := 0; i < saved.len()-1; i++ {
		id, found := stateToID[string(saved.at(i))]
		if !found {
			t.Fatalf("could not map saved in-flight pair %d", i)
		}
		second = append(second, `{"id":"`+id+`"}`)
	}
	mu.Unlock()
	phase.Store(1)
	before := len(rt.Entries())
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()) - before; got != 1 {
		t.Fatalf("poll with one new record enqueued %d entries, want 1", got)
	}
}

func TestNormalizedSnapshotRecordRetainsOnlyRequiredFields(t *testing.T) {
	typ := reflect.TypeOf(normalizedSnapshotRecord{})
	if typ.NumField() != 2 {
		t.Fatalf("normalized snapshot record retains %d fields, want only fingerprint and page", typ.NumField())
	}
	for _, name := range []string{"fingerprint", "page"} {
		if _, ok := typ.FieldByName(name); !ok {
			t.Fatalf("normalized snapshot record is missing %q", name)
		}
	}
	if typ.Size() > 40 {
		t.Fatalf("normalized snapshot record uses %d bytes, want no more than 40", typ.Size())
	}
}

func TestFailedPollReplayProtectionSurvivesBoltRestartAndDeletion(t *testing.T) {
	var phase atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if phase.Load() == 2 {
			_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
			return
		}
		if phase.Load() == 3 {
			_, _ = io.WriteString(w, `{"data":[{"id":"one"}],"nextCursor":null}`)
			return
		}
		if r.URL.Query().Get("cursor") == "next" {
			if phase.Load() == 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"two"}],"nextCursor":null}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"one"}],"nextCursor":"next"}`)
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "users")
	statePath := filepath.Join(t.TempDir(), "state.db")
	mock := newMockRuntime(t.Context())
	poll := func(wantError bool) {
		t.Helper()
		db, err := storage.OpenBoltHandler(statePath, true)
		if err != nil {
			t.Fatal(err)
		}
		bucket, err := db.GetBucketWriter("n8n-test")
		if err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		rt := &persistedProbeRuntime{Runtime: mock, bucket: bucket}
		_, handleErr := New(conf).Handle(t.Context(), rt)
		if (handleErr != nil) != wantError {
			t.Errorf("Handle error=%v wantedError=%v", handleErr, wantError)
		}
		if wantError {
			if _, err := rt.Get(conf.stateKey(datasetCatalog["users"])); !errors.Is(err, storage.ErrStorageNotFound) {
				t.Errorf("failed poll advanced completed manifest: %v", err)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for retry := 0; retry < 4; retry++ {
		poll(true)
		if got := len(mock.Entries()); got != 1 {
			t.Fatalf("retry %d enqueued %d entries, want one bounded prefix across restarts", retry, got)
		}
	}
	phase.Store(1)
	poll(false)
	poll(false)
	if got := len(mock.Entries()); got != 2 {
		t.Fatalf("successful completion/restart enqueued %d entries, want two identities", got)
	}
	phase.Store(2)
	poll(false)
	phase.Store(3)
	poll(false)
	if got := len(mock.Entries()); got != 3 {
		t.Fatalf("deletion/reappearance enqueued %d entries, want three", got)
	}
}

func TestStableChangedBoundaryVersionsDoNotReemit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "next" {
			_, _ = io.WriteString(w, `{"data":[{"id":"one","status":"success"}],"nextCursor":null}`)
		} else {
			_, _ = io.WriteString(w, `{"data":[{"id":"one","status":"running"}],"nextCursor":"next"}`)
		}
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "executions")
	rt := newMockRuntime(t.Context())
	for poll := 0; poll < 4; poll++ {
		if _, err := New(conf).Handle(t.Context(), rt); err != nil {
			t.Fatal(err)
		}
		if got := len(rt.Entries()); got != 2 {
			t.Fatalf("poll %d enqueued %d entries, want only the two original observed versions", poll, got)
		}
	}
}

func (m *mockRuntime) Alive() bool                         { return m.ctx.Err() == nil }
func (m *mockRuntime) Context() context.Context            { return m.ctx }
func (m *mockRuntime) Sleep(time.Duration) bool            { return m.ctx.Err() != nil }
func (m *mockRuntime) Debug(string, ...rfc5424.SDParam)    {}
func (m *mockRuntime) Info(string, ...rfc5424.SDParam)     {}
func (m *mockRuntime) Warn(string, ...rfc5424.SDParam)     {}
func (m *mockRuntime) Error(string, ...rfc5424.SDParam)    {}
func (m *mockRuntime) Critical(string, ...rfc5424.SDParam) {}

func (m *mockRuntime) Write(e entry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.WriteErrs > 0 {
		m.WriteErrs--
		return errors.New("write failed")
	}
	m.entries = append(m.entries, e)
	return nil
}

func (m *mockRuntime) NegotiateTag(name string) (entry.EntryTag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.NegotiateErr != nil {
		return 0, m.NegotiateErr
	}
	if tag, ok := m.tags[name]; ok {
		return tag, nil
	}
	m.nextTag++
	m.tags[name] = m.nextTag
	return m.nextTag, nil
}

func (m *mockRuntime) Get(key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.store[key]
	if !ok {
		return nil, storage.ErrStorageNotFound
	}
	return append([]byte(nil), value...), nil
}

func (m *mockRuntime) Put(key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = append([]byte(nil), value...)
	return nil
}

func (m *mockRuntime) GetString(key string) (string, error) {
	value, err := m.Get(key)
	return string(value), err
}
func (m *mockRuntime) PutString(key, value string) error { return m.Put(key, []byte(value)) }
func (m *mockRuntime) GetInt64(string) (int64, error)    { return 0, storage.ErrStorageNotFound }
func (m *mockRuntime) PutInt64(string, int64) error      { return nil }
func (m *mockRuntime) GetTime(string) (time.Time, error) {
	return time.Time{}, storage.ErrStorageNotFound
}
func (m *mockRuntime) PutTime(string, time.Time) error { return nil }

func (m *mockRuntime) Entries() []entry.Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]entry.Entry(nil), m.entries...)
}

func TestHandleIsolatesUnavailableDatasetAndScrubsCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "100" {
			t.Errorf("%s limit=%q want 100", r.URL.Path, got)
		}
		switch r.URL.Path {
		case "/api/v1/credentials":
			_, _ = w.Write([]byte(`{"data":[{"id":"cred-1","name":"metadata","type":"httpHeaderAuth","data":{"password":"DO_NOT_EMIT"},"apiKey":"DO_NOT_EMIT","key":"DO_NOT_EMIT","nested":{"access_token":"DO_NOT_EMIT","accessKeyId":"DO_NOT_EMIT"}}],"nextCursor":null}`))
		case "/api/v1/projects":
			w.WriteHeader(http.StatusMethodNotAllowed)
		case "/api/v1/users":
			w.WriteHeader(http.StatusForbidden)
		case "/api/v1/workflows":
			_, _ = w.Write([]byte(`{"data":[{"id":"wf-1","name":"<production>"}],"nextCursor":null}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "credentials", "projects", "users", "workflows")
	plugin := New(conf)
	runtime := newMockRuntime(t.Context())
	continuation, err := plugin.Handle(t.Context(), runtime)
	if err != nil {
		t.Fatal(err)
	}
	if continuation == nil {
		t.Fatal("missing continuation")
	}
	entries := runtime.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries=%d", len(entries))
	}
	for _, ent := range entries {
		if !json.Valid(ent.Data) || strings.Contains(string(ent.Data), "\n") {
			t.Fatalf("entry is not compact JSON: %q", ent.Data)
		}
		if len(ent.EnumeratedValues()) != 0 {
			t.Fatalf("provenance values=%d", len(ent.EnumeratedValues()))
		}
		// The ingest muxer stamps the real ingest source on an entry with no SRC,
		// so the plugin must not overwrite it with a placeholder address.
		if len(ent.SRC) != 0 {
			t.Fatalf("entry SRC was set by the plugin: %v", ent.SRC)
		}
	}
	joined := string(entries[0].Data) + string(entries[1].Data)
	if strings.Contains(joined, "DO_NOT_EMIT") || strings.Contains(joined, "apiKey") || strings.Contains(joined, `"key"`) || strings.Contains(joined, "access_token") || strings.Contains(joined, "accessKeyId") {
		t.Fatalf("credential secret escaped sanitization: %s", joined)
	}
	if !strings.Contains(joined, "<production>") || strings.Contains(joined, `\u003cproduction\u003e`) {
		t.Fatalf("HTML escaping changed n8n JSON: %s", joined)
	}
	warn, _ := runtime.Status()
	if !strings.Contains(warn, "users") || !strings.Contains(warn, "projects") {
		t.Fatalf("warning=%q", warn)
	}
	if _, err := runtime.Get(conf.stateKey(datasetCatalog["users"])); !errors.Is(err, storage.ErrStorageNotFound) {
		t.Fatalf("unavailable Dataset advanced state: %v", err)
	}
	credentialState, err := runtime.Get(conf.stateKey(datasetCatalog["credentials"]))
	if err != nil {
		t.Fatal(err)
	}
	// One credential record checkpoints as exactly one per-record state entry,
	// which is digest material only and never the record body.
	if len(credentialState) != recordStateEntrySize {
		t.Fatalf("credential snapshot state length=%d want %d", len(credentialState), recordStateEntrySize)
	}
	if _, ok := parseRecordState(credentialState); !ok {
		t.Fatalf("credential snapshot state does not decode: %x", credentialState)
	}
	if strings.Contains(string(credentialState), "DO_NOT_EMIT") || strings.Contains(string(credentialState), "metadata") {
		t.Fatalf("credential value reached state: %s", credentialState)
	}
}

// failPutRuntime fails every Put to one key and records Error log calls so a
// test can prove a hidden checkpoint failure took the logged hard-failure path.
type failPutRuntime struct {
	hosted.Runtime
	failKey   string
	failErr   error
	mu        sync.Mutex
	failures  int
	errorLogs []string
}

func (r *failPutRuntime) Put(key string, value []byte) error {
	if key == r.failKey {
		r.mu.Lock()
		r.failures++
		r.mu.Unlock()
		return r.failErr
	}
	return r.Runtime.Put(key, value)
}

func (r *failPutRuntime) Error(msg string, _ ...rfc5424.SDParam) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errorLogs = append(r.errorLogs, msg)
}

// laterPageUnavailableServer serves one users record on the first page and
// answers the second page with an unavailable status, while a sibling
// workflows dataset stays healthy.
func laterPageUnavailableServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users":
			if r.URL.Query().Get("cursor") == "next" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"one"}],"nextCursor":"next"}`)
		case "/api/v1/workflows":
			_, _ = io.WriteString(w, `{"data":[{"id":"wf-1"}],"nextCursor":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// Regression: an unavailable later page joined with a
// failed in-flight checkpoint must not be isolated as a warning-only success.
func TestLaterPageUnavailableJoinedWithInflightPutFailureIsHardFailure(t *testing.T) {
	server := laterPageUnavailableServer(t)
	conf := testConfig(t, server.URL+"/api/v1", "users", "workflows")
	mock := newMockRuntime(t.Context())
	usersKey := conf.stateKey(datasetCatalog["users"])
	putErr := errors.New("synthetic in-flight checkpoint failure")
	rt := &failPutRuntime{Runtime: mock, failKey: usersKey + "/inflight", failErr: putErr}

	continuation, err := New(conf).Handle(t.Context(), rt)
	if err == nil {
		t.Fatal("joined checkpoint failure was reported as warning-only success")
	}
	if continuation != nil {
		t.Fatal("hard failure returned a continuation")
	}
	if !errors.Is(err, putErr) {
		t.Fatalf("returned error does not carry the checkpoint failure: %v", err)
	}
	if !strings.Contains(err.Error(), "Dataset users") || !strings.Contains(err.Error(), putErr.Error()) {
		t.Fatalf("returned error does not name the dataset and checkpoint failure: %v", err)
	}
	var unavailableErr *datasetUnavailableError
	if !errors.As(err, &unavailableErr) {
		t.Fatalf("returned error lost the unavailable cause: %v", err)
	}
	rt.mu.Lock()
	failures, errorLogs := rt.failures, append([]string(nil), rt.errorLogs...)
	rt.mu.Unlock()
	if failures != 1 {
		t.Fatalf("in-flight checkpoint Put attempts=%d want 1", failures)
	}
	if !slices.Contains(errorLogs, "n8n Dataset collection failed") {
		t.Fatalf("hard failure was not logged at error level: %v", errorLogs)
	}
	if warn, _ := mock.Status(); strings.Contains(warn, "users") {
		t.Fatalf("hidden checkpoint failure was reported as an unavailable warning: %q", warn)
	}
	if got := len(mock.Entries()); got != 2 {
		t.Fatalf("entries=%d want the users prefix plus the workflows sibling", got)
	}
	if _, err := mock.Get(usersKey); !errors.Is(err, storage.ErrStorageNotFound) {
		t.Fatalf("failed Dataset advanced completed state: %v", err)
	}
	if _, err := mock.Get(usersKey + "/inflight"); !errors.Is(err, storage.ErrStorageNotFound) {
		t.Fatalf("failed checkpoint left in-flight state behind: %v", err)
	}
	if _, err := mock.Get(conf.stateKey(datasetCatalog["workflows"])); err != nil {
		t.Fatalf("healthy sibling did not advance state: %v", err)
	}
}

func TestLaterPageUnavailableWithHealthyStorageStaysIsolatedWarning(t *testing.T) {
	server := laterPageUnavailableServer(t)
	conf := testConfig(t, server.URL+"/api/v1", "users", "workflows")
	rt := newMockRuntime(t.Context())
	usersKey := conf.stateKey(datasetCatalog["users"])
	for poll := 0; poll < 2; poll++ {
		continuation, err := New(conf).Handle(t.Context(), rt)
		if err != nil {
			t.Fatalf("poll %d: ordinary unavailable endpoint became a hard failure: %v", poll, err)
		}
		if continuation == nil {
			t.Fatalf("poll %d: missing continuation", poll)
		}
		if warn, _ := rt.Status(); !strings.Contains(warn, "users") || strings.Contains(warn, "workflows") {
			t.Fatalf("poll %d: warning=%q", poll, warn)
		}
		if got := len(rt.Entries()); got != 2 {
			t.Fatalf("poll %d: entries=%d want the users prefix once plus the workflows sibling", poll, got)
		}
		if _, err := rt.Get(usersKey); !errors.Is(err, storage.ErrStorageNotFound) {
			t.Fatalf("poll %d: unavailable Dataset advanced completed state: %v", poll, err)
		}
		if _, err := rt.Get(usersKey + "/inflight"); err != nil {
			t.Fatalf("poll %d: unavailable Dataset did not checkpoint its written prefix: %v", poll, err)
		}
		if _, err := rt.Get(conf.stateKey(datasetCatalog["workflows"])); err != nil {
			t.Fatalf("poll %d: healthy sibling did not advance state: %v", poll, err)
		}
	}
}

func TestUnaccompaniedUnavailableRejectsJoinedCompanions(t *testing.T) {
	bare := &datasetUnavailableError{Dataset: "users", Status: http.StatusForbidden}
	other := errors.New("storage failure")
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"bare", bare, true},
		{"join with nil cleanup", errors.Join(bare, nil), true},
		{"nested single joins", errors.Join(errors.Join(bare, nil), nil), true},
		{"wrapped", fmt.Errorf("Dataset users: %w", bare), true},
		{"wrapped single join", fmt.Errorf("Dataset users: %w", errors.Join(bare, nil)), true},
		{"other", other, false},
		{"joined after", errors.Join(bare, other), false},
		{"joined before", errors.Join(other, bare), false},
		{"nested companion", errors.Join(errors.Join(bare, nil), other), false},
		{"wrapped joined", fmt.Errorf("Dataset users: %w", errors.Join(bare, other)), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := unaccompaniedUnavailable(tc.err)
			if ok != tc.want {
				t.Fatalf("isolated=%v want %v for %v", ok, tc.want, tc.err)
			}
			if ok && got != bare {
				t.Fatalf("returned %v, want the original unavailable error", got)
			}
		})
	}
}

func TestSplitCancellationPreservesCompanionFailures(t *testing.T) {
	checkpointErr := errors.New("checkpoint failed")
	cleanupErr := errors.New("cleanup failed")
	cases := []struct {
		name          string
		err           error
		wantCanceled  bool
		wantCompanion []error
	}{
		{name: "ordinary failure", err: checkpointErr, wantCompanion: []error{checkpointErr}},
		{name: "bare cancellation", err: context.Canceled, wantCanceled: true},
		{name: "wrapped cancellation", err: fmt.Errorf("request: %w", context.Canceled), wantCanceled: true},
		{name: "joined after", err: errors.Join(context.Canceled, checkpointErr), wantCanceled: true, wantCompanion: []error{checkpointErr}},
		{name: "joined before", err: errors.Join(cleanupErr, context.Canceled), wantCanceled: true, wantCompanion: []error{cleanupErr}},
		{name: "nested and wrapped", err: fmt.Errorf("collect: %w", errors.Join(context.Canceled, checkpointErr, cleanupErr)), wantCanceled: true, wantCompanion: []error{checkpointErr, cleanupErr}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			canceled, companion := splitCancellation(tc.err)
			if canceled != tc.wantCanceled {
				t.Fatalf("canceled=%v want %v", canceled, tc.wantCanceled)
			}
			if len(tc.wantCompanion) == 0 && companion != nil {
				t.Fatalf("companion=%v want nil", companion)
			}
			for _, want := range tc.wantCompanion {
				if !errors.Is(companion, want) {
					t.Fatalf("companion=%v does not preserve %v", companion, want)
				}
			}
			if errors.Is(companion, context.Canceled) {
				t.Fatalf("companion still matches context.Canceled: %v", companion)
			}
		})
	}
}

func TestCancellationJoinedWithInflightPutFailureIsHardFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "next" {
			cancel()
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"one"}],"nextCursor":"next"}`)
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "users")
	stateKey := conf.stateKey(datasetCatalog["users"])
	checkpointErr := errors.New("synthetic canceled-poll checkpoint failure")
	rt := &failPutRuntime{
		Runtime: newMockRuntime(ctx), failKey: stateKey + "/inflight", failErr: checkpointErr,
	}

	continuation, err := New(conf).Handle(ctx, rt)
	if err == nil {
		t.Fatal("cancellation hid the in-flight checkpoint failure")
	}
	if continuation != nil {
		t.Fatal("hard failure returned a continuation")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("hard failure still matches context.Canceled: %v", err)
	}
	if !errors.Is(err, checkpointErr) {
		t.Fatalf("hard failure lost checkpoint cause: %v", err)
	}
}

func TestCancellationDoesNotEraseEarlierDatasetFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users":
			http.Error(w, "synthetic users failure", http.StatusInternalServerError)
		case "/api/v1/workflows":
			cancel()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "users", "workflows")
	continuation, err := New(conf).Handle(ctx, newMockRuntime(ctx))
	if err == nil {
		t.Fatal("cancellation erased the earlier users failure")
	}
	if continuation != nil {
		t.Fatal("hard failure returned a continuation")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("earlier hard failure still matches context.Canceled: %v", err)
	}
	if !strings.Contains(err.Error(), "Dataset users") || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("earlier dataset failure was not preserved: %v", err)
	}
}

func TestOversizedRecordFailsWithoutWriteOrCheckpoint(t *testing.T) {
	const testLimit = 256
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"one","value":"`)
		_, _ = io.Copy(w, &repeatedByteReader{remaining: testLimit, value: 'x'})
		_, _ = io.WriteString(w, `"}],"nextCursor":null}`)
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	plugin.maxRecordSize = testLimit
	runtime := newMockRuntime(t.Context())
	if _, err := plugin.Handle(t.Context(), runtime); err == nil || !strings.Contains(err.Error(), "per-record safety limit of 256 bytes") {
		t.Fatalf("oversized record error=%v", err)
	}
	if got := len(runtime.Entries()); got != 0 {
		t.Fatalf("oversized record wrote %d entries", got)
	}
	stateKey := conf.stateKey(datasetCatalog["users"])
	if _, err := runtime.Get(stateKey); !errors.Is(err, storage.ErrStorageNotFound) {
		t.Fatalf("oversized record advanced completed state: %v", err)
	}
	if _, err := runtime.Get(stateKey + "/inflight"); !errors.Is(err, storage.ErrStorageNotFound) {
		t.Fatalf("oversized record advanced in-flight state: %v", err)
	}
}

func TestSnapshotMutationDeletionAndReappearance(t *testing.T) {
	var mu sync.Mutex
	body := `{"data":[{"id":"1","name":"one"}],"nextCursor":null}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "workflows")
	plugin := New(conf)
	runtime := newMockRuntime(t.Context())

	run := func() {
		t.Helper()
		if _, err := plugin.Handle(t.Context(), runtime); err != nil {
			t.Fatal(err)
		}
	}
	run()
	if got := len(runtime.Entries()); got != 1 {
		t.Fatalf("first entries=%d", got)
	}
	stateKey := conf.stateKey(datasetCatalog["workflows"])
	firstState, err := runtime.Get(stateKey)
	if err != nil {
		t.Fatal(err)
	}
	run()
	if got := len(runtime.Entries()); got != 1 {
		t.Fatalf("unchanged entries=%d", got)
	}
	secondState, _ := runtime.Get(stateKey)
	if string(firstState) != string(secondState) {
		t.Fatalf("unchanged manifest was republished: %s != %s", firstState, secondState)
	}
	mu.Lock()
	body = `{"data":[{"id":"1","name":"changed"}],"nextCursor":null}`
	mu.Unlock()
	run()
	if got := len(runtime.Entries()); got != 2 {
		t.Fatalf("mutation entries=%d", got)
	}
	mu.Lock()
	body = `{"data":[],"nextCursor":null}`
	mu.Unlock()
	run()
	if got := len(runtime.Entries()); got != 2 {
		t.Fatalf("deletion emitted an entry: %d", got)
	}
	mu.Lock()
	body = `{"data":[{"id":"1","name":"changed"}],"nextCursor":null}`
	mu.Unlock()
	run()
	if got := len(runtime.Entries()); got != 3 {
		t.Fatalf("reappearance entries=%d", got)
	}
}

func TestCredentialReloadsAtEveryPoll(t *testing.T) {
	var mu sync.Mutex
	var credentials []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		credentials = append(credentials, r.Header.Get("X-N8N-API-KEY"))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"data":[],"nextCursor":null}`))
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	runtime := newMockRuntime(t.Context())
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(filepath.Dir(conf.Credential_File), "replacement")
	if err := os.WriteFile(replacement, []byte("rotated-test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, conf.Credential_File); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"synthetic-test-key", "rotated-test-key"}
	if !slices.Equal(credentials, want) {
		t.Fatalf("credentials=%v want %v", credentials, want)
	}
}

func TestInlineCredentialIsUsedAtPoll(t *testing.T) {
	var credential string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credential = r.Header.Get("X-N8N-API-KEY")
		_, _ = w.Write([]byte(`{"data":[],"nextCursor":null}`))
	}))
	defer server.Close()
	conf := &Config{
		Base_URL: server.URL + "/api/v1", Credential: "inline-test-key", Dataset: []string{"users"},
		Allow_Plaintext: true, PollingConfig: hosted.PollingConfig{Requests_Per_Minute: 10000},
	}
	if err := conf.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if _, err := New(conf).Handle(t.Context(), newMockRuntime(t.Context())); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if credential != "inline-test-key" {
		t.Fatalf("inline credential was not sent: got length %d", len(credential))
	}
}

func TestPartialPaginationFailureMayReplayEnqueuedRecordsButDoesNotCheckpoint(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"partial"}],"nextCursor":"next"}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	runtime := newMockRuntime(t.Context())
	if _, err := plugin.Handle(t.Context(), runtime); err == nil {
		t.Fatal("partial failure unexpectedly succeeded")
	}
	if got := len(runtime.Entries()); got != 1 {
		t.Fatalf("partial entries=%d", got)
	}
	if _, err := runtime.Get(conf.stateKey(datasetCatalog["users"])); !errors.Is(err, storage.ErrStorageNotFound) {
		t.Fatalf("partial collection advanced state: %v", err)
	}
}

func TestHardDatasetFailureDoesNotBlockSibling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users":
			w.WriteHeader(http.StatusTooManyRequests)
		case "/api/v1/workflows":
			_, _ = w.Write([]byte(`{"data":[{"id":"workflow-1"}],"nextCursor":null}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "users", "workflows")
	plugin := New(conf)
	runtime := newMockRuntime(t.Context())
	if _, err := plugin.Handle(t.Context(), runtime); err == nil || !strings.Contains(err.Error(), "Dataset users") {
		t.Fatalf("Handle error=%v", err)
	}
	if got := len(runtime.Entries()); got != 1 {
		t.Fatalf("sibling entries=%d want 1", got)
	}
	if _, err := runtime.Get(conf.stateKey(datasetCatalog["users"])); !errors.Is(err, storage.ErrStorageNotFound) {
		t.Fatalf("failed Dataset advanced state: %v", err)
	}
	if _, err := runtime.Get(conf.stateKey(datasetCatalog["workflows"])); err != nil {
		t.Fatalf("successful sibling did not advance state: %v", err)
	}
}

func TestWriteFailureDoesNotAdvanceManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"1"}],"nextCursor":null}`))
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	runtime := newMockRuntime(t.Context())
	runtime.WriteErrs = 1
	if _, err := plugin.Handle(t.Context(), runtime); err == nil {
		t.Fatal("write failure unexpectedly succeeded")
	}
	if _, err := runtime.Get(conf.stateKey(datasetCatalog["users"])); !errors.Is(err, storage.ErrStorageNotFound) {
		t.Fatalf("write failure advanced state: %v", err)
	}
}

func TestAuditUsesPostAndEmptyObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/audit" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "{}" {
			t.Fatalf("body=%q", body)
		}
		_, _ = w.Write([]byte(`{"risk":"ok","sections":[]}`))
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "audit")
	plugin := New(conf)
	runtime := newMockRuntime(t.Context())
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if got := len(runtime.Entries()); got != 1 {
		t.Fatalf("entries=%d", got)
	}
}

func TestConflictingIdentityFailsWithoutCheckpointAfterEarlierEnqueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"1","name":"a"},{"id":"1","name":"b"}],"nextCursor":null}`))
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	runtime := newMockRuntime(t.Context())
	if _, err := plugin.Handle(t.Context(), runtime); err == nil || !strings.Contains(err.Error(), "conflicting records") {
		t.Fatalf("error=%v", err)
	}
	if got := len(runtime.Entries()); got != 1 {
		t.Fatalf("entries=%d", got)
	}
}

func TestIdenticalCursorBoundaryRecordIsEmittedOnce(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"boundary","name":"same"}],"nextCursor":"next"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"boundary","name":"same"}],"nextCursor":null}`))
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	runtime := newMockRuntime(t.Context())
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if got := len(runtime.Entries()); got != 1 {
		t.Fatalf("entries=%d", got)
	}
}

func TestChangedCursorBoundaryRecordUsesLaterPageVersion(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"7","status":"running"}],"nextCursor":"next"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"7","status":"success"},{"id":"8","status":"new"}],"nextCursor":null}`))
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "executions")
	plugin := New(conf)
	runtime := newMockRuntime(t.Context())
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatalf("changed boundary record failed collection: %v", err)
	}
	entries := runtime.Entries()
	if len(entries) != 3 {
		t.Fatalf("entries=%d want old and updated boundary versions plus new identity", len(entries))
	}
	if got := string(entries[0].Data); !strings.Contains(got, `"status":"running"`) {
		t.Fatalf("first boundary version missing: %s", got)
	}
	if got := string(entries[1].Data); !strings.Contains(got, `"status":"success"`) {
		t.Fatalf("updated boundary version missing: %s", got)
	}
	if !strings.Contains(string(entries[2].Data), `"id":"8"`) {
		t.Fatalf("new identity missing: %s", entries[2].Data)
	}
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatalf("unchanged repeat failed: %v", err)
	}
	if got := len(runtime.Entries()); got != 3 {
		t.Fatalf("unchanged follow-up emitted more entries: %d", got)
	}
}
