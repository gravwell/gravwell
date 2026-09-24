package servicenow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	ingestconfig "github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/jsonparser"
)

func secretFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "servicenow.json")
	if err := os.WriteFile(path, []byte(`{"mode":"basic","username":"reader","password":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func secretContents(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "servicenow.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCatalogCoversRequiredProducts(t *testing.T) {
	if got := len(Catalog()); got != 55 {
		t.Fatalf("catalog=%d want=55", got)
	}
	want := map[string]bool{
		"Audit": false, "Platform": false, "ITSM": false, "CMDB": false,
		"CSM": false, "Knowledge Management": false, "App Engine": false,
		"IT Operations Management": false, "IT Asset Management": false,
		"Security Operations": false, "Vulnerability Response": false,
		"Integrated Risk Management": false, "HR Service Delivery": false,
		"Strategic Portfolio Management": false, "Field Service Management": false,
		"DevOps": false,
	}
	for _, d := range Catalog() {
		if _, ok := want[d.Product]; ok {
			want[d.Product] = true
		}
		if d.Name == "" || d.Table == "" || d.Tag == "" || d.Timestamp == "" || d.RequiredRole == "" || d.Documentation == "" {
			t.Fatalf("incomplete dataset %#v", d)
		}
	}
	for product, seen := range want {
		if !seen {
			t.Errorf("missing product %s", product)
		}
	}
}

func TestProvenanceCoversTableAndVersionedRESTDatasets(t *testing.T) {
	source, recordType, endpoint, apiVersion := Provenance(catalog["incidents"])
	if source != "incidents" || recordType != "incident" || endpoint != "GET /api/now/table/incident" || apiVersion != "unversioned" {
		t.Fatalf("table provenance = %q %q %q %q", source, recordType, endpoint, apiVersion)
	}
	rest := Dataset{Name: "cases", Table: "sn_customerservice_case", REST: &RESTSpec{Path: "/api/sn_customerservice/v1/case"}}
	source, recordType, endpoint, apiVersion = Provenance(rest)
	if source != "cases" || recordType != "cases" || endpoint != "GET /api/sn_customerservice/v1/case" || apiVersion != "v1" {
		t.Fatalf("REST provenance = %q %q %q %q", source, recordType, endpoint, apiVersion)
	}
}

func TestClientPaginatesAndCompactsOneRecord(t *testing.T) {
	var gotQuery string
	var gotDisplayValue string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "reader" || p != "secret" {
			t.Errorf("basic auth missing")
		}
		gotQuery = r.URL.Query().Get("sysparm_query")
		gotDisplayValue = r.URL.Query().Get("sysparm_display_value")
		w.Header().Set("Link", `<https://example.invalid/api/now/table/incident?sysparm_offset=1>;rel="next"`)
		_, _ = w.Write([]byte("{\n  \"result\": [ { \"sys_id\": \"abc\", \"sys_updated_on\": \"2026-08-20 10:00:00\" } ]\n}"))
	}))
	defer server.Close()
	d := catalog["incidents"]
	c := NewClient(server.URL, secretFile(t), 10*time.Second, 1, 60000, server.Client().Transport)
	page, err := c.FetchPage(context.Background(), d, "active=true", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "active=true" {
		t.Fatalf("query=%q", gotQuery)
	}
	if gotDisplayValue != "false" {
		t.Fatalf("sysparm_display_value=%q want false", gotDisplayValue)
	}
	if len(page.Records) != 1 || !page.HasNext {
		t.Fatalf("page=%#v", page)
	}
	if strings.ContainsAny(string(page.Records[0].Raw), "\r\n") || strings.Contains(string(page.Records[0].Raw), "  ") {
		t.Fatalf("record not compact: %q", page.Records[0].Raw)
	}
	var value map[string]any
	if err := json.Unmarshal(page.Records[0].Raw, &value); err != nil {
		t.Fatal(err)
	}
}

func TestGenericReadOnlyAPIEndpointPaginatesAndCompacts(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/example/v1/events" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "1" {
			t.Errorf("limit=%q", got)
		}
		if got := r.URL.Query().Get("offset"); got != "2" {
			t.Errorf("offset=%q", got)
		}
		if got := r.URL.Query().Get("state"); got != "open" {
			t.Errorf("state=%q", got)
		}
		w.Header().Set("Link", `<https://example.invalid/api/example/v1/events?offset=3>; rel="next"`)
		_, _ = w.Write([]byte(`{"payload":{"items":[{"id":"evt-1","updated":"2026-09-03T10:11:12Z"}],"total":0}}`))
	}))
	defer server.Close()
	d := Dataset{Name: "example-events", Product: "Example", Tag: "servicenow-example-events", Timestamp: "updated", REST: &RESTSpec{
		Path: "/api/example/v1/events", ResultPath: "payload.items", IDField: "id",
		LimitParameter: "limit", OffsetParameter: "offset", Parameters: map[string]string{"state": "open"},
	}}
	client := NewClient(server.URL, secretFile(t), 10*time.Second, 1, 60000, server.Client().Transport)
	page, err := client.FetchPage(context.Background(), d, "ignored-table-window", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || !page.HasNext || page.Records[0].ID != "evt-1" {
		t.Fatalf("page=%#v", page)
	}
	if got := page.Records[0].Timestamp.Format(time.RFC3339); got != "2026-09-03T10:11:12Z" {
		t.Fatalf("timestamp=%s", got)
	}
}

