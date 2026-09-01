package jamf

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/hosted/storage"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/utils/jsoncompat"
)

// mockRuntime is a minimal in-memory implementation of hosted.Runtime,
// mirroring the pattern used in hosted/plugins/msgraph/msgraph_test.go.
type mockRuntime struct {
	mu      sync.Mutex
	entries []entry.Entry
	store   map[string][]byte
	tags    map[string]entry.EntryTag
	nextTag entry.EntryTag
	ctx     context.Context
	cancel  context.CancelFunc
}

// newMockRuntime constructs a ready-to-use mockRuntime backed by an empty
// entry log, key/value store, and tag table, derived from ctx.
func newMockRuntime(ctx context.Context) *mockRuntime {
	ctx, cancel := context.WithCancel(ctx)
	return &mockRuntime{
		entries: []entry.Entry{},
		store:   map[string][]byte{},
		tags:    map[string]entry.EntryTag{},
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (m *mockRuntime) Alive() bool              { return true }
func (m *mockRuntime) Context() context.Context { return m.ctx }
func (m *mockRuntime) Sleep(d time.Duration) bool {
	select {
	case <-time.After(d):
		return false
	case <-m.ctx.Done():
		return true
	}
}
func (m *mockRuntime) Debug(_ string, _ ...rfc5424.SDParam)    {}
func (m *mockRuntime) Info(_ string, _ ...rfc5424.SDParam)     {}
func (m *mockRuntime) Warn(_ string, _ ...rfc5424.SDParam)     {}
func (m *mockRuntime) Error(_ string, _ ...rfc5424.SDParam)    {}
func (m *mockRuntime) Critical(_ string, _ ...rfc5424.SDParam) {}

func (m *mockRuntime) Write(e entry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
	return nil
}

func (m *mockRuntime) NegotiateTag(name string) (entry.EntryTag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tags[name]; ok {
		return t, nil
	}
	m.nextTag++
	m.tags[name] = m.nextTag
	return m.nextTag, nil
}

func (m *mockRuntime) Get(key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.store[key]
	if !ok {
		return nil, storage.ErrStorageNotFound
	}
	return v, nil
}
func (m *mockRuntime) Put(key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = value
	return nil
}
func (m *mockRuntime) GetString(key string) (string, error) {
	v, err := m.Get(key)
	return string(v), err
}
func (m *mockRuntime) PutString(key, value string) error { return m.Put(key, []byte(value)) }
func (m *mockRuntime) GetInt64(_ string) (int64, error)  { return 0, storage.ErrStorageNotFound }
func (m *mockRuntime) PutInt64(_ string, _ int64) error  { return nil }
func (m *mockRuntime) GetTime(key string) (time.Time, error) {
	v, err := m.GetString(key)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, v)
}
func (m *mockRuntime) PutTime(key string, value time.Time) error {
	return m.PutString(key, value.Format(time.RFC3339Nano))
}

// newTestConfig builds a minimal, already-Verify()'d Config pointed at host,
// failing the test immediately if validation doesn't pass.
func newTestConfig(t *testing.T, host string) *Config {
	t.Helper()
	conf := &Config{
		Host:          host,
		Client_Id:     "id",
		Client_Secret: "secret",
	}
	if err := conf.Verify(); err != nil {
		t.Fatalf("unexpected error verifying test config: %v", err)
	}
	return conf
}

// TestHandle_IngestsRecords runs a single Handle cycle against a fake Jamf
// server returning one inventory record, and verifies the resulting entry's
// TS is set from general.reportDate, its Data has the "timestamp" field
// stamped in front of the original fields, and it's written under the
// default tag.
func TestHandle_IngestsRecords(t *testing.T) {
	reportDate := time.Now().Add(-2 * time.Hour).Truncate(time.Second).UTC()
	record := `{"id":1,"general":{"reportDate":"` + reportDate.Format(time.RFC3339Nano) + `"},"udid":"abc"}`

	mux := http.NewServeMux()
	mux.HandleFunc("/api/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, tokenResponse{AccessToken: "tok", ExpiresIn: 3600}, jsoncompat.Options)
	})
	calls := 0
	mux.HandleFunc("/api/v1/computers-inventory", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			json.MarshalWrite(w, Response{
				TotalCount: 1,
				Results:    []jsontext.Value{jsontext.Value(record)},
			}, jsoncompat.Options)
			return
		}
		json.MarshalWrite(w, Response{TotalCount: 0}, jsoncompat.Options)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	conf := newTestConfig(t, server.URL)
	rt := newMockRuntime(t.Context())
	j := New(conf)

	cont, err := j.Handle(t.Context(), rt)
	if err != nil {
		t.Fatal(err)
	}
	if cont == nil {
		t.Fatal("expected a non-nil continuation")
	}

	if len(rt.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(rt.entries))
	}
	e := rt.entries[0]
	if e.TS != entry.FromStandard(reportDate) {
		t.Errorf("expected TS %v, got %v", entry.FromStandard(reportDate), e.TS)
	}
	if !strings.HasPrefix(string(e.Data), `{"timestamp":"`+reportDate.Format(time.RFC3339Nano)+`",`) {
		t.Errorf("expected stamped timestamp prefix, got %s", e.Data)
	}
	if !strings.Contains(string(e.Data), `"id":1`) {
		t.Errorf("expected original fields preserved, got %s", e.Data)
	}

	wantTag, _ := rt.NegotiateTag(defaultTag)
	if e.Tag != wantTag {
		t.Errorf("expected tag %v, got %v", wantTag, e.Tag)
	}
}

