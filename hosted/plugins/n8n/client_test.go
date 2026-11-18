/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package n8n

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

type inspectingLimiter struct {
	err         error
	hadDeadline bool
}

func (l *inspectingLimiter) Wait(ctx context.Context) error {
	_, l.hadDeadline = ctx.Deadline()
	return l.err
}

type contextBodyReader struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (r contextBodyReader) Read([]byte) (int, error) {
	if r.cancel != nil {
		r.cancel()
	}
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func TestClientBodyCancellationPreservesSentinels(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint("deadline=", deadline), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			if deadline {
				ctx, cancel = context.WithTimeout(t.Context(), 20*time.Millisecond)
			}
			defer cancel()
			hc := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				var onRead context.CancelFunc
				if !deadline {
					onRead = cancel
				}
				body := io.MultiReader(strings.NewReader(`{"data":[`), contextBodyReader{req.Context(), onRead})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(body)}, nil
			})}
			c, err := newClient("https://example.invalid/api/v1", "synthetic", 10000, 100, 1, hc)
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.collect(ctx, datasetCatalog["users"])
			want := context.Canceled
			if deadline {
				want = context.DeadlineExceeded
			}
			if !errors.Is(err, want) {
				t.Fatalf("body error=%v does not preserve %v", err, want)
			}
		})
	}
}

type failingResponseReader struct {
	err error
}

func (r failingResponseReader) Read([]byte) (int, error) {
	return 0, r.err
}

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		value string
		want  time.Time
		ok    bool
	}{
		{name: "delta seconds", value: "120", want: now.Add(2 * time.Minute), ok: true},
		{name: "HTTP date", value: now.Add(3 * time.Minute).Format(http.TimeFormat), want: now.Add(3 * time.Minute), ok: true},
		{name: "delta capped", value: "3600", want: now.Add(maxRetryAfter), ok: true},
		{name: "HTTP date capped", value: now.Add(time.Hour).Format(http.TimeFormat), want: now.Add(maxRetryAfter), ok: true},
		{name: "malformed", value: "later", ok: false},
		{name: "negative", value: "-1", ok: false},
		{name: "zero", value: "0", ok: false},
		{name: "expired HTTP date", value: now.Add(-time.Second).Format(http.TimeFormat), ok: false},
		{name: "overflow", value: "9223372036854775807", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tc.value, now)
			if ok != tc.ok || !got.Equal(tc.want) {
				t.Fatalf("parseRetryAfter(%q)=(%v,%v) want (%v,%v)", tc.value, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestClientRateLimitWaitPrecedesRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[],"nextCursor":null}`))
	}))
	defer server.Close()
	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	limiter := &inspectingLimiter{}
	c.limiter = limiter
	if _, err := c.collect(t.Context(), datasetCatalog["users"]); err != nil {
		t.Fatal(err)
	}
	if limiter.hadDeadline {
		t.Fatal("rate-limit wait consumed the per-request timeout")
	}
}

func TestClientRateLimitFailureNamesDatasetAndPage(t *testing.T) {
	c, err := newClient("https://example.invalid/api/v1", "synthetic", 10000, 100, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("synthetic limiter failure")
	c.limiter = &inspectingLimiter{err: want}
	_, err = c.collect(t.Context(), datasetCatalog["users"])
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "Dataset users page 1") {
		t.Fatalf("error=%v want contextual limiter failure", err)
	}
}

func TestRequestGateKeepsLongestRetryDeadlineAndCancels(t *testing.T) {
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	gate := newRequestLimiter(10000).(*requestGate)
	gate.now = func() time.Time { return now }
	var waited []time.Duration
	gate.wait = func(_ context.Context, delay time.Duration) error {
		waited = append(waited, delay)
		now = now.Add(delay)
		return nil
	}
	gate.DeferUntil(now.Add(2 * time.Minute))
	gate.DeferUntil(now.Add(30 * time.Second))
	if err := gate.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(waited) != 1 || waited[0] != 2*time.Minute {
		t.Fatalf("retry waits=%v want [2m0s]", waited)
	}

	gate.DeferUntil(now.Add(time.Hour))
	gate.wait = waitForContext
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := gate.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled retry wait error=%v want context.Canceled", err)
	}
}

func TestRequestGateTokenWaitEndsOnCancellation(t *testing.T) {
	// One request per minute: the first Wait takes the only token, so the
	// second must block on the rate limiter until its context is canceled.
	gate := newRequestLimiter(1).(*requestGate)
	if err := gate.Wait(t.Context()); err != nil {
		t.Fatalf("first token wait: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	timer := time.AfterFunc(100*time.Millisecond, cancel)
	defer timer.Stop()
	done := make(chan error, 1)
	go func() { done <- gate.Wait(ctx) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled token wait error=%v want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("token wait did not end within 5s of cancellation")
	}
}

func TestClientCursorPaginationAndLimits(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if r.URL.Path != "/api/v1/credentials" || r.Header.Get("X-N8N-API-KEY") != "synthetic" || r.URL.Query().Get("limit") != "250" {
			t.Errorf("unexpected request: %s header=%q query=%v", r.URL.Path, r.Header.Get("X-N8N-API-KEY"), r.URL.Query())
		}
		if call == 1 {
			if r.URL.Query().Get("cursor") != "" {
				t.Errorf("first cursor=%q", r.URL.Query().Get("cursor"))
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"a"}],"nextCursor":"next"}`))
			return
		}
		if r.URL.Query().Get("cursor") != "next" {
			t.Errorf("second cursor=%q", r.URL.Query().Get("cursor"))
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"b"}],"nextCursor":null}`))
	}))
	defer server.Close()

	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 250, 3, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.collect(t.Context(), datasetCatalog["credentials"])
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 || calls.Load() != 2 {
		t.Fatalf("records=%d calls=%d", len(result.Records), calls.Load())
	}
}

// A base URL that still carries a forced empty query, a fragment, or an escaped path
// separator must never retarget a dataset request at the bare base path.
func TestClientEndpointIsBuiltFromTheConfiguredPath(t *testing.T) {
	var served []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = append(served, r.URL.Path)
		_, _ = w.Write([]byte(`{"data":[],"nextCursor":null}`))
	}))
	defer server.Close()

	for _, suffix := range []string{"/api/v1", "/api/v1/", "/api/v1?", "/api/v1#frag", "/api/v1%2f"} {
		c, err := newClient(server.URL+suffix, "synthetic", 10000, 100, 1, server.Client())
		if err != nil {
			t.Fatalf("%s: %v", suffix, err)
		}
		endpoint, err := c.endpoint("/users")
		if err != nil {
			t.Fatalf("%s: %v", suffix, err)
		}
		if endpoint.Path != "/api/v1/users" || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
			t.Fatalf("%s: endpoint=%q path=%q query=%q", suffix, endpoint.String(), endpoint.Path, endpoint.RawQuery)
		}
		_, err = c.collect(t.Context(), datasetCatalog["users"])
		if err != nil {
			t.Fatalf("%s: %v", suffix, err)
		}
	}
	for _, path := range served {
		if path != "/api/v1/users" {
			t.Fatalf("served paths=%v", served)
		}
	}
	if len(served) != 5 {
		t.Fatalf("served paths=%v", served)
	}
}

// A proxy or alternate origin may serve the n8n API below a path prefix; the
// dataset path is appended to the configured path, not to a fixed /api/v1.
func TestClientEndpointKeepsBaseURLPathPrefix(t *testing.T) {
	var mu sync.Mutex
	var served []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		served = append(served, r.URL.Path)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"data":[],"nextCursor":null}`))
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/proxy/n8n/api/v1", "users")
	if _, err := New(conf).Handle(t.Context(), newMockRuntime(t.Context())); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(served) != 1 || served[0] != "/proxy/n8n/api/v1/users" {
		t.Fatalf("served paths=%v want [/proxy/n8n/api/v1/users]", served)
	}
}

// The single-object datasets must not accept a JSON null as a complete report.
func TestClientRejectsNonObjectSingleResponse(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `"report"`, `7`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		result, err := c.collect(t.Context(), datasetCatalog["audit"])
		server.Close()
		if err == nil || !strings.Contains(err.Error(), "expected a JSON object") || len(result.Records) != 0 {
			t.Fatalf("body=%s records=%d error=%v", body, len(result.Records), err)
		}
	}
}

// A single-object dataset response must hold exactly one JSON value.
func TestClientRejectsTrailingJSONAfterSingleObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"risk":"a"} {"risk":"b"}`)
	}))
	defer server.Close()
	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.collect(t.Context(), datasetCatalog["audit"])
	if err == nil || !strings.Contains(err.Error(), "trailing") || len(result.Records) != 0 {
		t.Fatalf("records=%d error=%v want trailing JSON rejection", len(result.Records), err)
	}
}

func TestClientExecutionDataQueryOption(t *testing.T) {
	for _, tc := range []struct {
		name        string
		includeData bool
		want        string
	}{
		{name: "default excluded", want: "false"},
		{name: "explicit included", includeData: true, want: "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/executions" || r.URL.Query().Get("includeData") != tc.want {
					t.Fatalf("unexpected execution request: %s query=%v", r.URL.Path, r.URL.Query())
				}
				_, _ = w.Write([]byte(`{"data":[],"nextCursor":null}`))
			}))
			defer server.Close()
			c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			c.includeExecutionData = tc.includeData
			if _, err := c.collect(t.Context(), datasetCatalog["executions"]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestActiveExecutionIDsCoverAllNonterminalStatusesAndEnforceOneBound(t *testing.T) {
	wantStatuses := map[string]bool{"new": true, "running": true, "unknown": true, "waiting": true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		if !wantStatuses[status] {
			t.Fatalf("unexpected status probe %q", status)
		}
		_, _ = fmt.Fprintf(w, `{"data":[{"id":"%s-id","status":"%s"}],"nextCursor":null}`, status, status)
	}))
	defer server.Close()
	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ids, err := c.activeExecutionIDs(t.Context(), datasetCatalog["executions"], 4)
	if err != nil {
		t.Fatalf("activeExecutionIDs: %v", err)
	}
	if len(ids) != 4 {
		t.Fatalf("active IDs=%v", ids)
	}
	if _, err := c.activeExecutionIDs(t.Context(), datasetCatalog["executions"], 3); err == nil || !strings.Contains(err.Error(), "limit of 3") {
		t.Fatalf("over-bound error=%v", err)
	}
}

// A status probe must read every page, or a long-running execution listed on
// a later page would never be retained.
func TestActiveExecutionIDsFollowCursorPages(t *testing.T) {
	var runningRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") != "running" {
			_, _ = io.WriteString(w, `{"data":[],"nextCursor":null}`)
			return
		}
		runningRequests.Add(1)
		switch cursor := r.URL.Query().Get("cursor"); cursor {
		case "":
			_, _ = io.WriteString(w, `{"data":[{"id":"L1","status":"running"}],"nextCursor":"page-2"}`)
		case "page-2":
			_, _ = io.WriteString(w, `{"data":[{"id":"L2","status":"running"}],"nextCursor":"page-3"}`)
		case "page-3":
			_, _ = io.WriteString(w, `{"data":[{"id":"L3","status":"running"}],"nextCursor":null}`)
		default:
			t.Errorf("unexpected cursor %q", cursor)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 1, 5, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ids, err := c.activeExecutionIDs(t.Context(), datasetCatalog["executions"], 10)
	if err != nil {
		t.Fatalf("activeExecutionIDs: %v", err)
	}
	want := map[string]struct{}{"L1": {}, "L2": {}, "L3": {}}
	if len(ids) != len(want) {
		t.Fatalf("active IDs=%v want L1, L2 and L3", ids)
	}
	for id := range want {
		if _, ok := ids[id]; !ok {
			t.Fatalf("active IDs=%v missing %s", ids, id)
		}
	}
	if got := runningRequests.Load(); got != 3 {
		t.Fatalf("running probe requests=%d want 3", got)
	}
}

func TestClientExecutionDataOptionDoesNotAffectOtherDatasets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users" || r.URL.Query().Has("includeData") {
			t.Fatalf("unexpected users request: %s query=%v", r.URL.Path, r.URL.Query())
		}
		_, _ = w.Write([]byte(`{"data":[],"nextCursor":null}`))
	}))
	defer server.Close()
	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.includeExecutionData = true
	if _, err := c.collect(t.Context(), datasetCatalog["users"]); err != nil {
		t.Fatal(err)
	}
}

func TestClientObjectMemberLimitIsExecutionDataOnly(t *testing.T) {
	for _, tc := range []struct {
		name        string
		dataset     string
		includeData bool
		want        int
	}{
		{name: "expanded execution", dataset: "executions", includeData: true, want: 0},
		{name: "metadata execution", dataset: "executions", want: maxRecordMembers},
		{name: "other dataset", dataset: "users", includeData: true, want: maxRecordMembers},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &client{includeExecutionData: tc.includeData}
			if got := c.objectMemberLimit(datasetCatalog[tc.dataset]); got != tc.want {
				t.Fatalf("object member limit=%d want %d", got, tc.want)
			}
		})
	}
}

func TestClientExpandedExecutionAllowsAboveMetadataMemberLimit(t *testing.T) {
	const memberCount = maxRecordMembers + 1
	var body strings.Builder
	body.Grow(memberCount * 12)
	body.WriteString(`{"data":[{"id":"one"`)
	for i := 1; i < memberCount; i++ {
		fmt.Fprintf(&body, `,"k%d":0`, i)
	}
	body.WriteString(`}],"nextCursor":null}`)
	payload := body.String()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.includeExecutionData = true
	result, err := c.collect(t.Context(), datasetCatalog["executions"])
	if err != nil {
		t.Fatalf("expanded execution above metadata member limit: %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("expanded execution collected %d records, want 1", result.Count)
	}
}

func TestClientMaxPagesReturnsNoPartialCollection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"partial"}],"nextCursor":"more"}`))
	}))
	defer server.Close()
	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.collect(t.Context(), datasetCatalog["users"])
	if err == nil || !strings.Contains(err.Error(), "Max-Pages=1") || len(result.Records) != 0 {
		t.Fatalf("result=%v error=%v", result, err)
	}
}

