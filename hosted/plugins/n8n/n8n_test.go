/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package n8n

import (
	"bytes"
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
	"sort"
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

// Keep storage snapshots independent of caller buffers; common runtime
// behavior and failure injection come from the shared Hosted Runner mock.
type mockRuntime struct{ *hosted.Mock }

func newMockRuntime(ctx context.Context) *mockRuntime {
	return &mockRuntime{Mock: hosted.NewMock(ctx)}
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

func TestPluginRetainsRetryAfterFromServiceUnavailable(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "120")
			http.Error(w, "synthetic maintenance", http.StatusServiceUnavailable)
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

	if _, err := plugin.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("first failed poll error=%v want HTTP 503", err)
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

func TestSuccessfulPollContinuesAfterRequestInterval(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "users")
	conf.Request_Interval = 420
	continuation, err := New(conf).Handle(t.Context(), newMockRuntime(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	if continuation == nil || continuation.Delay != 7*time.Minute {
		t.Fatalf("continuation=%+v want a 7m0s delay", continuation)
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

func TestInflightStateNeverExceedsPairBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "next" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"a"},{"id":"b"},{"id":"c"}],"nextCursor":"next"}`)
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	plugin.maxPending = 2
	rt := newMockRuntime(t.Context())
	if _, err := plugin.Handle(t.Context(), rt); err == nil {
		t.Fatal("partial poll unexpectedly succeeded")
	}
	pendingBytes, err := rt.Get(conf.stateKey(datasetCatalog["users"]) + "/inflight")
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingBytes) < sha256.Size {
		t.Fatalf("saved in-flight state is %d bytes", len(pendingBytes))
	}
	saved, ok := parseRecordState(pendingBytes[sha256.Size:])
	if !ok || saved.len() > 2 {
		t.Fatalf("saved in-flight state valid=%t pairs=%d want at most 2", ok, saved.len())
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

func TestFailedPollReversionEmitsNewestValue(t *testing.T) {
	var mu sync.Mutex
	phase := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if phase == 1 && r.URL.Query().Get("cursor") == "tail" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		name := "A"
		next := "null"
		if phase == 1 {
			name = "B"
			next = `"tail"`
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"one","name":"`+name+`"}],"nextCursor":`+next+`}`)
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "users")
	rt := newMockRuntime(t.Context())
	p := New(conf)
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatalf("initial A poll: %v", err)
	}
	phase = 1
	if _, err := p.Handle(t.Context(), rt); err == nil {
		t.Fatal("B poll unexpectedly succeeded")
	}
	phase = 2
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatalf("reverted A poll: %v", err)
	}
	entries := rt.Entries()
	if len(entries) != 3 {
		t.Fatalf("entries=%d want A, B, reverted A; payloads=%v", len(entries), entries)
	}
	if !bytes.Contains(entries[2].Data, []byte(`"name":"A"`)) {
		t.Fatalf("newest payload=%s want reverted A", entries[2].Data)
	}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatalf("unchanged A poll: %v", err)
	}
	if got := len(rt.Entries()); got != 3 {
		t.Fatalf("unchanged A poll wrote %d total entries, want 3", got)
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

func (m *mockRuntime) Get(key string) ([]byte, error) {
	value, err := m.Mock.Get(key)
	return append([]byte(nil), value...), err
}

func (m *mockRuntime) Put(key string, value []byte) error {
	return m.Mock.Put(key, append([]byte(nil), value...))
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

// logCapturingRuntime keeps every Info, Warn and Error line so a test can
// inspect what the plugin logs.
type logCapturingRuntime struct {
	*hosted.Mock
	mu    sync.Mutex
	lines []string
}

func (r *logCapturingRuntime) record(msg string, params []rfc5424.SDParam) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, renderLogLine(msg, params))
}

func (r *logCapturingRuntime) Info(msg string, params ...rfc5424.SDParam)  { r.record(msg, params) }
func (r *logCapturingRuntime) Warn(msg string, params ...rfc5424.SDParam)  { r.record(msg, params) }
func (r *logCapturingRuntime) Error(msg string, params ...rfc5424.SDParam) { r.record(msg, params) }

func (r *logCapturingRuntime) logged() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.lines)
}

func TestCredentialOnlyInAPIKeyHeader(t *testing.T) {
	const marker = "synthetic-credential-marker-0123"
	for _, tc := range []struct {
		name    string
		status  int
		succeed bool
	}{
		{name: "success", status: http.StatusOK, succeed: true},
		{name: "server error", status: http.StatusInternalServerError},
		{name: "unauthorized", status: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var urls, keys []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				urls = append(urls, r.URL.String())
				keys = append(keys, r.Header.Get("X-N8N-API-KEY"))
				mu.Unlock()
				if tc.status != http.StatusOK {
					http.Error(w, "synthetic failure", tc.status)
					return
				}
				_, _ = io.WriteString(w, `{"data":[{"id":"u1"}],"nextCursor":null}`)
			}))
			defer server.Close()

			keyFile := filepath.Join(t.TempDir(), "n8n-api-key")
			if err := os.WriteFile(keyFile, []byte(marker+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			conf := testConfig(t, server.URL+"/api/v1", "users")
			conf.Credential_File = keyFile
			rt := &logCapturingRuntime{Mock: hosted.NewMock(t.Context())}
			_, err := New(conf).Handle(t.Context(), rt)
			if tc.succeed && err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if !tc.succeed && err == nil {
				t.Fatalf("Handle succeeded on HTTP %d", tc.status)
			}
			if strings.Contains(fmt.Sprintf("%+v", err), marker) {
				t.Fatalf("error exposes the credential: %v", err)
			}

			mu.Lock()
			defer mu.Unlock()
			if len(urls) == 0 {
				t.Fatal("no request reached the server")
			}
			for i, u := range urls {
				if strings.Contains(u, marker) {
					t.Fatalf("request URL exposes the credential: %s", u)
				}
				if keys[i] != marker {
					t.Fatalf("request %d X-N8N-API-KEY=%q want the configured credential", i, keys[i])
				}
			}
			lines := rt.logged()
			if len(lines) == 0 {
				t.Fatal("no log line was captured")
			}
			for _, line := range lines {
				if strings.Contains(line, marker) {
					t.Fatalf("log line exposes the credential: %s", line)
				}
			}
		})
	}
}

// Warning lines and the runner status name only the dataset, HTTP status,
// discard reason and byte count. The API key must not appear in any of them.
func TestCredentialAbsentFromWarningAndStatusPaths(t *testing.T) {
	const marker = "synthetic-credential-marker-0123"
	serve := func(t *testing.T, handler http.HandlerFunc) string {
		t.Helper()
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		return server.URL + "/api/v1"
	}
	forbidden := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "synthetic failure", http.StatusForbidden)
	}
	oneUser := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"u1"}],"nextCursor":null}`)
	}
	for _, tc := range []struct {
		name       string
		config     func(t *testing.T) *Config
		seed       func(conf *Config) map[string][]byte
		line       string
		wantStatus bool
	}{
		{
			name: "unavailable dataset",
			config: func(t *testing.T) *Config {
				return testConfig(t, serve(t, forbidden), "users")
			},
			line:       "n8n Dataset unavailable",
			wantStatus: true,
		},
		{
			name: "unreadable completed state",
			config: func(t *testing.T) *Config {
				return testConfig(t, serve(t, oneUser), "users")
			},
			seed: func(conf *Config) map[string][]byte {
				return map[string][]byte{conf.stateKey(datasetCatalog["users"]): []byte("x")}
			},
			line: "n8n discarding invalid snapshot state",
		},
		{
			name: "unreadable in-flight state",
			config: func(t *testing.T) *Config {
				return testConfig(t, serve(t, oneUser), "users")
			},
			seed: func(conf *Config) map[string][]byte {
				return map[string][]byte{conf.stateKey(datasetCatalog["users"]) + "/inflight": []byte("x")}
			},
			line: "n8n discarding stale or unreadable in-flight state",
		},
		{
			name: "invalid active-execution state",
			config: func(t *testing.T) *Config {
				return expandedExecutionConfig(t, newExecutionAPI(t), "executions")
			},
			seed: func(conf *Config) map[string][]byte {
				return map[string][]byte{executionActiveKey(conf.stateKey(datasetCatalog["executions"])): []byte("not-json")}
			},
			line: "n8n discarding invalid active-execution state",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keyFile := filepath.Join(t.TempDir(), "n8n-api-key")
			if err := os.WriteFile(keyFile, []byte(marker+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			conf := tc.config(t)
			conf.Credential_File = keyFile
			rt := &logCapturingRuntime{Mock: hosted.NewMock(t.Context())}
			if tc.seed != nil {
				for key, value := range tc.seed(conf) {
					if err := rt.Put(key, value); err != nil {
						t.Fatalf("seed %s: %v", key, err)
					}
				}
			}
			if _, err := New(conf).Handle(t.Context(), rt); err != nil {
				t.Fatalf("Handle: %v", err)
			}

			lines := rt.logged()
			if !hasLogLine(lines, tc.line) {
				t.Fatalf("no %q line in %q", tc.line, lines)
			}
			for _, line := range lines {
				if strings.Contains(line, marker) {
					t.Fatalf("log line exposes the credential: %s", line)
				}
			}
			warn, statusErr := rt.Status()
			if tc.wantStatus && warn == "" {
				t.Fatal("runner status carries no warning")
			}
			if strings.Contains(warn, marker) || strings.Contains(fmt.Sprint(statusErr), marker) {
				t.Fatalf("status exposes the credential: warn=%q err=%v", warn, statusErr)
			}
		})
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

func TestOversizedExpandedExecutionWritesMarkerAndDoesNotBlockProgress(t *testing.T) {
	const testLimit = 256
	start := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", start.Format(http.TimeFormat))
		if status := r.URL.Query().Get("status"); status != "" {
			_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"data":[{"id":"too-large","status":"success","startedAt":%q,"value":%q},{"id":"later","status":"success","startedAt":%q,"value":"ok"}],"nextCursor":null}`,
			start.Add(-30*time.Second).Format(time.RFC3339Nano), strings.Repeat("x", 2*testLimit), start.Add(-20*time.Second).Format(time.RFC3339Nano))
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "executions")
	conf.Include_Execution_Data = true
	conf.Request_Interval = 60
	plugin := New(conf)
	plugin.maxRecordSize = testLimit
	now := start
	plugin.now = func() time.Time { return now }
	rt := &logCapturingRuntime{Mock: hosted.NewMock(t.Context())}

	continuation, err := plugin.Handle(t.Context(), rt)
	if err != nil || continuation == nil {
		t.Fatalf("first poll continuation=%v error=%v", continuation, err)
	}
	entries := rt.Entries()
	if len(entries) != 2 {
		t.Fatalf("first poll entries=%d want marker plus later execution", len(entries))
	}
	var marker struct {
		ID       string `json:"id"`
		Gravwell struct {
			Reason string `json:"reason"`
			Limit  int64  `json:"limit"`
		} `json:"_gravwell"`
	}
	if err := json.Unmarshal(entries[0].Data, &marker); err != nil {
		t.Fatalf("decode marker: %v; data=%s", err, entries[0].Data)
	}
	if marker.ID != "too-large" || marker.Gravwell.Reason == "" || marker.Gravwell.Limit != testLimit {
		t.Fatalf("marker=%+v", marker)
	}
	if !strings.Contains(string(entries[1].Data), `"id":"later"`) {
		t.Fatalf("later execution was not delivered: %s", entries[1].Data)
	}
	if !hasLogLine(rt.logged(), "wrote a marker entry", "execution_id=too-large") {
		t.Fatalf("missing oversized execution warning: %v", rt.logged())
	}
	if got := rt.Entries(); len(got) != 2 {
		t.Fatalf("first poll entries changed while checking logs: %d", len(got))
	}
	highwater, err := rt.GetTime(executionHighwaterKey(conf.stateKey(datasetCatalog["executions"])))
	if err != nil || !highwater.Equal(start) {
		t.Fatalf("high-water=%s want %s error=%v", highwater, start, err)
	}

	now = start.Add(time.Minute)
	if continuation, err = plugin.Handle(t.Context(), rt); err != nil || continuation == nil {
		t.Fatalf("second poll continuation=%v error=%v", continuation, err)
	}
	if got := len(rt.Entries()); got != 2 {
		t.Fatalf("unchanged retry wrote %d total entries, want 2", got)
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

func TestIncludeExecutionDataConfigurationReachesRequest(t *testing.T) {
	for _, tc := range []struct {
		name        string
		includeData bool
		want        string
	}{
		{name: "default excluded", want: "false"},
		{name: "explicit included", includeData: true, want: "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query().Get("includeData")
				_, _ = w.Write([]byte(`{"data":[],"nextCursor":null}`))
			}))
			defer server.Close()
			conf := &Config{
				Base_URL: server.URL + "/api/v1", Credential: "inline-test-key", Dataset: []string{"executions"},
				Allow_Plaintext: true, Include_Execution_Data: tc.includeData,
				PollingConfig: hosted.PollingConfig{Requests_Per_Minute: 10000},
			}
			if err := conf.Verify(); err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if _, err := New(conf).Handle(t.Context(), newMockRuntime(t.Context())); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if got != tc.want {
				t.Fatalf("includeData=%q want %q", got, tc.want)
			}
		})
	}
}

func TestExpandedExecutionsUseIncrementalWindowAndRecoverCompletedWaitingExecutionAfterRestart(t *testing.T) {
	var phase atomic.Int32
	var firstStartedAfter, firstStartedBefore string
	var detailCalls atomic.Int32
	now := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", now.Format(http.TimeFormat))
		if r.URL.Path == "/api/v1/executions/old-running" {
			detailCalls.Add(1)
			_, _ = io.WriteString(w, `{"id":"old-running","status":"success","startedAt":"2026-01-01T00:00:00Z","data":{"resultData":{"runData":{}}}}`)
			return
		}
		if r.URL.Path != "/api/v1/executions" {
			http.NotFound(w, r)
			return
		}
		if status := r.URL.Query().Get("status"); status != "" {
			if status == "waiting" && phase.Load() == 0 {
				_, _ = io.WriteString(w, `{"data":[{"id":"old-running","status":"waiting","startedAt":"2026-01-01T00:00:00Z"}],"nextCursor":null}`)
			} else {
				_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
			}
			return
		}
		if r.URL.Query().Get("includeData") != "true" {
			t.Errorf("expanded query includeData=%q", r.URL.Query().Get("includeData"))
		}
		if phase.Load() == 0 {
			firstStartedAfter = r.URL.Query().Get("startedAfter")
			firstStartedBefore = r.URL.Query().Get("startedBefore")
		}
		_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "executions")
	conf.Include_Execution_Data = true
	conf.Request_Interval = 60
	plugin := New(conf)
	plugin.now = func() time.Time { return now }
	rt := newMockRuntime(t.Context())

	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	wantAfter := now.Add(-61 * time.Second).Format(time.RFC3339Nano)
	if firstStartedAfter != wantAfter || firstStartedBefore != now.Format(time.RFC3339Nano) {
		t.Fatalf("first window=(%q,%q) want=(%q,%q)", firstStartedAfter, firstStartedBefore, wantAfter, now.Format(time.RFC3339Nano))
	}
	if len(rt.Entries()) != 0 {
		t.Fatalf("running metadata emitted %d entries", len(rt.Entries()))
	}

	phase.Store(1)
	now = now.Add(2 * time.Hour)
	plugin = New(conf)
	plugin.now = func() time.Time { return now }
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("second Handle: %v", err)
	}
	if detailCalls.Load() != 1 {
		t.Fatalf("detail calls=%d want 1", detailCalls.Load())
	}
	if len(rt.Entries()) != 1 || !bytes.Contains(rt.Entries()[0].Data, []byte(`"resultData"`)) {
		t.Fatalf("completed running execution entries=%d", len(rt.Entries()))
	}
}

func TestExpandedExecutionSeenInMainWindowSkipsDetailFetch(t *testing.T) {
	var phase atomic.Int32
	var detailCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/executions/one" {
			detailCalls.Add(1)
			_, _ = io.WriteString(w, `{"id":"one","status":"success","data":{"detail":true}}`)
			return
		}
		if status := r.URL.Query().Get("status"); status != "" {
			if status == "running" && phase.Load() == 0 {
				_, _ = io.WriteString(w, `{"data":[{"id":"one","status":"running"}],"nextCursor":null}`)
			} else {
				_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
			}
			return
		}
		if phase.Load() == 1 {
			_, _ = io.WriteString(w, `{"data":[{"id":"one","status":"success","data":{"main":true}}],"nextCursor":null}`)
		} else {
			_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
		}
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "executions")
	conf.Include_Execution_Data = true
	plugin := New(conf)
	plugin.now = func() time.Time { return time.Now().UTC().Add(-time.Hour) }
	rt := newMockRuntime(t.Context())
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	phase.Store(1)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if detailCalls.Load() != 0 || len(rt.Entries()) != 1 {
		t.Fatalf("detail calls=%d entries=%d want 0,1", detailCalls.Load(), len(rt.Entries()))
	}
}

func TestExecutionIDsRoundTripAtMaximumEncodedSize(t *testing.T) {
	for _, limit := range []int{0, 1, 2, 7} {
		ids := make(map[string]struct{}, limit)
		for i := 0; i < limit; i++ {
			ids[fmt.Sprintf("%0128d", i)] = struct{}{}
		}
		encoded, err := encodeExecutionIDs(ids, limit)
		if err != nil {
			t.Fatalf("limit %d: encode maximum-length IDs: %v", limit, err)
		}
		want := 2
		if limit > 0 {
			want = limit*131 + 1
		}
		if len(encoded) != want {
			t.Fatalf("limit %d: encoded length=%d want %d", limit, len(encoded), want)
		}
		decoded, ok := decodeExecutionIDs(encoded, limit)
		if !ok || len(decoded) != limit {
			t.Fatalf("limit %d: decoder rejected encoder output: valid=%t IDs=%d", limit, ok, len(decoded))
		}
		for id := range ids {
			if _, exists := decoded[id]; !exists {
				t.Fatalf("limit %d: decoded state lost ID %q", limit, id)
			}
		}
	}
}

