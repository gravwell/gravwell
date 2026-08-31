/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package wiz

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/utils/jsoncompat"
)

// wizServer is an httptest server that speaks OAuth and answers the three
// built-in event queries with a single-page connection each.
type wizServer struct {
	server *httptest.Server
	nodeTS time.Time

	mu     sync.Mutex
	calls  map[string]int
	errFor map[string][]GraphQLError
}

func newWizServer(t *testing.T) *wizServer {
	t.Helper()
	s := &wizServer{
		nodeTS: time.Now().Add(-time.Hour).Truncate(time.Second),
		calls:  make(map[string]int),
		errFor: make(map[string][]GraphQLError),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.MarshalWrite(w, AuthToken{AccessToken: "tok", ExpireIn: 3600}, jsoncompat.Options)
	})
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		if err := json.UnmarshalRead(r.Body, &req, jsoncompat.Options); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		field := detectField(req.Query)
		if field == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		s.mu.Lock()
		s.calls[field]++
		errs := s.errFor[field]
		s.mu.Unlock()

		if len(errs) > 0 {
			_ = json.MarshalWrite(w, graphQLResponse{Errors: errs}, jsoncompat.Options)
			return
		}
		_ = json.MarshalWrite(w, graphQLResponse{Data: mustJSON(t, map[string]connOut{
			field: {Nodes: []jsontext.Value{s.nodeFor(t, field)}, PageInfo: pageInfoOut{HasNextPage: false}},
		})}, jsoncompat.Options)
	})
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func (s *wizServer) callCount(field string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[field]
}

func (s *wizServer) setError(field string, errs []GraphQLError) {
	s.mu.Lock()
	s.errFor[field] = errs
	s.mu.Unlock()
}

// nodeFor returns a record for the given root field with the timestamp field
// that field's built-in query selects.
func (s *wizServer) nodeFor(t *testing.T, field string) jsontext.Value {
	ts := s.nodeTS.Format(time.RFC3339Nano)
	// each record carries the timestamp field that source uses as its cursor.
	switch field {
	case "vulnerabilityFindings":
		return mustJSON(t, map[string]any{"id": "v1", "severity": "HIGH", "updatedAt": ts})
	case "issues":
		return mustJSON(t, map[string]any{"id": "i1", "status": "OPEN", "createdAt": ts})
	case "detections":
		return mustJSON(t, map[string]any{"id": "d1", "severity": "HIGH", "updatedAt": ts})
	case "configurationFindings":
		return mustJSON(t, map[string]any{"id": "c1", "result": "FAIL", "updatedAt": ts})
	case "auditLogEntries":
		return mustJSON(t, map[string]any{"id": "a1", "action": "Login", "timestamp": ts})
	default:
		return mustJSON(t, map[string]any{"id": "x"})
	}
}

func detectField(query string) string {
	switch {
	case strings.Contains(query, "auditLogEntries"):
		return "auditLogEntries"
	case strings.Contains(query, "vulnerabilityFindings"):
		return "vulnerabilityFindings"
	case strings.Contains(query, "detections"):
		return "detections"
	case strings.Contains(query, "configurationFindings"):
		return "configurationFindings"
	case strings.Contains(query, "issues"):
		return "issues"
	default:
		return ""
	}
}

type connOut struct {
	Nodes    []jsontext.Value `json:"nodes"`
	PageInfo pageInfoOut      `json:"pageInfo"`
}

type pageInfoOut struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

func mustJSON(t *testing.T, v any) jsontext.Value {
	t.Helper()
	b, err := json.Marshal(v, jsoncompat.Options)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newTestConfig(t *testing.T, s *wizServer) *Config {
	t.Helper()
	c := &Config{
		Client_Id:     "id",
		Client_Secret: "secret",
		Endpoint:      "https://api.us1.app.wiz.io/graphql", // passes Verify
		Auth_URL:      "https://auth.app.wiz.io/oauth/token",
		Tag_Name:      "wiz",
	}
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	}
	// point at the test server after validation.
	c.Endpoint = s.server.URL + "/graphql"
	c.Auth_URL = s.server.URL + "/oauth/token"
	return c
}