func TestClientMaxPagesBoundaryIsExact(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pages int32
	}{
		{name: "snapshot at limit", pages: 2},
		{name: "snapshot over limit", pages: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if n >= tc.pages {
					_, _ = fmt.Fprintf(w, `{"data":[{"id":"r%d"}],"nextCursor":null}`, n)
					return
				}
				_, _ = fmt.Fprintf(w, `{"data":[{"id":"r%d"}],"nextCursor":"page-%d"}`, n, n+1)
			}))
			defer server.Close()
			c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 1, 2, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			result, err := c.collect(t.Context(), datasetCatalog["users"])
			if tc.pages <= 2 {
				if err != nil || len(result.Records) != 2 {
					t.Fatalf("records=%d error=%v want 2 records", len(result.Records), err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "Max-Pages=2") || len(result.Records) != 0 {
				t.Fatalf("records=%d error=%v want Max-Pages=2 failure", len(result.Records), err)
			}
			if got := requests.Load(); got != 2 {
				t.Fatalf("server requests=%d want 2", got)
			}
		})
	}
}

func TestClientRejectsInvalidCursorType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"partial"}],"nextCursor":123}`))
	}))
	defer server.Close()
	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 2, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.collect(t.Context(), datasetCatalog["users"])
	if err == nil || !strings.Contains(err.Error(), "nextCursor") || len(result.Records) != 0 {
		t.Fatalf("result=%v error=%v", result, err)
	}
}

func TestClientRejectsExplicitEmptyCursor(t *testing.T) {
	for _, cursor := range []string{"", "   ", "\t\n\u2003"} {
		t.Run(fmt.Sprintf("cursor=%q", cursor), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, `{"data":[{"id":"partial"}],"nextCursor":%q}`, cursor)
			}))
			defer server.Close()
			c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 2, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			result, err := c.collect(t.Context(), datasetCatalog["users"])
			if err == nil || !strings.Contains(err.Error(), "non-empty string") || len(result.Records) != 0 {
				t.Fatalf("result=%v error=%v want explicit empty-cursor rejection", result, err)
			}
		})
	}
}

func TestClientRejectsMalformedListEnvelopes(t *testing.T) {
	for _, body := range []string{
		`{"data":[],"data":[],"nextCursor":null}`,
		`{"data":[],"\u0064ata":[],"nextCursor":null}`,
		`{"data":[],"nextCursor":null,"nextCursor":null}`,
		`{"data":[{"id":"x"},],"nextCursor":null}`,
		`{"data":[],"nextCursor":null,"metadata":{"x":}}`,
		`{"data":[],"nextCursor":null} 1`,
		`{"data":[],"error":"unavailable","nextCursor":null}`,
		`{"nextCursor":null}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			result, err := c.collect(t.Context(), datasetCatalog["users"])
			if err == nil || len(result.Records) != 0 {
				t.Fatalf("records=%d error=%v; expected malformed envelope rejection", len(result.Records), err)
			}
		})
	}
}

func TestClientClassifiesDatasetUnavailable(t *testing.T) {
	for _, status := range []int{
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusGone,
		http.StatusNotImplemented,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.collect(t.Context(), datasetCatalog["projects"])
			var unavailable *datasetUnavailableError
			if !errors.As(err, &unavailable) || unavailable.Status != status {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestCollectExecutionDetailFailureSemantics(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantFound bool
		wantError string
	}{
		{name: "found", status: http.StatusOK, body: `{"id":"wanted","status":"success","data":{}}`, wantFound: true},
		{name: "retention race", status: http.StatusNotFound},
		{name: "server failure", status: http.StatusInternalServerError, wantError: "HTTP 500"},
		{name: "identity mismatch", status: http.StatusOK, body: `{"id":"other","status":"success","data":{}}`, wantError: "different id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			count, found, err := c.collectExecution(t.Context(), "wanted", 2, func(collectedRecord, *normalizedRecord) error { return nil })
			if tc.wantError == "" {
				if err != nil || found != tc.wantFound {
					t.Fatalf("count=%d found=%t error=%v", count, found, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error=%v want %q", err, tc.wantError)
			}
		})
	}
}

func TestClientRawRecordLimitFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		page func(*testing.T, *http.Request, http.ResponseWriter)
	}{
		{name: "one page", page: func(t *testing.T, _ *http.Request, w http.ResponseWriter) {
			_, _ = w.Write([]byte(`{"data":[{"id":"1"},{"id":"2"},{"id":"3"}],"nextCursor":null}`))
		}},
		{name: "across pages", page: func(t *testing.T, r *http.Request, w http.ResponseWriter) {
			if r.URL.Query().Get("cursor") == "" {
				_, _ = w.Write([]byte(`{"data":[{"id":"1"},{"id":"2"}],"nextCursor":"next"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"3"}],"nextCursor":null}`))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { tc.page(t, r, w) }))
			defer server.Close()
			c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 2, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			c.maxRecords = 2
			result, err := c.collect(t.Context(), datasetCatalog["users"])
			if err == nil || !strings.Contains(err.Error(), "raw record limit") {
				t.Fatalf("records=%d error=%v; want raw record limit failure", len(result.Records), err)
			}
			if len(result.Records) != 0 {
				t.Fatalf("partial collection returned %d records", len(result.Records))
			}
		})
	}
}

func TestClientDoesNotFollowRedirect(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	c, err := newClient(source.URL+"/api/v1", "synthetic", 10000, 100, 1, source.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.collect(t.Context(), datasetCatalog["users"]); err == nil {
		t.Fatal("redirect unexpectedly succeeded")
	}
	if targetCalls.Load() != 0 {
		t.Fatal("credential-bearing redirect was followed")
	}
}

func TestClientCancellationBeforeRequest(t *testing.T) {
	c, err := newClient("https://n8n.example/api/v1", "synthetic", 1, 100, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.collect(ctx, datasetCatalog["users"]); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestRetryableStatusReturnsToJobAdapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.collect(t.Context(), datasetCatalog["users"]); err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("error=%v", err)
	}
}

func TestSafeURLRedactsEveryQueryValue(t *testing.T) {
	got := safeURL("https://n8n.example/api/v1/users?filter=DO_NOT_LOG&apiKey=ALSO_DO_NOT_LOG")
	if strings.Contains(got, "DO_NOT_LOG") {
		t.Fatalf("query value escaped redaction: %s", got)
	}
	if !strings.Contains(got, "filter=REDACTED") || !strings.Contains(got, "apiKey=REDACTED") {
		t.Fatalf("query names or redaction markers missing: %s", got)
	}
}

func TestClientTransportErrorDoesNotExposeCredential(t *testing.T) {
	hc := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("transport rejected " + req.URL.String())
	})}
	c, err := newClient("https://n8n.example/api/v1", "synthetic", 10000, 100, 1, hc)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	_, err = c.collect(ctx, datasetCatalog["users"])
	if err == nil || strings.Contains(err.Error(), "synthetic") {
		t.Fatalf("transport error exposed credential: %v", err)
	}
}

func TestClientResponseReadErrorDoesNotExposeCredential(t *testing.T) {
	hc := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(failingResponseReader{
				err: errors.New("body read failed for " + req.URL.String()),
			}),
			Header: make(http.Header),
		}, nil
	})}
	c, err := newClient("https://n8n.example/api/v1", "synthetic", 10000, 100, 1, hc)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.collect(t.Context(), datasetCatalog["users"])
	if err == nil || strings.Contains(err.Error(), "synthetic") {
		t.Fatalf("response read error exposed credential: %v", err)
	}
}

type countingReader struct {
	r io.Reader
	n int
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += n
	return n, err
}

func TestResponseScannerStopsOversizedValueAtEntryBound(t *testing.T) {
	input := `"` + strings.Repeat("x", 1<<20) + `"`
	reader := &countingReader{r: strings.NewReader(input)}
	scanner := newResponseScanner(reader)
	if _, _, err := scanner.value(64, true); err == nil || !strings.Contains(err.Error(), "maximum entry size") {
		t.Fatalf("error=%v want the Gravwell entry-size bound", err)
	}
	if reader.n > 4096 {
		t.Fatalf("read %d bytes before enforcing a 64-byte value bound", reader.n)
	}
}

func TestResponseScannerValidatesJSONValues(t *testing.T) {
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{`{"id":"a","nested":[null,true,3,-4.5e+2]}`, true},
		{`[]`, true},
		{`"escaped \" quote"`, true},
		{`{"x":}`, false},
		{`[1,]`, false},
		{`01`, false},
		{`truex`, false},
		{`"bad\q"`, false},
	} {
		t.Run(tc.input, func(t *testing.T) {
			scanner := newResponseScanner(strings.NewReader(tc.input))
			raw, _, err := scanner.value(1024, true)
			got := err == nil && json.Valid(raw) && scanner.eof() == nil
			if got != tc.valid {
				t.Fatalf("input=%q raw=%q err=%v got-valid=%t want=%t", tc.input, raw, err, got, tc.valid)
			}
		})
	}
}

// Cancel after a successful fill so the parser must notice cancellation even
// while it still has ordinary response bytes buffered locally.
type cancelBufferedReader struct {
	r           io.Reader
	cancel      context.CancelFunc
	read        int
	cancelAfter int
}

func (r *cancelBufferedReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.read += n
	if n > 0 && r.cancel != nil && r.read >= r.cancelAfter {
		r.cancel()
		r.cancel = nil
	}
	return n, err
}

type cancelOnCloseBody struct {
	io.Reader
	cancel context.CancelFunc
}

