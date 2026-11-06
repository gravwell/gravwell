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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStoredStateRejectsDuplicateScalarMembers(t *testing.T) {
	const (
		key       = "microsoft/entra-users"
		watermark = "2026-10-07T06:00:00Z"
	)

	t.Run("ordinary-cursor-reset-before-request", func(t *testing.T) {
		var requests atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}))
		defer server.Close()

		rt := newMicrosoftTestRuntime(t)
		mark, err := time.Parse(time.RFC3339, watermark)
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.PutTime(key+"/timestamp", mark); err != nil {
			t.Fatal(err)
		}
		if err := rt.Put(key+"/cursor", []byte(`{"window_start":"2026-10-07T06:00:00Z","window_start":"2036-10-07T06:00:00Z","window_end":"2036-10-07T07:00:00Z","continuation":{"version":1,"kind":"odata-get"}}`)); err != nil {
			t.Fatal(err)
		}

		plugin := newSelectorTestPlugin(t, server, "entra-users")
		if _, err := plugin.collect(context.Background(), rt, datasets["entra-users"], ""); err != nil {
			t.Fatal(err)
		}
		if got := requests.Load(); got != 0 {
			t.Fatalf("vendor requests=%d, want 0 while corrupt state is reset", got)
		}
		state, err := loadState(rt, key)
		if err != nil {
			t.Fatal(err)
		}
		if !state.Watermark.Equal(mark) || state.Continuation != nil || !state.WindowStart.IsZero() {
			t.Fatalf("reset state=%+v, want retained watermark and no cursor", state)
		}
	})

	tests := []struct {
		name     string
		key      string
		document string
		load     func(*microsoftTestRuntime) error
	}{
		{
			name: "nested-continuation", key: key + "/cursor",
			document: `{"window_start":"2026-10-07T06:00:00Z","window_end":"2026-10-07T07:00:00Z","continuation":{"version":1,"kind":"odata-get","next_url":"https://graph.microsoft.com/one","next_url":"https://graph.microsoft.com/two"}}`,
			load:     func(rt *microsoftTestRuntime) error { _, err := loadState(rt, key); return err },
		},
		{
			name: "fabric", key: key + "/fabric",
			document: `{"anchor":"2026-10-07T06:00:00Z","anchor":"2036-10-07T06:00:00Z","available_since":"2026-10-07T06:00:00Z","through":"2026-10-07T07:00:00Z"}`,
			load:     func(rt *microsoftTestRuntime) error { _, err := loadState(rt, key); return err },
		},
		{
			name: "overlap-receipt", key: key + "/overlap",
			document: `{"version":1,"receipts":[{"timestamp":"2026-10-07T06:00:00Z","fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","fingerprint":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}`,
			load:     func(rt *microsoftTestRuntime) error { _, err := loadState(rt, key); return err },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt := newMicrosoftTestRuntime(t)
			if err := rt.Put(tc.key, []byte(tc.document)); err != nil {
				t.Fatal(err)
			}
			if err := tc.load(rt); !errors.Is(err, errIncompatibleContinuation) || !strings.Contains(err.Error(), "repeats an object member") {
				t.Fatalf("error=%v, want incompatible state caused by a duplicate object member", err)
			}
		})
	}
}