func TestOffsetEndpointStopsOnShortPageDespiteStaleNextLink(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://example.invalid/api/example/v1/events?offset=200>; rel="next"`)
		_, _ = w.Write([]byte(`{"result":[{"sys_id":"evt-1"}]}`))
	}))
	defer server.Close()
	d := Dataset{Name: "example-events", Product: "Example", Tag: "servicenow-example-events", Timestamp: "sys_updated_on", REST: &RESTSpec{
		Path: "/api/example/v1/events", ResultPath: "result", IDField: "sys_id",
		LimitParameter: "sysparm_limit", OffsetParameter: "sysparm_offset",
	}}
	client := NewClient(server.URL, secretFile(t), 10*time.Second, 1, 60000, server.Client().Transport)
	page, err := client.FetchPage(context.Background(), d, "", 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if page.HasNext {
		t.Fatalf("short offset-paginated page followed stale next link: %#v", page)
	}
}

func TestValidatedAPIEndpointProfiles(t *testing.T) {
	responses := map[string]string{
		"/api/now/stats/incident":            `{"result":{"stats":{"count":"42"}}}`,
		"/api/now/attachment":                `{"result":[{"sys_id":"attachment-1","sys_updated_on":"2026-09-03 10:11:12"}]}`,
		"/api/sn_sc/servicecatalog/items":    `{"result":[{"sys_id":"item-1","name":"Laptop"}]}`,
		"/api/sn_sc/servicecatalog/catalogs": `{"result":[{"sys_id":"catalog-1","title":"Service Catalog"}]}`,
		"/api/sn_chg_rest/v1/change/model":   `{"result":[{"sys_id":{"value":"model-1"},"sys_updated_on":{"value":"2026-09-03 10:11:12"}}]}`,
		"/api/now/cmdb/instance/cmdb_ci":     `{"result":[{"sys_id":"ci-1","name":"Server"}]}`,
		"/api/now/table/sys_ws_definition":   `{"result":[{"sys_id":"service-1","sys_updated_on":"2026-09-03 10:11:12","active":"true"}]}`,
		"/api/now/table/sys_ws_operation":    `{"result":[{"sys_id":"resource-1","sys_updated_on":"2026-09-03 10:11:12","http_method":"GET"}]}`,
	}
	if got, want := len(EndpointCatalog()), len(responses); got != want {
		t.Fatalf("endpoint catalog=%d want=%d", got, want)
	}
	for _, d := range EndpointCatalog() {
		t.Run(d.Name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != d.REST.Path {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if d.REST.LimitParameter != "" && r.URL.Query().Get(d.REST.LimitParameter) != "2" {
					t.Errorf("missing endpoint limit parameter %q", d.REST.LimitParameter)
				}
				if d.REST.OffsetParameter != "" && r.URL.Query().Get(d.REST.OffsetParameter) != "4" {
					t.Errorf("missing endpoint offset parameter %q", d.REST.OffsetParameter)
				}
				for key, value := range d.REST.Parameters {
					if got := r.URL.Query().Get(key); got != value {
						t.Errorf("parameter %s=%q want=%q", key, got, value)
					}
				}
				_, _ = w.Write([]byte(responses[d.REST.Path]))
			}))
			defer server.Close()
			client := NewClient(server.URL, secretFile(t), 10*time.Second, 1, 60000, server.Client().Transport)
			page, err := client.FetchPage(context.Background(), d, "ignored-table-window", 2, 4)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Records) != 1 {
				t.Fatalf("records=%d", len(page.Records))
			}
			if strings.ContainsAny(string(page.Records[0].Raw), "\r\n") {
				t.Fatalf("record not compact: %q", page.Records[0].Raw)
			}
			if d.Name == "aggregate-incidents" && page.Records[0].ID != "incident-aggregate" {
				t.Fatalf("aggregate ID=%q", page.Records[0].ID)
			}
			if !strings.HasPrefix(d.Documentation, "https://www.servicenow.com/docs/") {
				t.Fatalf("non-vendor documentation %q", d.Documentation)
			}
		})
	}
}

func TestAPIEndpointAllExpandsValidatedProfiles(t *testing.T) {
	datasets, err := (&Config{API_Endpoint: []string{"all"}}).Datasets()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(datasets), len(EndpointCatalog()); got != want {
		t.Fatalf("API-Endpoint=all datasets=%d want=%d", got, want)
	}
	for _, dataset := range datasets {
		if dataset.REST == nil {
			t.Fatalf("non-REST dataset resolved from API-Endpoint=all: %#v", dataset)
		}
	}
}

func TestInvalidTableIsUnavailableButOtherBadRequestsAreNot(t *testing.T) {
	for _, tc := range []struct {
		name        string
		message     string
		unavailable bool
	}{
		{name: "invalid-table", message: "Invalid table sn_customerservice_case", unavailable: true},
		{name: "invalid-query", message: "Invalid query detected", unavailable: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": tc.message}})
			}))
			defer server.Close()
			client := NewClient(server.URL, secretFile(t), 10*time.Second, 0, 60000, server.Client().Transport)
			_, err := client.FetchPage(context.Background(), catalog["csm-cases"], "", 1, 0)
			if err == nil {
				t.Fatal("expected status error")
			}
			if got := IsUnavailable(err); got != tc.unavailable {
				t.Fatalf("IsUnavailable=%v want=%v error=%v", got, tc.unavailable, err)
			}
			if strings.Contains(err.Error(), tc.message) {
				t.Fatal("vendor response body leaked into error")
			}
		})
	}
}