func (r cancelOnCloseBody) Close() error { r.cancel(); return nil }

func syntheticClient(t *testing.T, transport roundTripFunc) *client {
	t.Helper()
	c, err := newClient("https://example.invalid/api/v1", "synthetic", 10000, 100, 4, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func syntheticResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestOpaqueCursorExactRoundTrip(t *testing.T) {
	for _, cursor := range []string{" leading", "trailing ", " \t雪+/= opaque\n ", "雪+/="} {
		t.Run(fmt.Sprintf("%q", cursor), func(t *testing.T) {
			calls := 0
			c := syntheticClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					raw, _ := json.Marshal(cursor)
					return syntheticResponse(`{"data":[],"nextCursor":` + string(raw) + `}`), nil
				}
				if got := req.URL.Query().Get("cursor"); got != cursor {
					t.Errorf("cursor=%q want exact %q", got, cursor)
				}
				return syntheticResponse(`{"data":[],"nextCursor":null}`), nil
			})
			if _, err := c.collect(t.Context(), datasetCatalog["users"]); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatalf("calls=%d want 2", calls)
			}
		})
	}
}

func TestClientBufferedCancellationStopsAllEnvelopeWork(t *testing.T) {
	for _, tc := range []struct{ name, dataset, body string }{
		{"retained", "users", `{"data":[{"id":"x","value":"` + strings.Repeat("x", 1<<20) + `"}],"nextCursor":null}`},
		{"scrubbed", "credentials", `{"data":[{"id":"x","password":"` + strings.Repeat("x", 1<<20) + `"}],"nextCursor":null}`},
		{"ignored", "users", `{"ignored":"` + strings.Repeat("x", 1<<20) + `","data":[],"nextCursor":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r := &cancelBufferedReader{r: strings.NewReader(tc.body), cancel: cancel, cancelAfter: 8192}
			c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
				resp := syntheticResponse("")
				resp.Body = io.NopCloser(r)
				return resp, nil
			})
			calls := 0
			_, err := c.collectStreaming(ctx, datasetCatalog[tc.dataset], func(collectedRecord, *normalizedRecord) error { calls++; return nil })
			canceled, hard := splitCancellation(err)
			if !canceled || hard != nil || calls != 0 || r.read >= len(tc.body) {
				t.Fatalf("error=%v hard=%v calls=%d read=%d/%d", err, hard, calls, r.read, len(tc.body))
			}
		})
	}
}

func TestClientCallbackHardErrorSurvivesCancellation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint("hard=", fail), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			hard := errors.New("synthetic callback failure")
			c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
				return syntheticResponse(`{"data":[{"id":"one"}],"nextCursor":null}`), nil
			})
			_, err := c.collectStreaming(ctx, datasetCatalog["users"], func(collectedRecord, *normalizedRecord) error {
				cancel()
				if fail {
					return hard
				}
				return nil
			})
			canceled, companion := splitCancellation(err)
			if !canceled || (fail && !errors.Is(companion, hard)) || (!fail && companion != nil) {
				t.Fatalf("error=%v canceled=%t companion=%v", err, canceled, companion)
			}
		})
	}
}

func TestClientRejectsCancellationAfterSuccessfulParse(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
		resp := syntheticResponse("")
		resp.Body = cancelOnCloseBody{strings.NewReader(`{"data":[],"nextCursor":null}`), cancel}
		return resp, nil
	})
	if result, err := c.collect(ctx, datasetCatalog["users"]); !errors.Is(err, context.Canceled) || result.Count != 0 {
		t.Fatalf("result=%v error=%v", result, err)
	}
}

func TestClientHTTP2GzipBufferedCancellation(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		_, _ = io.WriteString(gz, `{"data":[{"id":"one"},{"id":"two"}],"nextCursor":null}`)
		_ = gz.Close()
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	hc := server.Client()
	transport := hc.Transport
	hc.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp, err := transport.RoundTrip(req)
		if err == nil {
			if resp.ProtoMajor != 2 || !resp.Uncompressed {
				t.Errorf("protocol=%s uncompressed=%t", resp.Proto, resp.Uncompressed)
			}
			resp.Body = &cancelReadCloser{ReadCloser: resp.Body, cancel: cancel}
		}
		return resp, err
	})
	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, hc)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err = c.collectStreaming(ctx, datasetCatalog["users"], func(collectedRecord, *normalizedRecord) error { calls++; return nil })
	canceled, hard := splitCancellation(err)
	if !canceled || hard != nil || calls != 0 {
		t.Fatalf("error=%v hard=%v callbacks=%d", err, hard, calls)
	}
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 && r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	return n, err
}

func TestResponseReadFailuresRemainRedacted(t *testing.T) {
	const sensitive = "synthetic-sensitive-body-url?cursor=private"
	for _, cause := range []error{errors.New(sensitive), fmt.Errorf("%s: %w", sensitive, context.Canceled), fmt.Errorf("%s: %w", sensitive, context.DeadlineExceeded)} {
		c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
			resp := syntheticResponse("")
			resp.Body = io.NopCloser(io.MultiReader(strings.NewReader(`{"data":[{"id":"`), failingResponseReader{cause}))
			return resp, nil
		})
		_, err := c.collect(t.Context(), datasetCatalog["users"])
		if err == nil || strings.Contains(err.Error(), sensitive) {
			t.Fatalf("response error=%v", err)
		}
		if errors.Is(cause, context.Canceled) {
			canceled, hard := splitCancellation(err)
			if !canceled || hard != nil {
				t.Fatalf("cancellation error=%v hard=%v", err, hard)
			}
		}
	}
}

type canceledResponseChunk struct{ body string }

func (r canceledResponseChunk) Read(p []byte) (int, error) {
	return copy(p, r.body), fmt.Errorf("synthetic-sensitive-reader-error: %w", context.Canceled)
}

func TestCanceledReadCannotDeliverAccompanyingBytes(t *testing.T) {
	c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
		resp := syntheticResponse("")
		resp.Body = io.NopCloser(canceledResponseChunk{`{"data":[{"id":"one"}],"nextCursor":null}`})
		return resp, nil
	})
	calls := 0
	_, err := c.collectStreaming(t.Context(), datasetCatalog["users"], func(collectedRecord, *normalizedRecord) error {
		calls++
		return errors.New("synthetic processing failure")
	})
	canceled, hard := splitCancellation(err)
	if !canceled || hard != nil || calls != 0 || strings.Contains(err.Error(), "synthetic-sensitive-reader-error") {
		t.Fatalf("error=%v hard=%v callbacks=%d", err, hard, calls)
	}
}

// Only a 404 on the single-execution request means the execution is gone. Any
// other refusal is an ordinary failure and must not carry the type that Handle
// isolates as an unavailable dataset.
func TestCollectExecutionDetailRefusalIsNotDatasetUnavailable(t *testing.T) {
	for _, status := range []int{
		http.StatusForbidden,
		http.StatusMethodNotAllowed,
		http.StatusGone,
		http.StatusNotImplemented,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			c, err := newClient(server.URL+"/api/v1", "synthetic-credential", 10000, 100, 1, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			count, found, err := c.collectExecution(t.Context(), "wanted", 2, func(collectedRecord, *normalizedRecord) error { return nil })
			if err == nil || found || count != 0 {
				t.Fatalf("count=%d found=%t error=%v, want a failure", count, found, err)
			}
			var unavailable *datasetUnavailableError
			if errors.As(err, &unavailable) {
				t.Fatalf("error still classifies the dataset as unavailable: %v", err)
			}
			for _, want := range []string{fmt.Sprintf("HTTP %d", status), "execution wanted", "read single executions"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error=%v does not contain %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "synthetic-credential") || strings.Contains(err.Error(), "includeData") {
				t.Fatalf("error exposes the credential or query: %v", err)
			}
		})
	}
}

func TestClientRejectsRepeatedCursor(t *testing.T) {
	for _, tc := range []struct {
		name string
		// next maps the cursor of a request to the cursor its response returns.
		next map[string]string
	}{
		{name: "same cursor twice", next: map[string]string{"": "A", "A": "A"}},
		{name: "cycle", next: map[string]string{"": "A", "A": "B", "B": "A"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			hc := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				return syntheticResponse(fmt.Sprintf(`{"data":[{"id":"record-%d"}],"nextCursor":%q}`, requests, tc.next[req.URL.Query().Get("cursor")])), nil
			})}
			c, err := newClient("https://example.invalid/api/v1", "synthetic", 10000, 100, 10, hc)
			if err != nil {
				t.Fatal(err)
			}
			result, err := c.collect(t.Context(), datasetCatalog["users"])
			if err == nil || !strings.Contains(err.Error(), "n8n returned a repeated pagination cursor") {
				t.Fatalf("error=%v want repeated-cursor rejection", err)
			}
			if result.Count != 0 || len(result.Records) != 0 {
				t.Fatalf("repeated cursor returned a partial collection: count=%d records=%d", result.Count, len(result.Records))
			}
			if requests != len(tc.next) {
				t.Fatalf("requests=%d want %d; the repeated request must not be sent", requests, len(tc.next))
			}
		})
	}
}

// The 64 KiB cursor limit applies to the nextCursor value as it is encoded in
// the response, including its two quotes.
func TestClientCursorSizeBoundary(t *testing.T) {
	cursorOfEncodedSize := func(size int) string {
		return strings.Repeat("0123456789abcdef", size/16+1)[:size-2]
	}
	t.Run("65536 bytes", func(t *testing.T) {
		cursor := cursorOfEncodedSize(65536)
		calls := 0
		c := syntheticClient(t, func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return syntheticResponse(`{"data":[],"nextCursor":"` + cursor + `"}`), nil
			}
			if got := req.URL.Query().Get("cursor"); got != cursor {
				t.Errorf("replayed cursor has %d bytes and differs from the %d bytes returned", len(got), len(cursor))
			}
			return syntheticResponse(`{"data":[],"nextCursor":null}`), nil
		})
		if _, err := c.collect(t.Context(), datasetCatalog["users"]); err != nil {
			t.Fatal(err)
		}
		if calls != 2 {
			t.Fatalf("calls=%d want 2", calls)
		}
	})
	t.Run("65537 bytes", func(t *testing.T) {
		calls := 0
		c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			return syntheticResponse(`{"data":[],"nextCursor":"` + cursorOfEncodedSize(65537) + `"}`), nil
		})
		_, err := c.collect(t.Context(), datasetCatalog["users"])
		if err == nil || !strings.Contains(err.Error(), "exceeds the 64 KiB cursor limit") {
			t.Fatalf("error=%v want cursor-limit rejection", err)
		}
		if calls != 1 {
			t.Fatalf("calls=%d want 1; an oversized cursor must not be followed", calls)
		}
	})
}

// collected is what the collect helper returns: the descriptor of every record
// the client delivered, and the client's own record count.
type collected struct {
	Records []collectedRecord
	Count   int
}

// executionDetailStatusTransport answers every request with one HTTP status.
// When cancel is set it cancels the poll context before returning, so the
// response arrives together with parent cancellation.
type executionDetailStatusTransport struct {
	status int
	cancel context.CancelFunc
}

func (c *client) collect(ctx context.Context, definition datasetDefinition) (collected, error) {
	var records []collectedRecord
	result, err := c.collectStreaming(ctx, definition, func(record collectedRecord, _ *normalizedRecord) error {
		records = append(records, record)
		return nil
	})
	if err != nil {
		return collected{}, err
	}
	return collected{Records: records, Count: result.Count}, nil
}

