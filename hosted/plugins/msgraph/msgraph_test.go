package msgraph

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/utils/jsoncompat"
)

func TestPollOnce_IngestsAlerts(t *testing.T) {
	ts := time.Now().Add(-1 * time.Hour).Truncate(time.Second).UTC()
	mux := http.NewServeMux()
	mux.HandleFunc("/tid/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, AuthToken{AccessToken: "t", ExpiresIn: 3600}, jsoncompat.Options)
	})
	mux.HandleFunc("/v1.0/security/alerts_v2", func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, ODataResponse{
			Value: []jsontext.Value{jsontext.Value(`{"id":"a1","createdDateTime":"` + ts.Format(time.RFC3339) + `"}`)},
		}, jsoncompat.Options)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conf := &Config{Tenant_ID: "tid", Client_ID: "cid", Client_Secret: "s", Content_Type: []ContentType{ContentAlerts}, Lookback: 24, Requests_Per_Minute: 60, Request_Interval: 1, Graph_Host: srv.URL, Auth_Host: srv.URL}
	conf.Verify()
	rt := hosted.NewMock(t.Context())
	mg := NewIngester(conf)
	mg.client = NewClient(srv.URL, srv.URL, "tid", "cid", "s", srv.Client())

	tag, _ := rt.NegotiateTag("msgraph-alerts")
	if err := mg.pollOnce(t.Context(), rt, ContentAlerts, tag); err != nil {
		t.Fatal(err)
	}
	entries := rt.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1, got %d", len(entries))
	}
	if entries[0].TS != entry.FromStandard(ts) {
		t.Errorf("wrong TS: %v", entries[0].TS)
	}
	stored, _ := rt.GetTime(TimestampKey(ContentAlerts))
	if !stored.Equal(ts) {
		t.Errorf("expected stored %v, got %v", ts, stored)
	}
}

func TestPollOnce_PersistsNextLink(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/tid/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, AuthToken{AccessToken: "t", ExpiresIn: 3600}, jsoncompat.Options)
	})
	mux.HandleFunc("/v1.0/security/alerts_v2", func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, ODataResponse{
			Value:    []jsontext.Value{jsontext.Value(`{"id":"a","createdDateTime":"2026-05-14T10:00:00Z"}`)},
			NextLink: "http://example.com/next",
		}, jsoncompat.Options)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conf := &Config{Tenant_ID: "tid", Client_ID: "cid", Client_Secret: "s", Content_Type: []ContentType{ContentAlerts}, Lookback: 24, Requests_Per_Minute: 60, Request_Interval: 1, Graph_Host: srv.URL, Auth_Host: srv.URL}
	conf.Verify()
	rt := hosted.NewMock(t.Context())
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

// TestPollOnce_DeduplicatesSubSecondTimestamps is a regression test for the dedup bug where
// alerts with sub-second createdDateTime timestamps were re-ingested on every poll cycle.
// The OData filter must be expressed with sufficient precision to exclude an already-seen
// alert on the next poll — second precision truncation causes the filter to be too broad.
func TestPollOnce_DeduplicatesSubSecondTimestamps(t *testing.T) {
	// Sub-second precision is the trigger for the bug. Use a recent timestamp so it
	// falls within the lookback window, with an explicit .500Z sub-second component.
	alertTS := time.Now().Add(-time.Hour).Truncate(time.Second).Add(500 * time.Millisecond).UTC()
	alertJSON := `{"id":"a1","createdDateTime":"` + alertTS.Format("2006-01-02T15:04:05.000Z") + `"}`

	mux := http.NewServeMux()
	mux.HandleFunc("/tid/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, AuthToken{AccessToken: "t", ExpiresIn: 3600}, jsoncompat.Options)
	})
	mux.HandleFunc("/v1.0/security/alerts_v2", func(w http.ResponseWriter, r *http.Request) {
		filterStr := r.URL.Query().Get("$filter")
		// Parse "createdDateTime gt <timestamp>" and evaluate against alertTS.
		// Try both millisecond and second precision to handle either version of ODataTimeFormat.
		var filterTS time.Time
		if after, ok := strings.CutPrefix(filterStr, "createdDateTime gt "); ok {
			for _, layout := range []string{"2006-01-02T15:04:05.000Z", "2006-01-02T15:04:05Z", time.RFC3339} {
				if parsed, err := time.Parse(layout, after); err == nil {
					filterTS = parsed
					break
				}
			}
		}
		// Return the alert only if it satisfies createdDateTime gt filterTS.
		if filterTS.IsZero() || alertTS.After(filterTS) {
			json.MarshalWrite(w, ODataResponse{Value: []jsontext.Value{jsontext.Value(alertJSON)}}, jsoncompat.Options)
		} else {
			json.MarshalWrite(w, ODataResponse{}, jsoncompat.Options)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conf := &Config{
		Tenant_ID: "tid", Client_ID: "cid", Client_Secret: "s",
		Content_Type:        []ContentType{ContentAlerts},
		Lookback:            24,
		Requests_Per_Minute: 60,
		Request_Interval:    1,
		Graph_Host:          srv.URL,
		Auth_Host:           srv.URL,
	}
	conf.Verify()
	rt := hosted.NewMock(t.Context())
	mg := NewIngester(conf)
	mg.client = NewClient(srv.URL, srv.URL, "tid", "cid", "s", srv.Client())
	tag, _ := rt.NegotiateTag("msgraph-alerts")

	// First poll: alert should be ingested.
	if err := mg.pollOnce(t.Context(), rt, ContentAlerts, tag); err != nil {
		t.Fatal(err)
	}
	if len(rt.Entries()) != 1 {
		t.Fatalf("first poll: expected 1 entry, got %d", len(rt.Entries()))
	}

	// Second poll: alert must NOT be re-ingested.
	// With second-precision ODataTimeFormat the filter is too broad and the alert
	// satisfies it again every cycle — len(rt.Entries()) would be 2, not 1.
	if err := mg.pollOnce(t.Context(), rt, ContentAlerts, tag); err != nil {
		t.Fatal(err)
	}
	if len(rt.Entries()) != 1 {
		t.Fatalf("second poll: expected 1 entry total (no duplicate), got %d", len(rt.Entries()))
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
		{name: "negative lookback", cfg: Config{Tenant_ID: "t", Client_ID: "c", Client_Secret: "s", Content_Type: []ContentType{ContentAlerts}, Lookback: -1}, wantErr: true},
		{name: "negative requests per min", cfg: Config{Tenant_ID: "t", Client_ID: "c", Client_Secret: "s", Content_Type: []ContentType{ContentAlerts}, Requests_Per_Minute: -1}, wantErr: true},
		{name: "negative request interval", cfg: Config{Tenant_ID: "t", Client_ID: "c", Client_Secret: "s", Content_Type: []ContentType{ContentAlerts}, Request_Interval: -1}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Verify(); (err != nil) != tt.wantErr {
				t.Errorf("got err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