func TestExpandedExecutionOverflowDoesNotCommitManifest(t *testing.T) {
	oldHighwater := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", oldHighwater.Add(time.Hour).Format(http.TimeFormat))
		if r.URL.Path != "/api/v1/executions" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("status") != "" {
			_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
			return
		}
		if r.URL.Query().Get("cursor") == "" {
			_, _ = io.WriteString(w, `{"data":[{"id":"new-1","status":"running"},{"id":"new-2","status":"running"}],"nextCursor":"page2"}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"new-3","status":"running"},{"id":"completed","status":"success"}],"nextCursor":null}`)
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "executions")
	conf.Include_Execution_Data = true
	conf.Page_Size = 2
	conf.Max_Pages = 3
	rt := newMockRuntime(t.Context())
	stateKey := conf.stateKey(datasetCatalog["executions"])
	activeKey := executionActiveKey(stateKey)
	highwaterKey := executionHighwaterKey(stateKey)
	prior := map[string]struct{}{"old-1": {}, "old-2": {}, "old-3": {}}
	activeState, err := encodeExecutionIDs(prior, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(activeKey, activeState); err != nil {
		t.Fatal(err)
	}
	if err := rt.PutTime(highwaterKey, oldHighwater); err != nil {
		t.Fatal(err)
	}

	p := New(conf)
	p.maxTracked = 3
	p.now = func() time.Time { return oldHighwater.Add(time.Hour) }
	if _, err := p.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "exceeds the per-dataset limit of 3 tracked active or pending execution IDs") {
		t.Fatalf("Handle error=%v; want bounded active-state failure", err)
	}
	if _, err := rt.Get(stateKey); !errors.Is(err, storage.ErrStorageNotFound) {
		t.Fatalf("completed manifest was committed on failed poll: err=%v", err)
	}
	storedActive, err := rt.Get(activeKey)
	if err != nil || !bytes.Equal(storedActive, activeState) {
		t.Fatalf("active state changed on failed poll: err=%v", err)
	}
	storedHighwater, err := rt.GetTime(highwaterKey)
	if err != nil || !storedHighwater.Equal(oldHighwater) {
		t.Fatalf("high-water changed on failed poll: got=%v err=%v", storedHighwater, err)
	}
}

func TestExpandedExecutionMainWindowDoesNotEmitNonterminalPayloads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") != "" {
			_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":[`+
			`{"id":"new-id","status":"new","data":{"partial":true}},`+
			`{"id":"running-id","status":"running","data":{"partial":true}},`+
			`{"id":"unknown-id","status":"unknown","data":{"partial":true}},`+
			`{"id":"waiting-id","status":"waiting","data":{"partial":true}}`+
			`],"nextCursor":null}`)
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "executions")
	conf.Include_Execution_Data = true
	rt := newMockRuntime(t.Context())
	if _, err := New(conf).Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("nonterminal main-window entries=%d want 0", got)
	}
	raw, err := rt.Get(executionActiveKey(conf.stateKey(datasetCatalog["executions"])))
	if err != nil {
		t.Fatal(err)
	}
	tracked, ok := decodeExecutionIDs(raw, defaultMaxTrackedRecords)
	if !ok || len(tracked) != 4 {
		t.Fatalf("tracked active IDs=%v valid=%t", tracked, ok)
	}
}

func TestExpandedExecutionDetailRaceRetainsNonterminalID(t *testing.T) {
	var phase atomic.Int32
	var detailCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/executions/racing" {
			detailCalls.Add(1)
			_, _ = io.WriteString(w, `{"id":"racing","status":"waiting","data":{"partial":true}}`)
			return
		}
		if status := r.URL.Query().Get("status"); status != "" {
			if phase.Load() == 0 && status == "waiting" {
				_, _ = io.WriteString(w, `{"data":[{"id":"racing","status":"waiting"}],"nextCursor":null}`)
			} else {
				_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
			}
			return
		}
		_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "executions")
	conf.Include_Execution_Data = true
	rt := newMockRuntime(t.Context())
	if _, err := New(conf).Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	phase.Store(1)
	if _, err := New(conf).Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if detailCalls.Load() != 1 {
		t.Fatalf("detail calls=%d want 1", detailCalls.Load())
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("nonterminal detail entries=%d want 0", got)
	}
	raw, err := rt.Get(executionActiveKey(conf.stateKey(datasetCatalog["executions"])))
	if err != nil {
		t.Fatal(err)
	}
	tracked, ok := decodeExecutionIDs(raw, defaultMaxTrackedRecords)
	if !ok {
		t.Fatalf("active state is invalid: %q", raw)
	}
	if _, retained := tracked["racing"]; !retained {
		t.Fatalf("racing execution was dropped from active state: %v", tracked)
	}
}

func TestExpandedExecutionDetailBacklogIsBoundedAndSurvivesRestart(t *testing.T) {
	var phase atomic.Int32
	var detailCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/executions/") {
			detailCalls.Add(1)
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/executions/")
			_, _ = fmt.Fprintf(w, `{"id":"%s","status":"success","data":{"detail":true}}`, id)
			return
		}
		if status := r.URL.Query().Get("status"); status != "" {
			if status == "running" && phase.Load() == 0 {
				_, _ = io.WriteString(w, `{"data":[{"id":"a"},{"id":"b"},{"id":"c"}],"nextCursor":null}`)
			} else {
				_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
			}
			return
		}
		_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "executions")
	conf.Include_Execution_Data = true
	conf.Page_Size, conf.Max_Pages = 2, 1
	rt := newMockRuntime(t.Context())
	now := time.Now().UTC().Add(-time.Hour)
	for poll := 0; poll < 4; poll++ {
		plugin := New(conf)
		plugin.now = func() time.Time { return now }
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("poll %d: %v", poll+1, err)
		}
		if poll == 0 {
			phase.Store(1)
		} else if got := detailCalls.Load(); got != int32(poll) {
			t.Fatalf("poll %d detail calls=%d want %d", poll+1, got, poll)
		}
		now = now.Add(time.Minute)
	}
	if len(rt.Entries()) != 3 {
		t.Fatalf("entries=%d want 3", len(rt.Entries()))
	}
}

func TestInvalidCompletedExecutionStateKeepsRetryMarkerWhenProgressInvalidationFails(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "executions")
	conf.Include_Execution_Data = true
	definition := datasetCatalog["executions"]
	stateKey := conf.stateKey(definition)
	highwaterKey, activeKey := executionHighwaterKey(stateKey), executionActiveKey(stateKey)
	malformed := []byte{1, 2, 3}
	base := newMockRuntime(t.Context())
	if err := base.Put(stateKey, malformed); err != nil {
		t.Fatal(err)
	}
	if err := base.PutTime(highwaterKey, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	active, err := encodeExecutionIDs(map[string]struct{}{"tracked": {}}, defaultMaxTrackedRecords)
	if err != nil {
		t.Fatal(err)
	}
	if err := base.Put(activeKey, active); err != nil {
		t.Fatal(err)
	}
	highwater, err := base.Get(highwaterKey)
	if err != nil {
		t.Fatal(err)
	}
	rt := &failPutRuntime{Runtime: base, failKey: stateKey + "/inflight", failErr: errors.New("synthetic invalidation failure")}

	if _, err := New(conf).Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "invalidate Dataset executions in-flight state") {
		t.Fatalf("Handle error=%v", err)
	}
	got, err := base.Get(stateKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, malformed) {
		t.Fatalf("completed retry marker=%x want %x", got, malformed)
	}
	// The high-water mark and the tracked IDs do not depend on the manifest,
	// so rejecting the manifest must leave both exactly as they were stored.
	if got, err := base.Get(highwaterKey); err != nil || !bytes.Equal(got, highwater) {
		t.Fatalf("high-water state=%q err=%v want unchanged %q", got, err, highwater)
	}
	if got, err := base.Get(activeKey); err != nil || !bytes.Equal(got, active) {
		t.Fatalf("active-execution state=%q err=%v want unchanged %q", got, err, active)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("failed invalidation sent %d requests, want none", got)
	}
}

func TestExpandedExecutionInitialHighwaterSurvivesFailedCommitAndRestart(t *testing.T) {
	var phase atomic.Int32
	var secondStartedAfter string
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	gap := start.Add(30 * time.Second)
	sourceNow := start
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", sourceNow.Format(http.TimeFormat))
		if status := r.URL.Query().Get("status"); status != "" {
			_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
			return
		}
		if phase.Load() == 0 {
			_, _ = io.WriteString(w, `{"data":[{"id":"first","status":"success"}],"nextCursor":null}`)
			return
		}
		secondStartedAfter = r.URL.Query().Get("startedAfter")
		_, _ = io.WriteString(w, `{"data":[{"id":"first","status":"success"},{"id":"gap","status":"success"}],"nextCursor":null}`)
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "executions")
	conf.Include_Execution_Data = true
	conf.Request_Interval = 60
	base := newMockRuntime(t.Context())
	stateKey := conf.stateKey(datasetCatalog["executions"])
	first := New(conf)
	first.now = func() time.Time { return start }
	rt := &failPutRuntime{Runtime: base, failKey: executionActiveKey(stateKey), failErr: errors.New("synthetic active-state failure")}
	if _, err := first.Handle(t.Context(), rt); err == nil {
		t.Fatal("first poll unexpectedly committed")
	}
	if got := len(base.Entries()); got != 1 {
		t.Fatalf("first poll entries=%d want 1", got)
	}
	highwater, err := base.GetTime(executionHighwaterKey(stateKey))
	if err != nil {
		t.Fatal(err)
	}
	wantHighwater := start.Add(-time.Minute)
	if !highwater.Equal(wantHighwater) {
		t.Fatalf("highwater=%s want %s", highwater, wantHighwater)
	}

	phase.Store(1)
	sourceNow = start.Add(2 * time.Hour)
	restarted := New(conf)
	restarted.now = func() time.Time { return start.Add(2 * time.Hour) }
	if _, err := restarted.Handle(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	wantAfter := wantHighwater.Add(-time.Minute).Format(time.RFC3339Nano)
	if secondStartedAfter != wantAfter {
		t.Fatalf("restart startedAfter=%q want %q; gap at %s would be skipped", secondStartedAfter, wantAfter, gap)
	}
	if got := len(base.Entries()); got != 2 {
		t.Fatalf("restart entries=%d want first plus gap", got)
	}
}

func TestExpandedExecutionEstablishedEmptyPollRetainsLateVisibilityOverlap(t *testing.T) {
	var phase atomic.Int32
	var thirdStartedAfter string
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	lateStartedAt := start.Add(10 * time.Second)
	pollNow := start
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", pollNow.Format(http.TimeFormat))
		if status := r.URL.Query().Get("status"); status != "" {
			_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
			return
		}
		if phase.Load() < 2 {
			_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
			return
		}
		thirdStartedAfter = r.URL.Query().Get("startedAfter")
		after, err := time.Parse(time.RFC3339Nano, thirdStartedAfter)
		if err != nil {
			t.Errorf("parse startedAfter: %v", err)
			return
		}
		if after.Before(lateStartedAt) {
			_, _ = fmt.Fprintf(w, `{"data":[{"id":"late","status":"success","startedAt":%q}],"nextCursor":null}`, lateStartedAt.Format(time.RFC3339Nano))
			return
		}
		_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "executions")
	conf.Include_Execution_Data = true
	conf.Request_Interval = 60
	rt := newMockRuntime(t.Context())
	for poll := int32(0); poll < 3; poll++ {
		phase.Store(poll)
		plugin := New(conf)
		pollNow = start.Add(time.Duration(poll) * time.Minute)
		plugin.now = func() time.Time { return pollNow }
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("poll %d: %v", poll+1, err)
		}
	}
	if thirdStartedAfter != start.Format(time.RFC3339Nano) {
		t.Fatalf("third startedAfter=%q want %q", thirdStartedAfter, start.Format(time.RFC3339Nano))
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("late-visible entries=%d want 1", got)
	}
}

func TestExpandedExecutionAboveMetadataMemberLimitReachesNormalizer(t *testing.T) {
	const members = maxRecordMembers + 2
	var body strings.Builder
	body.Grow(members * 12)
	body.WriteString(`{"data":[{"id":"one"`)
	for i := 1; i < members; i++ {
		fmt.Fprintf(&body, `,"k%d":0`, i)
	}
	body.WriteString(`}],"nextCursor":null}`)
	payload := body.String()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") != "" {
			_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
			return
		}
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	conf := &Config{
		Base_URL: server.URL + "/api/v1", Credential: "inline-test-key", Dataset: []string{"executions"},
		Allow_Plaintext: true, Include_Execution_Data: true,
		PollingConfig: hosted.PollingConfig{Requests_Per_Minute: 10000},
	}
	if err := conf.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	runtime := newMockRuntime(t.Context())
	if _, err := New(conf).Handle(t.Context(), runtime); err != nil {
		t.Fatalf("Handle expanded execution above metadata member limit: %v", err)
	}
	if len(runtime.Entries()) != 1 {
		t.Fatalf("entries=%d want 1", len(runtime.Entries()))
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

func TestHandleRejectsDualCredentialsBeforeFileAccess(t *testing.T) {
	for _, file := range []string{testCredentialFile(t), filepath.Join(t.TempDir(), "missing")} {
		conf := testConfig(t, "https://example.invalid/api/v1", "users")
		conf.Credential = "synthetic-inline"
		conf.Credential_File = file
		_, err := New(conf).Handle(t.Context(), hosted.NewMock(t.Context()))
		if err == nil || !strings.Contains(err.Error(), "exactly one") || strings.Contains(err.Error(), file) || strings.Contains(err.Error(), conf.Credential) {
			t.Fatalf("credential selection error=%v", err)
		}
	}
}

type cancelDeliveryRuntime struct {
	*hosted.Mock
	cancel context.CancelFunc
	at     string
	hard   error
}

func (r *cancelDeliveryRuntime) NegotiateTag(name string) (entry.EntryTag, error) {
	if r.at == "tag" {
		r.cancel()
		if r.hard != nil {
			return 0, r.hard
		}
	}
	return r.Mock.NegotiateTag(name)
}
func (r *cancelDeliveryRuntime) Write(e entry.Entry) error {
	if r.at == "write" {
		r.cancel()
		if r.hard != nil {
			return r.hard
		}
	}
	return r.Mock.Write(e)
}

func TestHandleDeliveryCancellationPreservesHardFailures(t *testing.T) {
	old := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return syntheticResponse(`{"data":[{"id":"one"}],"nextCursor":null}`), nil
	})
	defer func() { http.DefaultTransport = old }()
	for _, at := range []string{"tag", "write"} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/hard=%t", at, fail), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				hard := errors.New("synthetic " + at + " failure")
				rt := &cancelDeliveryRuntime{Mock: hosted.NewMock(ctx), cancel: cancel, at: at}
				if fail {
					rt.hard = hard
				}
				conf := testConfig(t, "https://example.invalid/api/v1", "users")
				continuation, err := New(conf).Handle(ctx, rt)
				if continuation != nil {
					t.Fatal("cancellation returned a continuation")
				}
				if fail {
					if !errors.Is(err, hard) || errors.Is(err, context.Canceled) {
						t.Fatalf("hard error=%v", err)
					}
				} else if !errors.Is(err, context.Canceled) {
					t.Fatalf("clean cancellation=%v", err)
				}
				stateKey := conf.stateKey(datasetCatalog["users"])
				if _, err := rt.Get(stateKey); !errors.Is(err, storage.ErrStorageNotFound) {
					t.Fatalf("completed manifest error=%v", err)
				}
				wantWrites := 0
				if at == "write" && !fail {
					wantWrites = 1
				}
				if len(rt.Entries()) != wantWrites {
					t.Fatalf("writes=%d want %d", len(rt.Entries()), wantWrites)
				}
				progress, progressErr := rt.Get(stateKey + "/inflight")
				if wantWrites == 1 {
					if progressErr != nil || len(progress) != sha256.Size+recordStateEntrySize {
						t.Fatalf("accepted progress=%d error=%v", len(progress), progressErr)
					}
				} else if !errors.Is(progressErr, storage.ErrStorageNotFound) {
					t.Fatalf("unexpected progress error=%v", progressErr)
				}
			})
		}
	}
}

func TestBufferedCancellationNeverCompletesManifest(t *testing.T) {
	for _, at := range []string{"read", "close"} {
		t.Run(at, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
				resp := syntheticResponse("")
				body := `{"data":[{"id":"one"}],"nextCursor":null}`
				if at == "read" {
					resp.Body = io.NopCloser(&cancelBufferedReader{r: strings.NewReader(body), cancel: cancel})
				} else {
					resp.Body = cancelOnCloseBody{strings.NewReader(body), cancel}
				}
				return resp, nil
			})
			conf := testConfig(t, "https://example.invalid/api/v1", "users")
			rt := hosted.NewMock(ctx)
			if err := New(conf).collect(ctx, rt, c, datasetCatalog["users"]); !errors.Is(err, context.Canceled) {
				t.Fatalf("error=%v", err)
			}
			if _, err := rt.Get(conf.stateKey(datasetCatalog["users"])); !errors.Is(err, storage.ErrStorageNotFound) {
				t.Fatalf("completed manifest error=%v", err)
			}
			want := 0
			if at == "close" {
				want = 1
			}
			if len(rt.Entries()) != want {
				t.Fatalf("writes=%d want %d", len(rt.Entries()), want)
			}
		})
	}
}

