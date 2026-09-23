package microsoft

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
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
	conf.Page_Size = 1
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
			if !strings.Contains(r.URL.Query().Get("$filter"), "category eq 'Security'") {
				t.Fatalf("filter=%q", r.URL.Query().Get("$filter"))
			}
			_, _ = w.Write([]byte(`{"value":[]}`))
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
	if _, err := client.Fetch(context.Background(), datasets["azure-activity-security"], start, end, "33333333-3333-3333-3333-333333333333"); err != nil {
		t.Fatal(err)
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