// TestHandle_PaginatesUntilExhausted verifies that a single Handle call
// drains every page of results (requesting page 0, 1, then a final empty
// page 2 to stop) and writes an entry for each record found along the way.
func TestHandle_PaginatesUntilExhausted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, tokenResponse{AccessToken: "tok", ExpiresIn: 3600}, jsoncompat.Options)
	})

	var pagesRequested []string
	mux.HandleFunc("/api/v1/computers-inventory", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pagesRequested = append(pagesRequested, page)

		ts := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
		switch page {
		case "0":
			json.MarshalWrite(w, Response{
				TotalCount: 2,
				Results: []jsontext.Value{
					jsontext.Value(`{"id":1,"general":{"reportDate":"` + ts + `"}}`),
				},
			}, jsoncompat.Options)
		case "1":
			json.MarshalWrite(w, Response{
				TotalCount: 2,
				Results: []jsontext.Value{
					jsontext.Value(`{"id":2,"general":{"reportDate":"` + ts + `"}}`),
				},
			}, jsoncompat.Options)
		default:
			json.MarshalWrite(w, Response{TotalCount: 0}, jsoncompat.Options)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	conf := newTestConfig(t, server.URL)
	rt := newMockRuntime(t.Context())
	j := New(conf)

	if _, err := j.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}

	if len(rt.entries) != 2 {
		t.Fatalf("expected 2 entries across pages, got %d", len(rt.entries))
	}
	if want := []string{"0", "1", "2"}; !equalStrings(pagesRequested, want) {
		t.Errorf("expected pages requested %v, got %v", want, pagesRequested)
	}
}

// TestHandle_PersistsWindowEnd checks that after a successful Handle call,
// the trailing edge of the polled window is persisted to Storage under
// stateKeyLastEnd as roughly "now minus the poll buffer", so a restart
// resumes from there instead of re-fetching or dropping data.
func TestHandle_PersistsWindowEnd(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, tokenResponse{AccessToken: "tok", ExpiresIn: 3600}, jsoncompat.Options)
	})
	mux.HandleFunc("/api/v1/computers-inventory", func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, Response{TotalCount: 0}, jsoncompat.Options)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	conf := newTestConfig(t, server.URL)
	rt := newMockRuntime(t.Context())
	j := New(conf)

	before := time.Now()
	if _, err := j.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}

	stored, err := rt.GetTime(stateKeyLastEnd)
	if err != nil {
		t.Fatalf("expected state to be persisted: %v", err)
	}
	// stored end should be "now" (at call time) minus the poll buffer.
	expected := before.Add(-pollBufferSeconds * time.Second)
	if diff := stored.Sub(expected); diff < -5*time.Second || diff > 5*time.Second {
		t.Errorf("expected persisted end near %v, got %v", expected, stored)
	}
}