func TestPostCollectionCancellationPreservesManifestAndReplayCache(t *testing.T) {
	conf := testConfig(t, "https://example.invalid/api/v1", "users")
	definition := datasetCatalog["users"]
	stateKey := conf.stateKey(definition)

	var prior recordState
	prior[0], prior[sha256.Size] = 1, 2
	manifest := encodeRecordState(map[recordState]struct{}{prior: {}})
	binding := sha256.Sum256(manifest)
	replay := make([]byte, sha256.Size+len(manifest))
	copy(replay, binding[:])
	copy(replay[sha256.Size:], manifest)

	rt := newMockRuntime(t.Context())
	if err := rt.Put(stateKey, manifest); err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(stateKey+"/inflight", replay); err != nil {
		t.Fatal(err)
	}

	// The client performs the first check. The next two belong to collect's
	// post-collection commit boundary; cancellation on the third check must stop
	// before the completed manifest or its bound replay cache changes.
	ctx := &cancelAfterErrChecks{Context: context.Background(), pass: 2}
	client := syntheticClient(t, func(*http.Request) (*http.Response, error) {
		return syntheticResponse(`{"data":[],"nextCursor":null}`), nil
	})
	if err := New(conf).collect(ctx, rt, client, definition); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-collection cancellation error=%v checks=%d", err, ctx.checks)
	}
	if got, err := rt.Get(stateKey); err != nil || !bytes.Equal(got, manifest) {
		t.Fatalf("completed manifest=%x error=%v", got, err)
	}
	if got, err := rt.Get(stateKey + "/inflight"); err != nil || !bytes.Equal(got, replay) {
		t.Fatalf("replay cache=%x error=%v", got, err)
	}
}

func TestRuntimeStorageSnapshotsDoNotAlias(t *testing.T) {
	rt := newMockRuntime(t.Context())
	input := []byte("original")
	if err := rt.Put("snapshot", input); err != nil {
		t.Fatal(err)
	}
	input[0] = 'X'
	first, err := rt.Get("snapshot")
	if err != nil {
		t.Fatal(err)
	}
	first[0] = 'Y'
	second, err := rt.Get("snapshot")
	if err != nil || string(second) != "original" {
		t.Fatalf("snapshot=%q error=%v", second, err)
	}
}

func TestDeliveryErrorsWithoutCancellation(t *testing.T) {
	old := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return syntheticResponse(`{"data":[{"id":"one"}],"nextCursor":null}`), nil
	})
	defer func() { http.DefaultTransport = old }()
	for _, at := range []string{"tag", "write"} {
		t.Run(at, func(t *testing.T) {
			hard := errors.New("synthetic " + at + " failure")
			rt := &cancelDeliveryRuntime{Mock: hosted.NewMock(t.Context()), cancel: func() {}, at: at, hard: hard}
			conf := testConfig(t, "https://example.invalid/api/v1", "users")
			_, err := New(conf).Handle(t.Context(), rt)
			if !errors.Is(err, hard) || errors.Is(err, context.Canceled) {
				t.Fatalf("error=%v", err)
			}
			if _, e := rt.Get(conf.stateKey(datasetCatalog["users"])); !errors.Is(e, storage.ErrStorageNotFound) {
				t.Fatalf("completed manifest error=%v", e)
			}
		})
	}
}

func TestStandardAdapterReportsConcurrentDeliveryFailure(t *testing.T) {
	old := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return syntheticResponse(`{"data":[{"id":"one"}],"nextCursor":null}`), nil
	})
	defer func() { http.DefaultTransport = old }()
	for _, at := range []string{"tag", "write"} {
		ctx, cancel := context.WithCancel(t.Context())
		hard := errors.New("synthetic " + at + " failure")
		rt := &cancelDeliveryRuntime{Mock: hosted.NewMock(ctx), cancel: cancel, at: at, hard: hard}
		conf := testConfig(t, "https://example.invalid/api/v1", "users")
		if err := hosted.WrapJob(New(conf)).Run(ctx, rt); err != nil {
			t.Fatal(err)
		}
		cancel()
		if _, err := rt.Status(); !errors.Is(err, hard) {
			t.Fatalf("%s: adapter lost hard cause: %v", at, err)
		}
	}
}

func TestDecodeExecutionIDsRejectsInvalidState(t *testing.T) {
	longest, other := strings.Repeat("a", 128), strings.Repeat("b", 128)
	atLimit, err := encodeExecutionIDs(map[string]struct{}{longest: {}, other: {}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		raw   []byte
		limit int
		want  int // decoded IDs when accepted, -1 when rejected
	}{
		{name: "nil", raw: nil, limit: 2, want: 0},
		{name: "empty", raw: []byte{}, limit: 2, want: 0},
		{name: "empty with a negative limit", raw: nil, limit: -1, want: 0},
		{name: "encoder output at the limit", raw: atLimit, limit: 2, want: 2},
		// Trailing whitespace keeps the value valid JSON, so only the size
		// bound can reject it.
		{name: "one byte over the size bound", raw: append(bytes.Clone(atLimit), ' '), limit: 2, want: -1},
		{name: "invalid JSON", raw: []byte(`["a"`), limit: 2, want: -1},
		{name: "not an array of strings", raw: []byte(`[1]`), limit: 2, want: -1},
		{name: "more IDs than the limit", raw: []byte(`["a","b"]`), limit: 1, want: -1},
		{name: "invalid character", raw: []byte(`["a/b"]`), limit: 2, want: -1},
		{name: "empty ID", raw: []byte(`[""]`), limit: 2, want: -1},
		{name: "129-byte ID", raw: []byte(`["` + longest + `a"]`), limit: 2, want: -1},
		{name: "duplicate ID", raw: []byte(`["a","a"]`), limit: 2, want: -1},
		{name: "negative limit", raw: []byte(`[]`), limit: -1, want: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids, ok := decodeExecutionIDs(tc.raw, tc.limit)
			if tc.want < 0 {
				if ok || ids != nil {
					t.Fatalf("invalid state was accepted: %v", ids)
				}
				return
			}
			if !ok || ids == nil || len(ids) != tc.want {
				t.Fatalf("valid=%t IDs=%v want %d", ok, ids, tc.want)
			}
		})
	}
}

func TestExecutionIDLengthBoundary(t *testing.T) {
	longest := strings.Repeat("a", 128)
	if !validExecutionID(longest) {
		t.Fatal("128-byte execution ID was rejected")
	}
	encoded, err := encodeExecutionIDs(map[string]struct{}{longest: {}}, 1)
	if err != nil {
		t.Fatalf("encode 128-byte ID: %v", err)
	}
	decoded, ok := decodeExecutionIDs(encoded, 1)
	if _, kept := decoded[longest]; !ok || !kept || len(decoded) != 1 {
		t.Fatalf("128-byte ID did not round-trip at limit 1: valid=%t IDs=%d", ok, len(decoded))
	}

	tooLong := longest + "a"
	if validExecutionID(tooLong) {
		t.Fatal("129-byte execution ID was accepted")
	}
	if _, err := encodeExecutionIDs(map[string]struct{}{tooLong: {}}, 1); err == nil {
		t.Fatal("129-byte execution ID was encoded")
	}
	// Limit 1 rejects the value by its encoded size. Limit 2 leaves room for
	// the bytes, so the ID itself must be rejected.
	for _, limit := range []int{1, 2} {
		if ids, ok := decodeExecutionIDs([]byte(`["`+tooLong+`"]`), limit); ok {
			t.Fatalf("limit %d: 129-byte ID was decoded: %d IDs", limit, len(ids))
		}
	}
}

func TestDatasetCatalogContract(t *testing.T) {
	want := []datasetDefinition{
		{Name: "audit", TagKind: "audit", Method: "POST", Path: "/audit"},
		{Name: "credentials", TagKind: "creds", Method: "GET", Path: "/credentials", RecordsField: "data", CursorField: "nextCursor"},
		{Name: "executions", TagKind: "executions", Method: "GET", Path: "/executions", RecordsField: "data", CursorField: "nextCursor"},
		{Name: "projects", TagKind: "projects", Method: "GET", Path: "/projects", RecordsField: "data", CursorField: "nextCursor"},
		{Name: "tags", TagKind: "tags", Method: "GET", Path: "/tags", RecordsField: "data", CursorField: "nextCursor"},
		{Name: "users", TagKind: "users", Method: "GET", Path: "/users", RecordsField: "data", CursorField: "nextCursor"},
		{Name: "variables", TagKind: "variables", Method: "GET", Path: "/variables", RecordsField: "data", CursorField: "nextCursor"},
		{Name: "workflows", TagKind: "workflows", Method: "GET", Path: "/workflows", RecordsField: "data", CursorField: "nextCursor"},
	}
	if len(datasetCatalog) != 8 || len(want) != 8 {
		t.Fatalf("catalog has %d datasets and the table has %d, want 8", len(datasetCatalog), len(want))
	}
	for key, definition := range datasetCatalog {
		if key != definition.Name {
			t.Errorf("catalog key %q holds the definition named %q", key, definition.Name)
		}
	}
	tagKinds := make(map[string]string, len(want))
	endpoints := make(map[string]string, len(want))
	for _, expected := range want {
		t.Run(expected.Name, func(t *testing.T) {
			got, ok := datasetCatalog[expected.Name]
			if !ok {
				t.Fatal("dataset is missing from the catalog")
			}
			if got != expected {
				t.Fatalf("definition=%+v want %+v", got, expected)
			}
			if other, taken := tagKinds[got.TagKind]; taken {
				t.Fatalf("tag kind %q is shared with %s", got.TagKind, other)
			}
			tagKinds[got.TagKind] = got.Name
			endpoint := got.Method + " " + got.Path
			if other, taken := endpoints[endpoint]; taken {
				t.Fatalf("endpoint %q is shared with %s", endpoint, other)
			}
			endpoints[endpoint] = got.Name
		})
	}
}

// boltRuntime serves every state operation from a Bolt bucket, the store the
// runner uses, and records Warn and Error log lines. hosted.Mock answers
// GetTime for an empty value with a parse error where Bolt reports not-found,
// so tests of stored execution state read and write Bolt.
type boltRuntime struct {
	*hosted.Mock
	bucket *storage.BucketWriter
	logMu  sync.Mutex
	infos  []string
	warns  []string
	errs   []string
}

func (r *boltRuntime) Get(key string) ([]byte, error) { return r.bucket.Get(key) }

func (r *boltRuntime) Put(key string, value []byte) error { return r.bucket.Put(key, value) }

func (r *boltRuntime) GetString(key string) (string, error) { return r.bucket.GetString(key) }

func (r *boltRuntime) PutString(key, value string) error { return r.bucket.PutString(key, value) }

func (r *boltRuntime) GetInt64(key string) (int64, error) { return r.bucket.GetInt64(key) }

func (r *boltRuntime) PutInt64(key string, value int64) error { return r.bucket.PutInt64(key, value) }

func (r *boltRuntime) GetTime(key string) (time.Time, error) { return r.bucket.GetTime(key) }

func (r *boltRuntime) PutTime(key string, value time.Time) error { return r.bucket.PutTime(key, value) }

func renderLogLine(msg string, params []rfc5424.SDParam) string {
	var line strings.Builder
	line.WriteString(msg)
	for _, param := range params {
		fmt.Fprintf(&line, " %s=%s", param.Name, param.Value)
	}
	return line.String()
}

func (r *boltRuntime) Warn(msg string, params ...rfc5424.SDParam) {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	r.warns = append(r.warns, renderLogLine(msg, params))
}

func (r *boltRuntime) Info(msg string, params ...rfc5424.SDParam) {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	r.infos = append(r.infos, renderLogLine(msg, params))
}

func (r *boltRuntime) Error(msg string, params ...rfc5424.SDParam) {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	r.errs = append(r.errs, renderLogLine(msg, params))
}

// boltPolls runs polls against one Bolt state file. Every poll opens the file,
// runs Handle on a new Plugin, and closes the file again, so each poll starts
// from what the previous one made durable. Written entries and the status
// warning accumulate in output.
type boltPolls struct {
	t      *testing.T
	path   string
	output *hosted.Mock
	// wrap, when set, stands between Handle and the Bolt-backed runtime so a
	// test can fail one storage call.
	wrap func(hosted.Runtime) hosted.Runtime
}

type pollResult struct {
	continuation *hosted.Continuation
	err          error
	infos        []string
	warns, errs  []string
}

func newBoltPolls(t *testing.T) *boltPolls {
	t.Helper()
	return &boltPolls{t: t, path: filepath.Join(t.TempDir(), "state.db"), output: hosted.NewMock(t.Context())}
}

func (b *boltPolls) poll(conf *Config, now time.Time, adjust ...func(*Plugin)) (result pollResult) {
	b.t.Helper()
	if rawAPI, ok := executionAPIs.Load(conf.Base_URL); ok {
		rawAPI.(*executionAPI).syncServerTime(now)
	}
	withProgressBucket(b.t, b.path, func(bucket *storage.BucketWriter) {
		rt := &boltRuntime{Mock: b.output, bucket: bucket}
		plugin := New(conf)
		plugin.now = func() time.Time { return now }
		for _, apply := range adjust {
			apply(plugin)
		}
		var target hosted.Runtime = rt
		if b.wrap != nil {
			target = b.wrap(rt)
		}
		result.continuation, result.err = plugin.Handle(b.t.Context(), target)
		result.infos, result.warns, result.errs = rt.infos, rt.warns, rt.errs
	})
	return result
}

// listServer serves a mutable single-page list response for a dataset.
type listServer struct {
	mu      sync.Mutex
	records []string
	hits    int
}

// stored returns the raw bytes under key after reopening the state file, or
// nil when the key holds nothing.
func (b *boltPolls) stored(key string) []byte {
	b.t.Helper()
	return reopenedProgress(b.t, b.path, key)
}

func (b *boltPolls) store(key string, value []byte) {
	b.t.Helper()
	withProgressBucket(b.t, b.path, func(bucket *storage.BucketWriter) {
		if err := bucket.Put(key, value); err != nil {
			b.t.Fatal(err)
		}
	})
}

func (b *boltPolls) highwater(conf *Config) (value time.Time) {
	b.t.Helper()
	withProgressBucket(b.t, b.path, func(bucket *storage.BucketWriter) {
		var err error
		value, err = bucket.GetTime(executionHighwaterKey(conf.stateKey(datasetCatalog["executions"])))
		if err != nil {
			b.t.Fatalf("read high-water state: %v", err)
		}
	})
	return value
}