func TestIllegalParameterClassificationDoesNotLeakVendorBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Illegal query parameters","detail":"private detail"}}`))
	}))
	defer server.Close()
	d := EndpointCatalog()[0]
	client := NewClient(server.URL, secretFile(t), 10*time.Second, 0, 60000, server.Client().Transport)
	_, err := client.FetchPage(context.Background(), d, "", 100, 100)
	if err == nil || !IsIllegalParameters(err) {
		t.Fatalf("expected classified illegal-parameters error, got %v", err)
	}
	if strings.Contains(err.Error(), "Illegal query parameters") || strings.Contains(err.Error(), "private detail") {
		t.Fatalf("vendor response leaked into error: %v", err)
	}
}

func TestNullResultIsAnEmptyPage(t *testing.T) {
	records, err := resultRecords([]byte(`{"result":null}`), "result")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("null result produced %d records", len(records))
	}
}

func TestCrossOriginRedirectIsRejectedBeforeTarget(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetRequests.Add(1)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/escaped", http.StatusFound)
	}))
	defer source.Close()

	client := NewClient(source.URL, secretFile(t), time.Second, 1, 60000, source.Client().Transport)
	_, err := client.FetchPage(context.Background(), catalog["incidents"], "", 1, 0)
	if err == nil {
		t.Fatal("cross-origin redirect succeeded")
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("cross-origin target received %d request(s)", got)
	}
}

func TestSameOriginRedirectRemainsEndpointBound(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("redirected") == "" {
			http.Redirect(w, r, server.URL+r.URL.Path+"?redirected=true", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer server.Close()
	client := NewClient(server.URL, secretFile(t), time.Second, 1, 60000, server.Client().Transport)
	if _, err := client.FetchPage(context.Background(), catalog["incidents"], "", 1, 0); err != nil {
		t.Fatal(err)
	}
}

func TestSameOriginCrossEndpointRedirectIsRejectedBeforeTarget(t *testing.T) {
	var targetRequests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/other" {
			targetRequests.Add(1)
			_, _ = w.Write([]byte(`{"result":[]}`))
			return
		}
		http.Redirect(w, r, server.URL+"/api/other", http.StatusFound)
	}))
	defer server.Close()
	client := NewClient(server.URL, secretFile(t), time.Second, 1, 60000, server.Client().Transport)
	if _, err := client.FetchPage(context.Background(), catalog["incidents"], "", 1, 0); err == nil {
		t.Fatal("same-origin cross-endpoint redirect succeeded")
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("cross-endpoint target received %d request(s)", got)
	}
}

func TestRetryAfterOverflowTerminatesWithoutEarlyRetry(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "9223372036854775807")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := NewClient(server.URL, secretFile(t), time.Second, 2, 60000, server.Client().Transport)
	_, err := client.FetchPage(context.Background(), catalog["incidents"], "", 1, 0)
	if !errors.Is(err, errRetryAfterOverflow) {
		t.Fatalf("error=%v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests=%d want=1", got)
	}
}

func TestRetryWaitAndRatePacingHonorCancellation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := NewClient(server.URL, secretFile(t), time.Minute, 3, 60000, server.Client().Transport)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := client.FetchPage(ctx, catalog["incidents"], "", 1, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests=%d want=1", got)
	}
}

func TestTerminalRetryAttemptNeverSleeps(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := NewClient(server.URL, secretFile(t), time.Second, 0, 60000, server.Client().Transport)
	started := time.Now()
	if _, err := client.FetchPage(context.Background(), catalog["incidents"], "", 1, 0); err == nil {
		t.Fatal("terminal retryable status succeeded")
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("terminal attempt slept for %s", elapsed)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("Max-Retries=0 made %d requests, want 1", got)
	}
}

func TestSupportedAuthenticationModesAndAmbiguity(t *testing.T) {
	for _, test := range []struct {
		name   string
		secret string
		oauth  bool
	}{
		{name: "basic", secret: `{"mode":"basic","username":"reader","password":"synthetic"}`},
		{name: "bearer", secret: `{"mode":"bearer","bearer_token":"synthetic-bearer"}`},
		{name: "oauth", secret: `{"mode":"oauth-client-credentials","client_id":"synthetic-id","client_secret":"synthetic-secret"}`, oauth: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth_token.do" {
					if !test.oauth {
						t.Fatal("unexpected token request")
					}
					if err := r.ParseForm(); err != nil || r.Form.Get("client_id") != "synthetic-id" || r.Form.Get("client_secret") != "synthetic-secret" {
						t.Fatal("invalid OAuth form")
					}
					_, _ = w.Write([]byte(`{"access_token":"synthetic-access","expires_in":300}`))
					return
				}
				switch test.name {
				case "basic":
					username, password, ok := r.BasicAuth()
					if !ok || username != "reader" || password != "synthetic" {
						t.Fatal("basic credentials missing")
					}
				case "bearer":
					if r.Header.Get("Authorization") != "Bearer synthetic-bearer" {
						t.Fatal("bearer credential missing")
					}
				case "oauth":
					if r.Header.Get("Authorization") != "Bearer synthetic-access" {
						t.Fatal("OAuth access token missing")
					}
				}
				_, _ = w.Write([]byte(`{"result":[]}`))
			}))
			defer server.Close()
			client := NewClient(server.URL, secretContents(t, test.secret), time.Second, 1, 60000, server.Client().Transport)
			if _, err := client.FetchPage(context.Background(), catalog["incidents"], "", 1, 0); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, secret := range []string{
		`{"mode":"basic","username":"reader","password":"synthetic","bearer_token":"ambiguous"}`,
		`{"mode":"bearer","bearer_token":"synthetic","client_id":"ambiguous"}`,
		`{"mode":"oauth-client-credentials","client_id":"id","client_secret":"secret","username":"ambiguous"}`,
	} {
		if _, _, err := loadSecret(secretContents(t, secret)); err == nil || strings.Contains(err.Error(), "synthetic") || strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("unsafe ambiguous-secret error=%v", err)
		}
	}
}

