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
	"sync"
	"testing"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// fakeRuntime is a minimal, self-contained implementation of hosted.Runtime
// for exercising Handle() without a real ingest muxer or BoltDB. The real
// hosted package has its own equivalent (testRuntime), but it is unexported
// and lives in package hosted, so plugin packages need their own; this
// mirrors the pattern the real hosted/plugins/jamf and hosted/plugins/msgraph
// tests use, embedding hosted.StatusTracker for the SetError/ClearError/
// SetWarn/ClearWarn methods hosted.Runtime requires rather than hand-rolling
// them.
type fakeRuntime struct {
	hosted.StatusTracker

	mu      sync.Mutex
	ctx     context.Context
	store   map[string][]byte
	tags    map[string]entry.EntryTag
	nextTag entry.EntryTag
	entries []entry.Entry

	negotiateErr error // if set, NegotiateTag always fails with this error
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{
		ctx:   context.Background(),
		store: make(map[string][]byte),
		tags:  make(map[string]entry.EntryTag),
	}
}

// Runtime
func (r *fakeRuntime) Alive() bool              { return true }
func (r *fakeRuntime) Sleep(time.Duration) bool { return false }
func (r *fakeRuntime) Context() context.Context { return r.ctx }

// Storage
func (r *fakeRuntime) Get(key string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.store[key]
	if !ok {
		return nil, storage.ErrStorageNotFound
	}
	return append([]byte(nil), v...), nil
}
func (r *fakeRuntime) Put(key string, value []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store[key] = append([]byte(nil), value...)
	return nil
}
func (r *fakeRuntime) GetString(key string) (string, error) {
	v, err := r.Get(key)
	return string(v), err
}
func (r *fakeRuntime) PutString(key, value string) error { return r.Put(key, []byte(value)) }
func (r *fakeRuntime) GetInt64(string) (int64, error)    { return 0, storage.ErrStorageNotFound }
func (r *fakeRuntime) PutInt64(string, int64) error      { return nil }
func (r *fakeRuntime) GetTime(key string) (time.Time, error) {
	v, err := r.GetString(key)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, v)
}
func (r *fakeRuntime) PutTime(key string, value time.Time) error {
	return r.PutString(key, value.Format(time.RFC3339Nano))
}

// Logger (no-ops; nothing under test asserts on log output)
func (r *fakeRuntime) Debug(string, ...rfc5424.SDParam)    {}
func (r *fakeRuntime) Info(string, ...rfc5424.SDParam)     {}
func (r *fakeRuntime) Warn(string, ...rfc5424.SDParam)     {}
func (r *fakeRuntime) Error(string, ...rfc5424.SDParam)    {}
func (r *fakeRuntime) Critical(string, ...rfc5424.SDParam) {}

// Writer
func (r *fakeRuntime) Write(e entry.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
	return nil
}
func (r *fakeRuntime) NegotiateTag(name string) (entry.EntryTag, error) {
	if r.negotiateErr != nil {
		return 0, r.negotiateErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tags[name]; ok {
		return t, nil
	}
	r.nextTag++
	r.tags[name] = r.nextTag
	return r.nextTag, nil
}

func (r *fakeRuntime) writtenEntries() []entry.Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]entry.Entry(nil), r.entries...)
}

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
	c := &Config{Domain: domain, Token: "tok", Api: IncidentApi}
	c.Tag_Name = "thinkst-incidents"
	if err := c.Verify(); err != nil {
		panic(err) // programmer error in test setup
	}
	return c
}

