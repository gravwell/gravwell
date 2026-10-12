package slack

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
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
		if !errors.As(err, &deferred) || deferred.delay != 10*time.Minute || deferred.retryAt.IsZero() {
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

func TestClientBoundsSuccessfulResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"ok":true,"entries":[{"blob":%q}],"response_metadata":{}}`, strings.Repeat("x", maximumAuditPageSize))
	}))
	defer server.Close()
	client, err := newClient(server.URL, "synthetic-token", 30, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter.SetLimit(rate.Inf)
	if _, err := client.fetch(t.Context(), 1, 2, ""); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized response error=%v", err)
	}
}

func TestRetryDelayBounds(t *testing.T) {
	now := time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		raw  string
		want time.Duration
	}{
		{"120", 120 * time.Second},
		{"600", 10 * time.Minute},
		{"18446744073709551615", time.Duration(math.MaxInt64 - now.UnixNano())},
		{now.Add(2 * time.Minute).Format(http.TimeFormat), 2 * time.Minute},
		{"0", defaultRetryDelay},
		{"invalid", defaultRetryDelay},
	}
	for _, tc := range tests {
		t.Run(strings.ReplaceAll(tc.raw, " ", "_"), func(t *testing.T) {
			if got := retryDelay(tc.raw, now); got != tc.want {
				t.Fatalf("delay=%v want=%v", got, tc.want)
			}
		})
	}
}