func (b *boltPolls) activeIDs(conf *Config) []string {
	b.t.Helper()
	raw := b.stored(executionActiveKey(conf.stateKey(datasetCatalog["executions"])))
	ids, ok := decodeExecutionIDs(raw, defaultMaxTrackedRecords)
	if !ok {
		b.t.Fatalf("stored active-execution state does not decode: %q", raw)
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	return sorted
}

func (l *listServer) set(records ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = records
}

// delivered counts the written entries whose record has the given id.
func (b *boltPolls) delivered(id string) int {
	b.t.Helper()
	count := 0
	for _, ent := range b.output.Entries() {
		var record struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(ent.Data, &record); err != nil {
			b.t.Fatalf("entry is not a JSON object: %q", ent.Data)
		}
		if record.ID == id {
			count++
		}
	}
	return count
}

func (l *listServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		body := `{"data":[` + strings.Join(l.records, ",") + `],"nextCursor":null}`
		l.hits++
		l.mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func record(id, name string) string {
	return fmt.Sprintf(`{"id":%q,"name":%q}`, id, name)
}

func hasLogLine(lines []string, parts ...string) bool {
	return slices.ContainsFunc(lines, func(line string) bool {
		for _, part := range parts {
			if !strings.Contains(line, part) {
				return false
			}
		}
		return true
	})
}

// pollOnce runs one Handle and reports how many entries that poll wrote.
func pollOnce(t *testing.T, plugin *Plugin, rt *mockRuntime) int {
	t.Helper()
	before := len(rt.Entries())
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	return len(rt.Entries()) - before
}

type servedExecution struct {
	status    string
	startedAt time.Time
}

// executionAPI stands in for the n8n endpoints these tests need. The execution
// list applies the startedAfter and startedBefore filters, a status probe
// returns the executions in that status, one execution is served by ID, and
// the users list returns a single record.
type executionAPI struct {
	url string

	mu                sync.Mutex
	executions        map[string]servedExecution
	listStatus        int            // nonzero: the filtered list answers with this HTTP status
	detailStatus      map[string]int // nonzero: the single-execution request answers with this HTTP status
	windows           []string       // "startedAfter..startedBefore" of every filtered list request
	detailCalls       map[string]int
	executionRequests int
	userRequests      int
	serverTime        time.Time
	serverTimeFixed   bool
	// finishAtList holds IDs set to success just before the next filtered
	// list request is answered, after that poll's status probes.
	finishAtList []string
}

var executionAPIs sync.Map

// A dataset that only ever gains records must only ever emit the records it
// gained. Keying durable state on a single aggregate snapshot digest made any
// change re-emit every record in the snapshot, so an append-only dataset such as
// executions republished its entire retained history on every poll.
func TestGrowingDatasetEmitsOnlyNewRecords(t *testing.T) {
	// snapshot holds records 1..n, so each poll appends exactly one record and
	// leaves every earlier record byte-identical.
	snapshot := func(n int) []string {
		records := make([]string, 0, n)
		for i := 1; i <= n; i++ {
			records = append(records, record(fmt.Sprint(i), "name"))
		}
		return records
	}
	list := &listServer{}
	list.set(snapshot(1)...)
	server := list.start(t)

	conf := testConfig(t, server.URL+"/api/v1", "executions")
	plugin := New(conf)
	rt := newMockRuntime(t.Context())

	for poll := 1; poll <= 5; poll++ {
		if written := pollOnce(t, plugin, rt); written != 1 {
			t.Fatalf("poll %d over a snapshot of %d records wrote %d entries, want 1", poll, poll, written)
		}
		list.set(snapshot(poll + 1)...)
	}
	if got := len(rt.Entries()); got != 5 {
		t.Fatalf("total entries=%d want 5; the snapshot is being re-emitted", got)
	}
}

func newExecutionAPI(t *testing.T) *executionAPI {
	t.Helper()
	api := &executionAPI{
		executions:   make(map[string]servedExecution),
		detailStatus: make(map[string]int),
		detailCalls:  make(map[string]int),
	}
	server := httptest.NewServer(api)
	api.url = server.URL + "/api/v1"
	executionAPIs.Store(api.url, api)
	t.Cleanup(func() {
		executionAPIs.Delete(api.url)
		server.Close()
	})
	return api
}

func (a *executionAPI) set(id, status string, startedAt time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.executions[id] = servedExecution{status: status, startedAt: startedAt}
}

func (a *executionAPI) setServerTime(value time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.serverTime = value.UTC()
	a.serverTimeFixed = true
}

func (a *executionAPI) syncServerTime(value time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.serverTimeFixed {
		a.serverTime = value.UTC()
	}
}

// Only the mutated record may be emitted, not the whole snapshot alongside it.
func TestMutationEmitsOnlyTheChangedRecord(t *testing.T) {
	list := &listServer{}
	list.set(record("1", "one"), record("2", "two"), record("3", "three"))
	server := list.start(t)

	conf := testConfig(t, server.URL+"/api/v1", "workflows")
	plugin := New(conf)
	rt := newMockRuntime(t.Context())

	if written := pollOnce(t, plugin, rt); written != 3 {
		t.Fatalf("first poll wrote %d entries, want 3", written)
	}
	list.set(record("1", "one"), record("2", "CHANGED"), record("3", "three"))
	if written := pollOnce(t, plugin, rt); written != 1 {
		t.Fatalf("mutating one of three records wrote %d entries, want 1", written)
	}
	last := rt.Entries()[len(rt.Entries())-1]
	if !strings.Contains(string(last.Data), "CHANGED") {
		t.Fatalf("the emitted entry was not the mutated record: %s", last.Data)
	}
}

func (a *executionAPI) finish(ids ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range ids {
		execution := a.executions[id]
		execution.status = "success"
		a.executions[id] = execution
	}
}

func (a *executionAPI) remove(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.executions, id)
}

func (a *executionAPI) answerList(status int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listStatus = status
}

// A deletion changes the snapshot, but the records that survived it are already
// durable and must not be republished.
func TestDeletionDoesNotReEmitSurvivors(t *testing.T) {
	list := &listServer{}
	list.set(record("1", "one"), record("2", "two"), record("3", "three"))
	server := list.start(t)

	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	rt := newMockRuntime(t.Context())
	stateKey := conf.stateKey(datasetCatalog["users"])

	if written := pollOnce(t, plugin, rt); written != 3 {
		t.Fatalf("first poll wrote %d entries, want 3", written)
	}
	list.set(record("1", "one"), record("3", "three"))
	if written := pollOnce(t, plugin, rt); written != 0 {
		t.Fatalf("deleting one of three records wrote %d entries, want 0", written)
	}
	state, err := rt.Get(stateKey)
	if err != nil {
		t.Fatalf("deletion did not advance state: %v", err)
	}
	if len(state) != 2*recordStateEntrySize {
		t.Fatalf("state holds %d bytes, want %d for two surviving records", len(state), 2*recordStateEntrySize)
	}

	// Reappearance is still a new record for state, so it is emitted again.
	list.set(record("1", "one"), record("2", "two"), record("3", "three"))
	if written := pollOnce(t, plugin, rt); written != 1 {
		t.Fatalf("reappearance wrote %d entries, want 1", written)
	}
}

func (a *executionAPI) answerDetail(id string, status int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.detailStatus[id] = status
}

func (a *executionAPI) lastWindow() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.windows) == 0 {
		return ""
	}
	return a.windows[len(a.windows)-1]
}

func (a *executionAPI) detailRequests(id string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.detailCalls[id]
}

func (a *executionAPI) requestCounts() (executions, users int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.executionRequests, a.userRequests
}

func executionWindow(after, before time.Time) string {
	return after.UTC().Format(time.RFC3339Nano) + ".." + before.UTC().Format(time.RFC3339Nano)
}

// Durable state, not in-process memory, decides what has already been emitted.
func TestSnapshotStateSurvivesPluginRestart(t *testing.T) {
	list := &listServer{}
	list.set(record("1", "one"), record("2", "two"))
	server := list.start(t)

	conf := testConfig(t, server.URL+"/api/v1", "projects")
	rt := newMockRuntime(t.Context())

	if written := pollOnce(t, New(conf), rt); written != 2 {
		t.Fatalf("first poll wrote %d entries, want 2", written)
	}
	// A brand new Plugin over the same durable state, as happens after a restart
	// or a config-change driven runner restart.
	if written := pollOnce(t, New(conf), rt); written != 0 {
		t.Fatalf("restarted plugin re-emitted %d entries, want 0", written)
	}
	list.set(record("1", "one"), record("2", "two"), record("3", "three"))
	if written := pollOnce(t, New(conf), rt); written != 1 {
		t.Fatalf("restarted plugin wrote %d entries for one new record, want 1", written)
	}
}

// body renders an execution identically for the list and for the
// single-execution request, so both produce the same record fingerprint.
func (a *executionAPI) body(id string) string {
	execution := a.executions[id]
	return fmt.Sprintf(`{"id":%q,"status":%q,"startedAt":%q,"data":{"resultData":{}}}`,
		id, execution.status, execution.startedAt.UTC().Format(time.RFC3339Nano))
}

func (a *executionAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.serverTime.IsZero() {
		w.Header().Set("Date", a.serverTime.Format(http.TimeFormat))
	}
	const list = "/api/v1/executions"
	switch {
	case r.URL.Path == "/api/v1/users":
		a.userRequests++
		_, _ = io.WriteString(w, `{"data":[{"id":"user-1"}],"nextCursor":null}`)
	case r.URL.Path == list:
		a.executionRequests++
		ids := make([]string, 0, len(a.executions))
		for id := range a.executions {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		query := r.URL.Query()
		var selected []string
		if status := query.Get("status"); status != "" {
			for _, id := range ids {
				if a.executions[id].status == status {
					selected = append(selected, a.body(id))
				}
			}
		} else {
			a.windows = append(a.windows, query.Get("startedAfter")+".."+query.Get("startedBefore"))
			if a.listStatus != 0 {
				w.WriteHeader(a.listStatus)
				return
			}
			for _, id := range a.finishAtList {
				execution := a.executions[id]
				execution.status = "success"
				a.executions[id] = execution
			}
			a.finishAtList = nil
			after, afterErr := time.Parse(time.RFC3339Nano, query.Get("startedAfter"))
			before, beforeErr := time.Parse(time.RFC3339Nano, query.Get("startedBefore"))
			if afterErr != nil || beforeErr != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for _, id := range ids {
				if startedAt := a.executions[id].startedAt; startedAt.After(after) && startedAt.Before(before) {
					selected = append(selected, a.body(id))
				}
			}
		}
		_, _ = fmt.Fprintf(w, `{"data":[%s],"nextCursor":null}`, strings.Join(selected, ","))
	case strings.HasPrefix(r.URL.Path, list+"/"):
		a.executionRequests++
		id := strings.TrimPrefix(r.URL.Path, list+"/")
		a.detailCalls[id]++
		if status := a.detailStatus[id]; status != 0 {
			w.WriteHeader(status)
			return
		}
		if _, ok := a.executions[id]; !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, a.body(id))
	default:
		http.NotFound(w, r)
	}
}

// Per-record state is bounded. A snapshot too large to track must fail closed
// rather than silently collapsing back to whole-snapshot re-emission.
func TestRecordTrackingLimitFailsClosed(t *testing.T) {
	list := &listServer{}
	list.set(record("1", "one"), record("2", "two"), record("3", "three"))
	server := list.start(t)

	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	plugin.maxTracked = 2
	rt := newMockRuntime(t.Context())

	_, err := plugin.Handle(t.Context(), rt)
	if err == nil || !strings.Contains(err.Error(), "tracked records") {
		t.Fatalf("error=%v want a record tracking limit failure", err)
	}
	// The remedies the message names have to be ones that work for this dataset.
	// The limit is per dataset, so dropping other datasets cannot help.
	if strings.Contains(err.Error(), "narrow the collected Dataset set") {
		t.Fatalf("error suggests a remedy that cannot work: %v", err)
	}
	if !strings.Contains(err.Error(), "per-dataset") {
		t.Fatalf("error does not say the limit is per dataset: %v", err)
	}
	if got := len(rt.Entries()); got != 2 {
		t.Fatalf("entries=%d want the two records enqueued before the tracking limit is hit", got)
	}
	if _, err := rt.Get(conf.stateKey(datasetCatalog["users"])); !errors.Is(err, storage.ErrStorageNotFound) {
		t.Fatalf("oversized snapshot advanced state: %v", err)
	}
}

// The tracking limit bounds what gets stored, so pagination repeats that are
// deduplicated away must not count against it.
func TestTrackingLimitCountsDeduplicatedRecords(t *testing.T) {
	// Three raw records, but the third repeats the first exactly, as a cursor
	// boundary overlap does, so only two distinct records are ever tracked.
	list := &listServer{}
	list.set(record("1", "one"), record("2", "two"), record("1", "one"))
	server := list.start(t)

	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	plugin.maxTracked = 2
	rt := newMockRuntime(t.Context())

	if written := pollOnce(t, plugin, rt); written != 2 {
		t.Fatalf("wrote %d entries, want 2; the limit is counting raw records", written)
	}
	state, err := rt.Get(conf.stateKey(datasetCatalog["users"]))
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 2*recordStateEntrySize {
		t.Fatalf("state holds %d bytes, want %d", len(state), 2*recordStateEntrySize)
	}
}

func expandedExecutionConfig(t *testing.T, api *executionAPI, datasets ...string) *Config {
	t.Helper()
	conf := testConfig(t, api.url, datasets...)
	conf.Include_Execution_Data = true
	conf.Request_Interval = 300
	return conf
}

func TestExpandedExecutionUsesN8nClockWhenRunnerIsAhead(t *testing.T) {
	sourceNow := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	runnerNow := sourceNow.Add(3 * time.Minute)
	api := newExecutionAPI(t)
	api.setServerTime(sourceNow)
	for i := 1; i <= 6; i++ {
		api.set(fmt.Sprintf("exec-%d", i), "success", sourceNow.Add(time.Duration(-60+i*8)*time.Second))
	}
	conf := expandedExecutionConfig(t, api, "executions")
	conf.Request_Interval = 60
	state := newBoltPolls(t)

	result := state.poll(conf, runnerNow)
	if result.err != nil || result.continuation == nil {
		t.Fatalf("poll continuation=%v error=%v", result.continuation, result.err)
	}
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("exec-%d", i)
		if got := state.delivered(id); got != 1 {
			t.Fatalf("execution %s delivered %d times, want 1", id, got)
		}
	}
	if got, want := api.lastWindow(), executionWindow(sourceNow.Add(-61*time.Second), sourceNow); got != want {
		t.Fatalf("window=%s want source-clock window %s", got, want)
	}
	if got := state.highwater(conf); !got.Equal(sourceNow) {
		t.Fatalf("high-water=%s want n8n source time %s", got, sourceNow)
	}
}

func TestExpandedExecutionUsesN8nClockWhenRunnerIsBehind(t *testing.T) {
	sourceNow := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	runnerNow := sourceNow.Add(-3 * time.Minute)
	api := newExecutionAPI(t)
	api.setServerTime(sourceNow)
	for i := 1; i <= 6; i++ {
		api.set(fmt.Sprintf("exec-%d", i), "success", sourceNow.Add(time.Duration(-60+i*8)*time.Second))
	}
	conf := expandedExecutionConfig(t, api, "executions")
	conf.Request_Interval = 60
	state := newBoltPolls(t)

	result := state.poll(conf, runnerNow)
	if result.err != nil || result.continuation == nil {
		t.Fatalf("poll continuation=%v error=%v", result.continuation, result.err)
	}
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("exec-%d", i)
		if got := state.delivered(id); got != 1 {
			t.Fatalf("execution %s delivered %d times, want 1", id, got)
		}
	}
	if got, want := api.lastWindow(), executionWindow(sourceNow.Add(-61*time.Second), sourceNow); got != want {
		t.Fatalf("window=%s want source-clock window %s", got, want)
	}
	if got := state.highwater(conf); !got.Equal(sourceNow) {
		t.Fatalf("high-water=%s want n8n source time %s", got, sourceNow)
	}
}

func TestSourceExecutionWindowEndRejectsExtremeSkewWithoutOverflow(t *testing.T) {
	runnerTime := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		sourceTime time.Time
	}{
		{name: "far past", sourceTime: time.Date(2, 1, 1, 0, 0, 0, 0, time.UTC)},
		{name: "far future", sourceTime: time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := sourceExecutionWindowEnd(runnerTime, tc.sourceTime); err == nil || !strings.Contains(err.Error(), "more than 24 hours") {
				t.Fatalf("error=%v want bounded clock-skew rejection", err)
			}
		})
	}
}

// State that does not parse must be discarded and rebuilt rather than wedging
// the dataset or being read as a phantom record.
func TestUnreadableSnapshotStateSelfHeals(t *testing.T) {
	var a, b recordState
	a[0], b[0] = 1, 2
	// A single fixed-width record-state entry is valid by construction, so the
	// malformed cases exercise shape and ordering rules instead.
	cases := map[string][]byte{
		// Not a whole number of per-record entries.
		"partial entry": make([]byte, recordStateEntrySize+7),
		// Right length, but not the sorted duplicate-free form we write. Membership
		// is a binary search, so reading these would at best cost spurious
		// re-emission; they are rejected so that cost is explicit and warned about
		// rather than silent.
		"descending": append(append([]byte{}, b[:]...), a[:]...),
		"duplicated": append(append([]byte{}, a[:]...), a[:]...),
	}
	for name, blob := range cases {
		t.Run(name, func(t *testing.T) {
			list := &listServer{}
			list.set(record("1", "one"))
			server := list.start(t)

			conf := testConfig(t, server.URL+"/api/v1", "tags")
			plugin := New(conf)
			rt := newMockRuntime(t.Context())
			stateKey := conf.stateKey(datasetCatalog["tags"])

			if err := rt.Put(stateKey, blob); err != nil {
				t.Fatal(err)
			}
			if written := pollOnce(t, plugin, rt); written != 1 {
				t.Fatalf("unparseable state wrote %d entries, want a full re-emit of 1", written)
			}
			state, err := rt.Get(stateKey)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := parseRecordState(state); !ok {
				t.Fatalf("state was not repaired: %x", state)
			}
			if written := pollOnce(t, plugin, rt); written != 0 {
				t.Fatalf("repaired state still re-emits %d entries", written)
			}
		})
	}
}

