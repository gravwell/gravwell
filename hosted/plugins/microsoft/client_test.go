package microsoft

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"golang.org/x/time/rate"
)

func TestParentChildIdentityAndAmbiguousPredecessorReplay(t *testing.T) {
	for _, selector := range []string{"defender-users", "entra-access-reviews"} {
		t.Run(selector, func(t *testing.T) {
			d := datasets[selector]
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/token") {
					fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
					return
				}
				if r.URL.Path == d.ParentPath {
					fmt.Fprint(w, `{"value":[{"id":"parent-a"},{"id":"parent-b"}]}`)
					return
				}
				fmt.Fprintf(w, `{"value":[{"%s":"same-child","observedOn":%q}]}`, d.IDField, r.URL.Path)
			}))
			defer server.Close()
			rt := newMicrosoftTestRuntime()
			p := newMicrosoftTestPlugin(t, server, rt)
			p.conf.Api = []string{selector}
			rows, err := p.client.Fetch(context.Background(), d, p.now(), p.now(), "")
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 2 || rows[0].ID == rows[1].ID {
				t.Fatalf("parent identities collapsed: %v", rows)
			}
			for _, row := range rows {
				var parts []string
				if err := json.Unmarshal([]byte(row.ID), &parts); err != nil || len(parts) != 2 || parts[1] != "same-child" {
					t.Fatalf("non-composite ID %q", row.ID)
				}
			}
			// Ambiguous predecessor state cannot safely be assigned to either parent.
			digest, err := recordDigest(rows[1].Raw, d)
			if err != nil {
				t.Fatal(err)
			}
			key := stateKey(p.conf.StateNamespace(), selector, "")
			if err := saveState(rt, key, streamState{Records: map[string]recordState{"same-child": {Hash: digest, SeenAt: p.now()}}, Pending: &deliveryProgress{Start: p.now().Add(-time.Hour), End: p.now(), Receipts: map[string]bool{"same-child:" + digest: true}}}); err != nil {
				t.Fatal(err)
			}
			for poll := 0; poll < 3; poll++ {
				p = newMicrosoftTestPlugin(t, server, rt)
				p.conf.Api = []string{selector}
				if _, err := p.Handle(context.Background(), rt); err != nil {
					t.Fatal(err)
				}
				if len(rt.entries) != 2 {
					t.Fatalf("poll %d: entries=%d; want bounded parent-scoped replay of two records", poll, len(rt.entries))
				}
			}
			if string(rt.entries[0].Data) != string(rows[0].Raw) {
				t.Fatal("raw child payload altered")
			}
		})
	}
}

func TestGraphPaginationCompactsAndPreservesRecords(t *testing.T) {
	var tokenCalls atomic.Int32
	var apiCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"synthetic-access-token","expires_in":3600}`))
		case r.URL.Path == "/v1.0/auditLogs/directoryAudits":
			apiCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer synthetic-access-token" {
				t.Errorf("missing bearer token")
			}
			if r.URL.Query().Get("page") != "2" && !strings.Contains(r.URL.Query().Get("$filter"), "activityDateTime ge") {
				t.Errorf("filter=%q", r.URL.Query().Get("$filter"))
			}
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`{"value":[{"id":"event-2","activityDateTime":"2026-08-16T02:00:00Z","result":"success"}]}`))
				return
			}
			next := serverURL(r) + "/v1.0/auditLogs/directoryAudits?page=2"
			_ = json.NewEncoder(w).Encode(map[string]any{"value": []map[string]any{{"id": "event-1", "activityDateTime": "2026-08-16T01:00:00Z", "result": "success"}}, "@odata.nextLink": next})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	dataset := datasets["entra-directory-audits"]
	records, err := client.Fetch(context.Background(), dataset, time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || tokenCalls.Load() != 1 || apiCalls.Load() != 2 {
		t.Fatalf("records=%d tokenCalls=%d apiCalls=%d", len(records), tokenCalls.Load(), apiCalls.Load())
	}
	for _, record := range records {
		if !json.Valid(record.Raw) || strings.ContainsAny(string(record.Raw), "\r\n") {
			t.Fatalf("record is not compact JSON: %q", record.Raw)
		}
		var object map[string]any
		if err := json.Unmarshal(record.Raw, &object); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"tag", "Vendor", "Product", "_collector"} {
			if _, exists := object[forbidden]; exists {
				t.Fatalf("collector inserted %q into vendor record", forbidden)
			}
		}
	}
}

func TestContinuationHostIsRestricted(t *testing.T) {
	conf := testClientConfig(t, "https://graph.example.invalid")
	client, err := NewClient(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.validateURL("https://attacker.example.invalid/next"); err == nil {
		t.Fatal("unexpected host accepted")
	}
}

func TestRelativeContinuationResolvesOnAllowedOrigin(t *testing.T) {
	conf := testClientConfig(t, "https://management.example.invalid")
	client, err := NewClient(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.resolveContinuation(
		"https://management.example.invalid/subscriptions/example/providers/Microsoft.PolicyInsights/queryResults?api-version=1",
		"/subscriptions/example/providers/Microsoft.PolicyInsights/queryResults?api-version=1&$skiptoken=next",
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://management.example.invalid/subscriptions/example/providers/Microsoft.PolicyInsights/queryResults?api-version=1&$skiptoken=next"; got != want {
		t.Fatalf("resolved=%q want=%q", got, want)
	}
	if _, err := client.resolveContinuation(got, "//attacker.example.invalid/next"); err == nil {
		t.Fatal("scheme-relative host escape accepted")
	}
}

func TestARMListDoesNotInjectUnsupportedTop(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"synthetic-access-token","expires_in":3600}`))
			return
		}
		if r.URL.Query().Get("$top") != "" {
			t.Errorf("unexpected $top=%q", r.URL.Query().Get("$top"))
		}
		_, _ = w.Write([]byte(`{"value":[]}`))
	}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	if _, err := client.Fetch(context.Background(), datasets["defender-cloud-regulatory-standards"], time.Time{}, time.Time{}, "33333333-3333-3333-3333-333333333333"); err != nil {
		t.Fatal(err)
	}
}

