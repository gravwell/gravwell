package slack

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestClientFetchesOneAuthenticatedPage(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != auditPath || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Fatalf("request path=%q auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("oldest") != "10" || r.URL.Query().Get("latest") != "20" ||
			r.URL.Query().Get("limit") != "1000" || r.URL.Query().Get("cursor") != "cursor-1" {
			t.Fatalf("query=%v", r.URL.Query())
		}
		fmt.Fprint(w, `{"entries":[{"id":"one","date_create":10}],"response_metadata":{"next_cursor":"cursor-2"}}`)
	}))
	defer server.Close()

	client, err := newClient(server.URL, "synthetic-token", 30, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter.SetLimit(rate.Inf)
	page, err := client.fetch(t.Context(), 10, 20, "cursor-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.records) != 1 || page.nextCursor != "cursor-2" || requests.Load() != 1 {
		t.Fatalf("page=%+v requests=%d", page, requests.Load())
	}
}

func TestClientRejectsCredentialedRedirect(t *testing.T) {
	var escaped atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { escaped.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	client, err := newClient(source.URL, "synthetic-token", 30, source.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter.SetLimit(rate.Inf)
	if _, err := client.fetch(t.Context(), 1, 2, ""); err == nil {
		t.Fatal("redirect was accepted")
	}
	if escaped.Load() != 0 {
		t.Fatal("redirect target received the credentialed request")
	}
}

func TestClientClassifiesInvalidCursorAndRetryAfter(t *testing.T) {
	t.Run("invalid cursor", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"ok":false,"error":"invalid_cursor"}`)
		}))
		defer server.Close()
		client, _ := newClient(server.URL, "synthetic-token", 30, server.Client())
		client.limiter.SetLimit(rate.Inf)
		if _, err := client.fetch(t.Context(), 1, 2, "expired"); !errors.Is(err, errInvalidCursor) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("retry after", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "600")
			http.Error(w, "retry", http.StatusTooManyRequests)
		}))
		defer server.Close()
		client, _ := newClient(server.URL, "synthetic-token", 30, server.Client())
		client.limiter.SetLimit(rate.Inf)
		_, err := client.fetch(t.Context(), 1, 2, "")
		var deferred *retryDeferral
		if !errors.As(err, &deferred) || deferred.delay != maximumRetrySleep || deferred.retryAt.IsZero() {
			t.Fatalf("error=%v deferral=%+v", err, deferred)
		}
	})
}

func TestClientRejectsMalformedSuccessfulResponses(t *testing.T) {
	for name, response := range map[string]string{
		"application error": `{"ok":false,"error":"not_allowed","entries":[]}`,
		"missing entries":   `{"ok":true,"response_metadata":{}}`,
		"non-array entries": `{"ok":true,"entries":{},"response_metadata":{}}`,
		"trailing value":    `{"ok":true,"entries":[],"response_metadata":{}} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, response)
			}))
			defer server.Close()
			client, _ := newClient(server.URL, "synthetic-token", 30, server.Client())
			client.limiter.SetLimit(rate.Inf)
			if _, err := client.fetch(t.Context(), 1, 2, ""); err == nil {
				t.Fatal("malformed response accepted")
			}
		})
	}
}

// TestClientReducesRequestSizeUntilAnOversizedPageFits proves an oversized
// successful response is resolved by requesting fewer records at the same
// cursor, not by repeating one unreadable request, and that the reduced size is
// retained for later requests.
func TestClientReducesRequestSizeUntilAnOversizedPageFits(t *testing.T) {
	const fitsAt = 250
	var limits []string
	var cursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limits = append(limits, r.URL.Query().Get("limit"))
		cursors = append(cursors, r.URL.Query().Get("cursor"))
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil {
			t.Fatalf("limit=%q error=%v", r.URL.Query().Get("limit"), err)
		}
		if limit > fitsAt {
			fmt.Fprintf(w, `{"ok":true,"entries":[{"blob":%q}],"response_metadata":{}}`, strings.Repeat("x", maximumAuditPageSize))
			return
		}
		fmt.Fprint(w, `{"ok":true,"entries":[{"id":"one","date_create":10}],"response_metadata":{"next_cursor":"cursor-2"}}`)
	}))
	defer server.Close()

	client, err := newClient(server.URL, "synthetic-token", 30, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter.SetLimit(rate.Inf)
	page, err := client.fetch(t.Context(), 10, 20, "cursor-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.records) != 1 || page.nextCursor != "cursor-2" {
		t.Fatalf("page=%+v", page)
	}
	// 1000 and 500 are oversized, 250 fits. Every attempt keeps the same cursor.
	if want := []string{"1000", "500", "250"}; !slices.Equal(limits, want) {
		t.Fatalf("requested limits=%v want=%v", limits, want)
	}
	for _, cursor := range cursors {
		if cursor != "cursor-1" {
			t.Fatalf("reduction left the cursor: cursors=%v", cursors)
		}
	}
	if client.limit != 250 {
		t.Fatalf("retained limit=%d want=250", client.limit)
	}

	limits = nil
	if _, err := client.fetch(t.Context(), 10, 20, "cursor-2"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"250"}; !slices.Equal(limits, want) {
		t.Fatalf("second request limits=%v want=%v", limits, want)
	}
}