func (d executionDetailStatusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if d.cancel != nil {
		d.cancel()
	}
	return &http.Response{StatusCode: d.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

// Only a 404 that is the sole cause of a failed single-execution request
// counts the execution as dropped. A failure joined to the 404 or to any other
// refusal must survive, and the unavailable type must not leak to Handle,
// which would report the executions dataset as unavailable.
func TestCollectExecutionKeepsFailuresJoinedToDetailRefusal(t *testing.T) {
	for _, tc := range []struct {
		name          string
		status        int
		cancel        bool
		wantErr       bool
		wantCanceled  bool
		wantCompanion bool // a failure remains once splitCancellation removes the sentinel
	}{
		{name: "sole 404", status: http.StatusNotFound},
		{name: "404 with parent cancellation", status: http.StatusNotFound, cancel: true, wantErr: true, wantCanceled: true},
		{name: "sole 403", status: http.StatusForbidden, wantErr: true, wantCompanion: true},
		{name: "403 with parent cancellation", status: http.StatusForbidden, cancel: true, wantErr: true, wantCanceled: true, wantCompanion: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			transport := executionDetailStatusTransport{status: tc.status}
			if tc.cancel {
				transport.cancel = cancel
			}
			c, err := newClient("http://n8n.test/api/v1", "test-key", 60, 10, 10, &http.Client{Transport: transport})
			if err != nil {
				t.Fatal(err)
			}
			c.includeExecutionData = true
			delivered := 0
			count, found, err := c.collectExecution(ctx, "exec-1", 11, func(collectedRecord, *normalizedRecord) error {
				delivered++
				return nil
			})
			if count != 0 || found || delivered != 0 {
				t.Fatalf("count=%d found=%v delivered=%d want nothing collected", count, found, delivered)
			}
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("error=%v want nil so the execution is counted as dropped", err)
				}
				return
			}
			if err == nil {
				t.Fatal("a joined or refused detail failure was discarded and the execution counted as dropped")
			}
			var unavailable *datasetUnavailableError
			if errors.As(err, &unavailable) {
				t.Fatalf("error still classifies the dataset as unavailable: %v", err)
			}
			if got := errors.Is(err, context.Canceled); got != tc.wantCanceled {
				t.Fatalf("errors.Is(err, context.Canceled)=%v want %v: %v", got, tc.wantCanceled, err)
			}
			if tc.status != http.StatusNotFound && !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", tc.status)) {
				t.Fatalf("error=%v want the refused HTTP status", err)
			}
			canceled, companion := splitCancellation(fmt.Errorf("fetch completed execution detail: %w", err))
			if canceled != tc.wantCanceled || (companion != nil) != tc.wantCompanion {
				t.Fatalf("splitCancellation canceled=%v companion=%v want canceled=%v companion=%v", canceled, companion, tc.wantCanceled, tc.wantCompanion)
			}
		})
	}
}

// A scratch cleanup failure or an expired request context joined to a 404
// must fail the poll instead of being dropped with the execution.
func TestExecutionDetailFailureKeepsCompanionsOf404(t *testing.T) {
	notFound := func() error {
		return fmt.Errorf("request Dataset executions page 1: %w", &datasetUnavailableError{Dataset: "executions", Status: http.StatusNotFound})
	}
	if err := executionDetailFailure("exec-1", errors.Join(notFound())); err != nil {
		t.Fatalf("sole 404 error=%v want nil", err)
	}

	cleanupErr := errors.New("remove temporary n8n record failed")
	timed := fmt.Errorf("request Dataset executions page 1 within 90-second request context: %w",
		errors.Join(&datasetUnavailableError{Dataset: "executions", Status: http.StatusNotFound}, context.DeadlineExceeded))
	other := errors.New("unrelated failure")
	for _, tc := range []struct {
		name      string
		err, want error
	}{
		{name: "cleanup", err: errors.Join(notFound(), cleanupErr), want: cleanupErr},
		{name: "request deadline", err: errors.Join(timed), want: context.DeadlineExceeded},
		{name: "no refusal", err: other, want: other},
	} {
		err := executionDetailFailure("exec-1", tc.err)
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s: error=%v want it to keep %v", tc.name, err, tc.want)
		}
		var unavailable *datasetUnavailableError
		if errors.As(err, &unavailable) {
			t.Fatalf("%s: error still classifies the dataset as unavailable: %v", tc.name, err)
		}
	}
}

// Large pages are decoded incrementally and records are delivered one at a
// time, without a response/snapshot byte cap or an aggregate temporary spool.
func TestSnapshotStreamsResponsesBeyondFormerByteCaps(t *testing.T) {
	const fields = 65
	const chunk = 64 * 1024
	filler := strings.Repeat("x", chunk)
	var pages int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		page := pages
		_, _ = io.WriteString(w, "{")
		for field := 0; field < fields; field++ {
			if field > 0 {
				_, _ = io.WriteString(w, ",")
			}
			_, _ = fmt.Fprintf(w, `"pad%d":"`, field)
			for part := 0; part < 16; part++ {
				_, _ = io.WriteString(w, filler)
			}
			_, _ = io.WriteString(w, `"`)
		}
		cursor := `null`
		if page == 1 {
			cursor = `"next"`
		}
		_, _ = fmt.Fprintf(w, `,"data":[{"id":"p%d"}],"nextCursor":%s}`, page, cursor)
	}))
	defer server.Close()

	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 3, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var delivered [][]byte
	result, err := c.collectStreaming(t.Context(), datasetCatalog["users"], func(_ collectedRecord, body *normalizedRecord) error {
		payload, err := body.payload()
		if err != nil {
			return err
		}
		delivered = append(delivered, append([]byte(nil), payload...))
		return nil
	})
	if err != nil {
		t.Fatalf("streamed collection failed: %v", err)
	}
	if result.Count != 2 || pages != 2 {
		t.Fatalf("records=%d pages=%d want 2 streamed records over 2 pages", result.Count, pages)
	}
	if len(delivered) != 2 || string(delivered[0]) != `{"id":"p1"}` || string(delivered[1]) != `{"id":"p2"}` {
		t.Fatalf("delivered records=%q", delivered)
	}
}

// The single-object audit path rejects a JSON null so it cannot be framed as a
// report. Array elements need the same guard, or a null, a scalar, or a nested
// array is normalized into an entry and checkpointed as a valid snapshot.
func TestListRecordsMustBeJSONObjects(t *testing.T) {
	for _, body := range []string{
		`{"data":[null],"nextCursor":null}`,
		`{"data":[7],"nextCursor":null}`,
		`{"data":["loose"],"nextCursor":null}`,
		`{"data":[[1,2]],"nextCursor":null}`,
		`{"data":[true],"nextCursor":null}`,
		`{"data":[{"id":"ok"},null],"nextCursor":null}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			conf := testConfig(t, server.URL+"/api/v1", "users")
			rt := newMockRuntime(t.Context())
			_, err := New(conf).Handle(t.Context(), rt)
			if err == nil || !strings.Contains(err.Error(), "expected a JSON object") {
				t.Fatalf("error=%v want a JSON object rejection", err)
			}
			want := 0
			if strings.Contains(body, `{"id":"ok"}`) {
				want = 1
			}
			if got := len(rt.Entries()); got != want {
				t.Fatalf("entries=%d want %d", got, want)
			}
			if _, err := rt.Get(conf.stateKey(datasetCatalog["users"])); !errors.Is(err, storage.ErrStorageNotFound) {
				t.Fatalf("non-object record advanced state: %v", err)
			}
		})
	}
}

// An object-shaped record, including an empty one, is still accepted.
func TestListRecordsAcceptObjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{},{"id":"1"}],"nextCursor":null}`))
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "users")
	rt := newMockRuntime(t.Context())
	if _, err := New(conf).Handle(t.Context(), rt); err != nil {
		t.Fatalf("object records were rejected: %v", err)
	}
	if got := len(rt.Entries()); got != 2 {
		t.Fatalf("entries=%d want 2", got)
	}
}

type statusCloseBody struct {
	io.Reader
	close func()
}

func (b statusCloseBody) Close() error { b.close(); return nil }

// Schedule shutdown only after the server's hard status is available.
func TestRegressionHTTPStatus(t *testing.T) {
	for _, mode := range []string{"hard_only", "cancel_only", "hard_cancel", "hard_deadline", "request_deadline", "transport_detail", "request_cancel_only"} {
		t.Run(mode, func(t *testing.T) {
			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			if mode == "hard_deadline" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(t.Context(), 15*time.Millisecond)
				defer cancel()
			}
			c := syntheticClient(t, func(req *http.Request) (*http.Response, error) {
				if mode == "transport_detail" {
					return nil, errors.New("synthetic-private-transport")
				}
				if mode == "request_cancel_only" {
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				if mode == "cancel_only" {
					stop()
					return nil, ctx.Err()
				}
				close := func() {}
				if mode == "hard_cancel" {
					close = stop
				}
				if mode == "request_deadline" {
					close = func() { <-req.Context().Done() }
				}
				if mode == "hard_deadline" {
					close = func() { <-ctx.Done() }
				}
				return &http.Response{StatusCode: 500, Header: make(http.Header), Body: statusCloseBody{strings.NewReader("synthetic-private-response"), close}}, nil
			})
			if mode == "request_deadline" || mode == "request_cancel_only" {
				c.exchangeTimeout = time.Millisecond
			}
			_, err := c.collect(ctx, datasetCatalog["users"])
			t.Logf("mode=%s err=%v canceled=%t deadline=%t", mode, err, errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded))
			if mode == "transport_detail" {
				if err == nil || strings.Contains(err.Error(), "synthetic-private-transport") {
					t.Fatalf("unredacted or missing transport error: %v", err)
				}
				return
			}
			if mode == "request_cancel_only" {
				if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "HTTP") || ctx.Err() != nil {
					t.Fatalf("request-only cancellation: %v parent=%v", err, ctx.Err())
				}
				return
			}
			if mode == "cancel_only" {
				if canceled, hard := splitCancellation(err); !canceled || hard != nil {
					t.Fatalf("clean cancellation lost: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
				t.Fatalf("observed hard status lost: %v", err)
			}
			if mode == "hard_only" && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
				t.Fatalf("local cleanup fabricated cancellation: %v", err)
			}
			if mode == "request_deadline" && (!errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil) {
				t.Fatalf("request deadline branch not exercised: %v parent=%v", err, ctx.Err())
			}
			if strings.Contains(err.Error(), "synthetic-private-response") {
				t.Fatal("response detail disclosed")
			}
		})
	}
}

func TestRegressionHTTPAdapterStatus(t *testing.T) {
	for _, level := range []string{"Handle", "Adapter"} {
		t.Run(level, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			old := http.DefaultTransport
			defer func() { http.DefaultTransport = old }()
			http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 500, Header: make(http.Header), Body: statusCloseBody{strings.NewReader(""), cancel}}, nil
			})
			conf := testConfig(t, "https://example.invalid/api/v1", "users")
			rt := hosted.NewMock(ctx)
			var err error
			if level == "Handle" {
				_, err = New(conf).Handle(ctx, rt)
			} else {
				if e := hosted.WrapJob(New(conf)).Run(ctx, rt); e != nil {
					t.Fatal(e)
				}
				_, err = rt.Status()
			}
			t.Logf("level=%s error=%v", level, err)
			if err == nil || !strings.Contains(err.Error(), "HTTP 500") || errors.Is(err, context.Canceled) {
				t.Fatalf("hard status not reported: %v", err)
			}
		})
	}
}

func TestCanceledHTTPStatusRedactsCursorAndResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := syntheticClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("cursor") == "synthetic-private-cursor" {
			return &http.Response{StatusCode: 500, Header: make(http.Header), Body: statusCloseBody{strings.NewReader("synthetic-private-body"), cancel}}, nil
		}
		return syntheticResponse(`{"data":[],"nextCursor":"synthetic-private-cursor"}`), nil
	})
	_, err := c.collect(ctx, datasetCatalog["users"])
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") || !errors.Is(err, context.Canceled) {
		t.Fatalf("lost status or cancellation: %v", err)
	}
	for _, private := range []string{"synthetic-private-cursor", "synthetic-private-body"} {
		if strings.Contains(err.Error(), private) {
			t.Fatalf("disclosed %s", private)
		}
	}
}