func TestObjectResponseIsOneCompactRecord(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"synthetic-access-token","expires_in":3600}`))
			return
		}
		if r.URL.Path != "/api/exposureScore" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("{\n  \"time\": \"2026-08-16T02:00:00Z\",\n  \"score\": 12.5\n}"))
	}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	records, err := client.Fetch(context.Background(), datasets["defender-endpoint-exposure-score"], time.Time{}, time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || string(records[0].Raw) != `{"time":"2026-08-16T02:00:00Z","score":12.5}` {
		t.Fatalf("records=%#v", records)
	}
}

func TestCompositeIdentityPreventsPolicyCollisions(t *testing.T) {
	dataset := datasets["azure-policy-states"]
	first, err := decodeRecord(dataset, json.RawMessage(`{"policyAssignmentId":"assignment","policyDefinitionReferenceId":"definition-a","resourceId":"resource"}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := decodeRecord(dataset, json.RawMessage(`{"policyAssignmentId":"assignment","policyDefinitionReferenceId":"definition-b","resourceId":"resource"}`))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.ID == "" || second.ID == "" {
		t.Fatalf("policy IDs collided: %q %q", first.ID, second.ID)
	}
}

func TestSubAssessmentsUseResourceGraph(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"synthetic-access-token","expires_in":3600}`))
			return
		}
		if r.URL.Path != "/providers/Microsoft.ResourceGraph/resources" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.ToLower(request.Query), "microsoft.security/assessments/subassessments") {
			t.Fatalf("query=%q", request.Query)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"synthetic-subassessment","properties":{"timeGenerated":"2026-08-16T02:00:00Z"}}]}`))
	}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	conf.Subscription_ID = []string{"33333333-3333-3333-3333-333333333333"}
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	records, err := client.Fetch(context.Background(), datasets["defender-cloud-subassessments"], time.Time{}, time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || !json.Valid(records[0].Raw) || strings.ContainsAny(string(records[0].Raw), "\r\n") {
		t.Fatalf("records=%#v", records)
	}
}

func testClientConfig(t *testing.T, host string) *Config {
	t.Helper()
	secret := filepath.Join(t.TempDir(), "client.secret")
	if err := os.WriteFile(secret, []byte("synthetic-client-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Config{
		PollingConfig:      hosted.PollingConfig{Requests_Per_Minute: 6000},
		Tenant_ID:          "11111111-1111-1111-1111-111111111111",
		Client_ID:          "22222222-2222-2222-2222-222222222222",
		Client_Secret_File: secret,
		Graph_Host:         host, ARM_Host: host, Defender_Host: host, PowerBI_Host: host, Auth_Host: host,
		Page_Size: 100, Max_Pages: 10, Max_Retries: 0,
	}
}

func serverURL(r *http.Request) string { return "https://" + r.Host }