// A single-execution request that n8n refuses must stop the executions dataset
// with an error and leave its state where it was. Reporting the dataset as
// unavailable would end the poll as a success while the refused execution, and
// every tracked execution sorted after it, was never collected.
func TestExpandedExecutionDetailRefusalFailsDatasetAndKeepsTrackedState(t *testing.T) {
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	tracked := []string{"exec-a", "exec-b", "exec-c"}
	for _, status := range []int{http.StatusForbidden, http.StatusMethodNotAllowed, http.StatusGone, http.StatusNotImplemented} {
		t.Run(fmt.Sprintf("HTTP %d", status), func(t *testing.T) {
			api := newExecutionAPI(t)
			// The executions started before every window below, so only the
			// status probes and the single-execution request can reach them.
			for _, id := range tracked {
				api.set(id, "running", start.Add(-2*time.Hour))
			}
			conf := expandedExecutionConfig(t, api, "executions", "users")
			state := newBoltPolls(t)
			if first := state.poll(conf, start); first.err != nil {
				t.Fatalf("first poll: %v", first.err)
			}
			if got := state.activeIDs(conf); !slices.Equal(got, tracked) {
				t.Fatalf("tracked IDs after first poll=%v want %v", got, tracked)
			}

			api.finish(tracked...)
			api.answerDetail("exec-b", status)
			for retry := 1; retry <= 2; retry++ {
				result := state.poll(conf, start.Add(10*time.Minute+time.Duration(retry)*30*time.Second))
				if result.err == nil {
					t.Fatalf("retry %d: refused execution request was reported as success", retry)
				}
				if text := result.err.Error(); !strings.Contains(text, "Dataset executions") || !strings.Contains(text, fmt.Sprintf("HTTP %d", status)) {
					t.Fatalf("retry %d: error=%v want the dataset and HTTP %d", retry, result.err, status)
				}
				if result.continuation != nil {
					t.Fatalf("retry %d: failed poll returned a continuation", retry)
				}
				var unavailable *datasetUnavailableError
				if errors.As(result.err, &unavailable) {
					t.Fatalf("retry %d: error still classifies the dataset as unavailable: %v", retry, result.err)
				}
				if !hasLogLine(result.errs, "n8n Dataset collection failed", "dataset=executions") {
					t.Fatalf("retry %d: missing error log, got %v", retry, result.errs)
				}
				if hasLogLine(result.warns, "n8n Dataset unavailable") {
					t.Fatalf("retry %d: refusal was logged as an unavailable dataset: %v", retry, result.warns)
				}
				if warn, _ := state.output.Status(); strings.Contains(warn, "executions") {
					t.Fatalf("retry %d: status warning=%q", retry, warn)
				}
				if got := state.highwater(conf); !got.Equal(start) {
					t.Fatalf("retry %d: high-water=%s want unchanged %s", retry, got, start)
				}
				if got := state.activeIDs(conf); !slices.Equal(got, tracked) {
					t.Fatalf("retry %d: tracked IDs=%v want %v", retry, got, tracked)
				}
				if got := state.delivered("user-1"); got != 1 {
					t.Fatalf("retry %d: users record delivered %d times, want 1", retry, got)
				}
			}

			api.answerDetail("exec-b", 0)
			recovered := start.Add(12 * time.Minute)
			result := state.poll(conf, recovered)
			if result.err != nil || result.continuation == nil {
				t.Fatalf("recovery poll: continuation=%v error=%v", result.continuation, result.err)
			}
			for _, id := range tracked {
				if got := state.delivered(id); got != 1 {
					t.Fatalf("execution %s delivered %d times in total, want 1", id, got)
				}
			}
			if got := state.activeIDs(conf); len(got) != 0 {
				t.Fatalf("tracked IDs after recovery=%v want none", got)
			}
			if got := state.highwater(conf); !got.Equal(recovered) {
				t.Fatalf("high-water after recovery=%s want %s", got, recovered)
			}
		})
	}

	// A 404 on the same request means n8n no longer has the execution. The
	// poll records the lost detail, commits, and stops tracking that ID.
	t.Run("HTTP 404", func(t *testing.T) {
		api := newExecutionAPI(t)
		for _, id := range tracked {
			api.set(id, "running", start.Add(-2*time.Hour))
		}
		conf := expandedExecutionConfig(t, api, "executions", "users")
		state := newBoltPolls(t)
		if first := state.poll(conf, start); first.err != nil {
			t.Fatalf("first poll: %v", first.err)
		}
		api.finish(tracked...)
		api.remove("exec-b")
		next := start.Add(10 * time.Minute)
		result := state.poll(conf, next)
		if result.err != nil || result.continuation == nil {
			t.Fatalf("poll after removal: continuation=%v error=%v", result.continuation, result.err)
		}
		if got := api.detailRequests("exec-b"); got != 1 {
			t.Fatalf("removed execution requested %d times, want 1", got)
		}
		if !hasLogLine(result.warns, "n8n tracked execution no longer exists; its data was not collected", "dataset=executions", "execution_id=exec-b") {
			t.Fatalf("missing tracked-execution warning: %v", result.warns)
		}
		if !hasLogLine(result.infos, "n8n Dataset collection completed", "dataset=executions", "dropped=1") {
			t.Fatalf("missing tracked-execution drop count: %v", result.infos)
		}
		for id, want := range map[string]int{"exec-a": 1, "exec-b": 0, "exec-c": 1, "user-1": 1} {
			if got := state.delivered(id); got != want {
				t.Fatalf("record %s delivered %d times, want %d", id, got, want)
			}
		}
		if got := state.activeIDs(conf); len(got) != 0 {
			t.Fatalf("tracked IDs=%v want none", got)
		}
		if got := state.highwater(conf); !got.Equal(next) {
			t.Fatalf("high-water=%s want %s", got, next)
		}
	})
}

func TestStateKeyUsesInitialPublicSnapshotFormat(t *testing.T) {
	conf := testConfig(t, "http://127.0.0.1:12345/api/v1", "users")
	key := conf.stateKey(datasetCatalog["users"])
	if key != "n8n/users/snapshot-v1" {
		t.Fatalf("state key=%q want initial public snapshot namespace", key)
	}
}

func TestVerifiedReloadReusesRunnerScopedCheckpoint(t *testing.T) {
	first := &listServer{}
	first.set(`{"id":"one"}`)
	firstServer := first.start(t)
	conf := testConfig(t, firstServer.URL+"/api/v1", "users")
	rt := newMockRuntime(t.Context())
	if got := pollOnce(t, New(conf), rt); got != 1 {
		t.Fatalf("initial poll wrote %d entries, want 1", got)
	}

	renamed := *conf
	renamed.Tag_Prefix = "renamed"
	if got := pollOnce(t, New(&renamed), rt); got != 0 {
		t.Fatalf("tag-only reload replayed %d entries", got)
	}

	second := &listServer{}
	second.set(`{"id":"one"}`)
	secondServer := second.start(t)
	moved := renamed
	moved.Base_URL = secondServer.URL + "/api/v1"
	if got := pollOnce(t, New(&moved), rt); got != 0 {
		t.Fatalf("source reload in the same runner scope replayed %d entries", got)
	}

	reset := moved
	reset.Ingester_UUID = "eb2f2317-991a-4383-8ed9-5ef065d7d65d"
	resetRuntime := newMockRuntime(t.Context()) // models the runner's new UUID bucket
	if got := pollOnce(t, New(&reset), resetRuntime); got != 1 {
		t.Fatalf("new Ingester-UUID scope wrote %d entries, want intentional replay", got)
	}
}

func TestExpandedExecutionLookbackAppliesOnlyAtBootstrap(t *testing.T) {
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	api := newExecutionAPI(t)
	conf := expandedExecutionConfig(t, api, "executions")
	conf.Lookback = 24
	state := newBoltPolls(t)

	if first := state.poll(conf, start); first.err != nil {
		t.Fatalf("first poll: %v", first.err)
	}
	if got, want := api.lastWindow(), executionWindow(start.Add(-24*time.Hour-time.Second), start); got != want {
		t.Fatalf("first window=%s want %s", got, want)
	}

	second := start.Add(5 * time.Minute)
	if result := state.poll(conf, second); result.err != nil {
		t.Fatalf("second poll: %v", result.err)
	}
	if got, want := api.lastWindow(), executionWindow(start.Add(-5*time.Minute), second); got != want {
		t.Fatalf("second window=%s want %s; Lookback must not widen a poll that has a stored high-water mark", got, want)
	}
}

func TestOversizedCompletedStateFailsClosedBeforeFetch(t *testing.T) {
	conf := testConfig(t, "https://example.invalid/api/v1", "users")
	def := datasetCatalog["users"]
	key := conf.stateKey(def)
	base := newMockRuntime(t.Context())
	oversized := encodeRecordState(map[recordState]struct{}{
		regressionPair(t, `{"id":"one"}`):   {},
		regressionPair(t, `{"id":"two"}`):   {},
		regressionPair(t, `{"id":"three"}`): {},
	})
	if err := base.Put(key, oversized); err != nil {
		t.Fatal(err)
	}
	privateErr := errors.New("synthetic-private-storage-detail")
	rt := &failPutRuntime{Runtime: base, failKey: key, failErr: privateErr}
	calls := 0
	c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		return syntheticResponse(`{"data":[]}`), nil
	})
	p := New(conf)
	p.maxPending = 2
	err := p.collect(t.Context(), rt, c, def)
	if err == nil || !strings.Contains(err.Error(), "invalidate Dataset users completed state") || strings.Contains(err.Error(), privateErr.Error()) {
		t.Fatalf("error=%v want sanitized completed-state invalidation failure", err)
	}
	if calls != 0 || len(base.Entries()) != 0 {
		t.Fatalf("failed invalidation performed downstream work: calls=%d entries=%d", calls, len(base.Entries()))
	}
	if got, getErr := base.Get(key); getErr != nil || !bytes.Equal(got, oversized) {
		t.Fatalf("failed invalidation changed state: bytes=%d err=%v", len(got), getErr)
	}
}

func TestExpandedExecutionRaisingLookbackDoesNotReplayDeliveredExecutions(t *testing.T) {
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	api := newExecutionAPI(t)
	conf := expandedExecutionConfig(t, api, "executions")
	state := newBoltPolls(t)

	if first := state.poll(conf, start); first.err != nil {
		t.Fatalf("first poll: %v", first.err)
	}
	api.set("delivered-once", "success", start.Add(time.Minute))
	// The second poll delivers the execution, the third still sees it inside
	// the overlap, and after the fourth it has left both the window and the
	// completed manifest.
	for poll := 2; poll <= 4; poll++ {
		if result := state.poll(conf, start.Add(time.Duration(poll-1)*5*time.Minute)); result.err != nil {
			t.Fatalf("poll %d: %v", poll, result.err)
		}
		if got := state.delivered("delivered-once"); got != 1 {
			t.Fatalf("poll %d: execution delivered %d times, want 1", poll, got)
		}
	}
	if manifest := state.stored(conf.stateKey(datasetCatalog["executions"])); len(manifest) != 0 {
		t.Fatalf("completed manifest still holds %d bytes; the execution has not left the window", len(manifest))
	}

	reloaded := expandedExecutionConfig(t, api, "executions")
	reloaded.Lookback = 24
	if result := state.poll(reloaded, start.Add(20*time.Minute)); result.err != nil {
		t.Fatalf("poll after raising Lookback: %v", result.err)
	}
	if got, want := api.lastWindow(), executionWindow(start.Add(10*time.Minute), start.Add(20*time.Minute)); got != want {
		t.Errorf("window after raising Lookback=%s want %s", got, want)
	}
	if got := state.delivered("delivered-once"); got != 1 {
		t.Fatalf("execution delivered %d times after raising Lookback, want 1", got)
	}
}

func TestOversizedCompletedStateFailsClosedWhenInflightClearFails(t *testing.T) {
	conf := testConfig(t, "https://example.invalid/api/v1", "users")
	def := datasetCatalog["users"]
	key := conf.stateKey(def)
	base := newMockRuntime(t.Context())
	stable := regressionPair(t, `{"id":"stable"}`)
	oversized := encodeRecordState(map[recordState]struct{}{
		stable:                              {},
		regressionPair(t, `{"id":"two"}`):   {},
		regressionPair(t, `{"id":"three"}`): {},
	})
	stale := regressionProgress(nil, stable)
	if err := base.Put(key, oversized); err != nil {
		t.Fatal(err)
	}
	if err := base.Put(key+"/inflight", stale); err != nil {
		t.Fatal(err)
	}
	privateErr := errors.New("synthetic-private-storage-detail")
	rt := &failPutRuntime{Runtime: base, failKey: key + "/inflight", failErr: privateErr}
	calls := 0
	c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		return syntheticResponse(`{"data":[]}`), nil
	})
	p := New(conf)
	p.maxPending = 2
	err := p.collect(t.Context(), rt, c, def)
	if err == nil || !strings.Contains(err.Error(), "invalidate Dataset users in-flight state") || strings.Contains(err.Error(), privateErr.Error()) {
		t.Fatalf("error=%v want sanitized in-flight invalidation failure", err)
	}
	if calls != 0 || len(base.Entries()) != 0 {
		t.Fatalf("failed invalidation performed downstream work: calls=%d entries=%d", calls, len(base.Entries()))
	}
	if got, getErr := base.Get(key); getErr != nil || !bytes.Equal(got, oversized) {
		t.Fatalf("failed invalidation changed completed state: bytes=%d err=%v", len(got), getErr)
	}
	if got, getErr := base.Get(key + "/inflight"); getErr != nil || !bytes.Equal(got, stale) {
		t.Fatalf("failed invalidation changed in-flight state: bytes=%d err=%v", len(got), getErr)
	}
}

func TestExpandedExecutionFailedFirstPollDoesNotReapplyLookback(t *testing.T) {
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	api := newExecutionAPI(t)
	api.set("before-lookback", "success", start.Add(-90*time.Minute))
	api.set("inside-lookback", "success", start.Add(-30*time.Minute))
	api.answerList(http.StatusInternalServerError)
	conf := expandedExecutionConfig(t, api, "executions")
	conf.Lookback = 1
	state := newBoltPolls(t)

	if first := state.poll(conf, start); first.err == nil {
		t.Fatal("first poll succeeded although the execution list answered HTTP 500")
	}
	if got, want := api.lastWindow(), executionWindow(start.Add(-time.Hour-time.Second), start); got != want {
		t.Fatalf("first window=%s want %s", got, want)
	}

	api.answerList(0)
	retry := start.Add(30 * time.Second)
	if result := state.poll(conf, retry); result.err != nil {
		t.Fatalf("retry: %v", result.err)
	}
	// The failed poll stored its anchor one hour back. The retry starts one
	// Request-Interval before that anchor and does not subtract Lookback again.
	if got, want := api.lastWindow(), executionWindow(start.Add(-65*time.Minute), retry); got != want {
		t.Errorf("retry window=%s want %s", got, want)
	}
	if got := state.delivered("before-lookback"); got != 0 {
		t.Errorf("execution older than Lookback delivered %d times, want 0", got)
	}
	if got := state.delivered("inside-lookback"); got != 1 {
		t.Fatalf("execution inside Lookback delivered %d times, want 1", got)
	}
}

func TestOversizedCompletedStateClearsAndRecoversWithoutSuppression(t *testing.T) {
	conf := testConfig(t, "https://example.invalid/api/v1", "users")
	def := datasetCatalog["users"]
	key := conf.stateKey(def)
	rt := newMockRuntime(t.Context())
	stable := regressionPair(t, `{"id":"stable"}`)
	oversized := encodeRecordState(map[recordState]struct{}{
		stable:                              {},
		regressionPair(t, `{"id":"two"}`):   {},
		regressionPair(t, `{"id":"three"}`): {},
	})
	if err := rt.Put(key, oversized); err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(key+"/inflight", regressionProgress(nil, stable)); err != nil {
		t.Fatal(err)
	}

	calls := 0
	failing := syntheticClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("synthetic transport failure")
	})
	p := New(conf)
	p.maxPending = 2
	if err := p.collect(t.Context(), rt, failing, def); err == nil {
		t.Fatal("first poll unexpectedly succeeded")
	}
	if calls != 1 {
		t.Fatalf("requests=%d want one request after durable invalidation", calls)
	}
	for _, stateKey := range []string{key, key + "/inflight"} {
		got, err := rt.Get(stateKey)
		if err != nil && !errors.Is(err, storage.ErrStorageNotFound) {
			t.Fatalf("read invalidated state %q: %v", stateKey, err)
		}
		if len(got) != 0 {
			t.Fatalf("invalidated state %q retained %d bytes", stateKey, len(got))
		}
	}

	success := syntheticClient(t, func(*http.Request) (*http.Response, error) {
		return syntheticResponse(`{"data":[{"id":"stable"}],"nextCursor":null}`), nil
	})
	restarted := New(conf)
	restarted.maxPending = 2
	if err := restarted.collect(t.Context(), rt, success, def); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("recovery wrote %d entries, want previously suppressed record", got)
	}
	if err := restarted.collect(t.Context(), rt, success, def); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("stable retry duplicated entries: total=%d", got)
	}
}

