package microsoft

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"golang.org/x/time/rate"
)

type microsoftTestRuntime struct {
	*hosted.Mock
	writes   atomic.Int64
	failAt   atomic.Int64
	tagMu    sync.Mutex
	tagNames map[entry.EntryTag]string
	logMu    sync.Mutex
	logs     []capturedLog
	// warnState records what the plugin published on the status channel the
	// poll loop leaves alone. hosted/job.go clears the error channel after
	// every poll that returns none, so a condition an operator has to see
	// survives only here.
	warnState []string
}

// capturedLog records what the plugin reported to the operator. hosted.Mock
// discards log calls, so tests that assert on operator-visible diagnostics need
// their own capture.
type capturedLog struct {
	Level   string
	Message string
	Params  map[string]string
}

func (r *microsoftTestRuntime) record(level, message string, params []rfc5424.SDParam) {
	captured := capturedLog{Level: level, Message: message, Params: make(map[string]string, len(params))}
	for _, param := range params {
		captured.Params[param.Name] = param.Value
	}
	r.logMu.Lock()
	r.logs = append(r.logs, captured)
	r.logMu.Unlock()
}

func (r *microsoftTestRuntime) Warn(message string, params ...rfc5424.SDParam) {
	r.record("WARN", message, params)
}

func (r *microsoftTestRuntime) Error(message string, params ...rfc5424.SDParam) {
	r.record("ERROR", message, params)
}

func (r *microsoftTestRuntime) SetWarn(message string) {
	r.logMu.Lock()
	r.warnState = append(r.warnState, message)
	r.logMu.Unlock()
	r.Mock.SetWarn(message)
}

// warnStateContaining returns every published warning state containing
// fragment.
func (r *microsoftTestRuntime) warnStateContaining(fragment string) []string {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	var matched []string
	for _, warning := range r.warnState {
		if strings.Contains(warning, fragment) {
			matched = append(matched, warning)
		}
	}
	return matched
}

// clearCapturedReports discards everything the plugin has published on the log
// and status channels, the way the ingest-link recovery path's ClearWarn() and
// hosted/job.go's per-poll ClearError() discard them, so a test can show what
// survives that and what does not.
func (r *microsoftTestRuntime) clearCapturedReports() {
	r.logMu.Lock()
	r.logs, r.warnState = nil, nil
	r.logMu.Unlock()
}

// logsContaining returns every captured line at level whose message contains
// fragment.
func (r *microsoftTestRuntime) logsContaining(level, fragment string) []capturedLog {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	var matched []capturedLog
	for _, line := range r.logs {
		if line.Level == level && strings.Contains(line.Message, fragment) {
			matched = append(matched, line)
		}
	}
	return matched
}

func newMicrosoftTestRuntime(t *testing.T) *microsoftTestRuntime {
	t.Helper()
	return &microsoftTestRuntime{Mock: hosted.NewMock(t.Context()), tagNames: make(map[entry.EntryTag]string)}
}

func (r *microsoftTestRuntime) NegotiateTag(name string) (entry.EntryTag, error) {
	tag, err := r.Mock.NegotiateTag(name)
	if err == nil {
		r.tagMu.Lock()
		r.tagNames[tag] = name
		r.tagMu.Unlock()
	}
	return tag, err
}

func (r *microsoftTestRuntime) tagName(tag entry.EntryTag) string {
	r.tagMu.Lock()
	defer r.tagMu.Unlock()
	return r.tagNames[tag]
}

func (r *microsoftTestRuntime) Write(ent entry.Entry) error {
	call := r.writes.Add(1)
	if failAt := r.failAt.Load(); failAt > 0 && call == failAt {
		return errors.New("synthetic write failure")
	}
	return r.Mock.Write(ent)
}

func validTestConfig(t *testing.T, api ...string) *Config {
	t.Helper()
	c := &Config{
		Tenant_ID:     "11111111-1111-1111-1111-111111111111",
		Client_ID:     "22222222-2222-2222-2222-222222222222",
		Client_Secret: "synthetic-client-secret",
		Api:           api,
	}
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	}
	return c
}

func newMicrosoftTestPlugin(t *testing.T, server *httptest.Server, runtime hosted.Runtime) *Microsoft {
	t.Helper()
	conf := validTestConfig(t, "entra-users")
	conf.Graph_Host, conf.ARM_Host, conf.Defender_Host, conf.PowerBI_Host, conf.Auth_Host = server.URL, server.URL, server.URL, server.URL, server.URL
	conf.Defender_XDR_Host = server.URL
	p := New(conf)
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	p.client = client
	p.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
	return p
}

func TestHandleUsesStandardWriteAndPersistsSimpleState(t *testing.T) {
	var dataCalls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
			return
		}
		dataCalls++
		_, _ = w.Write([]byte(`{"value":[{"id":"user-1","displayName":"One"}]}`))
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("entries=%d, want 1", got)
	}
	if got := rt.writes.Load(); got != 1 {
		t.Fatalf("writes=%d, want 1", got)
	}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("snapshot entries=%d, want 1 before Request-Interval", got)
	}
	if dataCalls != 1 {
		t.Fatalf("data calls=%d, want 1 before Request-Interval", dataCalls)
	}
	if _, err := rt.GetTime("microsoft/entra-users/timestamp"); err != nil {
		t.Fatal(err)
	}
	state, err := loadState(rt, "microsoft/entra-users")
	if err != nil {
		t.Fatal(err)
	}
	if state.Continuation != nil {
		t.Fatalf("state=%+v", state)
	}
}