// TestClientReportsIrreduciblyOversizedPage proves a response that is still over
// the ceiling at one requested record is a visible error rather than a silent
// skip or an unbounded retry.
func TestClientReportsIrreduciblyOversizedPage(t *testing.T) {
	var requests atomic.Int32
	var smallest int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if limit, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && (smallest == 0 || limit < smallest) {
			smallest = limit
		}
		fmt.Fprintf(w, `{"ok":true,"entries":[{"blob":%q}],"response_metadata":{}}`, strings.Repeat("x", maximumAuditPageSize))
	}))
	defer server.Close()
	client, err := newClient(server.URL, "synthetic-token", 30, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter.SetLimit(rate.Inf)
	_, err = client.fetch(t.Context(), 1, 2, "")
	var oversized *oversizedPageError
	if !errors.As(err, &oversized) || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized response error=%v", err)
	}
	if oversized.limit != minimumPageSize || smallest != minimumPageSize {
		t.Fatalf("terminal limit=%d smallest requested=%d want=%d", oversized.limit, smallest, minimumPageSize)
	}
	// 1000, 500, 250, 125, 62, 31, 15, 7, 3, 1: a bounded reduction, not an
	// endless retry of the same request.
	if got := int(requests.Load()); got != 10 {
		t.Fatalf("attempts=%d want=10", got)
	}
	if client.limit != minimumPageSize {
		t.Fatalf("retained limit=%d want=%d", client.limit, minimumPageSize)
	}
}

// TestRetryDelayBounds pins every Retry-After class to the operational ceiling.
// The header is vendor controlled, and a delay above maximumRetrySleep would be
// persisted as a deadline the five-minute wake cadence can never clear.
func TestRetryDelayBounds(t *testing.T) {
	now := time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{name: "numeric below ceiling", raw: "120", want: 120 * time.Second},
		{name: "numeric at ceiling", raw: "300", want: maximumRetrySleep},
		{name: "numeric above ceiling", raw: "600", want: maximumRetrySleep},
		{name: "numeric overflow", raw: "18446744073709551615", want: maximumRetrySleep},
		{name: "numeric unsigned maximum plus one", raw: "18446744073709551616", want: defaultRetryDelay},
		{name: "http date below ceiling", raw: now.Add(2 * time.Minute).Format(http.TimeFormat), want: 2 * time.Minute},
		{name: "http date above ceiling", raw: now.Add(8760 * time.Hour).Format(http.TimeFormat), want: maximumRetrySleep},
		{name: "http date in the past", raw: now.Add(-time.Hour).Format(http.TimeFormat), want: defaultRetryDelay},
		{name: "zero", raw: "0", want: defaultRetryDelay},
		{name: "negative", raw: "-5", want: defaultRetryDelay},
		{name: "malformed", raw: "invalid", want: defaultRetryDelay},
		{name: "empty", raw: "", want: defaultRetryDelay},
		{name: "padded numeric", raw: " 120 ", want: 120 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := retryDelay(tc.raw, now)
			if got != tc.want {
				t.Fatalf("delay=%v want=%v", got, tc.want)
			}
			if got > maximumRetrySleep {
				t.Fatalf("delay=%v exceeds the operational ceiling %v", got, maximumRetrySleep)
			}
			if deadline := now.Add(got); deadline.UnixNano() < 0 || deadline.After(time.Unix(0, math.MaxInt64).UTC()) {
				t.Fatalf("delay=%v produces an unpersistable deadline %v", got, deadline)
			}
		})
	}
}

// TestRetryDelayNearTheRepresentableLimitStaysPersistable keeps the pre-existing
// persistable-deadline clamp honest once the operational ceiling is in front of
// it.
func TestRetryDelayNearTheRepresentableLimitStaysPersistable(t *testing.T) {
	now := time.Unix(0, math.MaxInt64).UTC().Add(-time.Minute)
	for _, raw := range []string{"600", "0", "invalid", now.Add(time.Hour).Format(http.TimeFormat)} {
		got := retryDelay(raw, now)
		if got > time.Minute {
			t.Fatalf("header %q delay=%v exceeds the persistable remainder", raw, got)
		}
		if now.Add(got).UnixNano() < 0 {
			t.Fatalf("header %q delay=%v overflowed the persisted deadline", raw, got)
		}
	}
}
