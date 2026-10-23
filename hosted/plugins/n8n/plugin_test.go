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