type repeatedByteReader struct {
	remaining int64
	value     byte
}

func (r *repeatedByteReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	size := min(int64(len(p)), r.remaining)
	for i := range p[:int(size)] {
		p[i] = r.value
	}
	r.remaining -= size
	return int(size), nil
}

func normalizeTestRecord(t *testing.T, text string, scrub bool) *normalizedRecord {
	t.Helper()
	r, err := normalizeRecordStream(t.Context(), newResponseScanner(strings.NewReader(text)), scrub, t.TempDir(), int64(entry.MaxDataSize))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func testStreamingTinyRecordsReuseScratchAndRetainNoMetadata(t *testing.T) {
	const count = 2000
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	text := `{"data":[` + strings.TrimSuffix(strings.Repeat(`{"id":"x"},`, count), ",") + `],"nextCursor":null}`
	hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(text))}, nil
	})}
	c, err := newClient("https://example.invalid/api/v1", "synthetic", 10000, 100, 1, hc)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]struct{})
	seen := 0
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = c.collectStreaming(t.Context(), datasetCatalog["users"], func(_ collectedRecord, body *normalizedRecord) error {
		files[body.file.Name()] = struct{}{}
		seen++
		return nil
	})
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if seen != count {
		t.Fatalf("delivered=%d want %d", seen, count)
	}
	if len(files) != 1 {
		t.Errorf("created %d scratch files for %d records, want one reusable file", len(files), count)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > count*16*1024 {
		t.Errorf("tiny-record allocation=%d bytes (%d/record), want less than 16 KiB/record", allocated, allocated/count)
	} else {
		t.Logf("tiny-record allocation=%d bytes (%d/record)", allocated, allocated/count)
	}
	left, err := os.ReadDir(dir)
	if err != nil || len(left) != 0 {
		t.Fatalf("scratch cleanup=%v error=%v", left, err)
	}
}

func testStreamIdentityDistinguishesNumericAndStringIDs(t *testing.T) {
	a := normalizeTestRecord(t, `{"id":"1"}`, false)
	b := normalizeTestRecord(t, `{"id":1}`, false)
	if a.identity == b.identity {
		t.Fatal("string and numeric IDs collide")
	}
}

func testStreamIdentityPrecedenceAndFingerprintFallback(t *testing.T) {
	first := normalizeTestRecord(t, `{"uuid":"u1","id":"i1","v":1}`, false)
	second := normalizeTestRecord(t, `{"id":"i1","uuid":"u2","v":2}`, false)
	if first.identity != second.identity {
		t.Fatal("records with the same id but different uuid have different identities")
	}
	if first.identityText != "i1" {
		t.Fatalf("identity text=%q want the id field", first.identityText)
	}
	withoutA := normalizeTestRecord(t, `{"v":1}`, false)
	withoutB := normalizeTestRecord(t, `{"v":2}`, false)
	if withoutA.identity == withoutB.identity {
		t.Fatal("different records without an identity field share an identity")
	}
}

func testStreamingScratchReusePreservesPayloadAndCleansCallbackFailure(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		t.Run(fmt.Sprint("writeFailure=", failWrite), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			records := []string{`{"id":"long","value":"` + strings.Repeat("x", 128*1024) + `"}`, `{"id":"s"}`, `{"id":"unicode","value":"λ"}`}
			hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[` + strings.Join(records, ",") + `]}`))}, nil
			})}
			c, err := newClient("https://example.invalid/api/v1", "synthetic", 10000, 100, 1, hc)
			if err != nil {
				t.Fatal(err)
			}
			i := 0
			writeErr := errors.New("synthetic write failure")
			result, err := c.collectStreaming(t.Context(), datasetCatalog["users"], func(_ collectedRecord, body *normalizedRecord) error {
				payload, err := body.payload()
				if err != nil || string(payload) != records[i] {
					t.Fatalf("record %d payload mismatch, error=%v", i, err)
				}
				i++
				if failWrite && i == 2 {
					return writeErr
				}
				return nil
			})
			if failWrite {
				if !errors.Is(err, writeErr) {
					t.Fatalf("lost callback error: %v", err)
				}
			} else if err != nil || i != len(records) || result.Count != len(records) {
				t.Fatalf("records=%d count=%d error=%v", i, result.Count, err)
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 {
				t.Fatalf("leftover scratch=%v, error=%v", files, err)
			}
		})
	}
}

// Independent, buffered oracle retained only for the streaming scrubber test.
func credentialSecretKey(key string) bool {
	var folded strings.Builder
	for _, r := range strings.TrimSpace(key) {
		if r >= '\uFF01' && r <= '\uFF5E' {
			r -= '\uFEE0'
		}
		if unicode.In(r, unicode.Cf) || r == '_' || r == '-' || r == '.' {
			continue
		}
		folded.WriteRune(unicode.ToLower(r))
	}
	normalized := folded.String()
	if normalized == "data" || normalized == "credentialdata" {
		return true
	}
	for _, word := range []string{
		"secret", "password", "token", "key", "pass", "pwd", "auth",
		"bearer", "cookie", "session", "signature", "certificate",
		"credential", "private",
	} {
		if strings.Contains(normalized, word) {
			return true
		}
	}
	return false
}

func testStreamNormalizerDoesNotAllocateRecordSizedInputOrMap(t *testing.T) {
	// No payload() call: an unchanged record must never allocate entry data.
	// The source is generated, so the probe itself does not hold a giant string.
	const length = 32 * 1024 * 1024
	for _, prefix := range []string{`{"id":"fixed","value":"`, `{"id":"`} {
		t.Run(prefix, func(t *testing.T) {
			dir := t.TempDir()
			reader := io.MultiReader(strings.NewReader(prefix), &repeatedByteReader{length, 'x'}, strings.NewReader(`"}`))
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			r, err := normalizeRecordStream(t.Context(), newResponseScanner(reader), false, dir, int64(entry.MaxDataSize))
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatal(err)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8*1024*1024 {
				t.Errorf("normalization allocated %d bytes for a %d-byte value; want bounded scratch below 8 MiB", allocated, length)
			}
			info, err := r.file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0600 {
				t.Errorf("scratch mode=%o", info.Mode().Perm())
			}
			if r.size < length {
				t.Errorf("normalized size=%d", r.size)
			}
			if err := r.close(); err != nil {
				t.Fatal(err)
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 {
				t.Fatalf("scratch files=%v error=%v", files, err)
			}
		})
	}
}

func testClientCollectionDoesNotAllocateRecordSizedCopies(t *testing.T) {
	const length = 32 * 1024 * 1024
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"fixed","value":"`)
		_, _ = io.Copy(w, &repeatedByteReader{length, 'x'})
		_, _ = io.WriteString(w, `"}],"nextCursor":null}`)
	}))
	defer server.Close()
	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	result, err := c.collect(t.Context(), datasetCatalog["users"])
	runtime.ReadMemStats(&after)
	if err != nil || len(result.Records) != 1 {
		t.Fatalf("collection records=%d error=%v", len(result.Records), err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 12*1024*1024 {
		t.Fatalf("collection allocated %d bytes for a %d-byte record before entry data was needed", allocated, length)
	}
}

func testClientRejectsRecordAbovePerRecordSafetyLimitBeforeDelivery(t *testing.T) {
	if maxNormalizedRecordSize != 64*1024*1024 {
		t.Fatalf("production per-record safety limit=%d, want %d", maxNormalizedRecordSize, 64*1024*1024)
	}
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	const testLimit = 256
	hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := io.MultiReader(
			strings.NewReader(`{"data":[{"id":"one","value":"`),
			&repeatedByteReader{remaining: testLimit, value: 'x'},
			strings.NewReader(`"}],"nextCursor":null}`),
		)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(body)}, nil
	})}
	c, err := newClient("https://example.invalid/api/v1", "synthetic", 10000, 100, 1, hc)
	if err != nil {
		t.Fatal(err)
	}
	c.recordLimit = testLimit
	delivered := 0
	result, err := c.collectStreaming(t.Context(), datasetCatalog["users"], func(collectedRecord, *normalizedRecord) error {
		delivered++
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "per-record safety limit of 256 bytes") {
		t.Fatalf("oversized record error=%v", err)
	}
	if delivered != 0 || result.Count != 0 {
		t.Fatalf("oversized record reached delivery: callbacks=%d count=%d", delivered, result.Count)
	}
	if files, readErr := os.ReadDir(dir); readErr != nil || len(files) != 0 {
		t.Fatalf("oversized-record scratch files=%v error=%v", files, readErr)
	}
}

func testPluginNormalizedRecordInclusiveLimit(t *testing.T) {
	const limit = 256
	for _, extra := range []int{0, 1} {
		t.Run(fmt.Sprintf("extra=%d", extra), func(t *testing.T) {
			conf := testConfig(t, "http://127.0.0.1:12345/api/v1", "users")
			record := `{"p":"` + strings.Repeat("x", limit-8+extra) + `"}`
			if len(record) != limit+extra {
				t.Fatalf("fixture size=%d", len(record))
			}
			oracle := normalizeTestRecord(t, record, false)
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(io.MultiReader(strings.NewReader(`{"data":[{"p":"`), &repeatedByteReader{remaining: int64(limit - 8 + extra), value: 'x'}, strings.NewReader(`"}],"nextCursor":null}`)))}, nil
			})}
			c, err := newClient(conf.Base_URL, "synthetic", 10000, 100, 1, hc)
			if err != nil {
				t.Fatal(err)
			}
			c.recordLimit = limit
			rt := newMockRuntime(t.Context())
			err = New(conf).collect(t.Context(), rt, c, datasetCatalog["users"])
			key := conf.stateKey(datasetCatalog["users"])
			if extra == 0 {
				if err != nil {
					t.Fatalf("exact limit: %v", err)
				}
				entries := rt.Entries()
				if len(entries) != 1 || len(entries[0].Data) != limit || string(entries[0].Data) != record {
					t.Fatalf("exact limit entries=%v", entries)
				}
				state, e := rt.Get(key)
				want := append(append([]byte{}, oracle.identity[:]...), oracle.fingerprint[:]...)
				if e != nil || !bytes.Equal(state, want) {
					t.Fatalf("completed state=%x want=%x error=%v", state, want, e)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "per-record safety limit of 256 bytes") {
					t.Fatalf("over limit error=%v", err)
				}
				if len(rt.Entries()) != 0 {
					t.Fatalf("over limit delivered %d entries", len(rt.Entries()))
				}
				if _, e := rt.Get(key); !errors.Is(e, storage.ErrStorageNotFound) {
					t.Fatalf("completed state error=%v", e)
				}
			}
			if _, e := rt.Get(key + "/inflight"); !errors.Is(e, storage.ErrStorageNotFound) {
				t.Fatalf("in-flight state error=%v", e)
			}
			if files, e := os.ReadDir(dir); e != nil || len(files) != 0 {
				t.Fatalf("scratch files=%v error=%v", files, e)
			}
		})
	}
}

func testNormalizedRecordPayloadRejectsSizeAboveBoundBeforeReading(t *testing.T) {
	normalizer := newRecordNormalizer(t.TempDir(), int64(entry.MaxDataSize))
	if normalizer.limit != maxNormalizedRecordSize {
		t.Fatalf("normalizer limit=%d, want hard cap %d", normalizer.limit, maxNormalizedRecordSize)
	}
	for name, record := range map[string]*normalizedRecord{
		"size above limit": {size: maxNormalizedRecordSize + 1, limit: maxNormalizedRecordSize},
		"discarded tail":   {size: 128, limit: maxNormalizedRecordSize, oversized: true},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := record.payload(); err == nil || !strings.Contains(err.Error(), "invalid normalized n8n entry size") {
				t.Fatalf("oversized payload error=%v", err)
			}
		})
	}
}

