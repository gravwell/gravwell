/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestRetryHttpClientClassifiesRateLimitWait(t *testing.T) {
	limiter := rate.NewLimiter(rate.Every(time.Hour), 1)
	if !limiter.Allow() {
		t.Fatal("could not consume initial limiter token")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	req, err := http.NewRequest(http.MethodGet, "https://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewRetryHttpClientWithClient(limiter, http.DefaultClient, time.Millisecond, ctx, nil).Do(req)
	if !errors.Is(err, ErrRateLimitWait) {
		t.Fatalf("err=%v", err)
	}
}

func TestRetryHttpClientDefaultBehaviorRemainsCompatible(t *testing.T) {
	var gotHeader atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader.Store(r.Header.Get("X-Custom-Token") == "synthetic")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	req, err := http.NewRequest(http.MethodGet, source.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Custom-Token", "synthetic")
	client := NewRetryHttpClient(nil, time.Second, time.Millisecond, t.Context(), nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	DrainResponse(resp)
	if !gotHeader.Load() {
		t.Fatal("legacy constructor behavior changed")
	}
}

func TestRetryHttpClientInjectedPolicyBlocksCrossOriginHeader(t *testing.T) {
	var targetRequests atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetRequests.Add(1)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	origin, err := url.Parse(source.URL)
	if err != nil {
		t.Fatal(err)
	}
	base := &http.Client{
		Timeout: time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if !sameOriginURL(origin, req.URL) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	req, err := http.NewRequest(http.MethodGet, source.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Custom-Token", "synthetic")
	client := NewRetryHttpClientWithClient(nil, base, time.Millisecond, t.Context(), nil)
	resp, err := client.Do(req)
	if resp != nil {
		DrainResponse(resp)
	}
	if err != nil || resp == nil || resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status=%v err=%v", responseStatus(resp), err)
	}
	if targetRequests.Load() != 0 {
		t.Fatalf("credential target received %d requests", targetRequests.Load())
	}
}

func TestRetryHttpClientInjectedPolicyAllowsSameOriginRedirect(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, server.URL+"/final", http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	origin, _ := url.Parse(server.URL)
	base := &http.Client{
		Timeout: time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if !sameOriginURL(origin, req.URL) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/start", nil)
	resp, err := NewRetryHttpClientWithClient(nil, base, time.Millisecond, t.Context(), nil).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	DrainResponse(resp)
	if resp.StatusCode != http.StatusNoContent || resp.Request.URL.Path != "/final" {
		t.Fatalf("status=%d url=%s", resp.StatusCode, resp.Request.URL)
	}
}

func TestRetryHttpClientWithClientRetainsRetryAndCancellation(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		var calls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		resp, err := NewRetryHttpClientWithClient(nil, server.Client(), time.Millisecond, t.Context(), nil).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		DrainResponse(resp)
		if calls.Load() != 2 {
			t.Fatalf("calls=%d", calls.Load())
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		resp, err := NewRetryHttpClientWithClient(nil, server.Client(), time.Millisecond, ctx, nil).Do(req)
		if resp != nil {
			DrainResponse(resp)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestRetryHttpClientHonorsRetryAfter(t *testing.T) {
	if got := boundedRetryAfter("999999999999999999999999999999", time.Now()); got != MaximumRetryAfter {
		t.Fatalf("oversized Retry-After=%s, want clamp %s", got, MaximumRetryAfter)
	}
	t.Run("delta-seconds-is-cancellable", func(t *testing.T) {
		var calls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		client := NewRetryHttpClientWithClient(nil, server.Client(), time.Millisecond, ctx, nil)
		client.SetRetryAfterLimit(MaximumRetryAfter)
		_, err := client.Do(req)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error=%v, want cancellable Retry-After wait", err)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("calls=%d, want no retry before Retry-After", got)
		}
	})

	t.Run("http-date-exceeds-local-backoff", func(t *testing.T) {
		var calls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Retry-After", time.Now().Add(time.Minute).UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		client := NewRetryHttpClientWithClient(nil, server.Client(), time.Millisecond, ctx, nil)
		client.SetRetryAfterLimit(MaximumRetryAfter)
		_, err := client.Do(req)
		if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
			t.Fatalf("calls=%d error=%v, want HTTP-date wait to outlive the context", calls.Load(), err)
		}
	})
}

func TestRetryHttpClientDefaultRetryAfterDoesNotOverrideBackoff(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "86400")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := NewRetryHttpClientWithClient(nil, server.Client(), 5*time.Millisecond, ctx, nil).Do(req)
	if err != nil {
		t.Fatalf("default retry policy remained blocked by origin Retry-After: %v", err)
	}
	DrainResponse(resp)
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls=%d, want one backoff and retry", got)
	}
}

func TestRetryHttpClientWithClientPreservesTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	base := server.Client()
	base.Timeout = 5 * time.Millisecond
	resp, err := NewRetryHttpClientWithClient(nil, base, time.Millisecond, ctx, nil).Do(req)
	if resp != nil {
		DrainResponse(resp)
	}
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

// retryRoundTripper lets a test hand the retry loop a response it could not get
// from a real server: one whose declared Content-Length does not match the body
// it carries, or one whose body never ends.
type retryRoundTripper func(*http.Request) (*http.Response, error)

func (f retryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// countingBody records how much of itself was read and whether it was closed,
// which is what makes the drain bound observable rather than assumed.
type countingBody struct {
	mu     sync.Mutex
	reader io.Reader
	read   int64
	closes int
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.mu.Lock()
	b.read += int64(n)
	b.mu.Unlock()
	return n, err
}

func (b *countingBody) Close() error {
	b.mu.Lock()
	b.closes++
	b.mu.Unlock()
	return nil
}

func (b *countingBody) bytesRead() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.read
}

func (b *countingBody) closed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closes > 0
}

// bulkReader stands in for a body far larger than the drain bound without
// allocating one. It carries a budget so that an unbounded drain fails on the
// byte count rather than running until the package times out; nothing under test
// may read anywhere near it.
type bulkReader struct{ remaining int64 }

func newBulkReader() *bulkReader { return &bulkReader{remaining: 64 << 20} }

func (r *bulkReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	for i := range p {
		p[i] = 'a'
	}
	r.remaining -= int64(len(p))
	return len(p), nil
}

// TestDrainResponseBoundedHonorsItsBound pins the exact inclusive boundary of
// the shared retry drain, so the bound cannot be removed, widened, or made
// off-by-one without a failure here.
func TestDrainResponseBoundedHonorsItsBound(t *testing.T) {
	const limit = 16
	for _, tc := range []struct {
		name          string
		size          int
		contentLength int64
		wantRead      int64
		reject        bool
	}{
		{name: "under-the-bound", size: limit - 1, contentLength: limit - 1, wantRead: limit - 1},
		{name: "exactly-at-the-bound", size: limit, contentLength: limit, wantRead: limit},
		{name: "one-byte-over", size: limit + 1, contentLength: -1, wantRead: limit + 1, reject: true},
		{name: "declared-over-is-never-read", size: limit, contentLength: limit + 1, wantRead: 0, reject: true},
		{name: "undeclared-and-bounded", size: limit, contentLength: -1, wantRead: limit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &countingBody{reader: bytes.NewReader(bytes.Repeat([]byte("x"), tc.size))}
			resp := &http.Response{ContentLength: tc.contentLength, Body: body}
			err := DrainResponseBounded(resp, limit)
			if tc.reject != errors.Is(err, ErrOversizedResponse) {
				t.Fatalf("err=%v, want oversized=%t", err, tc.reject)
			}
			if got := body.bytesRead(); got != tc.wantRead {
				t.Fatalf("read %d bytes, want %d: the bound did not govern the read", got, tc.wantRead)
			}
			if !body.closed() {
				t.Fatal("a drained body was left open")
			}
		})
	}

	t.Run("a-bulk-body-is-abandoned-at-the-bound", func(t *testing.T) {
		body := &countingBody{reader: newBulkReader()}
		resp := &http.Response{ContentLength: -1, Body: body}
		if err := DrainResponseBounded(resp, limit); !errors.Is(err, ErrOversizedResponse) {
			t.Fatalf("err=%v, want the bound to stop a body that keeps arriving", err)
		}
		if got := body.bytesRead(); got > limit+1 {
			t.Fatalf("read %d bytes of a body that keeps arriving, want at most %d", got, limit+1)
		}
		if !body.closed() {
			t.Fatal("an abandoned body was left open")
		}
	})

	t.Run("a-nil-body-is-not-an-error", func(t *testing.T) {
		if err := DrainResponseBounded(nil, limit); err != nil {
			t.Fatalf("err=%v", err)
		}
		if err := DrainResponseBounded(&http.Response{ContentLength: 1 << 40}, limit); err != nil {
			t.Fatalf("err=%v", err)
		}
	})
}

// boundedRetryContext gives the retry loop a deadline of its own. A retryable
// status with no drain bound is retried until the context ends, so a regression
// has to be able to end: this is what lets the assertions below report a failure
// rather than a package timeout.
func boundedRetryContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestRetryHttpClientBoundsRetryableResponseBodies pins the repair: a retryable
// 425, 429 or 5xx body is drained under DefaultRetryDrainLimit instead of being
// read whole once per attempt. Before this, a large, chunked, or transparently
// decompressed body cost unbounded bytes and CPU on every retry, and the plugin
// byte bounds downstream never saw it because the retry loop consumed the body
// before any of them applied.
func TestRetryHttpClientBoundsRetryableResponseBodies(t *testing.T) {
	t.Run("declared-length-is-refused-before-reading", func(t *testing.T) {
		var attempts atomic.Int64
		body := &countingBody{reader: newBulkReader()}
		base := &http.Client{Transport: retryRoundTripper(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{
				StatusCode:    http.StatusServiceUnavailable,
				Header:        make(http.Header),
				ContentLength: 4 * DefaultRetryDrainLimit,
				Body:          body,
			}, nil
		})}
		req, _ := http.NewRequest(http.MethodGet, "https://fixture.invalid", nil)
		resp, err := NewRetryHttpClientWithClient(nil, base, time.Millisecond, boundedRetryContext(t), nil).Do(req)
		if !errors.Is(err, ErrOversizedResponse) {
			t.Fatalf("err=%v, want the declared length refused", err)
		}
		if resp != nil {
			t.Fatal("an oversized response was handed back with its body already closed")
		}
		if got := body.bytesRead(); got != 0 {
			t.Fatalf("read %d bytes of a declared-oversize body, want none", got)
		}
		if !body.closed() {
			t.Fatal("a refused response body was left open")
		}
		if got := attempts.Load(); got != 1 {
			t.Fatalf("attempts=%d, want the retry loop to stop at the bound", got)
		}
	})

	t.Run("an-undeclared-body-is-abandoned-at-the-bound", func(t *testing.T) {
		var attempts atomic.Int64
		body := &countingBody{reader: newBulkReader()}
		base := &http.Client{Transport: retryRoundTripper(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{
				StatusCode:    http.StatusTooManyRequests,
				Header:        make(http.Header),
				ContentLength: -1,
				Body:          body,
			}, nil
		})}
		req, _ := http.NewRequest(http.MethodGet, "https://fixture.invalid", nil)
		_, err := NewRetryHttpClientWithClient(nil, base, time.Millisecond, boundedRetryContext(t), nil).Do(req)
		if !errors.Is(err, ErrOversizedResponse) {
			t.Fatalf("err=%v, want a chunked body refused at the bound", err)
		}
		if got := body.bytesRead(); got > DefaultRetryDrainLimit+1 {
			t.Fatalf("read %d bytes of a chunked body, want at most %d", got, DefaultRetryDrainLimit+1)
		}
		if !body.closed() || attempts.Load() != 1 {
			t.Fatalf("closed=%t attempts=%d", body.closed(), attempts.Load())
		}
	})

	t.Run("a-decompressed-body-is-bounded-by-what-it-expands-to", func(t *testing.T) {
		// A few hundred bytes on the wire that net/http expands past the bound.
		// The declared length describes the compressed form, so only the read
		// bound can stop this one.
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(bytes.Repeat([]byte("a"), 8*DefaultRetryDrainLimit)); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if compressed.Len() >= DefaultRetryDrainLimit {
			t.Fatalf("the compressed fixture is %d bytes, so it would fail the bound before expanding", compressed.Len())
		}
		var attempts atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			if r.Header.Get("Accept-Encoding") != "gzip" {
				t.Errorf("Accept-Encoding=%q, want net/http to have requested transparent gzip", r.Header.Get("Accept-Encoding"))
			}
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write(compressed.Bytes())
		}))
		defer server.Close()
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		_, err := NewRetryHttpClientWithClient(nil, server.Client(), time.Millisecond, boundedRetryContext(t), nil).Do(req)
		if !errors.Is(err, ErrOversizedResponse) {
			t.Fatalf("err=%v, want the expanded body refused at the bound", err)
		}
		if got := attempts.Load(); got != 1 {
			t.Fatalf("attempts=%d, want the retry loop to stop at the bound", got)
		}
	})

	t.Run("a-bounded-body-still-retries", func(t *testing.T) {
		// Exactly the bound is still drained and retried, so the repair costs
		// the retry semantics nothing for the diagnostic bodies these statuses
		// actually carry.
		var attempts atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if attempts.Add(1) == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write(bytes.Repeat([]byte("a"), DefaultRetryDrainLimit))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		resp, err := NewRetryHttpClientWithClient(nil, server.Client(), time.Millisecond, t.Context(), nil).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer DrainResponse(resp)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status=%d, want the attempt after the bounded retry", resp.StatusCode)
		}
		if got := attempts.Load(); got != 2 {
			t.Fatalf("attempts=%d, want the bounded body retried exactly once", got)
		}
	})

	t.Run("cancellation-stops-the-loop-after-a-bounded-drain", func(t *testing.T) {
		var attempts atomic.Int64
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			// The context ends while this response is being produced, so the
			// drain runs and the backoff is what has to observe cancellation.
			cancel()
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write(bytes.Repeat([]byte("a"), 1<<10))
		}))
		defer server.Close()
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		resp, err := NewRetryHttpClientWithClient(nil, server.Client(), time.Hour, ctx, nil).Do(req)
		if resp != nil {
			DrainResponse(resp)
		}
		if errors.Is(err, ErrOversizedResponse) {
			t.Fatalf("a bounded body was reported oversized: %v", err)
		}
		if got := attempts.Load(); got != 1 {
			t.Fatalf("attempts=%d, want cancellation to stop the loop after one bounded drain", got)
		}
	})

	t.Run("a-non-retryable-oversized-body-is-untouched", func(t *testing.T) {
		// The bound governs only the drain. A response the caller is going to
		// read keeps whatever limit the caller applies to it.
		body := &countingBody{reader: bytes.NewReader(bytes.Repeat([]byte("a"), 4*DefaultRetryDrainLimit))}
		base := &http.Client{Transport: retryRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusForbidden,
				Header:        make(http.Header),
				ContentLength: 4 * DefaultRetryDrainLimit,
				Body:          body,
			}, nil
		})}
		req, _ := http.NewRequest(http.MethodGet, "https://fixture.invalid", nil)
		resp, err := NewRetryHttpClientWithClient(nil, base, time.Millisecond, t.Context(), nil).Do(req)
		if err != nil || resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status=%d err=%v", responseStatus(resp), err)
		}
		if body.closed() || body.bytesRead() != 0 {
			t.Fatalf("closed=%t read=%d, want the caller's body handed over intact", body.closed(), body.bytesRead())
		}
		DrainResponse(resp)
	})
}

func sameOriginURL(left, right *url.URL) bool {
	return left != nil && right != nil && left.Scheme == right.Scheme && left.Host == right.Host
}

func responseStatus(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
