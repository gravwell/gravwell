package microsoft

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/processors"
	"golang.org/x/time/rate"
)

func TestEquivalentSelectorsCollectOnceWithoutMergingDistinctTags(t *testing.T) {
	if datasets["defender-incidents"].Scope != "https://api.security.microsoft.com/.default" {
		t.Fatal("Defender XDR incident collection uses the Endpoint OAuth audience")
	}
	resolved, err := ResolveDatasets([]string{"defender-endpoint-vulnerabilities", "defender-vulnerabilities", "defender-xdr-vulnerability"})
	if err != nil || len(resolved) != 2 {
		t.Fatalf("resolved=%v err=%v", resolved, err)
	}
	for _, name := range []string{"defender-endpoint-vulnerabilities", "defender-vulnerabilities"} {
		single, err := ResolveDatasets([]string{name})
		if err != nil || len(single) != 1 || single[0].Name != name {
			t.Fatalf("individual selector lost its existing state identity: %v %v", single, err)
		}
	}
	a := datasets["defender-endpoint-vulnerabilities"]
	b := a
	b.TagGroup = "different-consumer"
	if CollectionContractKey(a) == CollectionContractKey(b) {
		t.Fatal("distinct destination tags were merged")
	}
	b = a
	b.Filter = "severity eq 'High'"
	if CollectionContractKey(a) == CollectionContractKey(b) {
		t.Fatal("distinct source filters were merged")
	}
}

func TestHuntingDoesNotDiscardRowsOrCheckpointIncompleteResults(t *testing.T) {
	for _, tc := range []struct {
		name      string
		count     int
		body      string
		wantError bool
	}{
		{name: "more than ten rows", count: 12},
		{name: "exact budget", count: 20},
		{name: "overflow", count: 21, wantError: true},
		{name: "missing envelope", body: `{}`, wantError: true},
		{name: "null envelope", body: `{"results":null}`, wantError: true},
		{name: "malformed envelope", body: `{"results":{}}`, wantError: true},
		{name: "null record", body: `{"results":[null]}`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
					fmt.Fprint(w, `{"access_token":"synthetic-access-token","expires_in":3600}`)
					return
				}
				var request map[string]string
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request["Query"] != "DeviceProcessEvents | take 21" || request["Timespan"] == "" {
					t.Errorf("unexpected query contract: %v", request)
				}
				if tc.body != "" {
					fmt.Fprint(w, tc.body)
					return
				}
				rows := make([]map[string]any, tc.count)
				for i := range rows {
					rows[i] = map[string]any{"Timestamp": "2026-09-08T14:00:00Z", "ReportId": i, "DeviceId": "demo-device", "FileName": fmt.Sprintf("file-%d", i)}
				}
				json.NewEncoder(w).Encode(map[string]any{"results": rows})
			}))
			defer server.Close()
			conf := testClientConfig(t, server.URL)
			conf.Page_Size, conf.Max_Pages = 10, 2
			client, err := NewClient(conf, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			client.limiter = rate.NewLimiter(rate.Inf, 1)
			rt := newMicrosoftTestRuntime()
			plugin := New(conf, processors.NewProcessorSet(rt))
			plugin.client = client
			plugin.now = func() time.Time { return time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC) }
			err = plugin.collect(context.Background(), rt, datasets["defender-xdr-device-process-events"], "")
			if (err != nil) != tc.wantError {
				t.Fatalf("err=%v", err)
			}
			if tc.wantError {
				if len(rt.entries) != 0 || len(rt.values) != 0 {
					t.Fatal("partial result was written or checkpointed")
				}
			} else if len(rt.entries) != tc.count {
				t.Fatalf("wrote %d of %d rows", len(rt.entries), tc.count)
			}
		})
	}
}

func TestVulnerabilitySnapshotPreservesMultipleCVEsOnOneDevice(t *testing.T) {
	d := datasets["defender-xdr-vulnerability"]
	if d.Mode != ModeSnapshot || d.TimeField != "" {
		t.Fatal("TVM findings treated as timestamped events")
	}
	ids := map[string]bool{}
	for _, cve := range []string{"CVE-DEMO-1", "CVE-DEMO-2"} {
		raw := json.RawMessage(fmt.Sprintf(`{"DeviceId":"device-1","SoftwareVendor":"vendor","SoftwareName":"app","SoftwareVersion":"1","CveId":%q}`, cve))
		r, err := decodeRecord(d, raw)
		if err != nil {
			t.Fatal(err)
		}
		if ids[r.ID] {
			t.Fatal("different CVEs collided")
		}
		ids[r.ID] = true
	}
}

func TestPartialHuntingIdentityDoesNotCollapseDistinctEvidence(t *testing.T) {
	d := datasets["defender-xdr-alert-evidence"]
	a, err := decodeRecord(d, json.RawMessage(`{"Timestamp":"2026-09-08T14:00:00Z","DeviceId":"device-1","EntityType":"File","FileName":"a.exe"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := decodeRecord(d, json.RawMessage(`{"Timestamp":"2026-09-08T14:00:00Z","DeviceId":"device-1","EntityType":"File","FileName":"b.exe"}`))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("evidence rows without ReportId collided")
	}
}
