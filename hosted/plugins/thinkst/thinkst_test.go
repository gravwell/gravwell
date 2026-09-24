/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package thinkst

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
)

func incidentsPage(nextLink, next string, incidents string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		nl := "null"
		if nextLink != "" {
			nl = fmt.Sprintf("%q", nextLink)
		}
		n := "null"
		if next != "" {
			n = fmt.Sprintf("%q", next)
		}
		fmt.Fprintf(w, `{"cursor": {"next": %s, "next_link": %s}, "incidents": [%s]}`, n, nl, incidents)
	}
}

func auditPage(next string, records string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		n := "null"
		if next != "" {
			n = fmt.Sprintf("%q", next)
		}
		fmt.Fprintf(w, `{"cursor": {"next": %s}, "audit_trail": [%s]}`, n, records)
	}
}

func newIncidentConfig(domain string) *Config {
	c := &Config{Domain: domain, Token: "tok", Api: []Api{IncidentApi}}
	c.Tag_Name = "thinkst-incidents"
	if err := c.Verify(); err != nil {
		panic(err) // programmer error in test setup
	}
	return c
}

func newAuditConfig(domain string) *Config {
	c := &Config{Domain: domain, Token: "tok", Api: []Api{AuditApi}}
	c.Tag_Name = "thinkst-audit"
	if err := c.Verify(); err != nil {
		panic(err)
	}
	return c
}

func TestHandleIncidentsFirstPageContinuesNow(t *testing.T) {
	server := httptest.NewServer(incidentsPage(
		"https://example/next", "abc123",
		`{"updated_id": 5, "updated_std": "2026-01-01 00:00:00 UTC+0000"}`,
	))
	defer server.Close()

	th := New(newIncidentConfig(server.URL))
	rt := hosted.NewMock(t.Context())

	cont, err := th.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cont == nil || cont.Delay != 0 {
		t.Fatalf("got continuation %v, want ContinueNow (delay 0)", cont)
	}

	entries := rt.Entries()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if string(entries[0].Data) != `{"updated_id": 5, "updated_std": "2026-01-01 00:00:00 UTC+0000"}` {
		t.Errorf("unexpected entry data: %s", entries[0].Data)
	}

	sinceID, err := rt.GetString(incidentSinceIDKey)
	if err != nil || sinceID != "5" {
		t.Errorf("got since-id %q (err %v), want 5", sinceID, err)
	}
	cursor, err := rt.GetString(incidentCursorKey)
	if err != nil || cursor != "abc123" {
		t.Errorf("got cursor %q (err %v), want abc123", cursor, err)
	}
}

func TestHandleIncidentsLastPageWaitsInterval(t *testing.T) {
	server := httptest.NewServer(incidentsPage("", "", ""))
	defer server.Close()

	th := New(newIncidentConfig(server.URL))
	rt := hosted.NewMock(t.Context())

	cont, err := th.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Duration(defaultInterval) * time.Second
	if cont == nil || cont.Delay != want {
		t.Fatalf("got continuation %v, want ContinueAfter(%v)", cont, want)
	}
	if len(rt.Entries()) != 0 {
		t.Errorf("expected no entries written on an empty page")
	}
	if cursor, _ := rt.GetString(incidentCursorKey); cursor != "" {
		t.Errorf("expected empty cursor stored, got %q", cursor)
	}
}

func TestHandleIncidentsTracksMaxSinceID(t *testing.T) {
	// out-of-order updated_ids; the stored since-id should track the max seen, not the last seen.
	server := httptest.NewServer(incidentsPage("", "",
		`{"updated_id": 3, "updated_std": "2026-01-01 00:00:00 UTC+0000"},`+
			`{"updated_id": 9, "updated_std": "2026-01-01 00:01:00 UTC+0000"},`+
			`{"updated_id": 4, "updated_std": "2026-01-01 00:02:00 UTC+0000"}`,
	))
	defer server.Close()

	th := New(newIncidentConfig(server.URL))
	rt := hosted.NewMock(t.Context())
	if err := rt.PutString(incidentSinceIDKey, "1"); err != nil {
		t.Fatal(err)
	}

	if _, err := th.Handle(t.Context(), rt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.Entries()) != 3 {
		t.Fatalf("got %d entries, want 3", len(rt.Entries()))
	}
	sinceID, _ := rt.GetString(incidentSinceIDKey)
	if sinceID != "9" {
		t.Errorf("got since-id %q, want 9 (the max updated_id seen)", sinceID)
	}
}