func TestManagementStoredStateRejectsDuplicateScalarMembers(t *testing.T) {
	const key = "microsoft/microsoft365-audit-general/management-v1"
	validContent := `{"content_type":"Audit.General","content_id":"one","content_uri":"https://manage.office.com/api/v1.0/content/one","content_created":"2026-10-07T06:00:00Z"}`
	validReceipt := `{"content_id":"one","content_created":"2026-10-07T06:00:00Z"}`

	tests := []struct {
		name     string
		key      string
		document string
	}{
		{"head", key, `{"version":1,"completed_through":"2026-10-07T06:00:00Z","completed_through":"2036-10-07T06:00:00Z"}`},
		{"nested-head", key, `{"version":1,"refused_listing_start":{"window_start":"2026-10-07T06:00:00Z","contents":1,"contents":2}}`},
		{"content-descriptor", managementContentsKey(key), `{"version":1,"contents":[` + strings.Replace(validContent, `"content_id":"one"`, `"content_id":"one","content_id":"two"`, 1) + `]}`},
		{"receipt", managementReceiptsKey(key), `{"version":1,"receipts":[` + strings.Replace(validReceipt, `"content_id":"one"`, `"content_id":"one","content_id":"two"`, 1) + `]}`},
		{"progress", managementProgressKey(key), `{"version":1,"content_index":0,"event_index":1,"event_index":2}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt := newMicrosoftTestRuntime(t)
			if tc.key != key {
				if err := rt.Put(key, []byte(`{"version":1}`)); err != nil {
					t.Fatal(err)
				}
			}
			if err := rt.Put(tc.key, []byte(tc.document)); err != nil {
				t.Fatal(err)
			}
			if _, err := loadManagementState(rt, key); err == nil || !recoverableManagementCorruption(err) {
				t.Fatalf("error=%v, want recoverable Management state corruption", err)
			}
		})
	}

	t.Run("ambiguous-boundary-is-not-salvaged", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		if err := rt.Put(key, []byte(`{"version":1,"completed_through":"2026-10-07T06:00:00Z","completed_through":"2036-10-07T06:00:00Z"}`)); err != nil {
			t.Fatal(err)
		}
		recovered, err := recoverManagementState(rt, key)
		if err != nil {
			t.Fatal(err)
		}
		if !recovered.CompletedThrough.IsZero() {
			t.Fatalf("recovered ambiguous boundary=%s, want no selected value", recovered.CompletedThrough)
		}
	})
}

func TestStoredStateRejectsCaseFoldedScalarMembers(t *testing.T) {
	const (
		ordinaryKey   = "microsoft/entra-users"
		managementKey = "microsoft/microsoft365-audit-general/management-v1"
		watermark     = "2026-10-07T06:00:00Z"
	)

	t.Run("ordinary-cursor-reset-before-request", func(t *testing.T) {
		var requests atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}))
		defer server.Close()

		rt := newMicrosoftTestRuntime(t)
		mark, err := time.Parse(time.RFC3339, watermark)
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.PutTime(ordinaryKey+"/timestamp", mark); err != nil {
			t.Fatal(err)
		}
		if err := rt.Put(ordinaryKey+"/cursor", []byte(`{"window_start":"2026-10-07T06:00:00Z","WINDOW_START":"2036-10-07T06:00:00Z","window_end":"2036-10-07T07:00:00Z","continuation":{"version":1,"kind":"odata-get"}}`)); err != nil {
			t.Fatal(err)
		}

		plugin := newSelectorTestPlugin(t, server, "entra-users")
		if _, err := plugin.collect(context.Background(), rt, datasets["entra-users"], ""); err != nil {
			t.Fatal(err)
		}
		if got := requests.Load(); got != 0 {
			t.Fatalf("vendor requests=%d, want 0 while case-colliding state is reset", got)
		}
		state, err := loadState(rt, ordinaryKey)
		if err != nil {
			t.Fatal(err)
		}
		if !state.Watermark.Equal(mark) || state.Continuation != nil || !state.WindowStart.IsZero() {
			t.Fatalf("reset state=%+v, want retained watermark and no cursor", state)
		}
	})

	t.Run("nested-continuation", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		document := `{"window_start":"2026-10-07T06:00:00Z","window_end":"2026-10-07T07:00:00Z","continuation":{"version":1,"kind":"odata-get","next_url":"https://graph.microsoft.com/one","NEXT_URL":"https://graph.microsoft.com/two"}}`
		if err := rt.Put(ordinaryKey+"/cursor", []byte(document)); err != nil {
			t.Fatal(err)
		}
		if _, err := loadState(rt, ordinaryKey); !errors.Is(err, errIncompatibleContinuation) || !strings.Contains(err.Error(), "repeats an object member") {
			t.Fatalf("error=%v, want case-colliding nested member refused", err)
		}
	})

	t.Run("management-head-and-recovery", func(t *testing.T) {
		document := []byte(`{"version":1,"completed_through":"2026-10-07T06:00:00Z","COMPLETED_THROUGH":"2036-10-07T06:00:00Z"}`)
		rt := newMicrosoftTestRuntime(t)
		if err := rt.Put(managementKey, document); err != nil {
			t.Fatal(err)
		}
		if _, err := loadManagementState(rt, managementKey); err == nil || !recoverableManagementCorruption(err) {
			t.Fatalf("error=%v, want recoverable Management state corruption", err)
		}
		recovered, err := recoverManagementState(rt, managementKey)
		if err != nil {
			t.Fatal(err)
		}
		if !recovered.CompletedThrough.IsZero() {
			t.Fatalf("recovered ambiguous boundary=%s, want no selected value", recovered.CompletedThrough)
		}
	})
}

func TestStoredStateDuplicateScannerIsStructurallyBounded(t *testing.T) {
	t.Run("decoder-equivalent-names", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			document string
		}{
			{"escaped-case", `{"window_start":null,"WINDOW\u005fSTART":null}`},
			{"unicode-simple-fold", `{"kelvin":null,"\u212aELVIN":null}`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if err := rejectDuplicateStateMembers([]byte(tc.document)); err == nil || !strings.Contains(err.Error(), "repeats an object member") {
					t.Fatalf("error=%v, want decoder-equivalent names refused", err)
				}
			})
		}
	})

	t.Run("overflow-folded-name-set", func(t *testing.T) {
		var document strings.Builder
		document.WriteByte('{')
		for i := range maximumStoredJSONNameCache {
			if i != 0 {
				document.WriteByte(',')
			}
			fmt.Fprintf(&document, `"m%d":null`, i)
		}
		document.WriteString(`,"kelvin":null,"\u212aELVIN":null}`)
		if err := rejectDuplicateStateMembers([]byte(document.String())); err == nil || !strings.Contains(err.Error(), "repeats an object member") {
			t.Fatalf("error=%v, want overflow set to use Unicode simple-fold equivalence", err)
		}
	})

	t.Run("object-members", func(t *testing.T) {
		var document strings.Builder
		document.WriteByte('{')
		for i := range maximumStoredJSONObjectMembers + 1 {
			if i != 0 {
				document.WriteByte(',')
			}
			fmt.Fprintf(&document, `"m%d":null`, i)
		}
		document.WriteByte('}')
		if err := rejectDuplicateStateMembers([]byte(document.String())); err == nil || !strings.Contains(err.Error(), "object-member bound") {
			t.Fatalf("error=%v, want object-member bound", err)
		}
	})

	t.Run("depth", func(t *testing.T) {
		document := strings.Repeat(`[`, maximumStoredJSONDepth+1) + `null` + strings.Repeat(`]`, maximumStoredJSONDepth+1)
		if err := rejectDuplicateStateMembers([]byte(document)); err == nil || !strings.Contains(err.Error(), "nesting bound") {
			t.Fatalf("error=%v, want nesting bound", err)
		}
	})

	t.Run("valid-nested-state", func(t *testing.T) {
		document := []byte(`{"version":1,"receipts":[{"timestamp":"2026-10-07T06:00:00Z","fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`)
		if err := rejectDuplicateStateMembers(document); err != nil {
			t.Fatal(err)
		}
	})
}
