package microsoft

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestProductionKitGroupsOwnExactTags(t *testing.T) {
	tests := map[string][]string{
		"azure-activity-kit":         {"azure-activity", "azure-activity-administrative", "azure-activity-security", "azure-activity-service-health", "azure-activity-resource-health", "azure-activity-alert", "azure-activity-recommendation", "azure-activity-policy", "azure-activity-autoscale"},
		"microsoft-azure-kit":        {"azure-activity", "azure-app-registrations", "azure-conditional-access", "azure-defender-alerts", "azure-defender-assessments", "azure-groups", "azure-key-vaults", "azure-managed-identities", "azure-management-groups", "azure-network-security-groups", "azure-policy-assignments", "azure-policy-definitions", "azure-policy-exemptions", "azure-public-ips", "azure-resource-groups", "azure-resources", "azure-role-assignments", "azure-role-definitions", "azure-security-recommendations", "azure-service-principals", "azure-storage-accounts", "azure-subscriptions", "azure-users", "azure-virtual-machines", "azure-virtual-networks"},
		"microsoft-defender-kit":     {"microsoft-defender-actions", "microsoft-defender-alerts", "microsoft-defender-devices", "microsoft-defender-exposure", "microsoft-defender-incidents", "microsoft-defender-indicators", "microsoft-defender-investigations", "microsoft-defender-machine-groups", "microsoft-defender-recommendations", "microsoft-defender-secure-score", "microsoft-defender-software", "microsoft-defender-users", "microsoft-defender-vulnerabilities"},
		"microsoft-defender-xdr-kit": {"microsoft-defender-xdr-incident", "microsoft-defender-xdr-alert", "microsoft-defender-xdr-alert-evidence", "microsoft-defender-device-events", "microsoft-defender-device-process-events", "microsoft-defender-device-network-events", "microsoft-defender-device-file-events", "microsoft-defender-device-registry-events", "microsoft-defender-device-logon-events", "microsoft-defender-email-events", "microsoft-defender-email-attachment-info", "microsoft-defender-email-url-info", "microsoft-defender-url-click-events", "microsoft-defender-cloud-app-events", "microsoft-defender-identity-logon-events", "microsoft-defender-identity-query-events", "microsoft-defender-vulnerability"},
		"microsoft-entra-id-kit":     {"entra-access-reviews", "entra-applications", "entra-audit-logs", "entra-conditional-access-policies", "entra-devices", "entra-directory-roles", "entra-groups", "entra-pim-assignments", "entra-risk-detections", "entra-risky-users", "entra-service-principals", "entra-signins", "entra-users"},
	}
	for group, want := range tests {
		got := TagsForSelectors([]string{group})
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("%s tags=\n%s\nwant=\n%s", group, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

func TestAdvancedHuntingUsesV1BoundedResults(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"synthetic-access-token","expires_in":3600}`))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1.0/security/runHuntingQuery" {
			http.NotFound(w, r)
			return
		}
		var request map[string]string
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(request["Query"], "DeviceProcessEvents") || !strings.Contains(request["Timespan"], "/") {
			t.Fatalf("request=%#v", request)
		}
		_, _ = w.Write([]byte(`{"results":[{"Timestamp":"2026-09-06T01:00:00Z","ReportId":1},{"Timestamp":"2026-09-06T01:01:00Z","ReportId":2}]}`))
	}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	records, err := client.Fetch(context.Background(), datasets["defender-xdr-device-process-events"], time.Now().Add(-time.Hour), time.Now(), "")
	if err != nil || len(records) != 2 {
		t.Fatalf("records=%d err=%v", len(records), err)
	}
}

func TestActivityCategoryFilterAndParentChildFraming(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"synthetic-access-token","expires_in":3600}`))
			return
		}
		switch {
		case strings.Contains(r.URL.Path, "/eventtypes/management/values"):
			filter := r.URL.Query().Get("$filter")
			if !strings.Contains(filter, "eventTimestamp ge '") || !strings.Contains(filter, "eventTimestamp le '") || strings.Contains(filter, "category") {
				t.Fatalf("filter=%q", r.URL.Query().Get("$filter"))
			}
			_, _ = w.Write([]byte(`{"value":[
				{"eventDataId":"security","eventTimestamp":"2026-09-06T01:00:00Z","category":{"value":"Security"}},
				{"eventDataId":"administrative","eventTimestamp":"2026-09-06T01:01:00Z","category":{"value":"Administrative"}}
			]}`))
		case r.URL.Path == "/api/machines":
			_, _ = w.Write([]byte(`{"value":[{"id":"machine-1"}]}`))
		case r.URL.Path == "/api/machines/machine-1/logonusers":
			_, _ = w.Write([]byte(`{"value":[{"accountName":"alice"},{"accountName":"bob"}]}`))
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
	start, end := time.Now().Add(-time.Hour), time.Now()
	activity, err := client.Fetch(context.Background(), datasets["azure-activity-security"], start, end, "33333333-3333-3333-3333-333333333333")
	if err != nil {
		t.Fatal(err)
	}
	if len(activity) != 1 || activity[0].ID != "security" {
		t.Fatalf("security activity records=%#v", activity)
	}
	records, err := client.Fetch(context.Background(), datasets["defender-users"], start, end, "")
	if err != nil || len(records) != 2 {
		t.Fatalf("records=%d err=%v", len(records), err)
	}
	for _, record := range records {
		if !json.Valid(record.Raw) || strings.ContainsAny(string(record.Raw), "\r\n") {
			t.Fatalf("invalid framing: %q", record.Raw)
		}
	}
}

