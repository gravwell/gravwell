package microsoft

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestTemporalHalfOpenBoundaryAndEqualTimestamps(t *testing.T) {
	start := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	boundary := time.Date(2026, 10, 7, 11, 55, 0, 0, time.UTC)
	var dataCalls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		filter := r.URL.Query().Get("$filter")
		if !strings.Contains(filter, "activityDateTime ge ") || !strings.Contains(filter, "activityDateTime lt ") || strings.Contains(filter, " le ") {
			t.Errorf("non-half-open Graph filter %q", filter)
		}
		dataCalls++
		switch dataCalls {
		case 1:
			fmt.Fprintf(w, `{"value":[{"id":"at-start","activityDateTime":%q},{"id":"equal-a","activityDateTime":"2026-10-07T11:00:00Z"},{"id":"equal-b","activityDateTime":"2026-10-07T11:00:00Z"},{"id":"at-end","activityDateTime":%q}]}`, start.Format(time.RFC3339Nano), boundary.Format(time.RFC3339Nano))
		case 2:
			fmt.Fprintf(w, `{"value":[{"id":"at-end","activityDateTime":%q}]}`, boundary.Format(time.RFC3339Nano))
		default:
			fmt.Fprint(w, `{"value":[]}`)
		}
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"entra-directory-audits"}
	p.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
	if err := rt.PutTime("microsoft/entra-directory-audits/timestamp", start); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 3 {
		t.Fatalf("first window entries=%d, want start plus two equal-timestamp events", got)
	}
	p.now = func() time.Time { return time.Date(2026, 10, 7, 12, 10, 0, 0, time.UTC) }
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 4 {
		t.Fatalf("second window entries=%d, want boundary event exactly once", got)
	}
	p.now = func() time.Time { return time.Date(2026, 10, 7, 12, 20, 0, 0, time.UTC) }
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 4 {
		t.Fatalf("completed boundary redelivered: entries=%d", got)
	}
}

func TestTemporalConsistencyDelayRetainsLaterVisibleEvent(t *testing.T) {
	eventTime := time.Date(2026, 10, 7, 11, 57, 0, 0, time.UTC)
	visible := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		if visible {
			fmt.Fprintf(w, `{"value":[{"id":"later-visible","activityDateTime":%q}]}`, eventTime.Format(time.RFC3339Nano))
			return
		}
		fmt.Fprint(w, `{"value":[]}`)
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"entra-directory-audits"}
	p.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
	if err := rt.PutTime("microsoft/entra-directory-audits/timestamp", time.Date(2026, 10, 7, 11, 45, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("unfinalized event emitted early: %d", got)
	}
	watermark, err := rt.GetTime("microsoft/entra-directory-audits/timestamp")
	if err != nil || !watermark.Equal(time.Date(2026, 10, 7, 11, 55, 0, 0, time.UTC)) {
		t.Fatalf("first finalized watermark=%s error=%v", watermark, err)
	}
	visible = true
	p.now = func() time.Time { return time.Date(2026, 10, 7, 12, 5, 0, 0, time.UTC) }
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("later-visible event entries=%d, want 1", got)
	}
}