func TestHandlePullsAllSources(t *testing.T) {
	s := newWizServer(t)
	cfg := newTestConfig(t, s)

	rt := newTestRuntime(t.Context())
	w := New(cfg)

	allFields := []string{"vulnerabilityFindings", "issues", "detections", "configurationFindings", "auditLogEntries"}

	if _, err := w.Handle(t.Context(), rt); err != nil {
		t.Fatalf("handle: %v", err)
	}

	// one node per source on the first (snapshot) scan.
	if got := rt.entryCount(); got != len(allFields) {
		t.Fatalf("wrote %d entries, want %d", got, len(allFields))
	}
	for _, field := range allFields {
		if s.callCount(field) != 1 {
			t.Errorf("%s queried %d times, want 1", field, s.callCount(field))
		}
	}

	// every entry carries the "type" EV naming its source query.
	fields := map[string]bool{}
	for _, e := range rt.snapshot() {
		val, ok := e.GetEnumeratedValue(typeEVName)
		if !ok {
			t.Errorf("entry missing %q EV", typeEVName)
			continue
		}
		fields[val.(string)] = true
	}
	for _, field := range allFields {
		if !fields[field] {
			t.Errorf("no entry tagged with type=%s", field)
		}
	}

	// second cycle: nothing newer than the stored high water mark, so no new
	// entries even though the sources are queried again.
	if _, err := w.Handle(t.Context(), rt); err != nil {
		t.Fatalf("handle 2: %v", err)
	}
	if got := rt.entryCount(); got != len(allFields) {
		t.Fatalf("second cycle total entries = %d, want %d (no new data)", got, len(allFields))
	}
}

func TestHandleTagOverride(t *testing.T) {
	s := newWizServer(t)
	cfg := newTestConfig(t, s)
	cfg.Tag_Override = []string{"Audit:wiz-audit"}
	if err := cfg.parseTagOverrides(); err != nil {
		t.Fatal(err)
	}

	rt := newTestRuntime(t.Context())
	w := New(cfg)
	if _, err := w.Handle(t.Context(), rt); err != nil {
		t.Fatalf("handle: %v", err)
	}

	auditTag, ok := rt.tagFor("wiz-audit")
	if !ok {
		t.Fatal("expected wiz-audit override tag to be negotiated")
	}
	defaultTag, _ := rt.tagFor("wiz")
	for _, e := range rt.snapshot() {
		val, _ := e.GetEnumeratedValue(typeEVName)
		if val == "auditLogEntries" {
			if e.Tag != auditTag {
				t.Errorf("audit entry tag = %d, want override %d", e.Tag, auditTag)
			}
		} else if e.Tag != defaultTag {
			t.Errorf("non-audit entry tag = %d, want default %d", e.Tag, defaultTag)
		}
	}
}

func TestHandleAccessDeniedIgnored(t *testing.T) {
	s := newWizServer(t)
	s.setError("auditLogEntries", []GraphQLError{{
		Message: "access denied, missing permission, huge payload here",
		Extensions: struct {
			Code string `json:"code"`
		}{Code: "FORBIDDEN"},
	}})
	cfg := newTestConfig(t, s)

	rt := newTestRuntime(t.Context())
	w := New(cfg)
	if _, err := w.Handle(t.Context(), rt); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !w.isIgnored(sourceAudit) {
		t.Fatal("audit source should be quarantined after access denied")
	}
	// the other sources still ingested.
	if got := rt.entryCount(); got != 4 {
		t.Fatalf("wrote %d entries, want 4 (audit denied)", got)
	}

	// second cycle must not re-query the denied source.
	if _, err := w.Handle(t.Context(), rt); err != nil {
		t.Fatalf("handle 2: %v", err)
	}
	if s.callCount("auditLogEntries") != 1 {
		t.Fatalf("audit queried %d times, want 1 (quarantined)", s.callCount("auditLogEntries"))
	}
}