// Rejecting one stored value must not move the window. The high-water mark of
// the last good poll stays in place, so executions that started after it are
// still collected.
func TestExpandedExecutionRejectedStateKeepsValidHighwater(t *testing.T) {
	lastGood := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	next := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	wantWindow := executionWindow(lastGood.Add(-5*time.Minute), next)

	// setup runs the last good poll: "tracked" is still running and is held
	// by ID, and "early" is delivered from the window. Afterwards "later"
	// starts and finishes and "tracked" finishes.
	setup := func(t *testing.T) (*executionAPI, *Config, *boltPolls) {
		t.Helper()
		api := newExecutionAPI(t)
		api.set("tracked", "running", lastGood.Add(-time.Hour))
		api.set("early", "success", lastGood.Add(-2*time.Minute))
		conf := expandedExecutionConfig(t, api, "executions")
		state := newBoltPolls(t)
		if result := state.poll(conf, lastGood); result.err != nil {
			t.Fatalf("last good poll: %v", result.err)
		}
		if got := state.activeIDs(conf); !slices.Equal(got, []string{"tracked"}) {
			t.Fatalf("tracked IDs=%v want [tracked]", got)
		}
		if got := state.delivered("early"); got != 1 {
			t.Fatalf("early execution delivered %d times, want 1", got)
		}
		api.set("later", "success", lastGood.Add(30*time.Minute))
		api.finish("tracked")
		return api, conf, state
	}

	t.Run("invalid active-ID value", func(t *testing.T) {
		api, conf, state := setup(t)
		state.store(executionActiveKey(conf.stateKey(datasetCatalog["executions"])), []byte("not a JSON array"))

		result := state.poll(conf, next)
		if result.err != nil {
			t.Fatalf("poll: %v", result.err)
		}
		if got := api.lastWindow(); got != wantWindow {
			t.Errorf("window=%s want %s", got, wantWindow)
		}
		if got := state.delivered("later"); got != 1 {
			t.Fatalf("later execution delivered %d times, want 1", got)
		}
		if got := state.delivered("early"); got != 1 {
			t.Fatalf("early execution delivered %d times, want 1; the manifest was intact", got)
		}
		if !hasLogLine(result.warns, "n8n discarding invalid active-execution state", "dataset=executions") {
			t.Fatalf("missing warning, got %v", result.warns)
		}
		if got := state.highwater(conf); !got.Equal(next) {
			t.Fatalf("high-water=%s want %s", got, next)
		}
		following := state.poll(conf, next.Add(5*time.Minute))
		if following.err != nil || len(following.warns) != 0 {
			t.Fatalf("following poll: error=%v warnings=%v", following.err, following.warns)
		}
	})

	for _, tc := range []struct {
		name    string
		reason  string
		corrupt func(t *testing.T, state *boltPolls, stateKey string)
		adjust  func(*Plugin)
	}{
		{
			name:   "unparseable manifest",
			reason: "reason=unreadable",
			corrupt: func(_ *testing.T, state *boltPolls, stateKey string) {
				state.store(stateKey, []byte{1, 2, 3})
			},
			adjust: func(*Plugin) {},
		},
		{
			name:   "oversized manifest",
			reason: "reason=oversized",
			// Four well-formed pairs exceed a budget of three, which still
			// leaves room for the three entries this poll writes.
			corrupt: func(t *testing.T, state *boltPolls, stateKey string) {
				pairs := map[recordState]struct{}{{1}: {}, {2}: {}, {3}: {}}
				var existing recordState
				if copy(existing[:], state.stored(stateKey)) != recordStateEntrySize {
					t.Fatal("last good poll did not store exactly one manifest pair")
				}
				pairs[existing] = struct{}{}
				state.store(stateKey, encodeRecordState(pairs))
			},
			adjust: func(p *Plugin) { p.maxPending = 3 },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, conf, state := setup(t)
			stateKey := conf.stateKey(datasetCatalog["executions"])
			tc.corrupt(t, state, stateKey)

			result := state.poll(conf, next, tc.adjust)
			if result.err != nil {
				t.Fatalf("poll: %v", result.err)
			}
			if got := api.lastWindow(); got != wantWindow {
				t.Errorf("window=%s want %s", got, wantWindow)
			}
			if got := state.delivered("later"); got != 1 {
				t.Fatalf("later execution delivered %d times, want 1", got)
			}
			if got := api.detailRequests("tracked"); got != 1 {
				t.Fatalf("tracked execution requested by ID %d times, want 1", got)
			}
			if got := state.delivered("tracked"); got != 1 {
				t.Fatalf("tracked execution delivered %d times, want 1", got)
			}
			// The manifest that suppressed it is gone, so the execution inside
			// the overlap is written once more. That is the whole cost in this
			// test because no poll failed after the last good one, so no failed
			// poll wrote entries since the mark.
			if got := state.delivered("early"); got != 2 {
				t.Fatalf("early execution delivered %d times, want 2", got)
			}
			if !hasLogLine(result.warns, "n8n discarding invalid snapshot state", "dataset=executions", tc.reason) {
				t.Fatalf("missing warning, got %v", result.warns)
			}
			if got := state.highwater(conf); !got.Equal(next) {
				t.Fatalf("high-water=%s want %s", got, next)
			}
			if got := state.activeIDs(conf); len(got) != 0 {
				t.Fatalf("tracked IDs=%v want none", got)
			}
		})
	}
}

func TestOversizedCompletedStateEmptyBoundABADurableRecovery(t *testing.T) {
	conf := testConfig(t, "https://example.invalid/api/v1", "users")
	def := datasetCatalog["users"]
	key := conf.stateKey(def)
	stable := regressionPair(t, `{"id":"stable"}`)
	oversized := encodeRecordState(map[recordState]struct{}{
		stable:                              {},
		regressionPair(t, `{"id":"two"}`):   {},
		regressionPair(t, `{"id":"three"}`): {},
	})
	path := filepath.Join(t.TempDir(), "state.db")
	withProgressBucket(t, path, func(b *storage.BucketWriter) {
		if err := b.Put(key, oversized); err != nil {
			t.Fatal(err)
		}
		if err := b.Put(key+"/inflight", regressionProgress(nil, stable)); err != nil {
			t.Fatal(err)
		}
	})

	writes := make([]int, 0, 2)
	for poll := 0; poll < 2; poll++ {
		withProgressBucket(t, path, func(b *storage.BucketWriter) {
			output := hosted.NewMock(t.Context())
			rt := &durableProgressRuntime{Runtime: output, bucket: b}
			c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
				return syntheticResponse(`{"data":[{"id":"stable"}],"nextCursor":null}`), nil
			})
			p := New(conf)
			p.maxPending = 2
			if err := p.collect(t.Context(), rt, c, def); err != nil {
				t.Fatal(err)
			}
			writes = append(writes, len(output.Entries()))
		})
	}
	if !slices.Equal(writes, []int{1, 0}) {
		t.Fatalf("reopened polls wrote %v, want [1 0]; stale empty-bound progress suppressed recovery", writes)
	}
	if got := reopenedProgress(t, path, key+"/inflight"); len(got) != 0 {
		t.Fatalf("successful recovery retained %d bytes of in-flight state", len(got))
	}
}

func TestMalformedCompletedStateCannotAuthenticateInflight(t *testing.T) {
	conf := testConfig(t, "https://example.invalid/api/v1", "users")
	def := datasetCatalog["users"]
	key := conf.stateKey(def)
	stable := regressionPair(t, `{"id":"stable"}`)
	malformed := []byte("malformed-completed-state")
	path := filepath.Join(t.TempDir(), "state.db")
	withProgressBucket(t, path, func(b *storage.BucketWriter) {
		if err := b.Put(key, malformed); err != nil {
			t.Fatal(err)
		}
		if err := b.Put(key+"/inflight", regressionProgress(malformed, stable)); err != nil {
			t.Fatal(err)
		}
	})

	writes := make([]int, 0, 2)
	for poll := 0; poll < 2; poll++ {
		withProgressBucket(t, path, func(b *storage.BucketWriter) {
			output := hosted.NewMock(t.Context())
			rt := &durableProgressRuntime{Runtime: output, bucket: b}
			c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
				return syntheticResponse(`{"data":[{"id":"stable"}],"nextCursor":null}`), nil
			})
			if err := New(conf).collect(t.Context(), rt, c, def); err != nil {
				t.Fatal(err)
			}
			writes = append(writes, len(output.Entries()))
		})
	}
	if !slices.Equal(writes, []int{1, 0}) {
		t.Fatalf("reopened polls wrote %v, want [1 0]; malformed state authenticated stale progress", writes)
	}
}

func TestMalformedCompletedStateInvalidationFailuresDoNoWork(t *testing.T) {
	for _, failure := range []string{"inflight", "completed"} {
		t.Run(failure, func(t *testing.T) {
			conf := testConfig(t, "https://example.invalid/api/v1", "users")
			def := datasetCatalog["users"]
			key := conf.stateKey(def)
			base := newMockRuntime(t.Context())
			malformed := []byte("malformed-completed-state")
			stable := regressionPair(t, `{"id":"stable"}`)
			stale := regressionProgress(malformed, stable)
			if err := base.Put(key, malformed); err != nil {
				t.Fatal(err)
			}
			if err := base.Put(key+"/inflight", stale); err != nil {
				t.Fatal(err)
			}
			failKey := key
			wantError := "completed state"
			if failure == "inflight" {
				failKey = key + "/inflight"
				wantError = "in-flight state"
			}
			privateErr := errors.New("synthetic-private-storage-detail")
			rt := &failPutRuntime{Runtime: base, failKey: failKey, failErr: privateErr}
			calls := 0
			c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				return syntheticResponse(`{"data":[]}`), nil
			})
			err := New(conf).collect(t.Context(), rt, c, def)
			if err == nil || !strings.Contains(err.Error(), wantError) || strings.Contains(err.Error(), privateErr.Error()) {
				t.Fatalf("error=%v want sanitized %s invalidation failure", err, failure)
			}
			if calls != 0 || len(base.Entries()) != 0 {
				t.Fatalf("failed invalidation performed work: calls=%d entries=%d", calls, len(base.Entries()))
			}
			if got, getErr := base.Get(key); getErr != nil || !bytes.Equal(got, malformed) {
				t.Fatalf("failed invalidation changed completed state: bytes=%d err=%v", len(got), getErr)
			}
			pending, getErr := base.Get(key + "/inflight")
			if getErr != nil && !errors.Is(getErr, storage.ErrStorageNotFound) {
				t.Fatal(getErr)
			}
			wantPending := stale
			if failure == "completed" {
				wantPending = nil
			}
			if !bytes.Equal(pending, wantPending) {
				t.Fatalf("in-flight state bytes=%d want=%d", len(pending), len(wantPending))
			}
		})
	}
}

func TestExpandedExecutionUnreadableHighwaterFailsClosedWithoutEchoingStoredValue(t *testing.T) {
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	const marker = "stored-highwater-marker-7f3a"
	api := newExecutionAPI(t)
	api.set("running", "running", start.Add(-time.Hour))
	conf := expandedExecutionConfig(t, api, "executions", "users")
	state := newBoltPolls(t)
	if first := state.poll(conf, start); first.err != nil {
		t.Fatalf("first poll: %v", first.err)
	}
	stateKey := conf.stateKey(datasetCatalog["executions"])
	highwaterKey, activeKey := executionHighwaterKey(stateKey), executionActiveKey(stateKey)
	state.store(highwaterKey, []byte(marker))
	active := state.stored(activeKey)
	executionRequests, userRequests := api.requestCounts()

	for retry := 1; retry <= 2; retry++ {
		result := state.poll(conf, start.Add(time.Duration(retry)*30*time.Second))
		if result.err == nil || result.continuation != nil {
			t.Fatalf("retry %d: continuation=%v error=%v, want a failed poll", retry, result.continuation, result.err)
		}
		text := result.err.Error()
		if !strings.Contains(text, "Dataset executions") || !strings.Contains(text, "high-water") {
			t.Fatalf("retry %d: error=%v want the dataset and its high-water state", retry, result.err)
		}
		if strings.Contains(text, marker) {
			t.Fatalf("retry %d: error repeats the stored value: %v", retry, result.err)
		}
		if strings.Contains(text, "Dataset users") {
			t.Fatalf("retry %d: users failed with executions: %v", retry, result.err)
		}
		if !hasLogLine(result.errs, "n8n Dataset collection failed", "dataset=executions") {
			t.Fatalf("retry %d: missing error log, got %v", retry, result.errs)
		}
		if hasLogLine(result.errs, marker) || hasLogLine(result.warns, marker) {
			t.Fatalf("retry %d: a log line repeats the stored value: errors=%v warnings=%v", retry, result.errs, result.warns)
		}
		gotExecutions, gotUsers := api.requestCounts()
		if gotExecutions != executionRequests {
			t.Fatalf("retry %d: sent %d execution requests, want none", retry, gotExecutions-executionRequests)
		}
		if gotUsers != userRequests+retry {
			t.Fatalf("retry %d: users requests=%d want %d", retry, gotUsers, userRequests+retry)
		}
		if got := state.stored(highwaterKey); !bytes.Equal(got, []byte(marker)) {
			t.Fatalf("retry %d: stored high-water value changed to %q", retry, got)
		}
		if got := state.stored(activeKey); !bytes.Equal(got, active) {
			t.Fatalf("retry %d: active-execution state changed to %q", retry, got)
		}
		if got := state.delivered("user-1"); got != 1 {
			t.Fatalf("retry %d: users record delivered %d times, want 1", retry, got)
		}
		if len(state.stored(conf.stateKey(datasetCatalog["users"]))) != recordStateEntrySize {
			t.Fatalf("retry %d: users manifest is not committed", retry)
		}
	}
}

// A stored high-water mark that is zero or later than the source clock cannot
// start a window, so the poll falls back to the first-run window, replaces the
// mark with that window's anchor before any request, and says so in a warning.
func TestExpandedExecutionUnusableHighwaterRestartsFirstRunWindow(t *testing.T) {
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	const discard = "n8n discarding high-water state that is zero or later than the source clock"
	discards := func(result pollResult) []string {
		var lines []string
		for _, line := range result.warns {
			if strings.Contains(line, discard) {
				lines = append(lines, line)
			}
		}
		return lines
	}
	// The warning names the dataset and carries no other field, so the stored
	// value never reaches the log.
	wantDiscards := []string{discard + " dataset=executions"}

	t.Run("mark later than the source clock", func(t *testing.T) {
		api := newExecutionAPI(t)
		conf := expandedExecutionConfig(t, api, "executions")
		conf.Lookback = 24
		state := newBoltPolls(t)

		first := state.poll(conf, start)
		if first.err != nil {
			t.Fatalf("first poll: %v", first.err)
		}
		if got := discards(first); len(got) != 0 {
			t.Errorf("first poll had no stored mark to discard but logged %v", got)
		}
		second := state.poll(conf, start.Add(5*time.Minute))
		if second.err != nil || len(second.warns) != 0 {
			t.Fatalf("second poll: error=%v warnings=%v", second.err, second.warns)
		}

		// The runner clock steps back ten seconds, behind the mark the second
		// poll stored, and the execution list fails.
		api.answerList(http.StatusInternalServerError)
		back := start.Add(5*time.Minute - 10*time.Second)
		failed := state.poll(conf, back)
		if failed.err == nil {
			t.Fatal("poll succeeded although the execution list answered HTTP 500")
		}
		if got, want := api.lastWindow(), executionWindow(back.Add(-24*time.Hour-time.Second), back); got != want {
			t.Errorf("window after the clock stepped back=%s want %s", got, want)
		}
		if got, want := state.highwater(conf), back.Add(-24*time.Hour); !got.Equal(want) {
			t.Errorf("high-water after the failed poll=%s want the first-run anchor %s", got, want)
		}
		if got := discards(failed); !slices.Equal(got, wantDiscards) {
			t.Errorf("warnings about the discarded mark=%q want %q", got, wantDiscards)
		}

		// The anchor is now an ordinary stored mark. The retry starts one
		// Request-Interval before it and does not subtract Lookback again.
		api.answerList(0)
		retry := back.Add(30 * time.Second)
		recovered := state.poll(conf, retry)
		if recovered.err != nil {
			t.Fatalf("retry: %v", recovered.err)
		}
		if got := discards(recovered); len(got) != 0 {
			t.Errorf("retry used the stored anchor but logged %v", got)
		}
		if got, want := api.lastWindow(), executionWindow(back.Add(-24*time.Hour-5*time.Minute), retry); got != want {
			t.Errorf("retry window=%s want %s", got, want)
		}
	})

	t.Run("zero mark", func(t *testing.T) {
		api := newExecutionAPI(t)
		conf := expandedExecutionConfig(t, api, "executions")
		state := newBoltPolls(t)
		state.store(executionHighwaterKey(conf.stateKey(datasetCatalog["executions"])), []byte("0001-01-01T00:00:00Z"))

		result := state.poll(conf, start)
		if result.err != nil {
			t.Fatalf("poll: %v", result.err)
		}
		if got, want := api.lastWindow(), executionWindow(start.Add(-5*time.Minute-time.Second), start); got != want {
			t.Errorf("window=%s want %s", got, want)
		}
		if got := discards(result); !slices.Equal(got, wantDiscards) {
			t.Errorf("warnings about the discarded mark=%q want %q", got, wantDiscards)
		}
		if got := state.highwater(conf); !got.Equal(start) {
			t.Errorf("high-water=%s want %s", got, start)
		}
	})
}

func TestRecordStateRoundTrip(t *testing.T) {
	if set, ok := parseRecordState(nil); !ok || set.len() != 0 {
		t.Fatalf("absent state parsed as ok=%v len=%d", ok, set.len())
	}
	if got := encodeRecordState(map[recordState]struct{}{}); len(got) != 0 {
		t.Fatalf("empty state encoded to %d bytes", len(got))
	}
	for _, size := range []int{1, recordStateEntrySize - 1, recordStateEntrySize + 1, 2*recordStateEntrySize - 3} {
		if _, ok := parseRecordState(make([]byte, size)); ok {
			t.Fatalf("a %d byte blob parsed, want rejection", size)
		}
	}

	var a, b, c recordState
	a[0], b[0], c[0] = 1, 2, 3
	set := map[recordState]struct{}{c: {}, b: {}, a: {}}
	encoded := encodeRecordState(set)
	if len(encoded) != 3*recordStateEntrySize {
		t.Fatalf("encoded %d bytes want %d", len(encoded), 3*recordStateEntrySize)
	}
	// Encoding is sorted, so an unchanged snapshot always produces identical bytes
	// regardless of map iteration order.
	for i := 0; i < 20; i++ {
		if got := encodeRecordState(set); string(got) != string(encoded) {
			t.Fatal("encoding is not deterministic across map iteration orders")
		}
	}

	// Membership is a binary search over the stored form, so it only answers
	// correctly while that form really is sorted and duplicate free.
	parsed, ok := parseRecordState(encoded)
	if !ok || parsed.len() != 3 {
		t.Fatalf("round trip produced ok=%v len=%d", ok, parsed.len())
	}
	for _, key := range []recordState{a, b, c} {
		if !parsed.contains(key) {
			t.Fatalf("round trip lost entry %x", key[0])
		}
	}
	var absent recordState
	absent[0] = 9
	if parsed.contains(absent) {
		t.Fatal("membership reported an entry that was never stored")
	}
	if (recordStateSet)(nil).contains(a) {
		t.Fatal("empty state reported membership")
	}

	// Descending and duplicate blobs are the right length but are not the form
	// encodeRecordState produces, so a binary search over them would be wrong.
	descending := append(append([]byte{}, c[:]...), a[:]...)
	if _, ok := parseRecordState(descending); ok {
		t.Fatal("unsorted state parsed, which would break membership search")
	}
	duplicated := append(append([]byte{}, a[:]...), a[:]...)
	if _, ok := parseRecordState(duplicated); ok {
		t.Fatal("duplicated state parsed, which is not a form we ever write")
	}
}

var errStorageRead = errors.New("synthetic storage read failure")