func testStreamNormalizerGiantKeysUseBoundedMemory(t *testing.T) {
	const length = 8 * 1024 * 1024
	dir := t.TempDir()
	reader := io.MultiReader(strings.NewReader(`{"`), &repeatedByteReader{length, 'x'}, strings.NewReader(`Secret":"DROP","id":"one"}`))
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	r, err := normalizeRecordStream(t.Context(), newResponseScanner(reader), true, dir, int64(entry.MaxDataSize))
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8*1024*1024 {
		t.Errorf("giant-key scratch allocations=%d", allocated)
	}
	payload, err := r.payload()
	if err != nil || string(payload) != `{"id":"one"}` {
		t.Fatalf("payload=%s error=%v", payload, err)
	}
}

func testStreamNormalizerSemanticFingerprintAndScrubbing(t *testing.T) {
	a := normalizeTestRecord(t, `{"id":"one","name":"\u0061","nested":{"z":1,"a":[true,null,"\ud83d\ude00"]},"api_key":"DROP","child":{"password":"DROP","safe":"<ok>"}}`, true)
	b := normalizeTestRecord(t, `{"child":{"safe":"<ok>"},"nested":{"a":[true,null,"😀"],"z":1},"name":"a","id":"one"}`, true)
	if a.fingerprint != b.fingerprint || a.identity != b.identity {
		t.Fatal("key or string-escape reordering changed identity/fingerprint")
	}
	data, err := a.payload()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) || bytes.Contains(data, []byte("DROP")) || bytes.Contains(data, []byte{'\n'}) || !bytes.Contains(data, []byte("<ok>")) {
		t.Fatalf("invalid compact/scrubbed data: %s", data)
	}
	wantID := sha256.Sum256([]byte("id:one\x00\""))
	if a.identity != wantID {
		t.Fatal("identity selection changed")
	}
	c := normalizeTestRecord(t, `{"id":"one","name":"a","nested":{"a":[null,true,"😀"],"z":1},"child":{"safe":"<ok>"}}`, true)
	if a.fingerprint == c.fingerprint {
		t.Fatal("array order did not change fingerprint")
	}
}

func testStreamNormalizerWideObjectsSpillAndRemainOrderInvariant(t *testing.T) {
	const count = 3*memberHashBudget + 17
	var a, b strings.Builder
	a.WriteByte('{')
	b.WriteByte('{')
	for i := 0; i < count; i++ {
		if i > 0 {
			a.WriteByte(',')
			b.WriteByte(',')
		}
		fmt.Fprintf(&a, `"f%d":%d`, i, i)
		fmt.Fprintf(&b, `"f%d":%d`, count-i-1, count-i-1)
	}
	a.WriteByte('}')
	b.WriteByte('}')
	first := normalizeTestRecord(t, a.String(), false)
	second := normalizeTestRecord(t, b.String(), false)
	if first.fingerprint != second.fingerprint {
		t.Fatal("spill changed key-order-independent fingerprint")
	}
	// Duplicate keys split across sorted runs must fail before any write.
	bad := strings.TrimSuffix(a.String(), "}") + `,"f0":2}`
	dir := t.TempDir()
	if r, err := normalizeRecordStream(t.Context(), newResponseScanner(strings.NewReader(bad)), false, dir, int64(entry.MaxDataSize)); err == nil {
		_ = r.close()
		t.Fatal("duplicate key across runs was accepted")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("failed-spill scratch files=%v error=%v", files, err)
	}
}

func testStreamNormalizerValidationAndCleanup(t *testing.T) {
	for _, text := range []string{`null`, `{"a":1,}`, `{"a":[1,]}`, `{"a":"\x"}`, `{"a":01}`, `{"a":true,"\u0061":false}`, `{"a":"unterminated`} {
		t.Run(text, func(t *testing.T) {
			dir := t.TempDir()
			if r, err := normalizeRecordStream(t.Context(), newResponseScanner(strings.NewReader(text)), false, dir, int64(entry.MaxDataSize)); err == nil {
				_ = r.close()
				t.Fatal("invalid record was accepted")
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 {
				t.Fatalf("scratch files=%v error=%v", files, err)
			}
		})
	}
	dir := t.TempDir()
	if r, err := normalizeRecordStream(t.Context(), newResponseScanner(strings.NewReader(`{"a":"more than twenty bytes of data"}`)), false, dir, 20); err == nil || !strings.Contains(err.Error(), "per-record safety limit") {
		if r != nil {
			_ = r.close()
		}
		t.Fatalf("entry limit error=%v", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("entry-limit scratch files=%v error=%v", files, err)
	}
}

type failingRecordReader struct{ calls int }

func (r *failingRecordReader) Read(p []byte) (int, error) {
	if r.calls == 0 {
		r.calls++
		return copy(p, `{"a":"partial`), nil
	}
	return 0, errors.New("synthetic read failure")
}
func testStreamNormalizerReadFailureCleansScratch(t *testing.T) {
	dir := t.TempDir()
	if r, err := normalizeRecordStream(t.Context(), newResponseScanner(&failingRecordReader{}), false, dir, int64(entry.MaxDataSize)); err == nil {
		_ = r.close()
		t.Fatal("reader failure was ignored")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("scratch files=%v error=%v", files, err)
	}
}

func testStreamingSecretKeyMatcherMatchesExistingRules(t *testing.T) {
	for _, key := range []string{
		"data", " credentialData ", "api_key", "s_e-c.r_e_t", "PASSword",
		"accesstoken", "authorization", "auth", "passphrase", "pwd", "cookie",
		"bearer", "sessionId", "signature", "certificate", "privateValue",
		"ＴＯＫＥＮ", "to\u200dken", "monkey", "turkey", "keyring", "value",
		" a . data ", "._data  ", "  credential.data  ", " a\u2003data ",
		"Key", "da_ta", " ", "__", "data value",
	} {
		var tracker secretKeyTracker
		for _, r := range key {
			tracker.feed(r)
		}
		if got, want := tracker.secret(), credentialSecretKey(key); got != want {
			t.Errorf("key=%q got %v want %v", key, got, want)
		}
	}
}

func testCredentialScrubberRemovesCommonAndObfuscatedSecretFields(t *testing.T) {
	// Construct the zero-width joiner at runtime so the source stays lint-clean
	// while the key still exercises format-character removal by the scrubber.
	zwj := string(rune(0x200D))
	input := `{"id":"cred-1","name":"safe","authorization":"DROP","passphrase":"DROP","pwd":"DROP","cookie":"DROP","bearer":"DROP","sessionId":"DROP","signature":"DROP","certificate":"DROP","privateValue":"DROP","ＴＯＫＥＮ":"DROP","to` + zwj + `ken":"DROP"}`
	if !strings.ContainsRune(input, rune(0x200D)) || !strings.Contains(input, `"to`+zwj+`ken":"DROP"`) {
		t.Fatal("test input lost its zero-width-joiner obfuscated secret key")
	}
	record := normalizeTestRecord(t, input, true)
	payload, err := record.payload()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte("DROP")) {
		t.Fatalf("credential secret-shaped value survived scrubbing: %s", payload)
	}
	if !bytes.Contains(payload, []byte(`"id":"cred-1"`)) || !bytes.Contains(payload, []byte(`"name":"safe"`)) {
		t.Fatalf("credential metadata was removed: %s", payload)
	}
}

func testStreamNormalizerObjectMemberLimitAppliesPerObjectAndResets(t *testing.T) {
	dir := t.TempDir()
	n := newRecordNormalizer(dir, maxNormalizedRecordSize)
	if n.memberLimit != maxRecordMembers || maxRecordMembers != 250_000 {
		t.Fatalf("default member limit=%d constant=%d want 250000", n.memberLimit, maxRecordMembers)
	}
	n.memberLimit = 5
	defer func() {
		if err := n.cleanup(); err != nil {
			t.Error(err)
		}
	}()

	nestedTotalAboveLimit := `{"outer":{"a":1,"b":2,"c":3},"d":4,"e":5}`
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(nestedTotalAboveLimit)), false); err != nil {
		t.Fatalf("aggregate members across bounded objects: %v", err)
	}

	tooWide := `{"outer":{"a":1,"b":2,"c":3,"d":4,"e":5,"f":6}}`
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(tooWide)), false); err == nil || !strings.Contains(err.Error(), "object member limit") {
		t.Fatalf("per-object member-limit error=%v", err)
	}

	atLimit := `{"a":1,"b":2,"c":3,"d":4,"e":5}`
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(atLimit)), false); err != nil {
		t.Fatalf("record at member limit: %v", err)
	}
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(`{"a":1}`)), false); err != nil {
		t.Fatalf("member count was not reset between records: %v", err)
	}
}

func TestOversizedExpandedRecordStopsCreatingMemberSortScratch(t *testing.T) {
	const members = 100_000
	var body strings.Builder
	body.Grow(members * 12)
	body.WriteString(`{"id":"one"`)
	for i := 0; i < members; i++ {
		fmt.Fprintf(&body, `,"k%d":0`, i)
	}
	body.WriteByte('}')

	n := newRecordNormalizer(t.TempDir(), 64*1024)
	n.allowOversized = true
	n.memberLimit = 0
	defer func() {
		if err := n.cleanup(); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	original := body.String()
	record, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(original)), false)
	if err != nil {
		t.Fatal(err)
	}
	if !record.oversized {
		t.Fatal("wide record did not switch to the bounded oversized path")
	}
	if n.scratchCreated > 6 {
		t.Fatalf("created %d scratch files after crossing the output limit; want at most 6", n.scratchCreated)
	}
	if len(n.paths) != 1 {
		t.Fatalf("retained scratch paths=%d want only the reusable output file", len(n.paths))
	}
	if record.fingerprint == ([sha256.Size]byte{}) {
		t.Fatal("bounded oversized fingerprint is empty")
	}
	firstFingerprint := record.fingerprint
	repeated, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(original)), false)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.fingerprint != firstFingerprint {
		t.Fatal("identical oversized record produced an unstable fingerprint")
	}
	var reorderedBody strings.Builder
	reorderedBody.Grow(len(original))
	reorderedBody.WriteString(`{"id":"one"`)
	for i := members - 1; i >= 0; i-- {
		fmt.Fprintf(&reorderedBody, `,"k%d":0`, i)
	}
	reorderedBody.WriteByte('}')
	reordered, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(reorderedBody.String())), false)
	if err != nil {
		t.Fatal(err)
	}
	if reordered.fingerprint != firstFingerprint {
		t.Fatal("reordered oversized object produced a different fingerprint")
	}
	childFirst := `{"id":"one","child":{"a":1,"b":2},"fill":"` + strings.Repeat("x", 70*1024) + `"}`
	childLast := `{"id":"one","fill":"` + strings.Repeat("x", 70*1024) + `","child":{"b":2,"a":1}}`
	firstOrder, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(childFirst)), false)
	if err != nil {
		t.Fatal(err)
	}
	secondOrder, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(childLast)), false)
	if err != nil {
		t.Fatal(err)
	}
	if firstOrder.fingerprint != secondOrder.fingerprint {
		t.Fatal("moving a nested object across the oversized threshold changed the fingerprint")
	}
	mutatedBody := strings.TrimSuffix(original, "0}") + "1}"
	mutated, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(mutatedBody)), false)
	if err != nil {
		t.Fatal(err)
	}
	if mutated.fingerprint == firstFingerprint {
		t.Fatal("changed oversized record produced the same bounded fingerprint")
	}

	var belowLimit strings.Builder
	belowLimit.WriteString(`{"id":"small"`)
	for i := 0; i < memberHashBudget+100; i++ {
		fmt.Fprintf(&belowLimit, `,"m%d":0`, i)
	}
	belowLimit.WriteByte('}')
	for i := 0; i < 10; i++ {
		record, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(belowLimit.String())), false)
		if err != nil {
			t.Fatal(err)
		}
		if record.oversized {
			t.Fatal("member-sort cleanup fixture unexpectedly exceeded the output limit")
		}
		if len(n.paths) != 1 {
			t.Fatalf("iteration %d retained %d scratch paths; want only the reusable output file", i, len(n.paths))
		}
		if n.members != 0 {
			t.Fatalf("iteration %d retained %d in-memory member digests", i, n.members)
		}
	}
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(`{"id":"dup","x":1,"x":2}`)), false); err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
		t.Fatalf("expanded sub-limit duplicate-key error=%v", err)
	}
}

