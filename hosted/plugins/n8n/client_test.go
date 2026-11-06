/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package n8n

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestClientForcesExecutionOutputExclusion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/executions" || r.URL.Query().Get("includeData") != "false" {
			t.Fatalf("unexpected execution request: %s query=%v", r.URL.Path, r.URL.Query())
		}
		_, _ = w.Write([]byte(`{"data":[],"nextCursor":null}`))
	}))
	defer server.Close()
	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.collect(t.Context(), datasetCatalog["executions"])
	if err != nil {
		t.Fatal(err)
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
	for _, cursor := range []string{"", "   "} {
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
