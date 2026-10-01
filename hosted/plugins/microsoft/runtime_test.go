package microsoft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"golang.org/x/time/rate"
)

func TestHandleWritesOneCompactEntryAndSuppressesDuplicate(t *testing.T) {
	server := microsoftTestServer()
	defer server.Close()
	runtime := newMicrosoftTestRuntime()
	plugin := newMicrosoftTestPlugin(t, server, runtime)
	continuation, err := plugin.Handle(context.Background(), runtime)
	if err != nil {
		t.Fatal(err)
	}
	if continuation == nil || continuation.Delay != 300*time.Second {
		t.Fatalf("continuation=%#v", continuation)
	}
	if len(runtime.entries) != 1 {
		t.Fatalf("entries=%d", len(runtime.entries))
	}
	if !json.Valid(runtime.entries[0].Data) || strings.ContainsAny(string(runtime.entries[0].Data), "\r\n") {
		t.Fatalf("entry framing=%q", runtime.entries[0].Data)
	}
	if _, ok := runtime.values[stateKey("microsoft", "entra-directory-audits", "")]; !ok {
		t.Fatal("state was not persisted")
	}
	if _, err := plugin.Handle(context.Background(), runtime); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 1 {
		t.Fatalf("duplicate written: %d", len(runtime.entries))
	}
}

func TestRecordDigestIgnoresObjectKeyOrder(t *testing.T) {
	first := []byte(`{"large":9007199254740993,"nested":{"a":1,"b":2}}`)
	second := []byte(`{"nested":{"b":2,"a":1},"large":9007199254740993}`)
	changed := []byte(`{"large":9007199254740994,"nested":{"a":1,"b":2}}`)
	dataset := datasets["entra-directory-audits"]

	firstDigest, err := recordDigest(first, dataset)
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := recordDigest(second, dataset)
	if err != nil {
		t.Fatal(err)
	}
	changedDigest, err := recordDigest(changed, dataset)
	if err != nil {
		t.Fatal(err)
	}
	if firstDigest != secondDigest {
		t.Fatalf("semantically identical records produced different digests")
	}
	if firstDigest == changedDigest {
		t.Fatalf("changed record produced the same digest")
	}
}