func TestTemporalRestartPreservesFinalizedWindow(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	windowStart, windowEnd := now.Add(-time.Hour), now.Add(-sourceConsistencyDelay)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		if r.URL.Path != "/page2" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"value":[{"id":"resumed","activityDateTime":"2026-10-07T11:30:00Z"}]}`)
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	state := streamState{WindowStart: windowStart, WindowEnd: windowEnd, Continuation: &FetchContinuation{Version: fetchContinuationVersion, Kind: KindODataGET, NextURL: server.URL + "/page2"}}
	if err := saveState(rt, "microsoft/entra-directory-audits", state); err != nil {
		t.Fatal(err)
	}
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"entra-directory-audits"}
	p.now = func() time.Time { return now.Add(30 * time.Minute) }
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	watermark, err := rt.GetTime("microsoft/entra-directory-audits/timestamp")
	if err != nil || !watermark.Equal(windowEnd) {
		t.Fatalf("resumed watermark=%s error=%v, want frozen end %s", watermark, err, windowEnd)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("resumed entries=%d", got)
	}
}

func TestTemporalFutureTimestampWaitsForEligibleWindow(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	future := now.Add(-sourceConsistencyDelay + time.Minute)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		fmt.Fprintf(w, `{"value":[{"id":"future","activityDateTime":%q}]}`, future.Format(time.RFC3339Nano))
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"entra-directory-audits"}
	p.now = func() time.Time { return now }
	if err := rt.PutTime("microsoft/entra-directory-audits/timestamp", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("future record emitted before finalized horizon: %d", got)
	}
	p.now = func() time.Time { return now.Add(10 * time.Minute) }
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("eligible future record entries=%d, want 1", got)
	}
}

// TestTemporalRecordsWithoutASourceTimestampAreDelivered pins the repair for a
// timestamped selector whose source omits, nulls, or misformats its own time
// field. Those records failed the half-open window check on its zero-timestamp
// guard, were counted as records outside the finalized window, and were never
// written -- and the window then completed and advanced the watermark past them,
// so they were lost permanently and the warning blamed the wrong condition.
// They must be delivered instead, at the inside edge of the finalized window,
// without replaying forever and without two of them collapsing onto one
// identity.
func TestTemporalRecordsWithoutASourceTimestampAreDelivered(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	watermark := time.Date(2026, 10, 7, 11, 0, 0, 0, time.UTC)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		fmt.Fprint(w, `{"value":[
			{"id":"dated","activityDateTime":"2026-10-07T11:10:00Z"},
			{"id":"missing-field"},
			{"id":"null-field","activityDateTime":null},
			{"id":"unparsable-field","activityDateTime":"07/10/2026 11:20:00"}
		]}`)
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"entra-directory-audits"}
	p.now = func() time.Time { return now }
	if err := rt.PutTime("microsoft/entra-directory-audits/timestamp", watermark); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	entries := rt.Entries()
	if len(entries) != 4 {
		t.Fatalf("entries=%d, want every record including the three undated ones", len(entries))
	}
	windowEnd := now.Add(-sourceConsistencyDelay)
	undated := 0
	for _, ent := range entries {
		if ent.TS.StandardTime().Equal(windowEnd.Add(-time.Nanosecond)) {
			undated++
		}
	}
	if undated != 3 {
		t.Fatalf("records admitted at the window boundary=%d, want the three undated ones", undated)
	}
	if got := rt.logsContaining("WARN", "outside the finalized window"); len(got) != 0 {
		t.Fatalf("undated records were misreported as outside the window: %+v", got)
	}
	lines := rt.logsContaining("WARN", "without a readable source timestamp")
	if len(lines) != 1 {
		t.Fatalf("undated-record warnings=%d, want exactly 1 (%+v)", len(lines), lines)
	}
	if got := lines[0].Params["count"]; got != "3" {
		t.Fatalf("undated count=%q, want 3", got)
	}
	if got := lines[0].Params["time_field"]; got != "activityDateTime" {
		t.Fatalf("undated warning time_field=%q", got)
	}

	// The substituted timestamp moves with the window, so the overlap receipts
	// have to be keyed on source identity or every later poll would replay the
	// same undated records forever.
	p.now = func() time.Time { return now.Add(5 * time.Minute) }
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 4 {
		t.Fatalf("entries after the overlapping poll=%d, want no redelivery", got)
	}
}

func TestTemporalTransportsUseVendorRequiredUpperBounds(t *testing.T) {
	start := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Millisecond)
	conf := testClientConfig(t, "https://fixture.invalid")
	client, err := NewClient(conf, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	graphURL, err := client.envelopeStartURL(datasets["entra-directory-audits"], start, end, "")
	if err != nil {
		t.Fatal(err)
	}
	graphQuery, _ := url.Parse(graphURL)
	if filter := graphQuery.Query().Get("$filter"); !strings.Contains(filter, " ge ") || !strings.Contains(filter, " lt ") || strings.Contains(filter, " le ") {
		t.Fatalf("Graph filter=%q", filter)
	}
	activitySelectors := selectorGroups["azure-activity-kit"]
	if got, want := len(activitySelectors), 9; got != want {
		t.Fatalf("Azure Activity selector count=%d, want %d", got, want)
	}
	activityBounds := fmt.Sprintf("eventTimestamp ge '%s' and eventTimestamp le '%s'", start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	for _, selector := range activitySelectors {
		dataset, ok := datasets[selector]
		if !ok {
			t.Fatalf("Azure Activity selector %q is not registered", selector)
		}
		if dataset.Kind != KindAzureActivity {
			t.Fatalf("Azure Activity selector %q kind=%q", selector, dataset.Kind)
		}
		activityURL, err := client.envelopeStartURL(dataset, start, end, "33333333-3333-4333-8333-333333333333")
		if err != nil {
			t.Fatalf("Azure Activity selector %q: %v", selector, err)
		}
		activityQuery, err := url.Parse(activityURL)
		if err != nil {
			t.Fatalf("Azure Activity selector %q parse URL: %v", selector, err)
		}
		filter := activityQuery.Query().Get("$filter")
		if filter != activityBounds {
			t.Errorf("Azure Activity selector %q filter=%q, want exact supported filter %q", selector, filter, activityBounds)
		}
	}

	var fabricStart, fabricEnd, huntingQuery string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		if strings.Contains(r.URL.Path, "activityevents") {
			fabricStart, fabricEnd = r.URL.Query().Get("startDateTime"), r.URL.Query().Get("endDateTime")
			fmt.Fprint(w, `{"activityEventEntities":[]}`)
			return
		}
		var request map[string]string
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		huntingQuery = request["Query"]
		fmt.Fprint(w, `{"results":[]}`)
	}))
	defer server.Close()
	conf = testClientConfig(t, server.URL)
	client, err = NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchChunk(t.Context(), datasets["fabric-activity"], start, end, "", nil); err != nil {
		t.Fatal(err)
	}
	if fabricStart != "'"+start.Format(time.RFC3339Nano)+"'" || fabricEnd != "'"+end.Add(-time.Millisecond).Format(time.RFC3339Nano)+"'" {
		t.Fatalf("Fabric inclusive wire bounds=%q..%q for logical [%s,%s)", fabricStart, fabricEnd, start, end)
	}
	if _, err := client.FetchChunk(t.Context(), datasets["defender-xdr-device-events"], start, end, "", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(huntingQuery, "Timestamp >= datetime("+start.Format(time.RFC3339Nano)+")") || !strings.Contains(huntingQuery, "Timestamp < datetime("+end.Format(time.RFC3339Nano)+")") {
		t.Fatalf("Advanced Hunting query lacks half-open bound: %q", huntingQuery)
	}
}

func TestAzureActivityCategorySelectorsFilterLocally(t *testing.T) {
	wantCategories := map[string]string{
		"azure-activity-administrative":  "Administrative",
		"azure-activity-security":        "Security",
		"azure-activity-service-health":  "ServiceHealth",
		"azure-activity-resource-health": "ResourceHealth",
		"azure-activity-alert":           "Alert",
		"azure-activity-recommendation":  "Recommendation",
		"azure-activity-policy":          "Policy",
		"azure-activity-autoscale":       "Autoscale",
	}
	start := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	const subscriptionID = "33333333-3333-4333-8333-333333333333"
	var observedFilters []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		observedFilters = append(observedFilters, r.URL.Query().Get("$filter"))
		fmt.Fprint(w, `{"value":[
			{"eventDataId":"Administrative","eventTimestamp":"2026-10-07T10:10:00Z","category":{"value":"Administrative","localizedValue":"Administrative"}},
			{"eventDataId":"Security","eventTimestamp":"2026-10-07T10:11:00Z","category":{"value":"Security","localizedValue":"Security"}},
			{"eventDataId":"ServiceHealth","eventTimestamp":"2026-10-07T10:12:00Z","category":{"value":"ServiceHealth","localizedValue":"Service Health"}},
			{"eventDataId":"ResourceHealth","eventTimestamp":"2026-10-07T10:13:00Z","category":{"value":"ResourceHealth","localizedValue":"Resource Health"}},
			{"eventDataId":"Alert","eventTimestamp":"2026-10-07T10:14:00Z","category":{"value":"Alert","localizedValue":"Alert"}},
			{"eventDataId":"Recommendation","eventTimestamp":"2026-10-07T10:15:00Z","category":{"value":"Recommendation","localizedValue":"Recommendation"}},
			{"eventDataId":"Policy","eventTimestamp":"2026-10-07T10:16:00Z","category":{"value":"Policy","localizedValue":"Policy"}},
			{"eventDataId":"Autoscale","eventTimestamp":"2026-10-07T10:17:00Z","category":{"value":"Autoscale","localizedValue":"Autoscale"}}
		]}`)
	}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, selector := range selectorGroups["azure-activity-kit"] {
		chunk, err := client.FetchChunk(t.Context(), datasets[selector], start, end, subscriptionID, nil)
		if err != nil {
			t.Fatalf("%s: %v", selector, err)
		}
		if selector == "azure-activity" {
			if got := len(chunk.Records); got != len(wantCategories) {
				t.Fatalf("%s records=%d want=%d", selector, got, len(wantCategories))
			}
			continue
		}
		want := wantCategories[selector]
		if got := len(chunk.Records); got != 1 {
			t.Fatalf("%s records=%d want=1 for category %q", selector, got, want)
		}
		if got := chunk.Records[0].ID; got != want {
			t.Fatalf("%s record ID=%q want category record %q", selector, got, want)
		}
	}
	wantFilter := fmt.Sprintf("eventTimestamp ge '%s' and eventTimestamp le '%s'", start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	for i, got := range observedFilters {
		if got != wantFilter {
			t.Errorf("request %d filter=%q want exact supported filter %q", i+1, got, wantFilter)
		}
	}
}