func TestRetryAfterBoundsDatesAndMalformedValues(t *testing.T) {
	maximum := uint64((1<<63 - 1) / int64(time.Second))
	if got, present, err := retryAfterDelay(strconv.FormatUint(maximum, 10), time.Unix(0, 0)); err != nil || !present || got != time.Duration(maximum)*time.Second {
		t.Fatalf("maximum delay=%s present=%v err=%v", got, present, err)
	}
	if _, _, err := retryAfterDelay(strconv.FormatUint(maximum+1, 10), time.Unix(0, 0)); !errors.Is(err, errRetryAfterOverflow) {
		t.Fatalf("overflow error=%v", err)
	}
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	if got, present, err := retryAfterDelay(now.Add(90*time.Second).Format(http.TimeFormat), now); err != nil || !present || got != 90*time.Second {
		t.Fatalf("date delay=%s present=%v err=%v", got, present, err)
	}
	if _, _, err := retryAfterDelay(time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC).Format(http.TimeFormat), now); !errors.Is(err, errRetryAfterOverflow) {
		t.Fatalf("date overflow error=%v", err)
	}
	if _, present, err := retryAfterDelay("not-a-delay", now); err != nil || present {
		t.Fatalf("malformed present=%v err=%v", present, err)
	}
}

func TestMissingTableTimestampFailsClosedAndSnapshotUsesPollTime(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":[{"sys_id":"one"}]}`))
	}))
	defer server.Close()
	runtime := newServiceNowContractRuntime()
	config := serviceNowContractConfig(t, server.URL)
	client := NewClient(server.URL, config.Secret_File, time.Second, 1, 60000, server.Client().Transport)
	pollTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	plugin := New(config, processors.NewProcessorSet(runtime))
	plugin.now = func() time.Time { return pollTime }
	if err := plugin.collect(context.Background(), runtime, client, catalog["incidents"]); err == nil {
		t.Fatal("incremental record without ordering timestamp succeeded")
	}
	if len(runtime.entries) != 0 {
		t.Fatalf("incremental entries=%d", len(runtime.entries))
	}

	snapshot := Dataset{Name: "snapshot", Product: "ITSM", Tag: "servicenow-itsm", Timestamp: "sys_updated_on", REST: &RESTSpec{Path: "/api/snapshot", ResultPath: "result", IDField: "sys_id"}}
	if err := plugin.collect(context.Background(), runtime, client, snapshot); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 1 || !runtime.entries[0].TS.StandardTime().Equal(pollTime) {
		t.Fatalf("snapshot entries=%d timestamp=%s", len(runtime.entries), runtime.entries[0].TS.StandardTime())
	}
}

func TestResponseBodyLimitAcceptsBoundaryAndRejectsOverflow(t *testing.T) {
	base := `{"result":[]}`
	for _, test := range []struct {
		name    string
		size    int64
		wantErr bool
	}{
		{name: "at-limit", size: maxResponseBytes},
		{name: "over-limit", size: maxResponseBytes + 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(base + strings.Repeat(" ", int(test.size)-len(base))))
			}))
			defer server.Close()
			client := NewClient(server.URL, secretFile(t), 10*time.Second, 1, 60000, server.Client().Transport)
			_, err := client.FetchPage(context.Background(), catalog["incidents"], "", 1, 0)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, test.wantErr)
			}
		})
	}
}

func TestRawResultPreservesDuplicateKeysAndNumericRepresentation(t *testing.T) {
	want := `{"sys_id":"first","sys_id":"second","n":1.2300e+04}`
	records, err := resultRecords([]byte(`{"result":[`+want+`]}`), "result")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || string(records[0]) != want {
		t.Fatalf("records=%q want=%q", records, want)
	}
}

func TestNoOffsetContinuationIsSameOriginAndEndpointBound(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"result":[{"sys_id":"two"}]}`))
			return
		}
		w.Header().Set("Link", `<`+server.URL+r.URL.Path+`?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`{"result":[{"sys_id":"one"}]}`))
	}))
	defer server.Close()
	d := Dataset{Name: "bounded", Product: "ITSM", Tag: "servicenow-itsm", Timestamp: "sys_updated_on", REST: &RESTSpec{Path: "/api/example", ResultPath: "result", IDField: "sys_id"}}
	client := NewClient(server.URL, secretFile(t), time.Second, 1, 60000, server.Client().Transport)
	page, err := client.FetchPage(context.Background(), d, "", 100, 0)
	if err != nil || !page.HasNext || page.NextURL == "" {
		t.Fatalf("first page=%#v err=%v", page, err)
	}
	page, err = client.FetchPageAfter(context.Background(), d, "", 100, 0, page.NextURL)
	if err != nil || page.HasNext || len(page.Records) != 1 || page.Records[0].ID != "two" {
		t.Fatalf("second page=%#v err=%v", page, err)
	}
	if _, err := client.FetchPageAfter(context.Background(), d, "", 100, 0, "https://other.invalid/api/example?page=3"); err == nil {
		t.Fatal("cross-origin continuation accepted")
	}
	if _, err := client.FetchPageAfter(context.Background(), d, "", 100, 0, server.URL+"/api/other?page=3"); err == nil {
		t.Fatal("cross-endpoint continuation accepted")
	}
}