func testMemberHashBudgetReservesOneDigestPerOpenObject(t *testing.T) {
	dir := t.TempDir()
	n := newRecordNormalizer(dir, maxNormalizedRecordSize)
	n.memberLimit = memberHashBudget + 1
	n.members = memberHashBudget
	n.sorters = make([]*memberSorter, memberHashBudget)
	for i := range n.sorters {
		n.sorters[i] = &memberSorter{owner: n, memory: []memberDigest{{}}}
	}

	if err := n.sorters[len(n.sorters)-1].add(memberDigest{}); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(dir); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("adding one digest with one digest per open object created %d spill files", len(entries))
	}
}

func testStreamNormalizerObjectMemberLimitCountsScrubbedMembers(t *testing.T) {
	n := newRecordNormalizer(t.TempDir(), maxNormalizedRecordSize)
	n.memberLimit = 3
	defer func() {
		if err := n.cleanup(); err != nil {
			t.Error(err)
		}
	}()

	atLimit := `{"password":1,"token":2,"id":"x"}`
	record, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(atLimit)), true)
	if err != nil {
		t.Fatalf("record at member limit: %v", err)
	}
	if payload, err := record.payload(); err != nil {
		t.Fatal(err)
	} else if string(payload) != `{"id":"x"}` {
		t.Fatalf("record at member limit payload=%s", payload)
	}
	overLimit := `{"password":1,"token":2,"secret":3,"id":"x"}`
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(overLimit)), true); err == nil || !strings.Contains(err.Error(), "object member limit") {
		t.Fatalf("scrubbed member-limit error=%v", err)
	}
	nestedAtLimit := `{"password":{"a":1,"b":2,"c":3},"id":"x"}`
	if record, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(nestedAtLimit)), true); err != nil {
		t.Fatalf("scrubbed nested object at per-object limit: %v", err)
	} else if payload, payloadErr := record.payload(); payloadErr != nil || string(payload) != `{"id":"x"}` {
		t.Fatalf("scrubbed nested payload=%s error=%v", payload, payloadErr)
	}
	nestedOverLimit := `{"password":{"a":1,"b":2,"c":3,"d":4},"id":"x"}`
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(nestedOverLimit)), true); err == nil || !strings.Contains(err.Error(), "object member limit") {
		t.Fatalf("nested scrubbed member-limit error=%v", err)
	}
}

func testStreamNormalizerCountsScrubbedGrandchildObjectMembers(t *testing.T) {
	n := newRecordNormalizer(t.TempDir(), maxNormalizedRecordSize)
	n.memberLimit = 3
	defer func() {
		if err := n.cleanup(); err != nil {
			t.Error(err)
		}
	}()

	atLimit := `{"password":{"inner":{"a":1,"b":2,"c":3}},"id":"x"}`
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(atLimit)), true); err != nil {
		t.Fatalf("scrubbed grandchild object at per-object limit: %v", err)
	}
	overLimit := `{"password":{"inner":{"a":1,"b":2,"c":3,"d":4}},"id":"x"}`
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(overLimit)), true); err == nil || !strings.Contains(err.Error(), "object member limit") {
		t.Fatalf("scrubbed grandchild member-limit error=%v", err)
	}
}

type cancelAfterErrChecks struct {
	context.Context
	checks int
	pass   int
}

func (c *cancelAfterErrChecks) Err() error {
	c.checks++
	if c.checks > c.pass {
		return context.Canceled
	}
	return nil
}

func writeMemberRun(t *testing.T, n *recordNormalizer, start byte) string {
	t.Helper()
	f, err := n.scratch()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2048; i++ {
		var raw [64]byte
		raw[30] = byte(i >> 8)
		raw[31] = byte(i)
		raw[63] = start
		if _, err := f.Write(raw[:]); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

func testMemberHashMergeAndFinalDigestHonorCancellation(t *testing.T) {
	t.Run("merge loop", func(t *testing.T) {
		n := newRecordNormalizer(t.TempDir(), maxNormalizedRecordSize)
		defer n.cleanup()
		n.ctx = &cancelAfterErrChecks{Context: context.Background(), pass: 1}
		s := &memberSorter{owner: n}
		left, right := writeMemberRun(t, n, 1), writeMemberRun(t, n, 2)
		if _, err := s.merge(left, right); !errors.Is(err, context.Canceled) {
			t.Fatalf("merge error=%v want context.Canceled", err)
		}
	})

	t.Run("final digest", func(t *testing.T) {
		n := newRecordNormalizer(t.TempDir(), maxNormalizedRecordSize)
		defer n.cleanup()
		n.ctx = &cancelAfterErrChecks{Context: context.Background(), pass: 0}
		s := &memberSorter{owner: n}
		s.runs[0] = writeMemberRun(t, n, 1)
		if _, err := s.digest(); !errors.Is(err, context.Canceled) {
			t.Fatalf("digest error=%v want context.Canceled", err)
		}
	})
}

func testOrdinaryNormalizationCancellationAndCleanup(t *testing.T) {
	for _, scrub := range []bool{false, true} {
		for _, scalar := range []string{"string", "number"} {
			for _, midRead := range []bool{false, true} {
				t.Run(fmt.Sprintf("scrub=%t/%s/mid-read=%t", scrub, scalar, midRead), func(t *testing.T) {
					key := "value"
					if scrub {
						key = "password"
					}
					value := strings.Repeat("1", 1<<20)
					if scalar == "string" {
						value = `"` + value + `"`
					}
					body := `{"id":"one","` + key + `":` + value + `}`
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					var reader io.Reader = strings.NewReader(body)
					var observed *cancelBufferedReader
					if midRead {
						observed = &cancelBufferedReader{r: reader, cancel: cancel, cancelAfter: 8192}
						reader = observed
					} else {
						cancel()
					}
					dir := t.TempDir()
					result, err := normalizeRecordStream(ctx, newResponseScanner(reader), scrub, dir, maxNormalizedRecordSize)
					if result != nil {
						_ = result.close()
						t.Fatal("canceled normalization returned a record")
					}
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("error=%v", err)
					}
					if observed != nil && observed.read >= len(body) {
						t.Fatalf("canceled normalization consumed %d bytes", observed.read)
					}
					files, e := os.ReadDir(dir)
					if e != nil || len(files) != 0 {
						t.Fatalf("scratch files=%v error=%v", files, e)
					}
				})
			}
		}
	}
}

// An unpaired surrogate escape has no UTF-8 form. It is emitted and hashed as
// its own lowercase escape, so distinct keys stay distinct and distinct values
// keep distinct fingerprints.
func testStreamNormalizerUnpairedSurrogateEscapesStayDistinct(t *testing.T) {
	const replacement = "\xef\xbf\xbd" // a literal U+FFFD in the document
	for _, tc := range []struct{ name, input, want string }{
		{"two lone low surrogate keys", `{"\ude00":1,"\ude01":2}`, `{"\ude00":1,"\ude01":2}`},
		{"lone high and lone low surrogate keys", `{"\ud83d":1,"\ude00":2}`, `{"\ud83d":1,"\ude00":2}`},
		{"lone surrogate key beside a literal U+FFFD key", `{"\ud800":1,"` + replacement + `":2}`, `{"\ud800":1,"` + replacement + `":2}`},
		{"uppercase hex digits", `{"v":"\uD83D"}`, `{"v":"\ud83d"}`},
		{"lone high surrogate before a letter", `{"v":"\ud800A"}`, `{"v":"\ud800A"}`},
		{"lone high surrogate before an escaped letter", `{"v":"\ud800\u0041"}`, `{"v":"\ud800A"}`},
		{"lone high surrogate before a valid pair", `{"v":"\ud83d\ud83d\ude00"}`, `{"v":"\ud83d😀"}`},
		{"lone high surrogate before a literal character", `{"v":"\ud83d😀"}`, `{"v":"\ud83d😀"}`},
		{"valid pair", `{"v":"\ud83d\ude00"}`, `{"v":"😀"}`},
		{"low surrogate before a high surrogate", `{"v":"\ude00\ud83d"}`, `{"v":"\ude00\ud83d"}`},
		{"lone surrogate at the end of a string", `{"v":"end\udc00"}`, `{"v":"end\udc00"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := normalizeTestRecord(t, tc.input, false).payload()
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.want {
				t.Fatalf("payload=%q want %q", data, tc.want)
			}
			if !utf8.Valid(data) || !json.Valid(data) {
				t.Fatalf("payload is not valid UTF-8 JSON: %q", data)
			}
		})
	}

	for _, tc := range []struct{ name, input string }{
		{"one lone surrogate spelled in both hex cases", `{"\uDE00":1,"\ude00":2}`},
		{"a surrogate pair and its literal character", `{"\ud83d\ude00":1,"😀":2}`},
		{"a repeated plain key", `{"a":1,"a":2}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := normalizeRecordStream(t.Context(), newResponseScanner(strings.NewReader(tc.input)), false, t.TempDir(), int64(entry.MaxDataSize))
			if err == nil {
				_ = r.close()
				t.Fatal("duplicate key was accepted")
			}
			if !strings.Contains(err.Error(), "duplicate JSON object key") {
				t.Fatalf("error=%v want duplicate-key rejection", err)
			}
		})
	}

	fingerprint := func(value string) [sha256.Size]byte {
		return normalizeTestRecord(t, `{"v":"`+value+`"}`, false).fingerprint
	}
	first, last, literal := fingerprint(`\ud800`), fingerprint(`\udfff`), fingerprint(replacement)
	if first == last || first == literal || last == literal {
		t.Fatal("values that differ only in an unpaired surrogate escape share a fingerprint")
	}
	if fingerprint(`\uD800`) != first {
		t.Fatal("hex digit case of an unpaired surrogate escape changed the fingerprint")
	}
}

func testUnpairedSurrogateKeysDoNotFailDataset(t *testing.T) {
	const second = `{"id":"two","\ude00":1,"\ude01":2}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"one"},`+second+`,{"id":"three"}],"nextCursor":null}`)
	}))
	defer server.Close()
	conf := testConfig(t, server.URL+"/api/v1", "tags")
	state := newBoltPolls(t)

	if first := state.poll(conf, time.Now()); first.err != nil {
		t.Fatalf("first poll: %v", first.err)
	}
	entries := state.output.Entries()
	if len(entries) != 3 {
		t.Fatalf("entries=%d want 3", len(entries))
	}
	if string(entries[1].Data) != second {
		t.Fatalf("second entry=%q want %q", entries[1].Data, second)
	}
	if manifest := state.stored(conf.stateKey(datasetCatalog["tags"])); len(manifest) != 3*recordStateEntrySize {
		t.Fatalf("completed manifest holds %d bytes, want three pairs", len(manifest))
	}

	if again := state.poll(conf, time.Now()); again.err != nil {
		t.Fatalf("second poll: %v", again.err)
	}
	if got := len(state.output.Entries()); got != 3 {
		t.Fatalf("unchanged snapshot wrote %d more entries", got-3)
	}
}

