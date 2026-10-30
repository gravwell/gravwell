package microsoft

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// The list contract is documented at https://learn.microsoft.com/en-us/graph/api/rbacapplication-list-roleassignmentscheduleinstances.
// These are synthetic records exercising null and pre-watermark start dates.
func TestPIMCompleteSnapshotUsesServerPagination(t *testing.T) {
	for _, maxPages := range []int{1, 2} {
		t.Run(fmt.Sprint(maxPages), func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
					fmt.Fprint(w, `{"access_token":"synthetic-token","expires_in":3600}`)
					return
				}
				calls++
				if r.URL.Path != "/v1.0/roleManagement/directory/roleAssignmentScheduleInstances" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.URL.Query().Has("$top") || r.URL.Query().Has("$filter") {
					t.Errorf("unsupported or lossy request: %s", r.URL.RawQuery)
					http.Error(w, "unsupported query", http.StatusBadRequest)
					return
				}
				if r.URL.Query().Get("$skiptoken") == "opaque" {
					fmt.Fprint(w, `{"value":[{"id":"old-active","startDateTime":"2020-01-01T00:00:00Z","endDateTime":null}]}`)
					return
				}
				fmt.Fprintf(w, `{"value":[{"id":"permanent","startDateTime":null,"endDateTime":null}],"@odata.nextLink":%q}`, serverURL(r)+r.URL.Path+"?$skiptoken=opaque")
			}))
			defer server.Close()
			conf := testClientConfig(t, server.URL)
			conf.Max_Pages = maxPages
			client, err := NewClient(conf, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			client.limiter = rate.NewLimiter(rate.Inf, 1)
			dataset := datasets["entra-pim-assignments"]
			if dataset.Mode != ModeSnapshot {
				t.Fatal("PIM instances must be a complete snapshot")
			}
			end := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
			records, err := client.Fetch(context.Background(), dataset, end.Add(-time.Hour), end, "")
			if maxPages == 1 {
				if err == nil || !strings.Contains(err.Error(), "Max-Pages") || records != nil {
					t.Fatalf("partial snapshot accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != 2 || calls != 2 {
				t.Fatalf("records=%d calls=%d", len(records), calls)
			}
			if records[0].ID != "permanent" || !records[0].Timestamp.IsZero() || records[1].ID != "old-active" || records[1].Timestamp.Year() != 2020 {
				t.Fatal("null or old assignment lost")
			}
		})
	}
}