func TestHandleIncidentsSkipsUnparsableRecord(t *testing.T) {
	// A record whose metadata can't be parsed has an unknown updated_id, so
	// since-id can never advance past it; it's dropped (and logged) rather
	// than written, so it isn't re-fetched and re-written on every poll
	// forever.
	server := httptest.NewServer(incidentsPage("", "",
		`{"updated_id": "not-a-number", "updated_std": "2026-01-01 00:00:00 UTC+0000"},`+
			`{"updated_id": 7, "updated_std": "2026-01-01 00:01:00 UTC+0000"}`,
	))
	defer server.Close()

	th := New(newIncidentConfig(server.URL))
	rt := hosted.NewMock(t.Context())

	if _, err := th.Handle(t.Context(), rt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := rt.Entries()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (the unparsable record should be skipped, not written)", len(entries))
	}
	if string(entries[0].Data) != `{"updated_id": 7, "updated_std": "2026-01-01 00:01:00 UTC+0000"}` {
		t.Errorf("unexpected entry data: %s", entries[0].Data)
	}

	sinceID, _ := rt.GetString(incidentSinceIDKey)
	if sinceID != "7" {
		t.Errorf("got since-id %q, want 7 (only the well-formed record's id is tracked)", sinceID)
	}
}

func TestHandleIncidentsCursorEmptyWhenNextIsNull(t *testing.T) {
	// Regression test: the cursor gate checks next_link, but the cursor value
	// itself comes from the separate next field. If the API ever returns
	// next_link set while next is JSON null, the stored cursor must be the
	// empty string, not the literal "<nil>".
	server := httptest.NewServer(incidentsPage("https://example/next", "", ""))
	defer server.Close()

	th := New(newIncidentConfig(server.URL))
	rt := hosted.NewMock(t.Context())

	if _, err := th.Handle(t.Context(), rt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cursor, _ := rt.GetString(incidentCursorKey)
	if cursor != "" {
		t.Errorf(`got cursor %q, want "" (not the literal "<nil>") when next_link is set but next is null`, cursor)
	}
}

func TestHandleIncidentsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	th := New(newIncidentConfig(server.URL))
	// The production HTTP client treats 5xx as retryable and keeps retrying
	// for as long as its context allows, so bind a short-lived context here;
	// against a server that always returns 500 an uncancelled context would
	// retry forever instead of ever surfacing an error.
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	rt := hosted.NewMock(ctx)

	cont, err := th.Handle(ctx, rt)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if cont != nil {
		t.Errorf("expected nil continuation on error, got %v", cont)
	}
	if len(rt.Entries()) != 0 {
		t.Errorf("expected no entries written on error")
	}
}

func TestHandleAuditSkipsAlreadySeenAndTracksLatest(t *testing.T) {
	// The audit trail API has no server-side time filter, so every poll
	// re-fetches from the beginning; a record already ingested at the exact
	// watermark second is only recognized as "already seen" once its content
	// hash has been persisted (see auditTimestampHashesKey), which a real
	// prior poll would already have done. Seed that here to simulate steady
	// state.
	watermarkRecord := `{"timestamp": "2026-01-01 00:00:00 UTC+0000"}`
	server := httptest.NewServer(auditPage("",
		watermarkRecord+`,`+ // at the watermark, hash already known: skipped
			`{"timestamp": "2026-01-01 00:05:00 UTC+0000"},`+ // new: ingested
			`{"timestamp": "2026-01-01 00:10:00 UTC+0000"}`, // new, latest: ingested
	))
	defer server.Close()

	th := New(newAuditConfig(server.URL))
	rt := hosted.NewMock(t.Context())
	watermark, _ := time.Parse(TimeFormat, "2026-01-01 00:00:00 UTC+0000")
	if err := rt.PutTime(auditTimestampKey, watermark); err != nil {
		t.Fatal(err)
	}
	if err := rt.PutString(auditTimestampHashesKey, recordHash([]byte(watermarkRecord))); err != nil {
		t.Fatal(err)
	}

	cont, err := th.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Duration(defaultInterval) * time.Second
	if cont == nil || cont.Delay != want {
		t.Fatalf("got continuation %v, want ContinueAfter(%v)", cont, want)
	}

	entries := rt.Entries()
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (the already-hashed watermark record should be skipped)", len(entries))
	}

	storedTS, err := rt.GetTime(auditTimestampKey)
	if err != nil {
		t.Fatalf("unexpected error reading stored timestamp: %v", err)
	}
	wantTS, _ := time.Parse(TimeFormat, "2026-01-01 00:10:00 UTC+0000")
	if !storedTS.Equal(wantTS) {
		t.Errorf("got stored timestamp %v, want %v", storedTS, wantTS)
	}
}