// failedRead answers GetTime for one key, or Get for one key, with a storage
// error that is neither not-found nor a parse error. Every other call reaches
// the wrapped runtime. GetTime is overridden by itself because a runtime's
// GetTime need not go through its Get.
type failedRead struct {
	hosted.Runtime
	timeKey, bytesKey string
}

func (r *failedRead) GetTime(key string) (time.Time, error) {
	if key == r.timeKey {
		return time.Time{}, errStorageRead
	}
	return r.Runtime.GetTime(key)
}

func (r *failedRead) Get(key string) ([]byte, error) {
	if key == r.bytesKey {
		return nil, errStorageRead
	}
	return r.Runtime.Get(key)
}

// A storage error while reading the high-water mark or the active-ID set says
// nothing about what is stored. Reading it as an absent value would replace a
// valid mark with a later anchor, or drop every tracked ID at the next commit,
// so the dataset stops before any request and leaves both values in place.
func TestExpandedExecutionStateReadFailureFailsClosed(t *testing.T) {
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		fail func(rt hosted.Runtime, highwaterKey, activeKey string) hosted.Runtime
		want string
	}{
		{
			name: "high-water read",
			fail: func(rt hosted.Runtime, highwaterKey, _ string) hosted.Runtime {
				return &failedRead{Runtime: rt, timeKey: highwaterKey}
			},
			want: "load Dataset executions high-water state",
		},
		{
			name: "active-ID read",
			fail: func(rt hosted.Runtime, _, activeKey string) hosted.Runtime {
				return &failedRead{Runtime: rt, bytesKey: activeKey}
			},
			want: "load Dataset executions active-execution state",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newExecutionAPI(t)
			api.set("run-1", "running", start.Add(-time.Hour))
			conf := expandedExecutionConfig(t, api, "executions")
			state := newBoltPolls(t)
			if first := state.poll(conf, start); first.err != nil {
				t.Fatalf("first poll: %v", first.err)
			}
			if got := state.highwater(conf); !got.Equal(start) {
				t.Fatalf("high-water after first poll=%s want %s", got, start)
			}
			if got := state.activeIDs(conf); !slices.Equal(got, []string{"run-1"}) {
				t.Fatalf("tracked IDs after first poll=%v want [run-1]", got)
			}
			// Only the stored mark can bring gap-1 into a window, and only the
			// tracked ID can bring run-1 back, because it started before every
			// window below.
			api.set("gap-1", "success", start.Add(30*time.Minute))
			api.finish("run-1")

			stateKey := conf.stateKey(datasetCatalog["executions"])
			highwaterKey, activeKey := executionHighwaterKey(stateKey), executionActiveKey(stateKey)
			highwater, active := state.stored(highwaterKey), state.stored(activeKey)
			requests, _ := api.requestCounts()

			state.wrap = func(rt hosted.Runtime) hosted.Runtime { return tc.fail(rt, highwaterKey, activeKey) }
			failed := state.poll(conf, start.Add(2*time.Hour))
			state.wrap = nil
			if failed.err == nil || !strings.Contains(failed.err.Error(), tc.want) {
				t.Fatalf("poll with a failing read: error=%v want %q", failed.err, tc.want)
			}
			if failed.continuation != nil {
				t.Fatal("failed poll returned a continuation")
			}
			if got, _ := api.requestCounts(); got != requests {
				t.Fatalf("failed poll sent %d execution requests, want none", got-requests)
			}
			if got := state.stored(highwaterKey); !bytes.Equal(got, highwater) {
				t.Fatalf("stored high-water value changed from %q to %q", highwater, got)
			}
			if got := state.stored(activeKey); !bytes.Equal(got, active) {
				t.Fatalf("stored active-execution value changed from %q to %q", active, got)
			}

			following := state.poll(conf, start.Add(2*time.Hour+30*time.Second))
			if following.err != nil || following.continuation == nil {
				t.Fatalf("following poll: continuation=%v error=%v", following.continuation, following.err)
			}
			for _, id := range []string{"gap-1", "run-1"} {
				if got := state.delivered(id); got != 1 {
					t.Fatalf("execution %s delivered %d times in total, want 1", id, got)
				}
			}
		})
	}
}

// The mark a poll commits is the end of the window it requested. Committing
// the time the poll finished would move the mark past executions that started
// while a long poll was running, and the next window would begin after them.
func TestExpandedExecutionCommittedHighwaterIsWindowEnd(t *testing.T) {
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	api := newExecutionAPI(t)
	api.set("during-1", "success", start.Add(2*time.Minute))
	conf := expandedExecutionConfig(t, api, "executions")
	state := newBoltPolls(t)

	// The first poll takes ten minutes by its own clock: the first reading is
	// its start, and every later reading is ten minutes on.
	readings := 0
	slow := func(p *Plugin) {
		p.now = func() time.Time {
			readings++
			if readings == 1 {
				return start
			}
			return start.Add(10 * time.Minute)
		}
	}
	if first := state.poll(conf, start, slow); first.err != nil {
		t.Fatalf("first poll: %v", first.err)
	}
	if got, want := api.lastWindow(), executionWindow(start.Add(-5*time.Minute-time.Second), start); got != want {
		t.Errorf("first window=%s want %s", got, want)
	}
	if got := state.highwater(conf); !got.Equal(start) {
		t.Errorf("high-water after first poll=%s want the window end %s", got, start)
	}
	if got := state.delivered("during-1"); got != 0 {
		t.Fatalf("execution that started after the window end delivered %d times, want 0", got)
	}

	if result := state.poll(conf, start.Add(15*time.Minute)); result.err != nil {
		t.Fatalf("second poll: %v", result.err)
	}
	if got := state.delivered("during-1"); got != 1 {
		t.Fatalf("execution that started during the first poll delivered %d times, want 1", got)
	}
}

var errStorageStopped = errors.New("synthetic storage failure after the manifest write")

// stopAfterManifest passes every call through until a Put of the completed
// manifest succeeds, and then fails every later Put, PutTime, and Write. That
// is what a process stop between the manifest write and the rest of the
// commit leaves behind.
type stopAfterManifest struct {
	hosted.Runtime
	manifestKey string
	mu          sync.Mutex
	stopped     bool
}

func (r *stopAfterManifest) isStopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped
}

type durableProgressRuntime struct {
	hosted.Runtime
	bucket         *storage.BucketWriter
	refuseClear    bool
	clearAttempted bool
	beforePut      func(string, []byte)
	afterPut       func(string, []byte)
	putCalls       int
}

func (r *stopAfterManifest) Put(key string, value []byte) error {
	if r.isStopped() {
		return errStorageStopped
	}
	if err := r.Runtime.Put(key, value); err != nil {
		return err
	}
	if key == r.manifestKey {
		r.mu.Lock()
		r.stopped = true
		r.mu.Unlock()
	}
	return nil
}

func (r *durableProgressRuntime) Get(k string) ([]byte, error) { return r.bucket.Get(k) }

func (r *durableProgressRuntime) Put(k string, v []byte) error {
	r.putCalls++
	if r.beforePut != nil {
		r.beforePut(k, v)
	}
	if r.refuseClear && strings.HasSuffix(k, "/inflight") && len(v) == 0 {
		r.clearAttempted = true
		return errors.New("synthetic in-flight-clear transaction refused: synthetic-private-storage")
	}
	if err := r.bucket.Put(k, v); err != nil {
		return err
	}
	if r.afterPut != nil {
		r.afterPut(k, v)
	}
	return nil
}

func (r *stopAfterManifest) PutTime(key string, value time.Time) error {
	if r.isStopped() {
		return errStorageStopped
	}
	return r.Runtime.PutTime(key, value)
}

func (r *stopAfterManifest) Write(ent entry.Entry) error {
	if r.isStopped() {
		return errStorageStopped
	}
	return r.Runtime.Write(ent)
}

// Every successful Put reaches Sync=true Bolt. Every poll has a fresh Plugin,
// fresh Runtime and a close/reopen readback. Failed clears return explicit errors.
func TestRegressionDurableABA(t *testing.T) {
	for _, mask := range []int{0, 1, 2, 3} {
		t.Run(fmt.Sprintf("clear_failure_mask_%d", mask), func(t *testing.T) {
			conf := testConfig(t, "https://example.invalid/api/v1", "users")
			def := datasetCatalog["users"]
			key := conf.stateKey(def)
			dbpath := filepath.Join(t.TempDir(), "state.db")
			var first, oldProgress []byte
			total := 0
			poll := func(phase int, refuse bool) (manifest, pending []byte, err error) {
				db, e := storage.OpenBoltHandler(dbpath, true)
				if e != nil {
					t.Fatal(e)
				}
				bucket, e := db.GetBucketWriter("n8n-durable-regression")
				if e != nil {
					t.Fatal(e)
				}
				output := hosted.NewMock(t.Context())
				rt := &durableProgressRuntime{Runtime: output, bucket: bucket, refuseClear: refuse}
				requests := 0
				c := syntheticClient(t, func(req *http.Request) (*http.Response, error) {
					requests++
					switch phase {
					case 1:
						if req.URL.Query().Get("cursor") == "tail" {
							r := syntheticResponse("")
							r.StatusCode = 500
							return r, nil
						}
						return syntheticResponse(`{"data":[{"id":"temporary"}],"nextCursor":"tail"}`), nil
					case 2:
						return syntheticResponse(`{"data":[],"nextCursor":null}`), nil
					case 4:
						return syntheticResponse(`{"data":[{"id":"stable"},{"id":"temporary"}],"nextCursor":null}`), nil
					default:
						return syntheticResponse(`{"data":[{"id":"stable"}],"nextCursor":null}`), nil
					}
				})
				err = New(conf).collect(t.Context(), rt, c, def)
				writes := len(output.Entries())
				if phase == 3 && mask == 3 && refuse {
					if requests != 0 || writes != 0 || rt.putCalls != 1 || err == nil || !strings.Contains(err.Error(), "invalidate Dataset users in-flight state") || strings.Contains(err.Error(), "synthetic-private-storage") {
						t.Fatalf("early invalidation did not fail closed: requests=%d writes=%d puts=%d err=%v", requests, writes, rt.putCalls, err)
					}
				}
				total += writes
				if (phase == 1 || rt.clearAttempted) != (err != nil) {
					t.Fatalf("phase=%d unexpected result %v", phase, err)
				}
				if e = db.Close(); e != nil {
					t.Fatal(e)
				}
				db, e = storage.OpenBoltHandler(dbpath, true)
				if e != nil {
					t.Fatal(e)
				}
				bucket, e = db.GetBucketWriter("n8n-durable-regression")
				if e != nil {
					t.Fatal(e)
				}
				manifest, e = bucket.Get(key)
				if e != nil {
					t.Fatal(e)
				}
				pending, e = bucket.Get(key + "/inflight")
				if e != nil && !errors.Is(e, storage.ErrStorageNotFound) {
					t.Fatal(e)
				}
				manifest = bytes.Clone(manifest)
				pending = bytes.Clone(pending)
				if e = db.Close(); e != nil {
					t.Fatal(e)
				}
				t.Logf("reopened phase=%d refused_clear=%t manifest=%d progress=%d writes=%d total=%d err=%v", phase, rt.clearAttempted, len(manifest), len(pending), writes, total, err)
				return
			}
			for phase := 0; phase <= 4; phase++ {
				manifest, pending, err := poll(phase, (phase == 2 && mask&1 != 0) || (phase == 3 && mask&2 != 0))
				if phase == 0 {
					first = bytes.Clone(manifest)
				}
				if phase == 1 {
					oldProgress = bytes.Clone(pending)
					if len(pending) != 96 {
						t.Fatalf("progress=%d", len(pending))
					}
				}
				if phase == 2 && len(manifest) != 0 {
					t.Fatal("empty B did not durably commit")
				}
				if phase == 3 {
					// A repair may refuse to advance B until stale progress is invalidated.
					// Retry only when the durable manifest proves A was not committed.
					if !bytes.Equal(first, manifest) {
						if err == nil || len(manifest) != 0 || !bytes.Equal(pending, oldProgress) {
							t.Fatal("invalid pre-invalidation failure")
						}
						manifest, pending, err = poll(phase, false)
						if err != nil || !bytes.Equal(first, manifest) {
							t.Fatal("guarded retry did not commit A")
						}
					} else if mask == 3 && !bytes.Equal(oldProgress, pending) {
						t.Log("old progress changed before A recommit")
					}
				}
				if phase == 4 && len(pending) != 0 {
					t.Fatal("successful final snapshot retained progress")
				}
			}
			if total != 4 {
				t.Fatalf("durably recorded absences followed by reappearance: writes=%d want=4", total)
			}
		})
	}
}

var errEntryWrite = errors.New("synthetic entry write failure")

// failEntryWrite fails the Write of every entry whose record has the given id.
type failEntryWrite struct {
	hosted.Runtime
	id string
}

func (r *failEntryWrite) Write(ent entry.Entry) error {
	var record struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(ent.Data, &record) == nil && record.ID == r.id {
		return errEntryWrite
	}
	return r.Runtime.Write(ent)
}

// The status probes are the only way to find an execution that started before
// the window. A poll that fails after its probes must keep what they found, so
// that a later poll fetches the execution by ID once it completes, whether the
// failed poll was the first one or followed a discarded active-ID value.
func TestExpandedExecutionFailedPollKeepsProbedActiveIDs(t *testing.T) {
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name      string
		lookback  int
		status    string
		startedAt time.Time
		fail      func(api *executionAPI, state *boltPolls, manifestKey string)
		want      string // part of the failed poll's error
	}{
		{
			name:      "first poll list error",
			status:    "running",
			startedAt: start.Add(-time.Hour),
			fail: func(api *executionAPI, _ *boltPolls, _ string) {
				api.answerList(http.StatusInternalServerError)
			},
			want: "HTTP 500",
		},
		{
			name:      "first poll list error with Lookback",
			lookback:  24,
			status:    "waiting",
			startedAt: start.Add(-48 * time.Hour),
			fail: func(api *executionAPI, _ *boltPolls, _ string) {
				api.answerList(http.StatusInternalServerError)
			},
			want: "HTTP 500",
		},
		{
			name:      "first poll stops after the manifest write",
			status:    "running",
			startedAt: start.Add(-time.Hour),
			fail: func(_ *executionAPI, state *boltPolls, manifestKey string) {
				state.wrap = func(rt hosted.Runtime) hosted.Runtime {
					return &stopAfterManifest{Runtime: rt, manifestKey: manifestKey}
				}
			},
			want: "persist Dataset executions active-execution state",
		},
		{
			name:      "first poll entry write error",
			status:    "running",
			startedAt: start.Add(-time.Hour),
			fail: func(_ *executionAPI, state *boltPolls, _ string) {
				state.wrap = func(rt hosted.Runtime) hosted.Runtime {
					return &failEntryWrite{Runtime: rt, id: "in-window"}
				}
			},
			want: errEntryWrite.Error(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newExecutionAPI(t)
			api.set("long-1", tc.status, tc.startedAt)
			api.set("in-window", "success", start.Add(-time.Minute))
			conf := expandedExecutionConfig(t, api, "executions")
			conf.Lookback = tc.lookback
			state := newBoltPolls(t)
			tc.fail(api, state, conf.stateKey(datasetCatalog["executions"]))

			if failed := state.poll(conf, start); failed.err == nil || !strings.Contains(failed.err.Error(), tc.want) {
				t.Fatalf("first poll: error=%v want one containing %q", failed.err, tc.want)
			}
			if got := state.activeIDs(conf); !slices.Contains(got, "long-1") {
				t.Errorf("tracked IDs after the failed poll=%v want long-1 among them", got)
			}

			state.wrap = nil
			api.answerList(0)
			api.finish("long-1")
			for poll := 1; poll <= 3; poll++ {
				if result := state.poll(conf, start.Add(time.Duration(poll)*5*time.Minute)); result.err != nil {
					t.Fatalf("healthy poll %d: %v", poll, result.err)
				}
			}
			for _, id := range []string{"long-1", "in-window"} {
				if got := state.delivered(id); got < 1 {
					t.Errorf("execution %s delivered %d times, want at least 1", id, got)
				}
			}
		})
	}

	t.Run("list error after discarding the active-ID value", func(t *testing.T) {
		api := newExecutionAPI(t)
		api.set("long-3", "running", start.Add(-time.Hour))
		api.set("in-window", "success", start.Add(-time.Minute))
		conf := expandedExecutionConfig(t, api, "executions")
		state := newBoltPolls(t)
		if first := state.poll(conf, start); first.err != nil {
			t.Fatalf("first poll: %v", first.err)
		}
		if got := state.activeIDs(conf); !slices.Equal(got, []string{"long-3"}) {
			t.Fatalf("tracked IDs after first poll=%v want [long-3]", got)
		}

		state.store(executionActiveKey(conf.stateKey(datasetCatalog["executions"])), []byte("not-json"))
		api.answerList(http.StatusInternalServerError)
		failed := state.poll(conf, start.Add(5*time.Minute))
		if failed.err == nil {
			t.Fatal("poll succeeded although the execution list answered HTTP 500")
		}
		if !hasLogLine(failed.warns, "n8n discarding invalid active-execution state", "dataset=executions") {
			t.Fatalf("missing warning, got %v", failed.warns)
		}
		if got := state.activeIDs(conf); !slices.Contains(got, "long-3") {
			t.Errorf("tracked IDs after the failed poll=%v want long-3 among them", got)
		}

		api.answerList(0)
		api.finish("long-3")
		for poll := 2; poll <= 4; poll++ {
			if result := state.poll(conf, start.Add(time.Duration(poll)*5*time.Minute)); result.err != nil {
				t.Fatalf("healthy poll at +%dm: %v", poll*5, result.err)
			}
		}
		for _, id := range []string{"long-3", "in-window"} {
			if got := state.delivered(id); got < 1 {
				t.Errorf("execution %s delivered %d times, want at least 1", id, got)
			}
		}
	})

	t.Run("list error after a healthy poll that tracked an execution", func(t *testing.T) {
		api := newExecutionAPI(t)
		api.set("long-p", "running", start.Add(-time.Hour))
		api.set("in-window", "success", start.Add(-time.Minute))
		conf := expandedExecutionConfig(t, api, "executions")
		state := newBoltPolls(t)
		if first := state.poll(conf, start); first.err != nil {
			t.Fatalf("first poll: %v", first.err)
		}
		if got := state.activeIDs(conf); !slices.Equal(got, []string{"long-p"}) {
			t.Fatalf("tracked IDs after first poll=%v want [long-p]", got)
		}

		api.finish("long-p")
		api.set("new-q", "running", start.Add(time.Minute))
		api.answerList(http.StatusInternalServerError)
		if failed := state.poll(conf, start.Add(5*time.Minute)); failed.err == nil || !strings.Contains(failed.err.Error(), "HTTP 500") {
			t.Fatalf("second poll: error=%v want one containing %q", failed.err, "HTTP 500")
		}
		if got := state.activeIDs(conf); !slices.Equal(got, []string{"long-p", "new-q"}) {
			t.Errorf("tracked IDs after the failed poll=%v want [long-p new-q]", got)
		}

		api.answerList(0)
		api.finish("new-q")
		for poll := 2; poll <= 4; poll++ {
			if result := state.poll(conf, start.Add(time.Duration(poll)*5*time.Minute)); result.err != nil {
				t.Fatalf("healthy poll at +%dm: %v", poll*5, result.err)
			}
		}
		for _, id := range []string{"long-p", "new-q", "in-window"} {
			if got := state.delivered(id); got < 1 {
				t.Errorf("execution %s delivered %d times, want at least 1", id, got)
			}
		}
	})
}