func TestRawAndNormalizedFraming(t *testing.T) {
	d := catalog["users"]
	raw := []byte("{\n  \"sys_id\": \"1\", \"user_name\": \"alice\"\n}")
	rawConf := &Config{Normalization: "disabled"}
	got, err := FormatRecord(rawConf, d, raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("raw changed: %s", got)
	}
	normConf := &Config{Normalization: "enabled"}
	got, err = FormatRecord(normConf, d, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(string(got), "\r\n") {
		t.Fatal("normalized output is not one line")
	}
	var record map[string]any
	if err := json.Unmarshal(got, &record); err != nil {
		t.Fatal(err)
	}
	if record["userName"] != "alice" {
		t.Fatalf("canonical userName=%v", record["userName"])
	}
	if record["user_name"] != "alice" {
		t.Fatalf("source user_name was not preserved: %#v", record)
	}
	if _, ok := record["schema"]; ok {
		t.Fatal("normalization unexpectedly wrapped the vendor record")
	}
	if _, ok := record["record"]; ok {
		t.Fatal("normalization unexpectedly nested the vendor record")
	}
}

func TestNormalizationUsesCanonicalFirstPrecedenceAndReportsCollisions(t *testing.T) {
	d := catalog["users"]
	conf := &Config{
		Normalization:       "enabled",
		Normalization_Field: []string{"users:userName=user.name|user_name"},
	}
	prepared, err := PrepareRecord(conf, d, []byte(`{"userName":"canonical","user":{"name":"dotted"},"user_name":"snake"}`))
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(prepared.Data, &record); err != nil {
		t.Fatal(err)
	}
	if record["userName"] != "canonical" {
		t.Fatalf("canonical source lost precedence: %#v", record)
	}
	collisions := prepared.Intrinsic["_normalizationCollision"]
	for _, want := range []string{"userName(userName|user.name)", "userName(userName|user_name)"} {
		if !strings.Contains(collisions, want) {
			t.Fatalf("collision metadata %q missing %q", collisions, want)
		}
	}
}

func TestNormalizationCanDefineASeparateLowercaseTarget(t *testing.T) {
	conf := &Config{
		Normalization:       "enabled",
		Normalization_Field: []string{"users:username=user_name"},
	}
	prepared, err := PrepareRecord(conf, catalog["users"], []byte(`{"user_name":"alice"}`))
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(prepared.Data, &record); err != nil {
		t.Fatal(err)
	}
	if record["username"] != "alice" || record["userName"] != "alice" || record["user_name"] != "alice" {
		t.Fatalf("lowercase custom target did not remain distinct: %#v", record)
	}
}

func TestNormalizationPreservesVendorJSONLexemesAndDuplicateKeys(t *testing.T) {
	conf := &Config{
		Normalization:       "enabled",
		Normalization_Field: []string{"users:sourceNum=source_num"},
	}
	raw := []byte(`{"big":9007199254740993,"decimal":1.10,"k":1,"k":2,"text":"<&","source_num":9007199254740993}`)
	prepared, err := PrepareRecord(conf, catalog["users"], raw)
	if err != nil {
		t.Fatal(err)
	}
	got := string(prepared.Data)
	for _, exact := range []string{
		`"big":9007199254740993`, `"decimal":1.10`, `"k":1,"k":2`, `"text":"<&"`,
		`"sourceNum":9007199254740993`,
	} {
		if !strings.Contains(got, exact) {
			t.Fatalf("normalized record %q lost exact token %q", got, exact)
		}
	}
	if strings.Contains(got, `\u003c`) || strings.Contains(got, `\u0026`) || strings.ContainsAny(got, "\r\n") {
		t.Fatalf("normalized record rewrote vendor bytes or framing: %q", got)
	}
	if !json.Valid(prepared.Data) {
		t.Fatalf("normalized record is invalid JSON: %q", got)
	}
}

func TestNormalizationNestedSourceDoesNotPolluteSiblingRules(t *testing.T) {
	conf := &Config{
		Normalization: "enabled",
		Normalization_Field: []string{
			"all:alphaName=nest.name",
			"all:zuluName=name",
		},
	}
	prepared, err := PrepareRecord(conf, catalog["users"], []byte(`{"nest":{"name":"POLLUTED"},"name":"ORIGINAL"}`))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"alphaName": "POLLUTED", "zuluName": "ORIGINAL"} {
		got, err := jsonparser.GetString(prepared.Data, key)
		if err != nil || got != want {
			t.Fatalf("%s=%q err=%v want=%q record=%s", key, got, err, want, prepared.Data)
		}
	}

	phantomConf := &Config{
		Normalization: "enabled",
		Normalization_Field: []string{
			"all:alphaKey=nest.zuluName",
			"all:zuluName=src",
		},
	}
	prepared, err = PrepareRecord(phantomConf, catalog["users"], []byte(`{"nest":{"zuluName":"PHANTOM"},"src":"REAL"}`))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"alphaKey": "PHANTOM", "zuluName": "REAL"} {
		got, err := jsonparser.GetString(prepared.Data, key)
		if err != nil || got != want {
			t.Fatalf("%s=%q err=%v want=%q record=%s", key, got, err, want, prepared.Data)
		}
	}
}