func TestStandardHostedRuntimeCanCollect(t *testing.T) {
	var dataCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
			return
		}
		dataCalls.Add(1)
		_, _ = w.Write([]byte(`{"value":[]}`))
	}))
	defer server.Close()
	rt := hosted.NewMock(t.Context())
	p := newMicrosoftTestPlugin(t, server, rt)
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := dataCalls.Load(); got != 1 {
		t.Fatalf("data calls=%d, want one fetch through the standard Runtime", got)
	}
}

func TestWriteFailureAfterPartialPageDoesNotAdvanceStateOnRestart(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
			return
		}
		_, _ = w.Write([]byte(`{"value":[
			{"id":"event-1","activityDateTime":"2026-10-07T11:10:00Z"},
			{"id":"event-2","activityDateTime":"2026-10-07T11:11:00Z"}
		]}`))
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	rt.failAt.Store(2)
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"entra-directory-audits"}
	if _, err := p.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "write failure") {
		t.Fatalf("error=%v, want write failure", err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("accepted entries before failure=%d, want 1", got)
	}
	for _, key := range []string{"microsoft/entra-directory-audits/timestamp", "microsoft/entra-directory-audits/cursor", "microsoft/entra-directory-audits/overlap"} {
		if _, err := rt.Get(key); !errors.Is(err, storage.ErrStorageNotFound) {
			t.Fatalf("%s state error=%v, want unchanged checkpoint", key, err)
		}
	}

	// Recreate the plugin against the same state store. The first record may be
	// delivered again (at-least-once), but neither source record may be skipped.
	p = newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"entra-directory-audits"}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 3 {
		t.Fatalf("entries after restart=%d, want first replay plus both source records", got)
	}
	watermark, err := rt.GetTime("microsoft/entra-directory-audits/timestamp")
	if err != nil || watermark.IsZero() {
		t.Fatalf("completed watermark=%s error=%v", watermark, err)
	}
}

func TestWriteFailureDoesNotAdvanceState(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
			return
		}
		_, _ = w.Write([]byte(`{"value":[{"id":"user-1"}]}`))
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	rt.WriteErrs = 1
	if _, err := p.Handle(t.Context(), rt); err == nil {
		t.Fatal("expected write error")
	}
	for _, key := range []string{"microsoft/entra-users/timestamp", "microsoft/entra-users/cursor", "microsoft/entra-users/fabric"} {
		if _, err := rt.Get(key); !errors.Is(err, storage.ErrStorageNotFound) {
			t.Fatalf("%s state error=%v", key, err)
		}
	}
}

func TestStoredContinuationResumesAfterPluginRecreation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
			return
		}
		if r.URL.Path != "/page2" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"value":[{"id":"user-2","displayName":"Two"}]}`))
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	now := p.now()
	state := streamState{
		WindowStart: now.Add(-time.Hour), WindowEnd: now,
		Continuation: &FetchContinuation{Version: fetchContinuationVersion, Kind: KindODataGET, NextURL: server.URL + "/page2"},
	}
	if err := saveState(rt, "microsoft/entra-users", state); err != nil {
		t.Fatal(err)
	}
	p = newMicrosoftTestPlugin(t, server, rt)
	if continuation, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	} else if continuation == nil {
		t.Fatal("missing polling continuation")
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("resumed entries=%d, want 1", got)
	}
	saved, err := loadState(rt, "microsoft/entra-users")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Continuation != nil || saved.Watermark.IsZero() {
		t.Fatalf("saved state=%+v", saved)
	}
}

func TestEmptyTemporalPollUsesFinalizedHalfOpenWindow(t *testing.T) {
	dataset := datasets["entra-directory-audits"]
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	start, end, ready := finalizedWindow(dataset, time.Time{}, now, time.Hour)
	if !ready || !start.Equal(now.Add(-sourceConsistencyDelay-time.Hour)) || !end.Equal(now.Add(-sourceConsistencyDelay)) {
		t.Fatalf("window=[%s,%s) ready=%t", start, end, ready)
	}
}

// The ingest muxer fills SRC only for entries that arrive without one
// (ingest/muxer.go). A plugin that pins an address replaces the real ingest
// source on every record it writes, so every emitted entry must leave SRC unset.
func TestHandleLeavesEntrySourceToTheIngestMuxer(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
			return
		}
		_, _ = w.Write([]byte(`{"value":[{"id":"user-1"},{"id":"user-2"}]}`))
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	entries := rt.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries=%d, want 2", len(entries))
	}
	for i, ent := range entries {
		if len(ent.SRC) != 0 {
			t.Fatalf("entry %d has SRC %v; the plugin must not pin an ingest source", i, ent.SRC)
		}
		if ent.EVCount() != 1 {
			t.Fatalf("entry %d carries %d enumerated values, want 1", i, ent.EVCount())
		}
		if source, ok := ent.GetEnumeratedValue("_source"); !ok || source != "entra-users" {
			t.Fatalf("entry %d _source=%v present=%v", i, source, ok)
		}
	}
}