// A version observed in the preceding complete multi-page snapshot is already
// known even if the source rolls back to that version. This is the explicit
// observation-set contract, separate from resurrection after committed absence.
func TestRegressionObservationSetRollbackControl(t *testing.T) {
	conf := testConfig(t, "https://example.invalid/api/v1", "users")
	rt := hosted.NewMock(t.Context())
	for poll, kind := range []string{"overlap", "overlap", "old", "old", "new"} {
		c := syntheticClient(t, func(req *http.Request) (*http.Response, error) {
			if kind == "overlap" {
				if req.URL.Query().Get("cursor") == "tail" {
					return syntheticResponse(`{"data":[{"id":"one","version":"new"}],"nextCursor":null}`), nil
				}
				return syntheticResponse(`{"data":[{"id":"one","version":"old"}],"nextCursor":"tail"}`), nil
			}
			return syntheticResponse(fmt.Sprintf(`{"data":[{"id":"one","version":%q}],"nextCursor":null}`, kind)), nil
		})
		if err := New(conf).collect(t.Context(), rt, c, datasetCatalog["users"]); err != nil {
			t.Fatal(err)
		}
		want := 2
		if poll == 4 {
			want = 3
		}
		if got := len(rt.Entries()); got != want {
			t.Fatalf("poll=%d entries=%d want=%d", poll, got, want)
		}
		t.Logf("poll=%d kind=%s total=%d", poll, kind, len(rt.Entries()))
	}
}

// Every helper call closes the synchronous Bolt database. Readbacks therefore
// use reopened storage rather than the runtime's in-memory view.
func withProgressBucket(t *testing.T, path string, f func(*storage.BucketWriter)) {
	t.Helper()
	db, err := storage.OpenBoltHandler(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	b, err := db.GetBucketWriter("n8n-durable-regression")
	if err != nil {
		t.Fatal(err)
	}
	f(b)
}

func reopenedProgress(t *testing.T, path, key string) (value []byte) {
	t.Helper()
	withProgressBucket(t, path, func(b *storage.BucketWriter) {
		var err error
		value, err = b.Get(key)
		if err != nil && !errors.Is(err, storage.ErrStorageNotFound) {
			t.Fatal(err)
		}
		value = bytes.Clone(value)
	})
	return
}

func regressionPair(t *testing.T, body string) recordState {
	t.Helper()
	r := normalizeTestRecord(t, body, false)
	var pair recordState
	copy(pair[:sha256.Size], r.identity[:])
	copy(pair[sha256.Size:], r.fingerprint[:])
	return pair
}

func (a *executionAPI) finishAtNextList(ids ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.finishAtList = append(a.finishAtList, ids...)
}

func regressionProgress(manifest []byte, pairs ...recordState) []byte {
	digest := sha256.Sum256(manifest)
	set := make(map[recordState]struct{})
	for _, p := range pairs {
		set[p] = struct{}{}
	}
	return append(digest[:], encodeRecordState(set)...)
}

func TestInflightInvalidationDurableBeforeAnyWork(t *testing.T) {
	for _, kind := range []string{"mismatch", "short", "matching_malformed", "matching_unsorted", "matching_over_budget"} {
		t.Run(kind, func(t *testing.T) {
			conf := testConfig(t, "https://example.invalid/api/v1", "users")
			def := datasetCatalog["users"]
			key := conf.stateKey(def)
			stable := regressionPair(t, `{"id":"stable"}`)
			temporary := regressionPair(t, `{"id":"temporary"}`)
			manifest := encodeRecordState(map[recordState]struct{}{stable: {}})
			pending := regressionProgress(manifest, temporary)
			switch kind {
			case "mismatch":
				pending[0] ^= 1
			case "short":
				pending = []byte("private-short-evidence")
			case "matching_malformed":
				pending = append(pending, byte(1))
			case "matching_unsorted":
				pending = append(pending, temporary[:]...)
			case "matching_over_budget":
				pending = regressionProgress(manifest, stable, temporary)
			}
			path := filepath.Join(t.TempDir(), "state.db")
			withProgressBucket(t, path, func(b *storage.BucketWriter) {
				if err := b.Put(key, manifest); err != nil {
					t.Fatal(err)
				}
				if err := b.Put(key+"/inflight", pending); err != nil {
					t.Fatal(err)
				}
			})
			calls := 0
			makeClient := func() *client {
				return syntheticClient(t, func(*http.Request) (*http.Response, error) {
					calls++
					// Fetch can only start after stale state is durably cleared, even on the
					// retry which itself fails without producing a progress checkpoint.
					return nil, errors.New("synthetic later transport failure")
				})
			}
			for _, refuse := range []bool{true, false} {
				withProgressBucket(t, path, func(b *storage.BucketWriter) {
					output := hosted.NewMock(t.Context())
					rt := &durableProgressRuntime{Runtime: output, bucket: b, refuseClear: refuse}
					p := New(conf)
					if kind == "matching_over_budget" {
						p.maxPending = 1
					}
					err := p.collect(t.Context(), rt, makeClient(), def)
					if err == nil {
						t.Fatal("expected storage or request failure")
					}
					if refuse {
						if calls != 0 || len(output.Entries()) != 0 || rt.putCalls != 1 || !strings.Contains(err.Error(), "invalidate Dataset users in-flight state") || strings.Contains(err.Error(), "synthetic-private-storage") {
							t.Fatalf("failure did downstream work or disclosed storage detail: calls=%d entries=%d puts=%d err=%v", calls, len(output.Entries()), rt.putCalls, err)
						}
					} else if calls != 1 || rt.putCalls != 1 {
						t.Fatalf("retry work calls=%d puts=%d", calls, rt.putCalls)
					}
				})
				if got := reopenedProgress(t, path, key); !bytes.Equal(got, manifest) {
					t.Fatal("failed poll advanced completed manifest")
				}
				got := reopenedProgress(t, path, key+"/inflight")
				if refuse && !bytes.Equal(got, pending) {
					t.Fatal("failed invalidation modified durable progress")
				}
				if !refuse && len(got) != 0 {
					t.Fatal("failed retry resurrected stale progress")
				}
			}
			// A later successful retry emits the previously suppressed current pair.
			withProgressBucket(t, path, func(b *storage.BucketWriter) {
				output := hosted.NewMock(t.Context())
				rt := &durableProgressRuntime{Runtime: output, bucket: b}
				c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
					return syntheticResponse(`{"data":[{"id":"stable"},{"id":"temporary"}],"nextCursor":null}`), nil
				})
				if err := New(conf).collect(t.Context(), rt, c, def); err != nil {
					t.Fatal(err)
				}
				if len(output.Entries()) != 1 {
					t.Fatalf("recovery emitted=%d want=1", len(output.Entries()))
				}
			})
			if got := reopenedProgress(t, path, key); len(got) != 2*recordStateEntrySize {
				t.Fatalf("recovery manifest=%d", len(got))
			}
		})
	}
}

// An execution that the status probes find running can finish before the
// window is listed. The listing then delivers it with a terminal status, and
// the poll must stop tracking its ID. A still-tracked ID would be fetched by ID
// on the next poll: that spends the detail budget, reports a false drop when
// retention has already removed the execution, and writes the execution a
// second time when the fetch is postponed past the manifest that suppresses it.
func TestExpandedExecutionProbedIDFinishedInWindowIsNotTracked(t *testing.T) {
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

	t.Run("removed by retention", func(t *testing.T) {
		api := newExecutionAPI(t)
		// Lookback puts the execution inside the first window but outside the
		// one-Request-Interval overlap of the next one.
		api.set("probed-1", "running", start.Add(-30*time.Minute))
		api.finishAtNextList("probed-1")
		conf := expandedExecutionConfig(t, api, "executions")
		conf.Lookback = 1
		state := newBoltPolls(t)

		if first := state.poll(conf, start); first.err != nil {
			t.Fatalf("first poll: %v", first.err)
		}
		if got := state.delivered("probed-1"); got != 1 {
			t.Fatalf("execution delivered %d times by the first poll, want 1", got)
		}
		if got := state.activeIDs(conf); len(got) != 0 {
			t.Fatalf("tracked IDs after the first poll=%v want none; the window listing delivered the execution", got)
		}

		api.remove("probed-1")
		result := state.poll(conf, start.Add(5*time.Minute))
		if result.err != nil || result.continuation == nil {
			t.Fatalf("second poll: continuation=%v error=%v", result.continuation, result.err)
		}
		if got := api.detailRequests("probed-1"); got != 0 {
			t.Fatalf("delivered execution requested by ID %d times, want 0", got)
		}
		if hasLogLine(result.warns, "n8n tracked execution no longer exists") {
			t.Fatalf("delivered execution reported as not collected: %v", result.warns)
		}
		if !hasLogLine(result.infos, "n8n Dataset collection completed", "dataset=executions", "dropped=0") {
			t.Fatalf("missing zero drop count: %v", result.infos)
		}
		if got := state.delivered("probed-1"); got != 1 {
			t.Fatalf("execution delivered %d times in total, want 1", got)
		}
	})

	t.Run("detail budget exhausted", func(t *testing.T) {
		api := newExecutionAPI(t)
		// early-run started before every window and sorts before probed-2,
		// so with a detail budget of one it takes the only fetch.
		api.set("early-run", "running", start.Add(-2*time.Hour))
		api.set("probed-2", "running", start.Add(-30*time.Minute))
		api.finishAtNextList("probed-2")
		conf := expandedExecutionConfig(t, api, "executions")
		conf.Lookback = 1
		conf.Max_Pages = 1
		state := newBoltPolls(t)

		if first := state.poll(conf, start); first.err != nil {
			t.Fatalf("first poll: %v", first.err)
		}
		if got := state.activeIDs(conf); !slices.Equal(got, []string{"early-run"}) {
			t.Fatalf("tracked IDs after the first poll=%v want [early-run]", got)
		}

		api.finish("early-run")
		for poll := 1; poll <= 2; poll++ {
			if result := state.poll(conf, start.Add(time.Duration(poll)*5*time.Minute)); result.err != nil {
				t.Fatalf("poll %d after the first: %v", poll, result.err)
			}
		}
		if got := api.detailRequests("probed-2"); got != 0 {
			t.Fatalf("delivered execution requested by ID %d times, want 0", got)
		}
		for _, id := range []string{"early-run", "probed-2"} {
			if got := state.delivered(id); got != 1 {
				t.Fatalf("execution %s delivered %d times in total, want 1", id, got)
			}
		}
		if got := state.activeIDs(conf); len(got) != 0 {
			t.Fatalf("tracked IDs=%v want none", got)
		}
	})
}

func TestInflightInvalidationInterruptionReopen(t *testing.T) {
	for _, at := range []string{"before", "after"} {
		t.Run(at, func(t *testing.T) {
			conf := testConfig(t, "https://example.invalid/api/v1", "users")
			def := datasetCatalog["users"]
			key := conf.stateKey(def)
			path := filepath.Join(t.TempDir(), "state.db")
			stale := []byte("stale-private-progress")
			withProgressBucket(t, path, func(b *storage.BucketWriter) {
				if err := b.Put(key+"/inflight", stale); err != nil {
					t.Fatal(err)
				}
			})
			calls := 0
			interrupted := false
			withProgressBucket(t, path, func(b *storage.BucketWriter) {
				output := hosted.NewMock(t.Context())
				rt := &durableProgressRuntime{Runtime: output, bucket: b}
				interrupt := func(k string, v []byte) {
					if k == key+"/inflight" && len(v) == 0 {
						interrupted = true
						panic("synthetic interruption")
					}
				}
				if at == "before" {
					rt.beforePut = interrupt
				} else {
					rt.afterPut = interrupt
				}
				func() {
					defer func() {
						if v := recover(); v != "synthetic interruption" {
							t.Fatalf("unexpected interruption: %v", v)
						}
					}()
					c := syntheticClient(t, func(*http.Request) (*http.Response, error) { calls++; return syntheticResponse(`{"data":[]}`), nil })
					_ = New(conf).collect(t.Context(), rt, c, def)
				}()
				if !interrupted || calls != 0 || len(output.Entries()) != 0 || rt.putCalls != 1 {
					t.Fatalf("interruption boundary violated calls=%d puts=%d", calls, rt.putCalls)
				}
			})
			got := reopenedProgress(t, path, key+"/inflight")
			if at == "before" && !bytes.Equal(got, stale) {
				t.Fatal("pre-write interruption changed durable evidence")
			}
			if at == "after" && len(got) != 0 {
				t.Fatal("post-write interruption retained evidence")
			}
			if got := reopenedProgress(t, path, key); len(got) != 0 {
				t.Fatal("interrupted invalidation advanced manifest")
			}
			withProgressBucket(t, path, func(b *storage.BucketWriter) {
				output := hosted.NewMock(t.Context())
				rt := &durableProgressRuntime{Runtime: output, bucket: b}
				c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
					return syntheticResponse(`{"data":[{"id":"stable"}],"nextCursor":null}`), nil
				})
				if err := New(conf).collect(t.Context(), rt, c, def); err != nil {
					t.Fatal(err)
				}
				if len(output.Entries()) != 1 {
					t.Fatalf("restart emitted=%d", len(output.Entries()))
				}
			})
			if got := reopenedProgress(t, path, key); len(got) != recordStateEntrySize {
				t.Fatal("restart failed to commit")
			}
		})
	}
}

func TestDurableCompletedManifestPrecedesMatchingProgressClear(t *testing.T) {
	conf := testConfig(t, "https://example.invalid/api/v1", "users")
	def := datasetCatalog["users"]
	key := conf.stateKey(def)
	path := filepath.Join(t.TempDir(), "state.db")
	total := 0
	for phase := 0; phase < 3; phase++ {
		withProgressBucket(t, path, func(b *storage.BucketWriter) {
			output := hosted.NewMock(t.Context())
			base := &durableProgressRuntime{Runtime: output, bucket: b}
			var rt hosted.Runtime = base
			if phase == 1 {
				rt = &dropAfterPutRuntime{Runtime: base, dropAt: 2}
			}
			c := syntheticClient(t, func(req *http.Request) (*http.Response, error) {
				if phase == 0 {
					if req.URL.Query().Get("cursor") == "tail" {
						resp := syntheticResponse("")
						resp.StatusCode = 500
						return resp, nil
					}
					return syntheticResponse(`{"data":[{"id":"stable"}],"nextCursor":"tail"}`), nil
				}
				return syntheticResponse(`{"data":[{"id":"stable"},{"id":"temporary"}],"nextCursor":null}`), nil
			})
			err := New(conf).collect(t.Context(), rt, c, def)
			if (phase == 0) != (err != nil) {
				t.Fatalf("phase=%d error=%v", phase, err)
			}
			total += len(output.Entries())
		})
		completed := reopenedProgress(t, path, key)
		pending := reopenedProgress(t, path, key+"/inflight")
		if phase == 0 && (len(completed) != 0 || len(pending) != sha256.Size+recordStateEntrySize) {
			t.Fatal("incomplete poll not durable")
		}
		if phase == 1 && (len(completed) != 2*recordStateEntrySize || len(pending) != sha256.Size+recordStateEntrySize) {
			t.Fatal("manifest was not durable before interrupted matching cache cleanup")
		}
	}
	if total != 2 {
		t.Fatalf("matching replay across cleanup interruption emitted %d want=2", total)
	}
}

func TestPureTransportCancellationRemainsCleanAtAdapter(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) { cancel(); return nil, context.Canceled })
	conf := testConfig(t, "https://example.invalid/api/v1", "users")
	rt := hosted.NewMock(ctx)
	if err := hosted.WrapJob(New(conf)).Run(ctx, rt); err != nil {
		t.Fatal(err)
	}
	_, err := rt.Status()
	if err != nil {
		t.Fatalf("pure cancellation became hard adapter error: %v", err)
	}
}