// Two selectors that issue the identical request and own the identical tag must
// resolve to one collection contract. Before this was enforced, the kit alias
// for the Defender software inventory differed from the standalone selector
// only by its internal state mode, so the shipped Api="all" profile fetched
// /api/Software twice and wrote every record to microsoft-defender-software
// twice on every poll.
func TestEquivalentSoftwareSelectorsResolveToOneContract(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"synthetic-access-token","expires_in":3600}`))
			return
		}
		if r.URL.Path != "/api/Software" {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		_, _ = w.Write([]byte(`{"value":[{"id":"software-1","name":"demo"}]}`))
	}))
	defer server.Close()

	runtime := newMicrosoftTestRuntime(t)
	conf := testClientConfig(t, server.URL)
	conf.Ingester_UUID = "550e8400-e29b-41d4-a716-446655440000"
	conf.Api = []string{"defender-endpoint-software", "defender-software"}
	conf.Lookback = 1
	conf.Request_Interval = 300
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveDatasets(conf.Api)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 {
		names := make([]string, 0, len(resolved))
		for _, d := range resolved {
			names = append(names, d.Name+"/"+string(d.Mode))
		}
		t.Fatalf("equivalent software selectors resolved to %v", names)
	}
	if got := conf.TagForDataset(resolved[0]); got != "ms-defender-endpoint" {
		t.Fatalf("resolved software tag=%q", got)
	}
	plugin := New(conf)
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	plugin.client = client
	plugin.now = func() time.Time { return time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC) }
	if _, err := plugin.Handle(runtime.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("/api/Software was requested %d times for one poll", got)
	}
	written := len(runtime.Entries())
	if written != 1 {
		t.Fatalf("one software record produced %d entries on microsoft-defender-software", written)
	}

	// the shipped Api="all" profile must carry the same guarantee for every
	// selector pair, not only for this one
	all, err := ResolveDatasets([]string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	owner := make(map[string]string, len(all))
	for _, d := range all {
		wire := strings.Join([]string{string(d.Service), d.Method(), d.Path, d.Scope, d.Query, d.Filter, d.ActivityCategory, d.TagGroup}, "|")
		if other, ok := owner[wire]; ok {
			t.Fatalf("selectors %q and %q issue the same request and own tag %q", other, d.Name, d.TagGroup)
		}
		owner[wire] = d.Name
	}
}

func TestDefenderEndpointPermissionsMatchCustomerOnboardingContract(t *testing.T) {
	// Pin the application permissions published for each endpoint. Several list
	// APIs expose only write-capable application permissions even though the
	// ingester itself performs reads only.
	want := map[string]string{
		"defender-devices":         "Machine.Read.All",
		"defender-alerts":          "Alert.ReadWrite.All",
		"defender-actions":         "Machine.ReadWrite.All",
		"defender-indicators":      "Ti.ReadWrite.All",
		"defender-investigations":  "Alert.ReadWrite.All",
		"defender-machine-groups":  "Machine.Read.All",
		"defender-recommendations": "SecurityRecommendation.Read.All",
		"defender-software":        "Software.Read.All",
		"defender-vulnerabilities": "Vulnerability.Read.All",
		"defender-exposure":        "Score.Read.All",
		"defender-users":           "Machine.Read.All + User.Read.All",
	}
	for selector, permission := range want {
		dataset, ok := datasets[selector]
		if !ok {
			t.Errorf("missing Defender for Endpoint selector %q", selector)
			continue
		}
		if dataset.Permission != permission {
			t.Errorf("selector %q permission=%q, want %q", selector, dataset.Permission, permission)
		}
		if got := destinationTag(dataset, TagSchemaConsolidated); got != "ms-defender-endpoint" {
			t.Errorf("selector %q destination=%q, want ms-defender-endpoint", selector, got)
		}
	}
}