func TestRecordDigestUsesSelectorSpecificDefenderCloudState(t *testing.T) {
	tests := []struct {
		name       string
		dataset    Dataset
		first      string
		ignored    string
		actionable string
	}{
		{
			name:       "assessment additionalData enrichment",
			dataset:    datasets["defender-cloud-assessments"],
			first:      `{"id":"assessment-1","properties":{"status":{"code":"Unhealthy"},"additionalData":{"RecommendationManagementSources":"{\"source\":\"Policy\"}"}}}`,
			ignored:    `{"id":"assessment-1","properties":{"status":{"code":"Unhealthy"},"additionalData":null}}`,
			actionable: `{"id":"assessment-1","properties":{"status":{"code":"Healthy"},"additionalData":null}}`,
		},
		{
			name:       "secure score expanded definitions",
			dataset:    datasets["defender-cloud-secure-score-controls"],
			first:      `{"id":"control-1","properties":{"score":{"current":1,"max":10},"definition":{"properties":{"assessmentDefinitions":[{"id":"a"}]}}}}`,
			ignored:    `{"id":"control-1","properties":{"score":{"current":1,"max":10},"definition":{"properties":{"assessmentDefinitions":[{"id":"a"},{"id":"b"}]}}}}`,
			actionable: `{"id":"control-1","properties":{"score":{"current":2,"max":10},"definition":{"properties":{"assessmentDefinitions":[{"id":"a"},{"id":"b"}]}}}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			first, err := recordDigest([]byte(tc.first), tc.dataset)
			if err != nil {
				t.Fatal(err)
			}
			ignored, err := recordDigest([]byte(tc.ignored), tc.dataset)
			if err != nil {
				t.Fatal(err)
			}
			actionable, err := recordDigest([]byte(tc.actionable), tc.dataset)
			if err != nil {
				t.Fatal(err)
			}
			if first != ignored {
				t.Fatal("vendor-generated enrichment changed the state fingerprint")
			}
			if first == actionable {
				t.Fatal("actionable state change did not change the fingerprint")
			}
		})
	}
}

func TestAzureRoleDefinitionsDigestIgnoresVolatileTimestampsWithoutChangingRaw(t *testing.T) {
	dataset := datasets["azure-role-definitions"]
	first := []byte(`{"id":"role-1","properties":{"description":"Read resources","createdOn":"2024-07-11T17:43:35Z","updatedOn":"2024-07-11T17:43:35Z","permissions":[{"actions":["Microsoft.Resources/subscriptions/read"]}]}}`)
	updatedOnOnly := []byte(`{"id":"role-1","properties":{"description":"Read resources","createdOn":"2024-07-11T17:43:35Z","updatedOn":"2024-07-15T15:01:49Z","permissions":[{"actions":["Microsoft.Resources/subscriptions/read"]}]}}`)
	createdOnOnly := []byte(`{"id":"role-1","properties":{"description":"Read resources","createdOn":"2024-07-15T15:01:49Z","updatedOn":"2024-07-11T17:43:35Z","permissions":[{"actions":["Microsoft.Resources/subscriptions/read"]}]}}`)
	permissionChanged := []byte(`{"id":"role-1","properties":{"description":"Read resources","createdOn":"2024-07-15T15:01:49Z","updatedOn":"2024-07-15T15:01:49Z","permissions":[{"actions":["Microsoft.Resources/subscriptions/write"]}]}}`)

	firstDigest, err := recordDigest(first, dataset)
	if err != nil {
		t.Fatal(err)
	}
	updatedOnDigest, err := recordDigest(updatedOnOnly, dataset)
	if err != nil {
		t.Fatal(err)
	}
	createdOnDigest, err := recordDigest(createdOnOnly, dataset)
	if err != nil {
		t.Fatal(err)
	}
	permissionDigest, err := recordDigest(permissionChanged, dataset)
	if err != nil {
		t.Fatal(err)
	}
	if firstDigest != updatedOnDigest {
		t.Fatal("updatedOn-only change changed the state fingerprint")
	}
	if firstDigest != createdOnDigest {
		t.Fatal("createdOn-only change changed the state fingerprint")
	}
	if firstDigest == permissionDigest {
		t.Fatal("permission change did not change the state fingerprint")
	}

	emitted, _, err := formatRecord(updatedOnOnly, "role-1", time.Now(), dataset, "disabled")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(emitted, updatedOnOnly) {
		t.Fatalf("raw payload changed: got=%s want=%s", emitted, updatedOnOnly)
	}
}

func TestDefenderCloudEightPollSoakIsQuietUntilActionableStateChanges(t *testing.T) {
	server := defenderCloudStateTestServer()
	defer server.Close()
	runtime := newMicrosoftTestRuntime()
	conf := testClientConfig(t, server.URL)
	conf.Ingester_UUID = "550e8400-e29b-41d4-a716-446655440000"
	conf.Subscription_ID = []string{"33333333-3333-3333-3333-333333333333"}
	conf.Api = []string{"defender-cloud-assessments", "defender-cloud-secure-score-controls"}
	conf.Lookback = 1
	conf.Request_Interval = 300
	conf.Overlap = 60
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	plugin := New(conf, processors.NewProcessorSet(runtime))
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	plugin.client = client
	plugin.now = func() time.Time { return time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC) }

	for round := 1; round <= 8; round++ {
		if _, err := plugin.Handle(context.Background(), runtime); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if got, want := len(runtime.entries), 20; got != want {
			t.Fatalf("round %d wrote representation-only changes: entries=%d want=%d", round, got, want)
		}
	}
	if _, err := plugin.Handle(context.Background(), runtime); err != nil {
		t.Fatal(err)
	}
	if got, want := len(runtime.entries), 22; got != want {
		t.Fatalf("actionable status/score changes were not emitted exactly once: entries=%d want=%d", got, want)
	}
}

func defenderCloudStateTestServer() *httptest.Server {
	var mu sync.Mutex
	hits := make(map[string]int)
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"synthetic-access-token","expires_in":3600}`))
			return
		}
		mu.Lock()
		hits[r.URL.Path]++
		round := hits[r.URL.Path]
		mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/assessments"):
			records := make([]any, 0, 17)
			for index := 0; index < 17; index++ {
				status := "Unhealthy"
				if round >= 9 && index == 0 {
					status = "Healthy"
				}
				properties := map[string]any{
					"status": map[string]any{
						"code": status, "firstEvaluationDate": "2026-08-20T01:00:00Z",
					},
				}
				switch round {
				case 1:
					properties["additionalData"] = map[string]any{"RecommendationManagementSources": `{"source":"Policy"}`}
				case 2:
					properties["additionalData"] = nil
				case 3:
					properties["additionalData"] = "vendor-enrichment"
				case 4, 5, 6:
					// Simulate the observed vendor type/presence boundary.
				case 7, 8, 9:
					properties["additionalData"] = map[string]any{"RecommendationManagementSources": `{"source":"Policy","revision":2}`}
				}
				records = append(records, map[string]any{
					"id": fmt.Sprintf("assessment-%02d", index), "properties": properties,
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"value": records})
		case strings.HasSuffix(r.URL.Path, "/secureScoreControls"):
			records := make([]any, 0, 3)
			for index := 0; index < 3; index++ {
				current := 1
				if round >= 9 && index == 0 {
					current = 2
				}
				definitions := []any{map[string]any{"id": "assessment-a"}}
				if index < 2 && round >= 6 {
					definitions = []any{map[string]any{"id": fmt.Sprintf("assessment-round-%d", round)}}
				}
				if index < 2 && round >= 8 {
					definitions = []any{map[string]any{"id": "assessment-round-7"}}
				}
				records = append(records, map[string]any{
					"id": fmt.Sprintf("control-%02d", index),
					"properties": map[string]any{
						"score":      map[string]any{"current": current, "max": 10},
						"definition": map[string]any{"properties": map[string]any{"assessmentDefinitions": definitions}},
					},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"value": records})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestFormatRecordCompactsAtWriteBoundaryAndPreservesLargeNumbers(t *testing.T) {
	raw := []byte("{\n  \"id\": \"record-1\",\n  \"large\": 9007199254740993\n}")
	dataset := datasets["entra-directory-audits"]
	ts := time.Date(2026, 8, 20, 1, 2, 3, 0, time.UTC)

	plain, plainMetadata, err := formatRecord(raw, "record-1", ts, dataset, "disabled")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(plain), `{"id":"record-1","large":9007199254740993}`; got != want {
		t.Fatalf("raw framing=%q want=%q", got, want)
	}
	if plainMetadata["_vendor"] != "Microsoft" || plainMetadata["_source"] != dataset.Name || len(plainMetadata) != 6 {
		t.Fatalf("raw intrinsic metadata=%#v", plainMetadata)
	}

	normalized, normalizedMetadata, err := formatRecord(raw, "record-1", ts, dataset, "enabled")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(string(normalized), "\r\n") || !strings.Contains(string(normalized), `"large":9007199254740993`) {
		t.Fatalf("normalized record lost framing or numeric precision: %s", normalized)
	}
	if normalizedMetadata["_apiVersion"] == "" || !strings.Contains(string(normalized), `"_vendor":"Microsoft"`) {
		t.Fatalf("normalized metadata missing: payload=%s metadata=%#v", normalized, normalizedMetadata)
	}
}

func TestHandleNormalizationUsesDistinctTagStateAndPreservesRecord(t *testing.T) {
	server := microsoftTestServer()
	defer server.Close()
	runtime := newMicrosoftTestRuntime()
	plugin := newMicrosoftTestPlugin(t, server, runtime)
	plugin.conf.Normalization = "enabled"
	if _, err := plugin.Handle(context.Background(), runtime); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 1 {
		t.Fatalf("entries=%d", len(runtime.entries))
	}
	var wrapped struct {
		ContractVersion string          `json:"contractVersion"`
		Selector        string          `json:"selector"`
		Record          json.RawMessage `json:"record"`
	}
	if err := json.Unmarshal(runtime.entries[0].Data, &wrapped); err != nil {
		t.Fatal(err)
	}
	if wrapped.ContractVersion != "microsoft-normalization-v1" || wrapped.Selector != "entra-directory-audits" {
		t.Fatalf("wrapper=%s", runtime.entries[0].Data)
	}
	if strings.ContainsAny(string(runtime.entries[0].Data), "\r\n") || !json.Valid(wrapped.Record) {
		t.Fatalf("normalized framing=%q", runtime.entries[0].Data)
	}
	if _, ok := runtime.values[stateKey("microsoft-normalized-v1", "entra-directory-audits", "")]; !ok {
		t.Fatal("normalized state was not isolated")
	}
	if got := plugin.conf.Tag("entra-audit-logs"); got != "entra-audit-logs-normalized" {
		t.Fatalf("normalized tag=%q", got)
	}
}

func TestWriteFailureDoesNotAdvanceMicrosoftState(t *testing.T) {
	server := microsoftTestServer()
	defer server.Close()
	runtime := newMicrosoftTestRuntime()
	runtime.writeErr = errors.New("synthetic write failure")
	plugin := newMicrosoftTestPlugin(t, server, runtime)
	if _, err := plugin.Handle(context.Background(), runtime); !errors.Is(err, runtime.writeErr) {
		t.Fatalf("Handle error=%v", err)
	}
	if len(runtime.values) != 0 {
		t.Fatalf("state advanced after failed write: %#v", runtime.values)
	}
}

func TestHandleLogsBlockedSelectorAndContinuesAfterConfiguredInterval(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"synthetic-access-token","expires_in":3600}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/regulatoryComplianceStandards") {
			http.Error(w, "unavailable in this plan", http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/secureScores") {
			_, _ = w.Write([]byte(`{"value":[{"id":"score-1","name":"ascScore","properties":{"score":{"current":0,"max":0,"percentage":0}}}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	runtime := newMicrosoftTestRuntime()
	conf := testClientConfig(t, server.URL)
	conf.Ingester_UUID = "550e8400-e29b-41d4-a716-446655440000"
	conf.Subscription_ID = []string{"33333333-3333-3333-3333-333333333333"}
	conf.Api = []string{"defender-cloud-regulatory-standards", "defender-cloud-secure-scores"}
	conf.Lookback = 1
	conf.Request_Interval = 300
	conf.Overlap = 60
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	plugin := New(conf, processors.NewProcessorSet(runtime))
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	plugin.client = client
	plugin.now = func() time.Time { return time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC) }
	continuation, err := plugin.Handle(context.Background(), runtime)
	if err != nil {
		t.Fatalf("blocked selector escaped the configured poll cadence: %v", err)
	}
	if continuation == nil || continuation.Delay != 300*time.Second {
		t.Fatalf("continuation=%#v", continuation)
	}
	if len(runtime.entries) != 1 {
		t.Fatalf("later dataset was starved; entries=%d", len(runtime.entries))
	}
	assertMicrosoftSelectorError(t, runtime, "defender-cloud-regulatory-standards", true, "HTTP 400")
}

func TestHostedAdapterDoesNotReplaceBlockedSelectorIntervalWithJobErrorDelay(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprintf("HTTP_%d", status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
					_, _ = w.Write([]byte(`{"access_token":"synthetic-access-token","expires_in":3600}`))
					return
				}
				http.Error(w, "not entitled", status)
			}))
			defer server.Close()
			runtime := newMicrosoftTestRuntime()
			runtime.stopOnSleep = true
			conf := testClientConfig(t, server.URL)
			conf.Ingester_UUID = "550e8400-e29b-41d4-a716-446655440000"
			conf.Subscription_ID = []string{"33333333-3333-3333-3333-333333333333"}
			conf.Api = []string{"defender-cloud-assessments"}
			conf.Lookback = 1
			conf.Request_Interval = 300
			conf.Overlap = 60
			if err := conf.Verify(); err != nil {
				t.Fatal(err)
			}
			plugin := New(conf, processors.NewProcessorSet(runtime))
			client, err := NewClient(conf, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			client.limiter = rate.NewLimiter(rate.Inf, 1)
			plugin.client = client
			plugin.now = func() time.Time { return time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC) }

			if err := hosted.WrapJob(plugin).Run(context.Background(), runtime); err != nil {
				t.Fatal(err)
			}
			if got, want := runtime.sleeps, []time.Duration{300 * time.Second}; !equalDurations(got, want) {
				t.Fatalf("scheduler sleeps=%v want=%v; generic JobErrorDelay=%v must not replace Request-Interval", got, want, hosted.JobErrorDelay)
			}
			assertMicrosoftSelectorError(t, runtime, "defender-cloud-assessments", true, fmt.Sprintf("HTTP %d", status))
			for _, event := range runtime.errorLogs {
				if event.message == "handle failed" {
					t.Fatalf("blocked selector reached generic Hosted Runner error path: %#v", event)
				}
			}
		})
	}
}

func TestSafePollErrorRedactsConfiguredMicrosoftIdentifiers(t *testing.T) {
	conf := &Config{
		Tenant_ID:       "aaaaaaaa-1111-1111-1111-111111111111",
		Client_ID:       "bbbbbbbb-2222-2222-2222-222222222222",
		Subscription_ID: []string{"cccccccc-3333-3333-3333-333333333333"},
	}
	plugin := &Microsoft{conf: conf}
	got := plugin.safePollError(fmt.Errorf("tenant %s client %s GET /subscriptions/%s failed with HTTP 403", conf.Tenant_ID, strings.ToUpper(conf.Client_ID), conf.Subscription_ID[0]))
	for _, identifier := range []string{conf.Tenant_ID, strings.ToUpper(conf.Client_ID), conf.Subscription_ID[0]} {
		if strings.Contains(got, identifier) {
			t.Fatalf("configured identifier leaked in poll error: %q", got)
		}
	}
	if !strings.Contains(got, "HTTP 403") || strings.Count(got, "[redacted-id]") != 3 {
		t.Fatalf("safe poll error lost useful status or redaction markers: %q", got)
	}
}

func microsoftTestServer() *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"synthetic-access-token","expires_in":3600}`))
			return
		}
		if r.URL.Path != "/v1.0/auditLogs/directoryAudits" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
  "value": [
    {
      "id": "event-1",
      "activityDateTime": "2026-08-16T01:00:00Z",
      "result": "success"
    }
  ]
}`))
	}))
}

func newMicrosoftTestPlugin(t *testing.T, server *httptest.Server, runtime *microsoftTestRuntime) *Microsoft {
	t.Helper()
	conf := testClientConfig(t, server.URL)
	conf.Ingester_UUID = "550e8400-e29b-41d4-a716-446655440000"
	conf.Api = []string{"entra-directory-audits"}
	conf.Lookback = 1
	conf.Request_Interval = 300
	conf.Overlap = 60
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	plugin := New(conf, processors.NewProcessorSet(runtime))
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	plugin.client = client
	plugin.now = func() time.Time { return time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC) }
	return plugin
}

type microsoftTestRuntime struct {
	mu          sync.Mutex
	values      map[string][]byte
	entries     []entry.Entry
	writeErr    error
	errorLogs   []microsoftTestLog
	sleeps      []time.Duration
	stopOnSleep bool
}

type microsoftTestLog struct {
	message string
	params  []rfc5424.SDParam
}

func newMicrosoftTestRuntime() *microsoftTestRuntime {
	return &microsoftTestRuntime{values: make(map[string][]byte)}
}
func (r *microsoftTestRuntime) Alive() bool { return true }
func (r *microsoftTestRuntime) Sleep(delay time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sleeps = append(r.sleeps, delay)
	return r.stopOnSleep
}
func (r *microsoftTestRuntime) Context() context.Context         { return context.Background() }
func (r *microsoftTestRuntime) Debug(string, ...rfc5424.SDParam) {}
func (r *microsoftTestRuntime) Info(string, ...rfc5424.SDParam)  {}
func (r *microsoftTestRuntime) Warn(string, ...rfc5424.SDParam)  {}
func (r *microsoftTestRuntime) Error(message string, params ...rfc5424.SDParam) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errorLogs = append(r.errorLogs, microsoftTestLog{message: message, params: append([]rfc5424.SDParam(nil), params...)})
}
func (r *microsoftTestRuntime) SetError(error)                              {}
func (r *microsoftTestRuntime) ClearError()                                 {}
func (r *microsoftTestRuntime) SetWarn(string)                              {}
func (r *microsoftTestRuntime) ClearWarn()                                  {}
func (r *microsoftTestRuntime) Critical(string, ...rfc5424.SDParam)         {}
func (r *microsoftTestRuntime) NegotiateTag(string) (entry.EntryTag, error) { return 1, nil }
func (r *microsoftTestRuntime) LookupTag(entry.EntryTag) (string, bool) {
	return "entra-audit-logs", true
}
func (r *microsoftTestRuntime) KnownTags() []string            { return []string{"entra-audit-logs"} }
func (r *microsoftTestRuntime) GetInt64(string) (int64, error) { return 0, storage.ErrStorageNotFound }
func (r *microsoftTestRuntime) PutInt64(string, int64) error   { return nil }
func (r *microsoftTestRuntime) GetTime(string) (time.Time, error) {
	return time.Time{}, storage.ErrStorageNotFound
}
func (r *microsoftTestRuntime) PutTime(string, time.Time) error { return nil }
func (r *microsoftTestRuntime) GetString(key string) (string, error) {
	value, err := r.Get(key)
	return string(value), err
}
func (r *microsoftTestRuntime) PutString(key, value string) error { return r.Put(key, []byte(value)) }
func (r *microsoftTestRuntime) Get(key string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.values[key]
	if !ok {
		return nil, storage.ErrStorageNotFound
	}
	return append([]byte(nil), value...), nil
}
func (r *microsoftTestRuntime) Put(key string, value []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[key] = append([]byte(nil), value...)
	return nil
}
func (r *microsoftTestRuntime) Write(value entry.Entry) error {
	copy := value
	return r.WriteEntry(&copy)
}
func (r *microsoftTestRuntime) WriteEntry(value *entry.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeErr != nil {
		return r.writeErr
	}
	copy := *value
	copy.Data = append([]byte(nil), value.Data...)
	r.entries = append(r.entries, copy)
	return nil
}
func (r *microsoftTestRuntime) WriteEntryContext(_ context.Context, value *entry.Entry) error {
	return r.WriteEntry(value)
}
func (r *microsoftTestRuntime) WriteBatch(values []*entry.Entry) error {
	for _, value := range values {
		if err := r.WriteEntry(value); err != nil {
			return err
		}
	}
	return nil
}
func (r *microsoftTestRuntime) WriteBatchContext(_ context.Context, values []*entry.Entry) error {
	return r.WriteBatch(values)
}

func assertMicrosoftSelectorError(t *testing.T, runtime *microsoftTestRuntime, selector string, subscriptionScoped bool, errorFragment string) {
	t.Helper()
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	for _, event := range runtime.errorLogs {
		if event.message != "Microsoft API selector poll failed" {
			continue
		}
		fields := make(map[string]string, len(event.params))
		for _, param := range event.params {
			fields[param.Name] = param.Value
		}
		if fields["selector"] == selector && fields["subscription_scoped"] == fmt.Sprint(subscriptionScoped) && strings.Contains(fields["error"], errorFragment) {
			if strings.Contains(fields["error"], "33333333-3333-3333-3333-333333333333") {
				t.Fatal("selector error exposed the subscription identifier")
			}
			return
		}
	}
	t.Fatalf("missing structured selector error selector=%q scoped=%v error~%q: %#v", selector, subscriptionScoped, errorFragment, runtime.errorLogs)
}

func equalDurations(left, right []time.Duration) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

var _ hosted.Runtime = (*microsoftTestRuntime)(nil)