func TestNormalizationAliasPrecedesNullOrEmptyVendorTarget(t *testing.T) {
	conf := &Config{
		Normalization:       "enabled",
		Normalization_Field: []string{"users:userName=user_name"},
	}
	for _, target := range []string{"null", `""`} {
		raw := []byte(`{"userName":` + target + `,"user_name":"REAL"}`)
		prepared, err := PrepareRecord(conf, catalog["users"], raw)
		if err != nil {
			t.Fatal(err)
		}
		value, kind, _, err := jsonparser.Get(prepared.Data, "userName")
		if err != nil || kind != jsonparser.String || string(value) != "REAL" {
			t.Fatalf("target=%s value=%q kind=%v err=%v record=%s", target, value, kind, err, prepared.Data)
		}
	}
}

func TestCatalogTagsAreNotDoublePrefixed(t *testing.T) {
	d := catalog["system"]
	for _, tc := range []struct {
		normalization string
		want          string
	}{
		{normalization: "disabled", want: "servicenow-platform"},
		{normalization: "enabled", want: "servicenow-platform"},
	} {
		c := &Config{Normalization: tc.normalization}
		if got := c.Tag(d); got != tc.want {
			t.Fatalf("normalization=%s tag=%q want=%q", tc.normalization, got, tc.want)
		}
	}
}

func TestBuiltInCatalogUsesSixteenSemanticProductTags(t *testing.T) {
	tags := map[string]bool{}
	for _, d := range append(Catalog(), EndpointCatalog()...) {
		tags[d.Tag] = true
	}
	want := []string{
		"servicenow-app-engine", "servicenow-audit", "servicenow-cmdb-assets", "servicenow-csm",
		"servicenow-devops", "servicenow-fsm", "servicenow-hrsd", "servicenow-irm",
		"servicenow-itam", "servicenow-itom", "servicenow-itsm", "servicenow-km",
		"servicenow-platform", "servicenow-secops", "servicenow-spm", "servicenow-vulnerability-response",
	}
	if len(tags) != len(want) {
		t.Fatalf("semantic tags=%d want=%d: %#v", len(tags), len(want), tags)
	}
	for _, tag := range want {
		if !tags[tag] {
			t.Errorf("missing semantic tag %q", tag)
		}
	}
}

func TestTagNameAllowsOneSemanticProductAndRejectsCrossProductCollapse(t *testing.T) {
	base := Config{
		BaseConfig:     hosted.BaseConfig{Ingester_UUID: "42000000-0000-4000-8000-000000000001"},
		Instance:       "https://example.service-now.com",
		Secret_File:    secretFile(t),
		Normalization:  "disabled",
		MultiTagConfig: hosted.MultiTagConfig{Tag_Name: "servicenow-custom"},
	}
	platform := base
	platform.Product = []string{"platform"}
	if err := platform.Verify(); err != nil {
		t.Fatalf("single product Tag-Name rejected: %v", err)
	}
	all := base
	all.Product = []string{"all"}
	all.Table_Override = []string{`app-engine-custom={"Table":"x_example_table"}`}
	if err := all.Verify(); err == nil || !strings.Contains(err.Error(), "semantic product tag") {
		t.Fatalf("cross-product Tag-Name error=%v", err)
	}
}

func TestNormalizationStateNamespaceTracksRuleContract(t *testing.T) {
	if got := (&Config{Normalization: "disabled"}).StateNamespace(); got != "servicenow/raw" {
		t.Fatalf("raw namespace=%q", got)
	}
	if got := (&Config{Normalization: "enabled"}).StateNamespace(); got != "servicenow/normalized-v3" {
		t.Fatalf("default normalized namespace=%q", got)
	}
	a := (&Config{Normalization: "enabled", Normalization_Field: []string{"users:userName=user.name|user_name", "all:sourceIp=source_ip"}}).StateNamespace()
	b := (&Config{Normalization: "enabled", Normalization_Field: []string{"all:sourceIp=source_ip", "users:userName=user.name|user_name"}}).StateNamespace()
	c := (&Config{Normalization: "enabled", Normalization_Field: []string{"users:userName=user_name"}}).StateNamespace()
	if a != b || a == c || a == "servicenow/normalized-v3" {
		t.Fatalf("rule namespaces a=%q b=%q c=%q", a, b, c)
	}
}

func TestRecordIDHandlesDisplayValueAndMissingField(t *testing.T) {
	if got := recordID([]byte(`{"sys_id":{"display_value":"abc","value":"native-id"}}`)); got != "native-id" {
		t.Fatalf("display-value sys_id=%q", got)
	}
	if got := recordID([]byte(`{"number":"INC0001"}`)); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("missing sys_id fallback=%q", got)
	}
}

