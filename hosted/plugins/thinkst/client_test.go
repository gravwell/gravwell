/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package thinkst

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetIncidents(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"cursor": {"next": "abc123", "next_link": "https://example/next"},
			"incidents": [{"updated_id": 5, "updated_std": "2026-01-01 00:00:00 UTC+0000"}]
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	resp, err := client.GetIncidents(t.Context(), "3", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Incidents) != 1 {
		t.Fatalf("got %d incidents, want 1", len(resp.Incidents))
	}
	if resp.Cursor.NextLink == nil {
		t.Errorf("expected next_link to be set")
	}
	if gotQuery == "" || !containsParam(gotQuery, "incidents_since=3") {
		t.Errorf("query %q missing incidents_since=3", gotQuery)
	}
	if !containsParam(gotQuery, "auth_token=token") {
		t.Errorf("query %q missing auth_token", gotQuery)
	}
}

func TestGetIncidentsPrefersCursor(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"cursor": {}, "incidents": []}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	if _, err := client.GetIncidents(t.Context(), "3", "next-page-token"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsParam(gotQuery, "incidents_since") {
		t.Errorf("query %q should not contain incidents_since when a cursor is set", gotQuery)
	}
	if !containsParam(gotQuery, "cursor=next-page-token") {
		t.Errorf("query %q missing cursor", gotQuery)
	}
	// The Canary API rejects limit+cursor together with a 400, so limit must
	// be dropped once paginating via cursor.
	if containsParam(gotQuery, "limit=100") {
		t.Errorf("query %q should not contain limit once a cursor is set", gotQuery)
	}
}

func TestGetIncidentsIncludesLimitOnFirstPage(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"cursor": {}, "incidents": []}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	if _, err := client.GetIncidents(t.Context(), "", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsParam(gotQuery, "limit=100") {
		t.Errorf("query %q missing limit on the initial request", gotQuery)
	}
}

func TestGetAuditTrail(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{
			"cursor": {"next": null},
			"audit_trail": [{"timestamp": "2026-01-01 00:00:00 UTC+0000"}]
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	resp, err := client.GetAuditTrail(t.Context(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.AuditTrail) != 1 {
		t.Fatalf("got %d audit records, want 1", len(resp.AuditTrail))
	}
	if resp.Cursor.Next != nil {
		t.Errorf("expected nil cursor, got %v", resp.Cursor.Next)
	}
	if !containsParam(gotQuery, "limit=100") {
		t.Errorf("query %q missing limit on the initial request", gotQuery)
	}
}

func TestGetAuditTrailOmitsLimitWithCursor(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"cursor": {"next": null}, "audit_trail": []}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	if _, err := client.GetAuditTrail(t.Context(), "next-page-token"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsParam(gotQuery, "cursor=next-page-token") {
		t.Errorf("query %q missing cursor", gotQuery)
	}
	// The audit_trail/fetch docs state limit "Cannot be used with a cursor".
	if containsParam(gotQuery, "limit=100") {
		t.Errorf("query %q should not contain limit once a cursor is set", gotQuery)
	}
}

func TestGetIncidentsBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	if _, err := client.GetIncidents(t.Context(), "", ""); err == nil {
		t.Errorf("expected error on bad status, got nil")
	}
}

func TestGetIncidentsBadStatusDoesNotRetry(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	if _, err := client.GetIncidents(t.Context(), "", ""); err == nil {
		t.Fatal("expected error on bad status, got nil")
	}
	if calls != 1 {
		t.Errorf("got %d calls, want 1 (a clean 4xx should not be retried)", calls)
	}
}

// flakyDoer simulates transport-level errors and/or a response body that
// fails partway through reading (mirroring an HTTP/2 GOAWAY arriving
// mid-stream) for the first N calls, then serves a valid response.
type flakyDoer struct {
	transportFailures int // number of leading calls that fail at Do()
	bodyReadFailures  int // number of calls after that whose body errors on Read()
	successBody       string

	calls int
}

type erroringBody struct{ err error }

func (e *erroringBody) Read([]byte) (int, error) { return 0, e.err }
func (e *erroringBody) Close() error             { return nil }

func (f *flakyDoer) Do(*http.Request) (*http.Response, error) {
	f.calls++
	if f.calls <= f.transportFailures {
		return nil, fmt.Errorf("simulated transport error")
	}
	if f.calls <= f.transportFailures+f.bodyReadFailures {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       &erroringBody{err: fmt.Errorf("http2: server sent GOAWAY and closed the connection")},
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(f.successBody)),
	}, nil
}

func withShortRetryDelay(t *testing.T) {
	orig := readRetryDelay
	readRetryDelay = time.Millisecond
	t.Cleanup(func() { readRetryDelay = orig })
}

func TestGetIncidentsRetriesOnTransportError(t *testing.T) {
	withShortRetryDelay(t)
	d := &flakyDoer{transportFailures: 1, successBody: `{"cursor": {}, "incidents": []}`}
	client := NewClient("https://example.canary.tools", "token", d)

	if _, err := client.GetIncidents(t.Context(), "", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.calls != 2 {
		t.Errorf("got %d calls, want 2 (one failure, one success)", d.calls)
	}
}

func TestGetIncidentsRetriesOnBodyReadError(t *testing.T) {
	withShortRetryDelay(t)
	d := &flakyDoer{bodyReadFailures: 1, successBody: `{"cursor": {}, "incidents": []}`}
	client := NewClient("https://example.canary.tools", "token", d)

	if _, err := client.GetIncidents(t.Context(), "", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.calls != 2 {
		t.Errorf("got %d calls, want 2 (one body-read failure, one success)", d.calls)
	}
}

func TestGetIncidentsGivesUpAfterMaxAttempts(t *testing.T) {
	withShortRetryDelay(t)
	d := &flakyDoer{transportFailures: maxReadAttempts + 5}
	client := NewClient("https://example.canary.tools", "token", d)

	if _, err := client.GetIncidents(t.Context(), "", ""); err == nil {
		t.Fatal("expected error after exhausting retries, got nil")
	}
	if d.calls != maxReadAttempts {
		t.Errorf("got %d calls, want exactly %d (bounded retries)", d.calls, maxReadAttempts)
	}
}

func containsParam(rawQuery, param string) bool {
	for _, v := range splitAmp(rawQuery) {
		if v == param {
			return true
		}
	}
	return false
}

func splitAmp(s string) (out []string) {
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return
}