// TestHandle_NoOpWhenWindowNotYetOpen seeds state so the last processed
// window ends only 5 seconds ago, inside the poll buffer, and asserts
// Handle makes zero HTTP requests and writes zero entries — there's
// nothing new to fetch yet.
func TestHandle_NoOpWhenWindowNotYetOpen(t *testing.T) {
	requests := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		requests++
		json.MarshalWrite(w, tokenResponse{AccessToken: "tok", ExpiresIn: 3600}, jsoncompat.Options)
	})
	mux.HandleFunc("/api/v1/computers-inventory", func(w http.ResponseWriter, r *http.Request) {
		requests++
		json.MarshalWrite(w, Response{TotalCount: 0}, jsoncompat.Options)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	conf := newTestConfig(t, server.URL)
	rt := newMockRuntime(t.Context())
	// Seed state so the window's start is more recent than "now - buffer",
	// meaning there's nothing new to fetch yet.
	if err := rt.PutTime(stateKeyLastEnd, time.Now().Add(-5*time.Second)); err != nil {
		t.Fatal(err)
	}

	j := New(conf)
	cont, err := j.Handle(t.Context(), rt)
	if err != nil {
		t.Fatal(err)
	}
	if cont == nil {
		t.Fatal("expected a non-nil continuation to keep polling")
	}
	if requests != 0 {
		t.Errorf("expected no HTTP requests when window is not open, got %d", requests)
	}
	if len(rt.entries) != 0 {
		t.Errorf("expected no entries written, got %d", len(rt.entries))
	}
}

// TestHandle_SkipsMalformedRecordsButContinues feeds Handle one well-formed
// record alongside one missing general.reportDate and one with an
// unparseable reportDate, and verifies the bad records are logged and
// skipped rather than failing the whole poll cycle.
func TestHandle_SkipsMalformedRecordsButContinues(t *testing.T) {
	goodTS := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, tokenResponse{AccessToken: "tok", ExpiresIn: 3600}, jsoncompat.Options)
	})
	mux.HandleFunc("/api/v1/computers-inventory", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "0" {
			// Only the first page has results; subsequent pages signal
			// the drain loop to stop, matching real API pagination.
			json.MarshalWrite(w, Response{TotalCount: 0}, jsoncompat.Options)
			return
		}
		json.MarshalWrite(w, Response{
			TotalCount: 3,
			Results: []jsontext.Value{
				jsontext.Value(`{"id":1,"general":{"reportDate":"` + goodTS + `"}}`), // good
				jsontext.Value(`{"id":2,"general":{}}`),                              // missing reportDate
				jsontext.Value(`{"id":3,"general":{"reportDate":"not-a-time"}}`),     // bad format
			},
		}, jsoncompat.Options)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	conf := newTestConfig(t, server.URL)
	rt := newMockRuntime(t.Context())
	j := New(conf)

	if _, err := j.Handle(t.Context(), rt); err != nil {
		t.Fatalf("Handle should not fail because of individual bad records: %v", err)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("expected only the well-formed record to be written, got %d entries", len(rt.entries))
	}
	if !strings.Contains(string(rt.entries[0].Data), `"id":1`) {
		t.Errorf("expected the surviving record to be id 1, got %s", rt.entries[0].Data)
	}
}

// equalStrings reports whether a and b contain the same strings in the
// same order.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