func TestConfigRequiresCustomTableOverride(t *testing.T) {
	c := &Config{BaseConfig: hosted.BaseConfig{Ingester_UUID: "42000000-0000-4000-8000-000000000001"}, Instance: "https://example.service-now.com", Secret_File: secretFile(t), Product: []string{"app-engine"}, Normalization: "disabled"}
	if err := c.Verify(); err == nil || !strings.Contains(err.Error(), "requires Table-Override") {
		t.Fatalf("unexpected error: %v", err)
	}
	c.Table_Override = []string{`app-engine-custom={"Table":"x_example_table"}`}
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestProductAllExpandsEveryCatalogDataset(t *testing.T) {
	c := &Config{
		Product:        []string{"all"},
		Table_Override: []string{`app-engine-custom={"Table":"x_example_table"}`},
	}
	datasets, err := c.Datasets()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(datasets), len(Catalog()); got != want {
		t.Fatalf("Product=all datasets=%d want=%d", got, want)
	}
}

func TestAPIAllExcludesOnlyTenantSpecificDataset(t *testing.T) {
	c := &Config{API: []string{"all"}}
	datasets, err := c.Datasets()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(datasets), len(Catalog())-1; got != want {
		t.Fatalf("API=all datasets=%d want=%d", got, want)
	}
	for _, d := range datasets {
		if d.RequiresTableOverride {
			t.Fatalf("API=all included tenant-specific dataset %q", d.Name)
		}
	}
}

func TestDocumentedAPIEndpointConfiguration(t *testing.T) {
	c := &Config{API_Endpoint: []string{`case-list={"Product":"Customer Service Management","Path":"/api/sn_customerservice/v1/case","Tag":"servicenow-csm-case-api","Result_Path":"result.cases","ID_Field":"sys_id","Timestamp":"sys_updated_on","Limit_Parameter":"sysparm_limit","Offset_Parameter":"sysparm_offset","Required_Role":"csm_ws_integration","Documentation":"https://www.servicenow.com/docs/r/api-reference/rest-apis/case-api.html","Parameters":{"active":"true"}}`}}
	datasets, err := c.Datasets()
	if err != nil {
		t.Fatal(err)
	}
	if len(datasets) != 1 || datasets[0].REST == nil || datasets[0].REST.Path != "/api/sn_customerservice/v1/case" {
		t.Fatalf("datasets=%#v", datasets)
	}
	for _, invalid := range []string{
		`bad={"Product":"X","Path":"https://evil.invalid/api/x","Tag":"servicenow-x","Required_Role":"reader","Documentation":"https://www.servicenow.com/docs/r/api-reference/rest-apis/api-rest.html"}`,
		`bad={"Product":"X","Path":"/api/x","Tag":"servicenow-x","Required_Role":"reader","Documentation":"https://evil.invalid/docs"}`,
	} {
		if _, err := (&Config{API_Endpoint: []string{invalid}}).Datasets(); err == nil {
			t.Fatalf("unexpected success for %s", invalid)
		}
	}
}

func TestCustomBuiltInEndpointParametersDoNotMutateCatalog(t *testing.T) {
	const name = "scripted-rest-resources"
	original := cloneStringMap(endpointCatalog[name].Parameters)
	custom, err := (&Config{API_Endpoint: []string{name + `={"Parameters":{"sysparm_query":"OVERRIDDEN"}}`}}).EndpointDatasets()
	if err != nil {
		t.Fatal(err)
	}
	if custom[0].REST.Parameters["sysparm_query"] != "OVERRIDDEN" {
		t.Fatalf("custom parameters=%#v", custom[0].REST.Parameters)
	}
	if got := endpointCatalog[name].Parameters["sysparm_query"]; got != original["sysparm_query"] {
		t.Fatalf("global catalog mutated to %q", got)
	}
	plain, err := (&Config{API_Endpoint: []string{name}}).EndpointDatasets()
	if err != nil {
		t.Fatal(err)
	}
	plain[0].REST.Parameters["sysparm_query"] = "SECOND-MUTATION"
	if got := endpointCatalog[name].Parameters["sysparm_query"]; got != original["sysparm_query"] {
		t.Fatalf("returned dataset aliases global map: %q", got)
	}
}

func TestVerifyPreservesExplicitZeroAndRejectsTagConflict(t *testing.T) {
	base := func() *Config {
		return &Config{
			BaseConfig: hosted.BaseConfig{Ingester_UUID: "42000000-0000-4000-8000-000000000001"},
			Instance:   "https://example.service-now.com", Secret_File: secretFile(t), API: []string{"incidents"},
		}
	}
	explicitZero := base()
	explicitZero.Overlap = intPointer(0)
	explicitZero.Max_Retries = intPointer(0)
	if err := explicitZero.Verify(); err != nil {
		t.Fatal(err)
	}
	if explicitZero.OverlapSeconds() != 0 || explicitZero.MaxRetries() != 0 {
		t.Fatalf("explicit zero replaced: overlap=%d retries=%d", explicitZero.OverlapSeconds(), explicitZero.MaxRetries())
	}
	defaults := base()
	if err := defaults.Verify(); err != nil {
		t.Fatal(err)
	}
	if defaults.OverlapSeconds() != 300 || defaults.MaxRetries() != 4 {
		t.Fatalf("defaults overlap=%d retries=%d", defaults.OverlapSeconds(), defaults.MaxRetries())
	}
	conflict := base()
	conflict.Tag_Name = "one"
	conflict.Tag_Prefix = "two"
	if err := conflict.Verify(); err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("tag conflict error=%v", err)
	}
}

func TestProductAndTableSelection(t *testing.T) {
	c := &Config{Product: []string{"platform"}, Table: []string{"incident", "sys_user"}}
	datasets, err := c.Datasets()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(datasets), 11; got != want {
		t.Fatalf("resolved datasets=%d want=%d", got, want)
	}
	seen := map[string]bool{}
	for _, d := range datasets {
		seen[d.Table] = true
	}
	for _, table := range []string{"syslog", "sys_user", "incident"} {
		if !seen[table] {
			t.Errorf("missing table %q", table)
		}
	}
}

func TestAPIProductAndTableSelectionDeduplicates(t *testing.T) {
	c := &Config{Product: []string{"itom"}, API: []string{"itom-events", "vulnerability-items"}, Table: []string{"em_alert"}}
	datasets, err := c.Datasets()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(datasets), 3; got != want {
		t.Fatalf("resolved datasets=%d want=%d", got, want)
	}
	seen := map[string]bool{}
	for _, d := range datasets {
		seen[d.Name] = true
	}
	for _, name := range []string{"itom-events", "itom-alerts", "vulnerability-items"} {
		if !seen[name] {
			t.Errorf("missing API dataset %q", name)
		}
	}
}

