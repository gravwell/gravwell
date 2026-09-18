package restpoller

import (
	"context"
	"errors"
	"fmt"
	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

type auditRuntime struct {
	hosted.Runtime
	saved        []byte
	writes       int
	writeCalls   int
	fail         bool
	failOnCall   int
	deliveryFail bool
}

func (r *auditRuntime) Get(string) ([]byte, error) {
	if r.saved == nil {
		return nil, storage.ErrStorageNotFound
	}
	return r.saved, nil
}
func (r *auditRuntime) Put(_ string, b []byte) error { r.saved = append([]byte(nil), b...); return nil }
func (r *auditRuntime) Write(entry.Entry) error {
	r.writeCalls++
	if r.fail || r.writeCalls == r.failOnCall {
		return errors.New("synthetic write failure")
	}
	r.writes++
	return nil
}
func (r *auditRuntime) NegotiateTag(string) (entry.EntryTag, error) { return 1, nil }
func (r *auditRuntime) Info(string, ...rfc5424.SDParam)             {}
func TestSlackWatermarkOverlapStateAndWriteFailure(t *testing.T) {
	var oldest int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oldest, _ = strconv.ParseInt(r.URL.Query().Get("oldest"), 10, 64)
		if r.URL.Query().Get("latest") == "" {
			t.Error("missing inclusive upper bound")
		}
		_, _ = w.Write([]byte(`{"entries":[{"id":"synthetic","date_create":1700000000}],"response_metadata":{"next_cursor":""}}`))
	}))
	defer s.Close()
	c := &Config{Base_URL: s.URL, Scope_Identity: "synthetic", Page_Size: 10, Max_Pages: 3}
	c.SetProduct("slack")
	c.ApplyDefaults(24, 30, 300)
	cl, e := NewClient(s.URL, "test", 10000, 10, 3, s.Client())
	if e != nil {
		t.Fatal(e)
	}
	p := New(c)
	r := &auditRuntime{fail: true}
	d := catalog["slack"]["audit-logs"]
	if e = p.collect(context.Background(), r, cl, d, ""); e == nil {
		t.Fatal("expected write failure to be returned")
	}
	if st0, e0 := loadState(r, c.stateKey(d)); e0 != nil || len(st0.Manifest) != 0 || st0.Watermark != 0 {
		t.Fatal("delivery checkpoint advanced after rejected write")
	}
	r.fail = false
	if e = p.collect(context.Background(), r, cl, d, ""); e != nil {
		t.Fatal(e)
	}
	st, e := loadState(r, c.stateKey(d))
	if e != nil || st.Watermark < time.Now().Unix()-310 {
		t.Fatal("missing watermark")
	}
	if e = p.collect(context.Background(), r, cl, d, ""); e != nil {
		t.Fatal(e)
	}
	if oldest != st.Watermark || r.writes != 1 {
		t.Fatal("overlap or dedup failed")
	}
	k := c.stateKey(d)
	c.Scope_Identity = "different"
	if k == c.stateKey(d) {
		t.Fatal("scope state collision")
	}
}
func TestSlackMissingArrayAndApplicationErrorAreFailures(t *testing.T) {
	for _, body := range []string{`{}`, `{"error":"not_authed"}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		c, _ := NewClient(s.URL, "test", 10000, 1, 2, s.Client())
		if _, e := c.Collect(t.Context(), catalog["slack"]["audit-logs"], nil, "", ""); e == nil {
			t.Fatal("invalid response accepted")
		}
		s.Close()
	}
}
func TestRetryDelayHonorsLongVendorWindow(t *testing.T) {
	if retryDelay("120", 0) < 120*time.Second {
		t.Fatal("Retry-After shortened")
	}
}

func TestPartialBatchWriteFailureDoesNotDuplicateAlreadyWrittenRecords(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"entries":[{"id":"rec-1","date_create":1700000000},{"id":"rec-2","date_create":1700000001}],"response_metadata":{"next_cursor":""}}`))
	}))
	defer s.Close()
	c := &Config{Base_URL: s.URL, Scope_Identity: "synthetic", Page_Size: 10, Max_Pages: 3}
	c.SetProduct("slack")
	c.ApplyDefaults(24, 30, 300)
	cl, e := NewClient(s.URL, "test", 10000, 10, 3, s.Client())
	if e != nil {
		t.Fatal(e)
	}
	p := New(c)
	// Fail the write of the 2nd record in the page; rec-1 must already have been handed to rt.Write.
	r := &auditRuntime{failOnCall: 2}
	d := catalog["slack"]["audit-logs"]
	if e = p.collect(t.Context(), r, cl, d, ""); e == nil {
		t.Fatal("expected mid-page write failure to be returned")
	}
	if r.writes != 1 {
		t.Fatalf("writes=%d, want 1 (only rec-1 delivered before the failure)", r.writes)
	}
	if r.saved == nil {
		t.Fatal("partial manifest was not persisted after the mid-page failure")
	}
	r.failOnCall = 0
	if e = p.collect(t.Context(), r, cl, d, ""); e != nil {
		t.Fatal(e)
	}
	if r.writes != 2 {
		t.Fatalf("writes=%d, want 2 total (rec-1 must not be re-written; only rec-2 delivered on retry)", r.writes)
	}
}