// The secret-key matcher sees U+FFFD for an unpaired surrogate escape, so a
// key such as "api<escape>key" is still scrubbed, and a retained key keeps
// its escape.
func testStreamNormalizerUnpairedSurrogateKeepsScrubDecision(t *testing.T) {
	data, err := normalizeTestRecord(t, `{"id":"c1","api\ud800key":"s","plain\ud800":"kept"}`, true).payload()
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"id":"c1","plain\ud800":"kept"}`; string(data) != want {
		t.Fatalf("payload=%q want %q", data, want)
	}
}

// A raw byte that is not valid UTF-8 is outside JSON. It is replaced with
// U+FFFD in the entry and in the hashes, and keys that become identical after
// replacement are duplicates.
func testStreamNormalizerInvalidUTF8IsReplaced(t *testing.T) {
	data, err := normalizeTestRecord(t, "{\"k\":\"a\xffb\"}", false).payload()
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"k\":\"a\xef\xbf\xbdb\"}"; string(data) != want {
		t.Fatalf("payload=%q want %q", data, want)
	}
	r, err := normalizeRecordStream(t.Context(), newResponseScanner(strings.NewReader("{\"\xff\":1,\"\xfe\":2}")), false, t.TempDir(), int64(entry.MaxDataSize))
	if err == nil {
		_ = r.close()
		t.Fatal("keys that are equal after replacement were accepted")
	}
	if !strings.Contains(err.Error(), "duplicate JSON object key") {
		t.Fatalf("error=%v want duplicate-key rejection", err)
	}
}

// Nesting is counted from the record for a retained value, so the value of a
// top-level member may hold 10,000 nested containers. A scrubbed value is
// validated on its own and may hold 10,001.
func testJSONNestingDepthBoundary(t *testing.T) {
	nested := func(key string, depth int) string {
		return `{"` + key + `":` + strings.Repeat("[", depth) + strings.Repeat("]", depth) + `}`
	}
	for _, tc := range []struct {
		name   string
		key    string
		scrub  bool
		depth  int
		accept bool
	}{
		{name: "retained value at the limit", key: "v", depth: 10000, accept: true},
		{name: "retained value one level deeper", key: "v", depth: 10001},
		{name: "scrubbed value at the limit", key: "data", scrub: true, depth: 10001, accept: true},
		{name: "scrubbed value one level deeper", key: "data", scrub: true, depth: 10002},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := normalizeRecordStream(t.Context(), newResponseScanner(strings.NewReader(nested(tc.key, tc.depth))), tc.scrub, t.TempDir(), int64(entry.MaxDataSize))
			if err == nil {
				defer func() { _ = r.close() }()
			}
			if tc.accept {
				if err != nil {
					t.Fatalf("depth %d was rejected: %v", tc.depth, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "JSON nesting limit exceeded") {
				t.Fatalf("depth %d error=%v want the nesting limit", tc.depth, err)
			}
		})
	}
}

// The identity digest reads an unpaired surrogate escape as U+FFFD, so
// identity values are compared after that replacement while the entry and the
// fingerprint keep the escape. Two records on one page whose IDs differ only
// in such escapes, or in an escape against a literal U+FFFD, share an identity
// with different fingerprints and fail the dataset as conflicting records. An
// escape and the same six characters written as text stay two identities.
func testIdentityValuesAreComparedAfterReplacement(t *testing.T) {
	const replacement = "\xef\xbf\xbd" // a literal U+FFFD in the document
	const escaped = `{"id":"\ud800","v":1}`
	for _, tc := range []struct {
		name, other string
		conflict    bool
	}{
		{"two unpaired surrogate escapes", `{"id":"\ud801","v":1}`, true},
		{"an escape and a literal U+FFFD", `{"id":"` + replacement + `","v":1}`, true},
		{"an escape and the six-character text", `{"id":"\\ud800","v":1}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"data":[`+escaped+`,`+tc.other+`],"nextCursor":null}`)
			}))
			defer server.Close()
			conf := testConfig(t, server.URL+"/api/v1", "tags")
			state := newBoltPolls(t)

			result := state.poll(conf, time.Now())
			entries := state.output.Entries()
			manifest := state.stored(conf.stateKey(datasetCatalog["tags"]))
			if !tc.conflict {
				if result.err != nil {
					t.Fatalf("poll: %v", result.err)
				}
				if len(entries) != 2 {
					t.Fatalf("entries=%d want 2", len(entries))
				}
				if len(manifest) != 2*recordStateEntrySize {
					t.Fatalf("completed manifest holds %d bytes, want two pairs", len(manifest))
				}
				return
			}
			if result.err == nil || !strings.Contains(result.err.Error(), "conflicting records with one identity on page 1") {
				t.Fatalf("poll error=%v want a conflicting-records failure", result.err)
			}
			if len(entries) != 1 {
				t.Fatalf("entries=%d want 1", len(entries))
			}
			if string(entries[0].Data) != escaped {
				t.Fatalf("entry=%q want the first record %q", entries[0].Data, escaped)
			}
			if len(manifest) != 0 {
				t.Fatalf("completed manifest holds %d bytes after a failed poll, want none", len(manifest))
			}
		})
	}
}

// Standalone normalization transfers the scratch file to its caller. Streaming
// collection instead owns one normalizer and reuses its file and write buffer.
func normalizeRecordStream(ctx context.Context, scanner *responseScanner, scrub bool, tempDir string, limit int64) (result *normalizedRecord, err error) {
	n := newRecordNormalizer(tempDir, limit)
	defer func() { err = errors.Join(err, n.cleanup()) }()
	result, err = n.normalize(ctx, scanner, scrub)
	if err == nil {
		delete(n.paths, result.file.Name())
		n.output = nil
	}
	return
}

func (r *normalizedRecord) close() error {
	err := r.file.Close()
	removeErr := os.Remove(r.file.Name())
	if err != nil || (removeErr != nil && !errors.Is(removeErr, os.ErrNotExist)) {
		return errors.New("remove temporary n8n record failed")
	}
	return nil
}

func testRecordTimestampRejectsOutOfRangeNumericAndStringValues(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name  string
		value string
	}{
		{name: "numeric milliseconds", value: "1766000000000"},
		{name: "numeric negative", value: "-1"},
		{name: "numeric far future", value: "9999999999"},
		{name: "string before epoch", value: `"0001-01-01T00:00:00Z"`},
		{name: "string far future", value: `"9999-12-31T23:59:59Z"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeTestRecord(t, fmt.Sprintf(`{"createdAt":%s}`, tc.value), false).timestamp
			if got.Before(now.Add(-time.Minute)) || got.After(now.Add(25*time.Hour)) {
				t.Fatalf("timestamp=%s is outside the sane fallback window", got)
			}
		})
	}
	valid := normalizeTestRecord(t, `{"createdAt":1766000000}`, false).timestamp
	if got, want := valid.Unix(), int64(1766000000); got != want {
		t.Fatalf("valid seconds timestamp=%d want %d", got, want)
	}
	validString := normalizeTestRecord(t, `{"createdAt":"2026-10-19T12:00:00Z"}`, false).timestamp
	if got, want := validString.Unix(), int64(1792411200); got != want {
		t.Fatalf("valid string timestamp=%d want %d", got, want)
	}
	for _, first := range []string{"1766000000000", `"9999-12-31T23:59:59Z"`} {
		validLaterField := normalizeTestRecord(t, fmt.Sprintf(`{"timestamp":%s,"startedAt":"2026-10-19T12:00:00Z"}`, first), false).timestamp
		if got, want := validLaterField.Unix(), int64(1792411200); got != want {
			t.Fatalf("out-of-range value %s did not fall through: got %d want %d", first, got, want)
		}
	}
}

// The streaming normalizer is part of the n8n client boundary. These grouped
// tests keep that responsibility visible without creating another public test file.
func TestClientRecordEncodingAndCancellation(t *testing.T) {
	t.Run("MemberHashMergeAndFinalDigestHonorCancellation", testMemberHashMergeAndFinalDigestHonorCancellation)
	t.Run("OrdinaryNormalizationCancellationAndCleanup", testOrdinaryNormalizationCancellationAndCleanup)
	t.Run("StreamNormalizerUnpairedSurrogateEscapesStayDistinct", testStreamNormalizerUnpairedSurrogateEscapesStayDistinct)
	t.Run("UnpairedSurrogateKeysDoNotFailDataset", testUnpairedSurrogateKeysDoNotFailDataset)
	t.Run("StreamNormalizerUnpairedSurrogateKeepsScrubDecision", testStreamNormalizerUnpairedSurrogateKeepsScrubDecision)
	t.Run("StreamNormalizerInvalidUTF8IsReplaced", testStreamNormalizerInvalidUTF8IsReplaced)
	t.Run("RecordTimestampRejectsOutOfRangeNumericAndStringValues", testRecordTimestampRejectsOutOfRangeNumericAndStringValues)
}

func TestClientRecordIdentityAndScrubbing(t *testing.T) {
	t.Run("StreamIdentityDistinguishesNumericAndStringIDs", testStreamIdentityDistinguishesNumericAndStringIDs)
	t.Run("StreamIdentityPrecedenceAndFingerprintFallback", testStreamIdentityPrecedenceAndFingerprintFallback)
	t.Run("StreamNormalizerSemanticFingerprintAndScrubbing", testStreamNormalizerSemanticFingerprintAndScrubbing)
	t.Run("StreamingSecretKeyMatcherMatchesExistingRules", testStreamingSecretKeyMatcherMatchesExistingRules)
	t.Run("CredentialScrubberRemovesCommonAndObfuscatedSecretFields", testCredentialScrubberRemovesCommonAndObfuscatedSecretFields)
	t.Run("IdentityValuesAreComparedAfterReplacement", testIdentityValuesAreComparedAfterReplacement)
}

func TestClientRecordResourceBounds(t *testing.T) {
	t.Run("StreamNormalizerGiantKeysUseBoundedMemory", testStreamNormalizerGiantKeysUseBoundedMemory)
	t.Run("StreamNormalizerWideObjectsSpillAndRemainOrderInvariant", testStreamNormalizerWideObjectsSpillAndRemainOrderInvariant)
	t.Run("StreamNormalizerValidationAndCleanup", testStreamNormalizerValidationAndCleanup)
	t.Run("StreamNormalizerReadFailureCleansScratch", testStreamNormalizerReadFailureCleansScratch)
	t.Run("StreamNormalizerObjectMemberLimitAppliesPerObjectAndResets", testStreamNormalizerObjectMemberLimitAppliesPerObjectAndResets)
	t.Run("MemberHashBudgetReservesOneDigestPerOpenObject", testMemberHashBudgetReservesOneDigestPerOpenObject)
	t.Run("StreamNormalizerObjectMemberLimitCountsScrubbedMembers", testStreamNormalizerObjectMemberLimitCountsScrubbedMembers)
	t.Run("StreamNormalizerCountsScrubbedGrandchildObjectMembers", testStreamNormalizerCountsScrubbedGrandchildObjectMembers)
	t.Run("JSONNestingDepthBoundary", testJSONNestingDepthBoundary)
}

func TestClientStreamingDelivery(t *testing.T) {
	t.Run("StreamingTinyRecordsReuseScratchAndRetainNoMetadata", testStreamingTinyRecordsReuseScratchAndRetainNoMetadata)
	t.Run("StreamingScratchReusePreservesPayloadAndCleansCallbackFailure", testStreamingScratchReusePreservesPayloadAndCleansCallbackFailure)
	t.Run("StreamNormalizerDoesNotAllocateRecordSizedInputOrMap", testStreamNormalizerDoesNotAllocateRecordSizedInputOrMap)
	t.Run("ClientCollectionDoesNotAllocateRecordSizedCopies", testClientCollectionDoesNotAllocateRecordSizedCopies)
	t.Run("ClientRejectsRecordAbovePerRecordSafetyLimitBeforeDelivery", testClientRejectsRecordAbovePerRecordSafetyLimitBeforeDelivery)
	t.Run("PluginNormalizedRecordInclusiveLimit", testPluginNormalizedRecordInclusiveLimit)
	t.Run("NormalizedRecordPayloadRejectsSizeAboveBoundBeforeReading", testNormalizedRecordPayloadRejectsSizeAboveBoundBeforeReading)
}