func TestSelectorResolutionAndMixingGuard(t *testing.T) {
	c := &Config{Selector: []string{"incidents"}}
	datasets, err := c.Datasets()
	if err != nil {
		t.Fatal(err)
	}
	if len(datasets) != 1 || datasets[0].Table != "incident" {
		t.Fatalf("selector resolved %#v", datasets)
	}
	c.Product = []string{"itsm"}
	if _, err := c.Datasets(); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("unexpected mixed selection error: %v", err)
	}
}

func TestUnknownProductAndTableAreRejected(t *testing.T) {
	for _, c := range []*Config{
		{Product: []string{"not-a-product"}},
		{Table: []string{"not_a_servicenow_table"}},
	} {
		if _, err := c.Datasets(); err == nil {
			t.Fatalf("unexpected success for %#v", c)
		}
	}
}

func TestWindowQueryIncludesStableBounds(t *testing.T) {
	d := catalog["audit"]
	start := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	q := WindowQuery(d, start, end, 300)
	for _, part := range []string{"sys_created_on>2026-08-20 09:55:00", "sys_created_on<=2026-08-20 11:00:00", "ORDERBYsys_created_on", "ORDERBYsys_id"} {
		if !strings.Contains(q, part) {
			t.Errorf("missing %q in %q", part, q)
		}
	}
}

func TestPruneSeenRemovesMatchingDigest(t *testing.T) {
	old := time.Now().UTC().Add(-time.Hour)
	seen := map[string]time.Time{"old": old, "new": old.Add(2 * time.Hour)}
	hashes := map[string][sha256.Size]byte{
		"old": sha256.Sum256([]byte("old")),
		"new": sha256.Sum256([]byte("new")),
	}
	pruneSeen(seen, hashes, old.Add(time.Minute))
	if _, ok := seen["old"]; ok {
		t.Fatal("old cursor record was not pruned")
	}
	if _, ok := hashes["old"]; ok {
		t.Fatal("old digest was not pruned")
	}
	if _, ok := hashes["new"]; !ok {
		t.Fatal("current digest was pruned")
	}
}

func TestHostedRunnerServiceNowLookbackSyntaxAndBounds(t *testing.T) {
	type registeredConfig struct {
		ServiceNow map[string]*Config
	}
	path := filepath.Join(t.TempDir(), "hosted.conf")
	if err := os.WriteFile(path, []byte("[ServiceNow \"test\"]\nLookback=24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var parsed registeredConfig
	if err := ingestconfig.LoadConfigFile(&parsed, path); err != nil {
		t.Fatalf("stock Hosted Runner loader rejected integer Lookback: %v", err)
	}
	if stanza := parsed.ServiceNow["test"]; stanza == nil || stanza.Lookback != 24 {
		t.Fatalf("integer Lookback stanza=%+v want=24", stanza)
	}
	zeroPath := filepath.Join(t.TempDir(), "hosted.conf")
	if err := os.WriteFile(zeroPath, []byte("[ServiceNow \"test\"]\nOverlap=0\nMax-Retries=0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var zeroParsed registeredConfig
	if err := ingestconfig.LoadConfigFile(&zeroParsed, zeroPath); err != nil {
		t.Fatalf("stock Hosted Runner loader rejected explicit zero: %v", err)
	}
	zeroStanza := zeroParsed.ServiceNow["test"]
	if zeroStanza == nil || zeroStanza.Overlap == nil || *zeroStanza.Overlap != 0 || zeroStanza.Max_Retries == nil || *zeroStanza.Max_Retries != 0 {
		t.Fatalf("stock loader did not preserve explicit zero: %+v", zeroStanza)
	}
	for _, value := range []string{"24h", "7d", "1d12h", "1w", "999999999999999999999d"} {
		path := filepath.Join(t.TempDir(), "hosted.conf")
		body := `[ServiceNow "test"]
Lookback=` + value + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		var rejected registeredConfig
		if err := ingestconfig.LoadConfigFile(&rejected, path); err == nil {
			t.Fatalf("invalid Lookback=%s accepted", value)
		}
	}
	for _, value := range []int{0, 2161} {
		config := &Config{BaseConfig: hosted.BaseConfig{Ingester_UUID: "42000000-0000-4000-8000-000000000001"}, PollingConfig: hosted.PollingConfig{Lookback: value, Requests_Per_Minute: 60, Request_Interval: 300}, Instance: "https://example.service-now.com", Secret_File: secretFile(t), API: []string{"incidents"}}
		if value == 0 {
			// Zero is the documented default and must resolve to 24 hours.
			if err := config.Verify(); err != nil || config.Lookback != 24 {
				t.Fatalf("default Lookback=%d err=%v", config.Lookback, err)
			}
			continue
		}
		if err := config.Verify(); err == nil {
			t.Fatalf("out-of-range Lookback=%d accepted", value)
		}
	}
}

func TestHostedRunnerServiceNowRejectsUnsupportedPreprocessorSelection(t *testing.T) {
	type registeredConfig struct {
		ServiceNow map[string]*Config
	}
	path := filepath.Join(t.TempDir(), "hosted.conf")
	body := "[ServiceNow \"test\"]\nPreprocessor=\"unexpected\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var parsed registeredConfig
	if err := ingestconfig.LoadConfigFile(&parsed, path); err == nil {
		t.Fatal("unsupported ServiceNow Preprocessor selection was accepted")
	}
}
