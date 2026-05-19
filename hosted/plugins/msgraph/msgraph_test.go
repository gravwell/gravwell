package msgraph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type mockRuntime struct {
	mu      sync.Mutex
	entries []entry.Entry
	store   map[string][]byte
	tags    map[string]entry.EntryTag
	nextTag entry.EntryTag
	ctx     context.Context
	cancel  context.CancelFunc
}

func newMockRuntime(ctx context.Context) *mockRuntime {
	ctx, cancel := context.WithCancel(ctx)
	return &mockRuntime{entries: []entry.Entry{}, store: map[string][]byte{}, tags: map[string]entry.EntryTag{}, ctx: ctx, cancel: cancel}
}

func (m *mockRuntime) Alive() bool             { return true }
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

func TestPollOnce_IngestsAlerts(t *testing.T) {
	ts := time.Now().Add(-1 * time.Hour).Truncate(time.Second).UTC()
	mux := http.NewServeMux()
	mux.HandleFunc("/tid/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(AuthToken{AccessToken: "t", ExpiresIn: 3600})
	})
	mux.HandleFunc("/v1.0/security/alerts_v2", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ODataResponse{
			Value: []json.RawMessage{json.RawMessage(`{"id":"a1","createdDateTime":"` + ts.Format(time.RFC3339) + `"}`)},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conf := &Config{Tenant_ID: "tid", Client_ID: "cid", Client_Secret: "s", Content_Type: []ContentType{ContentAlerts}, Lookback: 24, Requests_Per_Minute: 60, Request_Interval: 1, Graph_Host: srv.URL, Auth_Host: srv.URL}
	conf.Verify()
	rt := newMockRuntime(t.Context())
	mg := NewIngester(conf)
	mg.client = NewClient(srv.URL, srv.URL, "tid", "cid", "s", srv.Client())

	tag, _ := rt.NegotiateTag("msgraph-alerts")
	if err := mg.pollOnce(t.Context(), rt, ContentAlerts, tag); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("expected 1, got %d", len(rt.entries))
	}
	if rt.entries[0].TS != entry.FromStandard(ts) {
		t.Errorf("wrong TS: %v", rt.entries[0].TS)
	}
	stored, _ := rt.GetTime(TimestampKey(ContentAlerts))
	if !stored.Equal(ts) {
		t.Errorf("expected stored %v, got %v", ts, stored)
	}
}

func TestPollOnce_PersistsNextLink(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/tid/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(AuthToken{AccessToken: "t", ExpiresIn: 3600})
	})
	mux.HandleFunc("/v1.0/security/alerts_v2", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ODataResponse{
			Value:    []json.RawMessage{json.RawMessage(`{"id":"a","createdDateTime":"2026-05-14T10:00:00Z"}`)},
			NextLink: "http://example.com/next",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conf := &Config{Tenant_ID: "tid", Client_ID: "cid", Client_Secret: "s", Content_Type: []ContentType{ContentAlerts}, Lookback: 24, Requests_Per_Minute: 60, Request_Interval: 1, Graph_Host: srv.URL, Auth_Host: srv.URL}
	conf.Verify()
	rt := newMockRuntime(t.Context())
	mg := NewIngester(conf)
	mg.client = NewClient(srv.URL, srv.URL, "tid", "cid", "s", srv.Client())

	tag, _ := rt.NegotiateTag("msgraph-alerts")
	mg.pollOnce(t.Context(), rt, ContentAlerts, tag)

	nl, _ := rt.GetString(NextLinkKey(ContentAlerts))
	if nl != "http://example.com/next" {
		t.Errorf("expected nextLink persisted, got %q", nl)
	}
	_, err := rt.GetTime(TimestampKey(ContentAlerts))
	if err == nil {
		t.Error("timestamp should not advance when nextLink present")
	}
}

func TestConfig_Verify(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "valid", cfg: Config{Tenant_ID: "t", Client_ID: "c", Client_Secret: "s", Content_Type: []ContentType{ContentAlerts}}},
		{name: "no tenant", cfg: Config{Client_ID: "c", Client_Secret: "s", Content_Type: []ContentType{ContentAlerts}}, wantErr: true},
		{name: "no client", cfg: Config{Tenant_ID: "t", Client_Secret: "s", Content_Type: []ContentType{ContentAlerts}}, wantErr: true},
		{name: "no secret", cfg: Config{Tenant_ID: "t", Client_ID: "c", Content_Type: []ContentType{ContentAlerts}}, wantErr: true},
		{name: "no types", cfg: Config{Tenant_ID: "t", Client_ID: "c", Client_Secret: "s"}, wantErr: true},
		{name: "bad type", cfg: Config{Tenant_ID: "t", Client_ID: "c", Client_Secret: "s", Content_Type: []ContentType{"x"}}, wantErr: true},
		{name: "tag+multi", cfg: Config{Tenant_ID: "t", Client_ID: "c", Client_Secret: "s", Content_Type: []ContentType{ContentAlerts, ContentSecureScores}, Tag_Name: "x"}, wantErr: true},
		{name: "tag+prefix", cfg: Config{Tenant_ID: "t", Client_ID: "c", Client_Secret: "s", Content_Type: []ContentType{ContentAlerts}, Tag_Name: "x", Tag_Prefix: "y"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Verify(); (err != nil) != tt.wantErr {
				t.Errorf("got err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