func TestIntraPollDuplicateIdenticalRecordsAreWrittenOnce(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"entries":[{"id":"dup-1","date_create":1700000000},{"id":"dup-1","date_create":1700000000}],"response_metadata":{"next_cursor":""}}`))
	}))
	defer s.Close()
	c := &Config{Base_URL: s.URL, Scope_Identity: "synthetic", Page_Size: 10, Max_Pages: 3}
	c.SetProduct("slack")
	c.ApplyDefaults(24, 30, 300)
	cl, e := NewClient(s.URL, "test", 10000, 10, 3, s.Client())
	if e != nil {
		t.Fatal(e)
	}
	p := New(c)
	r := &auditRuntime{}
	d := catalog["slack"]["audit-logs"]
	if e = p.collect(t.Context(), r, cl, d, ""); e != nil {
		t.Fatal(e)
	}
	if r.writes != 1 {
		t.Fatalf("writes=%d, want 1 (identical repeated identity within one poll must be deduped)", r.writes)
	}
}

func TestPartialFailureDoesNotLoseEarlierSameIdentityDifferentContentRecord(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"entries":[{"id":"rec-x","date_create":1700000000},{"id":"rec-x","date_create":1700000001}],"response_metadata":{"next_cursor":""}}`))
	}))
	defer s.Close()
	c := &Config{Base_URL: s.URL, Scope_Identity: "synthetic", Page_Size: 10, Max_Pages: 3}
	c.SetProduct("slack")
	c.ApplyDefaults(24, 30, 300)
	cl, e := NewClient(s.URL, "test", 10000, 10, 3, s.Client())
	if e != nil {
		t.Fatal(e)
	}
	p := New(c)
	// Same identity, different content (different date_create -> different fingerprint).
	// Fail the write of the 2nd occurrence; the 1st must not be replayed on retry.
	r := &auditRuntime{failOnCall: 2}
	d := catalog["slack"]["audit-logs"]
	if e = p.collect(t.Context(), r, cl, d, ""); e == nil {
		t.Fatal("expected mid-page write failure to be returned")
	}
	if r.writes != 1 {
		t.Fatalf("writes=%d, want 1 (only the 1st occurrence delivered before the failure)", r.writes)
	}
	r.failOnCall = 0
	if e = p.collect(t.Context(), r, cl, d, ""); e != nil {
		t.Fatal(e)
	}
	if r.writes != 2 {
		t.Fatalf("writes=%d, want 2 total (1st occurrence must not be re-written; only the 2nd delivered on retry)", r.writes)
	}
}

func TestFailedWriteDoesNotLosePendingRecordToOldestDrift(t *testing.T) {
	var recordOldest int64
	var seen bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oldest, _ := strconv.ParseInt(r.URL.Query().Get("oldest"), 10, 64)
		w.Header().Set("Content-Type", "application/json")
		if !seen {
			seen = true
			recordOldest = oldest
		}
		if oldest > recordOldest {
			// The vendor's own oldest/latest window advanced past the pending
			// record: simulate the vendor excluding it, as a real windowed
			// API would once "oldest" drifts forward past its timestamp.
			fmt.Fprint(w, `{"entries":[],"response_metadata":{"next_cursor":""}}`)
			return
		}
		fmt.Fprintf(w, `{"entries":[{"id":"pending-1","date_create":%d}],"response_metadata":{"next_cursor":""}}`, recordOldest)
	}))
	defer s.Close()
	c := &Config{Base_URL: s.URL, Scope_Identity: "synthetic", Page_Size: 10, Max_Pages: 3}
	c.SetProduct("slack")
	c.ApplyDefaults(24, 30, 300)
	cl, e := NewClient(s.URL, "test", 10000, 10, 3, s.Client())
	if e != nil {
		t.Fatal(e)
	}
	p := New(c)
	r := &auditRuntime{fail: true}
	d := catalog["slack"]["audit-logs"]
	if e = p.collect(t.Context(), r, cl, d, ""); e == nil {
		t.Fatal("expected write failure to be returned")
	}
	time.Sleep(1100 * time.Millisecond) // cross a wall-clock second boundary
	r.fail = false
	if e = p.collect(t.Context(), r, cl, d, ""); e != nil {
		t.Fatal(e)
	}
	if r.writes != 1 {
		t.Fatalf("writes=%d, want 1 (pending record must not be lost to oldest drift on retry)", r.writes)
	}
}

func (r *auditRuntime) SyncDelivered(context.Context, time.Duration) error {
	if r.deliveryFail {
		return errors.New("synthetic delivery failure")
	}
	return nil
}
func TestDeliveryFailureDoesNotAdvanceCheckpoint(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"entries\":[{\"id\":\"test-delivery\",\"date_create\":1700000000}],\"response_metadata\":{\"next_cursor\":\"\"}}"))
	}))
	defer s.Close()
	c := &Config{Base_URL: s.URL, Scope_Identity: "synthetic", Page_Size: 10, Max_Pages: 3}
	c.SetProduct("slack")
	c.ApplyDefaults(24, 30, 300)
	cl, e := NewClient(s.URL, "test", 10000, 10, 3, s.Client())
	if e != nil {
		t.Fatal(e)
	}
	p := New(c)
	r := &auditRuntime{deliveryFail: true}
	d := catalog["slack"]["audit-logs"]
	if e = p.collect(t.Context(), r, cl, d, ""); e == nil || r.writes != 1 {
		t.Fatal("checkpoint advanced without acknowledged delivery")
	}
	if st0, e0 := loadState(r, c.stateKey(d)); e0 != nil || len(st0.Manifest) != 0 || st0.Watermark != 0 {
		t.Fatal("checkpoint advanced without acknowledged delivery")
	}
	r.deliveryFail = false
	if e = p.collect(t.Context(), r, cl, d, ""); e != nil {
		t.Fatal(e)
	}
	if r.saved == nil || r.writes != 2 {
		t.Fatal("failed batch did not replay")
	}
}