func TestHandleInternalErrorRetries(t *testing.T) {
	s := newWizServer(t)
	s.setError("issues", []GraphQLError{{Message: "oops! an internal error occurred ref 999"}})
	cfg := newTestConfig(t, s)

	rt := newTestRuntime(t.Context())
	w := New(cfg)
	if _, err := w.Handle(t.Context(), rt); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if w.isIgnored(sourceIssue) {
		t.Fatal("issues should NOT be ignored after a transient internal error")
	}
	if _, err := w.Handle(t.Context(), rt); err != nil {
		t.Fatalf("handle 2: %v", err)
	}
	if s.callCount("issues") != 2 {
		t.Fatalf("issues queried %d times, want 2 (retried)", s.callCount("issues"))
	}
}

func TestHandleQueryErrorQuarantined(t *testing.T) {
	s := newWizServer(t)
	s.setError("vulnerabilityFindings", []GraphQLError{{Message: "unknown field frobnicate on VulnerabilityFinding"}})
	cfg := newTestConfig(t, s)

	rt := newTestRuntime(t.Context())
	w := New(cfg)
	if _, err := w.Handle(t.Context(), rt); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !w.isIgnored(sourceVulnerability) {
		t.Fatal("a deterministic query error should quarantine the source")
	}
	if _, err := w.Handle(t.Context(), rt); err != nil {
		t.Fatalf("handle 2: %v", err)
	}
	if s.callCount("vulnerabilityFindings") != 1 {
		t.Fatalf("vuln queried %d times, want 1 (quarantined)", s.callCount("vulnerabilityFindings"))
	}
}

func TestCleanNodeStripsNulls(t *testing.T) {
	node := jsontext.Value(`{
		"id": "abc",
		"tags": null,
		"name": "prod",
		"nested": {"a": 1, "b": null},
		"list": [{"x": null, "y": 2}, null, "keep"],
		"bignum": 123456789012345678,
		"emptyObj": {},
		"emptyArr": [],
		"allNull": {"z": null}
	}`)

	cleaned, obj := cleanNode(node)

	if _, ok := obj["tags"]; ok {
		t.Error("tags: null should have been stripped")
	}
	if obj["id"] != "abc" || obj["name"] != "prod" {
		t.Errorf("non-null fields lost: %v", obj)
	}
	for _, key := range []string{"emptyObj", "emptyArr", "allNull"} {
		if _, ok := obj[key]; ok {
			t.Errorf("%s should have been pruned as empty", key)
		}
	}
	if list, ok := obj["list"].([]any); !ok || len(list) != 2 {
		t.Errorf("list = %v, want 2 elements (null dropped)", obj["list"])
	}
	if !strings.Contains(string(cleaned), "123456789012345678") {
		t.Errorf("large integer not preserved: %s", cleaned)
	}
	if strings.Contains(string(cleaned), "null") {
		t.Errorf("cleaned data still contains null: %s", cleaned)
	}
}

func TestExtractTimestamp(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := extractTimestamp(mustJSON(t, map[string]any{"id": "x", "createdAt": ts.Format(time.RFC3339)})); !got.Equal(ts) {
		t.Errorf("extractTimestamp = %v, want %v", got, ts)
	}
	newer := ts.Add(time.Hour)
	node := mustJSON(t, map[string]any{"createdAt": ts.Format(time.RFC3339), "updatedAt": newer.Format(time.RFC3339)})
	if got := extractTimestamp(node); !got.Equal(newer) {
		t.Errorf("extractTimestamp = %v, want %v (updatedAt priority)", got, newer)
	}
	if got := extractTimestamp(mustJSON(t, map[string]any{"id": "x"})); !got.IsZero() {
		t.Errorf("extractTimestamp = %v, want zero", got)
	}
}