func TestHandleAuditIngestsDistinctRecordSharingWatermarkSecond(t *testing.T) {
	// Regression test: two distinct audit records can legitimately share the
	// same second-resolution timestamp. A record already known at that exact
	// second (by content hash) must still be skipped, but a genuinely new
	// record sharing that second must not be dropped just because it isn't
	// strictly after the watermark.
	priorRecord := `{"timestamp": "2026-01-01 00:00:00 UTC+0000", "id": "a"}`
	newRecord := `{"timestamp": "2026-01-01 00:00:00 UTC+0000", "id": "b"}`
	server := httptest.NewServer(auditPage("", newRecord))
	defer server.Close()

	th := New(newAuditConfig(server.URL))
	rt := hosted.NewMock(t.Context())
	watermark, _ := time.Parse(TimeFormat, "2026-01-01 00:00:00 UTC+0000")
	if err := rt.PutTime(auditTimestampKey, watermark); err != nil {
		t.Fatal(err)
	}
	if err := rt.PutString(auditTimestampHashesKey, recordHash([]byte(priorRecord))); err != nil {
		t.Fatal(err)
	}

	if _, err := th.Handle(t.Context(), rt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := rt.Entries()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (a distinct record sharing the watermark second must still be ingested)", len(entries))
	}
	if string(entries[0].Data) != newRecord {
		t.Errorf("unexpected entry data: %s", entries[0].Data)
	}

	seen := splitHashes(mustGetString(t, rt, auditTimestampHashesKey))
	if !seen[recordHash([]byte(priorRecord))] || !seen[recordHash([]byte(newRecord))] {
		t.Errorf("got hashes %v, want both the prior and the new record's hash tracked", seen)
	}
}

func mustGetString(t *testing.T, rt *hosted.Mock, key string) string {
	t.Helper()
	v, err := rt.GetString(key)
	if err != nil {
		t.Fatalf("unexpected error reading %q: %v", key, err)
	}
	return v
}

func TestHandleAuditMorePagesContinuesNow(t *testing.T) {
	server := httptest.NewServer(auditPage("next-cursor",
		`{"timestamp": "2026-01-01 00:05:00 UTC+0000"}`,
	))
	defer server.Close()

	th := New(newAuditConfig(server.URL))
	rt := hosted.NewMock(t.Context())

	cont, err := th.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cont == nil || cont.Delay != 0 {
		t.Fatalf("got continuation %v, want ContinueNow (delay 0)", cont)
	}
	cursor, _ := rt.GetString(auditCursorKey)
	if cursor != "next-cursor" {
		t.Errorf("got cursor %q, want next-cursor", cursor)
	}
}

func TestHandleMultipleApisProcessesEach(t *testing.T) {
	// Regression test: Handle must walk every configured Api, not just the
	// first, and negotiate a distinct tag per Api via Tag-Prefix.
	mux := http.NewServeMux()
	mux.HandleFunc(incidentsPath, incidentsPage("", "",
		`{"updated_id": 1, "updated_std": "2026-01-01 00:00:00 UTC+0000"}`,
	))
	mux.HandleFunc(auditTrailPath, auditPage("",
		`{"timestamp": "2026-01-01 00:00:00 UTC+0000"}`,
	))
	server := httptest.NewServer(mux)
	defer server.Close()

	c := &Config{Domain: server.URL, Token: "tok", Api: []Api{IncidentApi, AuditApi}}
	c.Tag_Prefix = "canary"
	if err := c.Verify(); err != nil {
		t.Fatalf("unexpected error verifying config: %v", err)
	}
	th := New(c)
	rt := hosted.NewMock(t.Context())
	// The audit record's timestamp is a fixed date in the past; without a
	// seeded watermark, handleAudit would fall back to time.Now()-lookback
	// (relative to the real clock) and treat that fixed date as already-old.
	watermark, _ := time.Parse(TimeFormat, "2025-01-01 00:00:00 UTC+0000")
	if err := rt.PutTime(auditTimestampKey, watermark); err != nil {
		t.Fatal(err)
	}

	if _, err := th.Handle(t.Context(), rt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := rt.Entries()
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (one from each configured Api)", len(entries))
	}

	incidentTag, err := rt.NegotiateTag("canary-incident")
	if err != nil {
		t.Fatal(err)
	}
	auditTag, err := rt.NegotiateTag("canary-audit")
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Tag != incidentTag {
		t.Errorf("got incidents entry tag %v, want %v", entries[0].Tag, incidentTag)
	}
	if entries[1].Tag != auditTag {
		t.Errorf("got audit entry tag %v, want %v", entries[1].Tag, auditTag)
	}
}

func TestHandleUnsupportedApi(t *testing.T) {
	// Construct directly rather than via Verify(), which would already reject
	// this Api value: this test targets Handle's own defensive default case.
	// Requests_Per_Minute is set explicitly since Verify() (which would
	// normally supply it) is deliberately skipped here.
	c := &Config{Domain: "example.canary.tools", Token: "tok", Api: []Api{"bogus"}}
	c.Requests_Per_Minute = defaultRequestsPerMinute
	th := New(c)
	rt := hosted.NewMock(t.Context())

	cont, err := th.Handle(t.Context(), rt)
	if err == nil {
		t.Fatal("expected error for unsupported api, got nil")
	}
	if cont != nil {
		t.Errorf("expected nil continuation, got %v", cont)
	}
}

func TestHandleTagNegotiationError(t *testing.T) {
	th := New(newIncidentConfig("example.canary.tools"))
	rt := hosted.NewMock(t.Context())
	rt.NegotiateErr = fmt.Errorf("tag negotiation boom")

	_, err := th.Handle(t.Context(), rt)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