func newAuditConfig(domain string) *Config {
	c := &Config{Domain: domain, Token: "tok", Api: AuditApi}
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
	rt := newFakeRuntime()

	cont, err := th.Handle(context.Background(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cont == nil || cont.Delay != 0 {
		t.Fatalf("got continuation %v, want ContinueNow (delay 0)", cont)
	}

	entries := rt.writtenEntries()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if string(entries[0].Data) != `{"updated_id": 5, "updated_std": "2026-01-01 00:00:00 UTC+0000"}` {
		t.Errorf("unexpected entry data: %s", entries[0].Data)
	}

	sinceID, err := rt.GetString(sinceIDKey)
	if err != nil || sinceID != "5" {
		t.Errorf("got since-id %q (err %v), want 5", sinceID, err)
	}
	cursor, err := rt.GetString(cursorKey)
	if err != nil || cursor != "abc123" {
		t.Errorf("got cursor %q (err %v), want abc123", cursor, err)
	}
}

func TestHandleIncidentsLastPageWaitsInterval(t *testing.T) {
	server := httptest.NewServer(incidentsPage("", "", ""))
	defer server.Close()

	th := New(newIncidentConfig(server.URL))
	rt := newFakeRuntime()

	cont, err := th.Handle(context.Background(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Duration(defaultInterval) * time.Second
	if cont == nil || cont.Delay != want {
		t.Fatalf("got continuation %v, want ContinueAfter(%v)", cont, want)
	}
	if len(rt.writtenEntries()) != 0 {
		t.Errorf("expected no entries written on an empty page")
	}
	if cursor, _ := rt.GetString(cursorKey); cursor != "" {
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
	rt := newFakeRuntime()
	if err := rt.PutString(sinceIDKey, "1"); err != nil {
		t.Fatal(err)
	}

	if _, err := th.Handle(context.Background(), rt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.writtenEntries()) != 3 {
		t.Fatalf("got %d entries, want 3", len(rt.writtenEntries()))
	}
	sinceID, _ := rt.GetString(sinceIDKey)
	if sinceID != "9" {
		t.Errorf("got since-id %q, want 9 (the max updated_id seen)", sinceID)
	}
}

func TestHandleIncidentsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	th := New(newIncidentConfig(server.URL))
	rt := newFakeRuntime()
	// The production HTTP client treats 5xx as retryable and keeps retrying
	// for as long as its context allows, so bind a short-lived context here;
	// against a server that always returns 500 an uncancelled context would
	// retry forever instead of ever surfacing an error.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	rt.ctx = ctx

	cont, err := th.Handle(ctx, rt)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if cont != nil {
		t.Errorf("expected nil continuation on error, got %v", cont)
	}
	if len(rt.writtenEntries()) != 0 {
		t.Errorf("expected no entries written on error")
	}
}

func TestHandleAuditSkipsAlreadySeenAndTracksLatest(t *testing.T) {
	server := httptest.NewServer(auditPage("",
		`{"timestamp": "2026-01-01 00:00:00 UTC+0000"},`+ // at the watermark: skipped
			`{"timestamp": "2026-01-01 00:05:00 UTC+0000"},`+ // new: ingested
			`{"timestamp": "2026-01-01 00:10:00 UTC+0000"}`, // new, latest: ingested
	))
	defer server.Close()

	th := New(newAuditConfig(server.URL))
	rt := newFakeRuntime()
	watermark, _ := time.Parse(TimeFormat, "2026-01-01 00:00:00 UTC+0000")
	if err := rt.PutTime(timestampKey, watermark); err != nil {
		t.Fatal(err)
	}

	cont, err := th.Handle(context.Background(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Duration(defaultInterval) * time.Second
	if cont == nil || cont.Delay != want {
		t.Fatalf("got continuation %v, want ContinueAfter(%v)", cont, want)
	}

	entries := rt.writtenEntries()
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (the watermark record should be skipped)", len(entries))
	}

	storedTS, err := rt.GetTime(timestampKey)
	if err != nil {
		t.Fatalf("unexpected error reading stored timestamp: %v", err)
	}
	wantTS, _ := time.Parse(TimeFormat, "2026-01-01 00:10:00 UTC+0000")
	if !storedTS.Equal(wantTS) {
		t.Errorf("got stored timestamp %v, want %v", storedTS, wantTS)
	}
}

func TestHandleAuditMorePagesContinuesNow(t *testing.T) {
	server := httptest.NewServer(auditPage("next-cursor",
		`{"timestamp": "2026-01-01 00:05:00 UTC+0000"}`,
	))
	defer server.Close()

	th := New(newAuditConfig(server.URL))
	rt := newFakeRuntime()

	cont, err := th.Handle(context.Background(), rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cont == nil || cont.Delay != 0 {
		t.Fatalf("got continuation %v, want ContinueNow (delay 0)", cont)
	}
	cursor, _ := rt.GetString(cursorKey)
	if cursor != "next-cursor" {
		t.Errorf("got cursor %q, want next-cursor", cursor)
	}
}

func TestHandleUnsupportedApi(t *testing.T) {
	// Construct directly rather than via Verify(), which would already reject
	// this Api value: this test targets Handle's own defensive default case.
	// Requests_Per_Minute is set explicitly since Verify() (which would
	// normally supply it) is deliberately skipped here.
	c := &Config{Domain: "example.canary.tools", Token: "tok", Api: Api("bogus")}
	c.Requests_Per_Minute = defaultRequestsPerMinute
	th := New(c)
	rt := newFakeRuntime()

	cont, err := th.Handle(context.Background(), rt)
	if err == nil {
		t.Fatal("expected error for unsupported api, got nil")
	}
	if cont != nil {
		t.Errorf("expected nil continuation, got %v", cont)
	}
}

func TestHandleTagNegotiationError(t *testing.T) {
	th := New(newIncidentConfig("example.canary.tools"))
	rt := newFakeRuntime()
	rt.negotiateErr = fmt.Errorf("tag negotiation boom")

	_, err := th.Handle(context.Background(), rt)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
