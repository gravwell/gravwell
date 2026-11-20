/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package microsoft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestStoredManagementSetsRejectDuplicateBoundedMembers(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"contents", `{"version":1,"contents":[],"contents":[]}`},
		{"receipts", `{"version":1,"receipts":[],"receipts":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var document struct {
				Version  int                      `json:"version"`
				Contents managementStoredContents `json:"contents,omitempty"`
				Receipts managementStoredReceipts `json:"receipts,omitempty"`
			}
			if err := json.Unmarshal([]byte(tc.raw), &document); err == nil || !strings.Contains(err.Error(), "repeats bounded member "+tc.name) {
				t.Fatalf("error=%v, want duplicate %s refused", err, tc.name)
			}
		})
	}
}

func TestStoredCursorRejectsDuplicateContinuationMembers(t *testing.T) {
	rt := newMicrosoftTestRuntime(t)
	key := stateKey("microsoft", "entra-users", "")
	document := `{"window_start":"2026-10-07T06:00:00Z","window_end":"2026-10-07T07:00:00Z",` +
		`"continuation":{"version":1,"kind":"odata-get","recent":["one"]},` +
		`"continuation":{"version":1,"kind":"odata-get","recent":["two"]}}`
	seedStoredCursorDocument(t, rt, key, document)
	if _, err := loadState(rt, key); err == nil || !strings.Contains(err.Error(), "repeats bounded member continuation") {
		t.Fatalf("error=%v, want duplicate continuation refused", err)
	}
}

func TestTemporalOverlapRejectsDuplicateReceiptMembers(t *testing.T) {
	receipt := fmt.Sprintf(`{"timestamp":"2026-10-21T00:00:00Z","fingerprint":"%s"}`, strings.Repeat("a", temporalFingerprintLength))
	raw := []byte(`{"version":1,"receipts":[` + receipt + `],"receipts":[` + receipt + `]}`)
	if _, err := decodeTemporalOverlap(raw); err == nil || !strings.Contains(err.Error(), "repeats bounded member receipts") {
		t.Fatalf("error=%v, want duplicate receipts refused", err)
	}
}

func TestFetchStopsAtAggregateRetentionBudget(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"synthetic","expires_in":3600}`))
			return
		}
		page := calls.Add(1)
		next := ""
		if page < 3 {
			next = fmt.Sprintf(`,"@odata.nextLink":%q`, serverURL(r)+fmt.Sprintf("/v1.0/users?page=%d", page+1))
		}
		_, _ = fmt.Fprintf(w, `{"value":[{"id":"%d"}]%s}`, page, next)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.chunkRetentionRecords = 1
	records, err := client.Fetch(context.Background(), datasets["entra-users"], time.Now().Add(-time.Hour), time.Now(), "")
	if err == nil {
		t.Fatalf("Fetch returned %d records without reporting aggregate truncation", len(records))
	}
	if len(records) != 1 || calls.Load() != 1 {
		t.Fatalf("records=%d calls=%d, want one bounded chunk preserved", len(records), calls.Load())
	}
	var limit *FetchLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("error=%T %v, want *FetchLimitError", err, err)
	}
	if limit.Continuation == nil {
		t.Fatal("aggregate truncation did not return its resumable continuation")
	}
}

func TestFetchReportsCumulativePageBudget(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"synthetic","expires_in":3600}`))
			return
		}
		page := calls.Add(1)
		next := serverURL(r) + fmt.Sprintf("/v1.0/users?page=%d", page+1)
		_, _ = fmt.Fprintf(w, `{"value":[{"id":"%d"}],"@odata.nextLink":%q}`, page, next)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.continuationPageLimit = 1
	records, err := client.Fetch(context.Background(), datasets["entra-users"], time.Now().Add(-time.Hour), time.Now(), "")
	var limit *FetchLimitError
	if !errors.As(err, &limit) || limit.Reason != "page budget" {
		t.Fatalf("records=%d error=%T %v, want page-budget FetchLimitError", len(records), err, err)
	}
	if len(records) != 1 || calls.Load() != 1 {
		t.Fatalf("records=%d calls=%d, want the bounded prefix only", len(records), calls.Load())
	}
}

func TestFetchStopsWhenAggregateDeadlineExpires(t *testing.T) {
	client, err := NewClient(testClientConfig(t, "https://graph.example.invalid"), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.fetchDeadline = 50 * time.Millisecond
	dataset := datasets["entra-users"]
	client.tokens[dataset.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
	var calls atomic.Int64
	client.http = staticDoer(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return syntheticResponse(`{"value":[{"id":"one"}],"@odata.nextLink":"https://graph.example.invalid/v1.0/users?page=2"}`), nil
		}
		<-r.Context().Done()
		return nil, stalledDispatch(r)
	})

	records, err := client.Fetch(t.Context(), dataset, time.Now().Add(-time.Hour), time.Now(), "")
	var limit *FetchLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("records=%d error=%T %v, want deadline FetchLimitError", len(records), err, err)
	}
	if limit.Reason != "deadline" || !errors.Is(limit, context.DeadlineExceeded) {
		t.Fatalf("limit=%+v cause=%v, want deadline with context cause", limit, limit.Cause)
	}
	if len(records) != 1 || limit.Chunks != 1 || limit.Continuation == nil || calls.Load() != 2 {
		t.Fatalf("records=%d chunks=%d continuation=%v calls=%d, want one bounded chunk and two page attempts",
			len(records), limit.Chunks, limit.Continuation != nil, calls.Load())
	}
}

func TestNestedFetchDeadlineKeepsPluginOwnership(t *testing.T) {
	outer, cancelOuter := withFetchDeadline(context.Background(), time.Second)
	defer cancelOuter()
	inner, cancelInner := withFetchDeadline(outer, time.Second)
	defer cancelInner()
	if !internalFetchDeadline(inner) {
		t.Fatal("nested FetchChunk lost ownership of the aggregate Fetch deadline")
	}

	caller, cancelCaller := context.WithTimeout(context.Background(), time.Second)
	defer cancelCaller()
	bounded, cancelBounded := withFetchDeadline(caller, time.Minute)
	defer cancelBounded()
	if internalFetchDeadline(bounded) {
		t.Fatal("caller-owned deadline was misclassified as plugin-owned")
	}
}

func TestEffectiveOriginAcceptsDefaultHTTPSPortOnly(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current string
		next    string
		wantErr bool
	}{
		{"add-default-port", "https://graph.example.invalid/v1.0/users", "https://graph.example.invalid:443/v1.0/users?page=2", false},
		{"remove-default-port", "https://graph.example.invalid:443/v1.0/users", "https://graph.example.invalid/v1.0/users?page=2", false},
		{"add-zero-padded-default-port", "https://graph.example.invalid/v1.0/users", "https://graph.example.invalid:0443/v1.0/users?page=2", false},
		{"normalize-zero-padded-default-port", "https://graph.example.invalid:00443/v1.0/users", "https://graph.example.invalid:443/v1.0/users?page=2", false},
		{"reject-non-default-port", "https://graph.example.invalid/v1.0/users", "https://graph.example.invalid:444/v1.0/users?page=2", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewClient(testClientConfig(t, "https://graph.example.invalid"), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.resolveContinuation(tc.current, tc.next)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v wantErr=%t", err, tc.wantErr)
			}
		})
	}
}
