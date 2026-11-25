package microsoft

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// The page count, the per-request work budget, and the per-response byte cap
// bound a chunk in three dimensions that do not bound what it retains. Without
// a cumulative budget one chunk could hold defaultMaxPages x
// maximumAPIResponseBody of compacted records -- and prepareRecords a second
// copy of all of it -- before the first entry reached the muxer, defeating the
// resident-set bound maximumAPIResponseBody exists to provide and risking the
// whole Hosted Runner process, which hosts every other configured plugin.
func TestChunkRetentionCeilingIsFarBelowThePageCeiling(t *testing.T) {
	if maximumChunkRecordBytes <= 0 || maximumChunkRecords <= 0 {
		t.Fatalf("retention budget must be positive: bytes=%d records=%d", maximumChunkRecordBytes, maximumChunkRecords)
	}
	// One response may still overshoot the budget, so the real ceiling is the
	// budget plus one response. It has to stay far under the page ceiling or the
	// budget is not buying anything.
	ceiling := maximumChunkRecordBytes + maximumAPIResponseBody
	pageCeiling := defaultMaxPages * maximumAPIResponseBody
	if ceiling*4 > pageCeiling {
		t.Fatalf("retention ceiling %d is not meaningfully below the page ceiling %d", ceiling, pageCeiling)
	}
	// A single maximum-sized response must always fit, so every chunk makes at
	// least one page of forward progress whatever the source returns.
	if maximumChunkRecordBytes < maximumAPIResponseBody {
		t.Fatalf("retention budget %d cannot admit one %d-byte response", maximumChunkRecordBytes, maximumAPIResponseBody)
	}
}

func TestNewClientInstallsTheProductionRetentionBudget(t *testing.T) {
	client, err := NewClient(testClientConfig(t, "https://graph.microsoft.com"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if client.chunkRetentionBytes != maximumChunkRecordBytes || client.chunkRetentionRecords != maximumChunkRecords {
		t.Fatalf("client budget bytes=%d records=%d, want %d/%d",
			client.chunkRetentionBytes, client.chunkRetentionRecords, maximumChunkRecordBytes, maximumChunkRecords)
	}
	// A zero or negative override must fall back to the production bound rather
	// than disabling the guard, so a client built another way stays bounded.
	for _, tc := range []struct{ bytes, records int }{{0, 0}, {-1, -1}} {
		client.chunkRetentionBytes, client.chunkRetentionRecords = tc.bytes, tc.records
		if !client.withinRetentionBudget(&FetchChunk{retained: maximumChunkRecordBytes - 1}) {
			t.Fatalf("override %+v rejected a chunk inside the production bound", tc)
		}
		if client.withinRetentionBudget(&FetchChunk{retained: maximumChunkRecordBytes}) {
			t.Fatalf("override %+v admitted a chunk at the production bound", tc)
		}
	}
	client.chunkRetentionBytes, client.chunkRetentionRecords = maximumChunkRecordBytes, maximumChunkRecords
	if client.withinRetentionBudget(&FetchChunk{Records: make([]FetchedRecord, maximumChunkRecords)}) {
		t.Fatal("record count at the bound was admitted")
	}
}

// Each paginating kind has to stop on the retention budget and hand back the
// durable continuation it would have returned at the request budget.
func TestEveryPaginatingKindStopsOnTheRetentionBudget(t *testing.T) {
	const perPage = 8
	filler := strings.Repeat("x", 512)
	for _, tc := range []struct {
		selector string
		prior    func() *FetchContinuation
		body     func(page int64, host string, r *http.Request) string
	}{
		{selector: "azure-activity", body: func(page int64, host string, _ *http.Request) string {
			return envelopePage(page, perPage, filler, `"eventDataId"`, `"eventTimestamp":"2026-10-20T00:00:00Z"`,
				fmt.Sprintf(`,"nextLink":%q`, host+"/next?page="+fmt.Sprint(page+1)))
		}},
		{selector: "entra-users", body: func(page int64, host string, _ *http.Request) string {
			return envelopePage(page, perPage, filler, `"id"`, `"createdDateTime":"2026-10-20T00:00:00Z"`,
				fmt.Sprintf(`,"@odata.nextLink":%q`, host+"/next?page="+fmt.Sprint(page+1)))
		}},
		{selector: "azure-resource-inventory", body: func(page int64, _ string, _ *http.Request) string {
			return `{"data":[` + jsonRecords(page, perPage, filler, `"id"`, "") + `],"skipToken":"token-` + fmt.Sprint(page+1) + `"}`
		}},
		{selector: "fabric-activity", body: func(page int64, host string, _ *http.Request) string {
			return `{"activityEventEntities":[` + jsonRecords(page, perPage, filler, `"Id"`, `"CreationTime":"2026-10-20T00:00:00Z"`) +
				fmt.Sprintf(`],"continuationUri":%q}`, host+"/next?page="+fmt.Sprint(page+1))
		}},
		{
			// Append-mode hunting paginates over planned windows rather than
			// continuation URLs, so hand it more windows than the budget admits.
			selector: "defender-xdr-device-events",
			prior: func() *FetchContinuation {
				start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
				windows := make([]FetchWindow, 10)
				for i := range windows {
					windows[i] = FetchWindow{Start: start.Add(time.Duration(i) * time.Minute), End: start.Add(time.Duration(i+1) * time.Minute)}
				}
				return &FetchContinuation{Version: fetchContinuationVersion, Kind: KindAdvancedHunting, HuntingWindows: windows}
			},
			body: func(page int64, _ string, _ *http.Request) string {
				return `{"results":[` + jsonRecords(page, perPage, filler, `"ReportId"`, `"Timestamp":"2026-10-20T00:00:00Z","DeviceId":"device"`) + `]}`
			},
		},
		{
			// Parent/child charges only the child records it actually retains,
			// so one parent page has to hand out more children than fit.
			selector: "defender-users",
			body: func(page int64, _ string, r *http.Request) string {
				if r.URL.Path == "/api/machines" {
					return `{"value":[` + jsonRecords(0, 12, "", `"id"`, "") + `]}`
				}
				return envelopePage(page, perPage, filler, `"accountName"`, "", "")
			},
		},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			var requests atomic.Int64
			var server *httptest.Server
			server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/token") {
					fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
					return
				}
				_, _ = w.Write([]byte(tc.body(requests.Add(1), server.URL, r)))
			}))
			defer server.Close()

			client, err := NewClient(testClientConfig(t, server.URL), server.Client())
			if err != nil {
				t.Fatal(err)
			}
			client.limiter = rate.NewLimiter(rate.Inf, 1)
			client.fetchDeadline = time.Minute
			// Two pages' worth, so the budget and not the page ceiling is what
			// stops the loop and the overshoot stays observable.
			client.chunkRetentionBytes = perPage * 600 * 2
			client.chunkRetentionRecords = maximumChunkRecords

			dataset := datasets[tc.selector]
			start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
			var prior *FetchContinuation
			if tc.prior != nil {
				prior = tc.prior()
			}
			chunk, err := client.FetchChunk(t.Context(), dataset, start, start.Add(time.Hour), "33333333-3333-3333-3333-333333333333", prior)
			if err != nil {
				t.Fatal(err)
			}
			if chunk.Complete || chunk.Continuation == nil {
				t.Fatalf("complete=%t continuation=%v", chunk.Complete, chunk.Continuation)
			}
			if got := requests.Load(); got >= defaultMaxPages {
				t.Fatalf("requests=%d: the page ceiling stopped the loop, not the retention budget", got)
			}
			if chunk.retained < client.chunkRetentionBytes {
				t.Fatalf("retained=%d stopped before reaching the budget %d", chunk.retained, client.chunkRetentionBytes)
			}
			// Overshoot is bounded by the one response that crossed the budget.
			// 1 KiB per record is a generous ceiling on the 512-byte filler plus
			// its field scaffolding, so this is one page's worth.
			if ceiling := client.chunkRetentionBytes + perPage*1024; chunk.retained >= ceiling {
				t.Fatalf("retained=%d overshot the budget %d by more than one page (ceiling %d)",
					chunk.retained, client.chunkRetentionBytes, ceiling)
			}
			if len(chunk.Records) == 0 {
				t.Fatal("budgeted chunk delivered nothing")
			}
			// The cursor it yields has to be one this build can resume.
			if _, err := continuationFor(dataset, chunk.Continuation); err != nil {
				t.Fatalf("yielded continuation is not resumable: %v", err)
			}
		})
	}
}

// Stopping on the budget is a boundary, not a loss: driving the chunks to
// completion has to deliver every source record exactly once.
func TestRetentionBudgetResumesWithoutLossOrDuplication(t *testing.T) {
	const pages = 9
	const perPage = 7
	var requests atomic.Int64
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		page := requests.Add(1)
		next := ""
		if page < pages {
			next = fmt.Sprintf(`,"@odata.nextLink":%q`, server.URL+"/next?page="+fmt.Sprint(page+1))
		}
		_, _ = w.Write([]byte(envelopePage(page, perPage, "", `"id"`, `"createdDateTime":"2026-10-20T00:00:00Z"`, next)))
	}))
	defer server.Close()

	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.fetchDeadline = time.Minute
	// Small enough that the budget trips several times across the page set.
	client.chunkRetentionRecords = perPage * 2

	dataset := datasets["entra-users"]
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	seen := map[string]int{}
	var cursor *FetchContinuation
	chunks := 0
	for {
		chunks++
		if chunks > pages+2 {
			t.Fatalf("resume did not converge after %d chunks", chunks)
		}
		chunk, err := client.FetchChunk(t.Context(), dataset, start, start.Add(time.Hour), "", cursor)
		if err != nil {
			t.Fatal(err)
		}
		if len(chunk.Records) > client.chunkRetentionRecords+perPage {
			t.Fatalf("chunk retained %d records past the %d-record budget", len(chunk.Records), client.chunkRetentionRecords)
		}
		for _, record := range chunk.Records {
			seen[record.ID]++
		}
		if chunk.Complete {
			break
		}
		if chunk.Continuation == nil {
			t.Fatal("incomplete chunk returned no continuation")
		}
		cursor = chunk.Continuation
	}
	if chunks < 2 {
		t.Fatalf("budget never tripped: chunks=%d", chunks)
	}
	if got := requests.Load(); got != pages {
		t.Fatalf("source requests=%d, want exactly %d: a page was re-read or skipped", got, pages)
	}
	if len(seen) != pages*perPage {
		t.Fatalf("delivered %d distinct records, want %d", len(seen), pages*perPage)
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("record %q delivered %d times", id, count)
		}
	}
}

// The whole poll still converges through the runner's immediate-drain path and
// writes every record exactly once.
func TestHandleConvergesAcrossRetentionBudgetContinuations(t *testing.T) {
	const pages = 6
	const perPage = 5
	var requests atomic.Int64
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
			return
		}
		page := requests.Add(1)
		next := ""
		if page < pages {
			next = fmt.Sprintf(`,"@odata.nextLink":%q`, server.URL+"/next?page="+fmt.Sprint(page+1))
		}
		_, _ = w.Write([]byte(envelopePage(page, perPage, "", `"id"`, "", next)))
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newMicrosoftTestPlugin(t, server, rt)
	plugin.conf.Api = []string{"entra-users"}
	plugin.client.chunkRetentionRecords = perPage * 2

	passes := 0
	for {
		passes++
		if passes > pages+2 {
			t.Fatalf("Handle did not converge after %d passes", passes)
		}
		continuation, err := plugin.Handle(t.Context(), rt)
		if err != nil {
			t.Fatal(err)
		}
		if continuation.String() != "immediate" {
			break
		}
	}
	if passes < 2 {
		t.Fatalf("budget never tripped across Handle passes: passes=%d", passes)
	}
	if got := requests.Load(); got != pages {
		t.Fatalf("source requests=%d, want exactly %d", got, pages)
	}
	entries := rt.Entries()
	if len(entries) != pages*perPage {
		t.Fatalf("entries=%d, want %d", len(entries), pages*perPage)
	}
	seen := map[string]int{}
	for _, item := range entries {
		seen[string(item.Data)]++
	}
	if len(seen) != pages*perPage {
		t.Fatalf("distinct entries=%d, want %d", len(seen), pages*perPage)
	}
	for payload, count := range seen {
		if count != 1 {
			t.Fatalf("entry %s written %d times", payload, count)
		}
	}
	// The window completed, so nothing may be left pending.
	state, err := loadState(rt, "microsoft/entra-users")
	if err != nil {
		t.Fatal(err)
	}
	if state.Continuation != nil {
		t.Fatalf("converged poll retained a continuation: %+v", state.Continuation)
	}
}

func envelopePage(page int64, perPage int, filler, idField, extra, next string) string {
	return `{"value":[` + jsonRecords(page, perPage, filler, idField, extra) + `]` + next + `}`
}

func jsonRecords(page int64, perPage int, filler, idField, extra string) string {
	parts := make([]string, 0, perPage)
	for i := 0; i < perPage; i++ {
		fields := fmt.Sprintf(`%s:"p%d-r%d"`, idField, page, i)
		if extra != "" {
			fields += "," + extra
		}
		if filler != "" {
			fields += fmt.Sprintf(`,"blob":%q`, filler)
		}
		parts = append(parts, "{"+fields+"}")
	}
	return strings.Join(parts, ",")
}

// This plugin issues reads only. Pin every endpoint for which Microsoft exposes
// only a write-capable application permission so a future read-only permission
// can replace it deliberately rather than silently changing provisioning.
func TestWriteCapablePermissionsAreDocumentedExceptions(t *testing.T) {
	mandatoryWriteScopes := map[string]string{
		"defender-actions":        "Machine.ReadWrite.All",
		"defender-alerts":         "Alert.ReadWrite.All",
		"defender-indicators":     "Ti.ReadWrite.All",
		"defender-investigations": "Alert.ReadWrite.All",
	}
	for name, dataset := range datasets {
		permission := dataset.Permission
		if permission == "" {
			t.Errorf("selector %q declares no permission", name)
			continue
		}
		writeCapable := strings.Contains(permission, "ReadWrite") || strings.Contains(permission, ".Write")
		want, pinned := mandatoryWriteScopes[name]
		switch {
		case writeCapable && !pinned:
			t.Errorf("read-only selector %q requires write-capable permission %q", name, permission)
		case !writeCapable && pinned:
			t.Errorf("selector %q no longer needs the pinned write scope %q; drop it from the exception set", name, want)
		case writeCapable && permission != want:
			t.Errorf("selector %q permission=%q, want pinned exception %q", name, permission, want)
		}
		if method := dataset.Method(); method != http.MethodGet && method != http.MethodPost {
			t.Errorf("selector %q uses non-read method %q", name, method)
		}
		if dataset.Method() == http.MethodPost {
			switch dataset.Kind {
			case KindResourceGraph, KindPolicyStates, KindAdvancedHunting:
			default:
				t.Errorf("selector %q posts with kind %q, which is not a documented read-query kind", name, dataset.Kind)
			}
		}
	}
	// Each exception has to carry its reason in the catalog, so the elevated
	// grant is a recorded decision rather than an unremarked default.
	catalog, err := os.ReadFile("catalog_production.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(catalog), "publish only write-capable application permissions") {
		t.Fatal("catalog does not record why a write-capable permission is unavoidable")
	}
}

// densePageBody builds one page whose named array holds elements valid elements
// followed by text no JSON decoder can parse.
//
// The trailing garbage is what makes the density regressions non-vacuous. A
// decoder that reads the page whole before any bound applies -- which is what
// unmarshalling into a []json.RawMessage did -- fails on the garbage, so the
// error names a JSON syntax problem. One that streams and charges every element
// against the page bound stops at the bound and never reaches the garbage, so
// the error names the bound. The assertion is on which of the two comes back.
func densePageBody(array string, elements int, tail string) string {
	return `{"` + array + `":[` + strings.Repeat("{},", elements) + tail + "]}"
}

// An element-dense page is rejected before its elements are retained, with the
// cursor that produced it untouched. maximumAPIResponseBody bounds a page in
// bytes and the retention budget bounds a chunk between pages, but
// `[{},{},...]` reaches millions of elements inside the byte bound, and every
// one of them used to become a slice header, a FetchedRecord and a synthetic
// identity digest before the record budget was next consulted.
func TestStreamedPageRejectsElementDensityBeforeRetention(t *testing.T) {
	const pageBound = 32
	for _, tc := range []struct{ selector, array string }{
		{"entra-users", "value"},
		{"azure-activity", "value"},
		{"azure-resource-inventory", "data"},
		{"fabric-activity", "activityEventEntities"},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/token") {
					fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
					return
				}
				requests.Add(1)
				fmt.Fprint(w, densePageBody(tc.array, pageBound+1, "!!!not-json!!!"))
			}))
			defer server.Close()

			client, err := NewClient(testClientConfig(t, server.URL), server.Client())
			if err != nil {
				t.Fatal(err)
			}
			client.limiter = rate.NewLimiter(rate.Inf, 1)
			client.fetchDeadline = time.Minute
			client.pageRetentionRecords = pageBound

			start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
			chunk, err := client.FetchChunk(t.Context(), datasets[tc.selector], start, start.Add(time.Hour),
				"33333333-3333-3333-3333-333333333333", nil)
			if err == nil {
				t.Fatal("an element-dense page was accepted")
			}
			if !strings.Contains(err.Error(), "page bound") {
				t.Fatalf("error=%v, want the page bound: the page was decoded whole before the bound applied", err)
			}
			if len(chunk.Records) != 0 || chunk.Continuation != nil || chunk.Complete {
				t.Fatalf("a rejected page was partially retained: %+v", chunk)
			}
			if got := requests.Load(); got != 1 {
				t.Fatalf("source requests=%d, want exactly 1: a rejected page must not advance a cursor", got)
			}
		})
	}
}

// The per-page bound has to leave room for every page the source legitimately
// publishes, and the overshoot it permits has to stay well inside the per-chunk
// record budget the retention guard exists to hold.
func TestPageElementBoundAdmitsEveryLegitimatePageAndBoundsOvershoot(t *testing.T) {
	if maximumEnvelopePageRecords <= defaultPageSize*10 {
		t.Fatalf("page bound %d is too close to the %d-record page size the plugin itself requests",
			maximumEnvelopePageRecords, defaultPageSize)
	}
	// Graph caps $top at 999 and the ARM, Resource Graph and Fabric pages this
	// plugin reads are of the same order, so the bound must sit far above them.
	if maximumEnvelopePageRecords < 10000 {
		t.Fatalf("page bound %d is below the largest page a Microsoft endpoint publishes", maximumEnvelopePageRecords)
	}
	// One page of overshoot may not double the per-chunk record budget.
	if maximumEnvelopePageRecords*2 > maximumChunkRecords {
		t.Fatalf("page bound %d lets one page overshoot the %d-record chunk budget by more than half",
			maximumEnvelopePageRecords, maximumChunkRecords)
	}
	client, err := NewClient(testClientConfig(t, "https://graph.microsoft.com"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if client.pageRetentionRecords != maximumEnvelopePageRecords {
		t.Fatalf("client page bound=%d, want %d", client.pageRetentionRecords, maximumEnvelopePageRecords)
	}
	// A zero or negative override falls back to the production bound rather than
	// disabling the guard.
	for _, override := range []int{0, -1} {
		client.pageRetentionRecords = override
		if got := client.pageRecordBound(); got != maximumEnvelopePageRecords {
			t.Fatalf("override %d resolved to %d, want the production bound", override, got)
		}
	}
}

// admitPageElement is the only place a streamed element is charged, so both
// retention ceilings it enforces are pinned directly. The per-chunk budget
// itself stays a between-pages guard that may be overshot by one page; these
// are the ceilings that overshoot may not cross.
func TestAdmitPageElementEnforcesBothRetentionCeilings(t *testing.T) {
	client, err := NewClient(testClientConfig(t, "https://graph.microsoft.com"), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.chunkRetentionBytes, client.chunkRetentionRecords = 1024, 4
	client.pageRetentionRecords = 2
	dataset := datasets["entra-users"]

	if err := client.admitPageElement(&FetchChunk{}, dataset, 1, 16); err != nil {
		t.Fatalf("an element inside every bound was refused: %v", err)
	}
	if err := client.admitPageElement(&FetchChunk{}, dataset, 2, 16); err != nil {
		t.Fatalf("the element at the page bound was refused: %v", err)
	}
	if err := client.admitPageElement(&FetchChunk{}, dataset, 3, 16); err == nil ||
		!strings.Contains(err.Error(), "page bound") {
		t.Fatalf("the element past the page bound was admitted: %v", err)
	}

	byteCeiling := 1024 + maximumAPIResponseBody
	if err := client.admitPageElement(&FetchChunk{retained: byteCeiling - 16}, dataset, 1, 16); err != nil {
		t.Fatalf("an element reaching exactly the byte ceiling was refused: %v", err)
	}
	if err := client.admitPageElement(&FetchChunk{retained: byteCeiling - 15}, dataset, 1, 16); err == nil ||
		!strings.Contains(err.Error(), "byte retention ceiling") {
		t.Fatalf("an element past the byte ceiling was admitted: %v", err)
	}

	recordCeiling := 4 + 2
	if err := client.admitPageElement(&FetchChunk{Records: make([]FetchedRecord, recordCeiling-1)}, dataset, 1, 16); err != nil {
		t.Fatalf("an element below the record ceiling was refused: %v", err)
	}
	if err := client.admitPageElement(&FetchChunk{Records: make([]FetchedRecord, recordCeiling)}, dataset, 1, 16); err == nil ||
		!strings.Contains(err.Error(), "record retention ceiling") {
		t.Fatalf("an element at the record ceiling was admitted: %v", err)
	}
}

// Streaming replaced a fixed envelope struct, so every envelope-shape judgement
// that struct made has to still hold: a missing or null array is not an empty
// page, a non-array member is a decode failure, trailing data is rejected, and
// the two continuation members keep their precedence. A repeated array member is
// newly rejected rather than resolved last-wins, because last-wins let one
// response carry two value arrays whose elements both reached the record loop.
func TestStreamedEnvelopePreservesEnvelopeAndContinuationSemantics(t *testing.T) {
	dataset := datasets["entra-users"]
	client, err := NewClient(testClientConfig(t, "https://graph.microsoft.com"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"missing-array", `{"@odata.nextLink":"https://graph.microsoft.com/next"}`, "missing the value array"},
		{"null-array", `{"value":null}`, "missing the value array"},
		{"object-array", `{"value":{"id":"one"}}`, "is not an array"},
		{"not-an-object", `[{"id":"one"}]`, "not a JSON object"},
		{"trailing-data", `{"value":[]} {"value":[]}`, "trailing data"},
		{"repeated-array", `{"value":[{"id":"one"}],"value":[{"id":"two"}]}`, "repeats the value member"},
		{"non-string-next", `{"value":[],"@odata.nextLink":42}`, "is not a string"},
		{"invalid-element", `{"value":[{]}`, "invalid character"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chunk := FetchChunk{}
			_, err := client.decodeEnvelopePage(&chunk, dataset, []byte(tc.body), "https://graph.microsoft.com/first", 1)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want one naming %q", err, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"empty-array-completes", `{"value":[]}`, ""},
		{"odata-next", `{"value":[],"@odata.nextLink":"https://graph.microsoft.com/a"}`, "https://graph.microsoft.com/a"},
		{"plain-next", `{"value":[],"nextLink":"https://graph.microsoft.com/b"}`, "https://graph.microsoft.com/b"},
		// nextLink wins over @odata.nextLink exactly as the struct field order did.
		{"next-precedence", `{"value":[],"nextLink":"https://graph.microsoft.com/c","@odata.nextLink":"https://graph.microsoft.com/d"}`, "https://graph.microsoft.com/c"},
		{"null-next-completes", `{"value":[],"@odata.nextLink":null}`, ""},
		{"unknown-members-skipped", `{"@odata.context":"ctx","value":[],"extra":{"nested":[1,2,3]}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chunk := FetchChunk{}
			next, err := client.decodeEnvelopePage(&chunk, dataset, []byte(tc.body), "https://graph.microsoft.com/first", 1)
			if err != nil {
				t.Fatalf("a legitimate envelope was refused: %v", err)
			}
			if next != tc.want {
				t.Fatalf("continuation=%q, want %q", next, tc.want)
			}
			if len(chunk.Records) != 0 {
				t.Fatalf("an empty page retained %d records", len(chunk.Records))
			}
		})
	}

	// Elements still arrive in source order with one-based ordinals, which is
	// what the malformed-record diagnostics report.
	chunk := FetchChunk{}
	next, err := client.decodeEnvelopePage(&chunk, dataset,
		[]byte(`{"value":[{"id":"first"},"not-an-object",{"id":"third"}]}`), "https://graph.microsoft.com/first", 7)
	if err != nil || next != "" {
		t.Fatalf("next=%q error=%v", next, err)
	}
	if len(chunk.Records) != 2 || chunk.Records[0].ID != "first" || chunk.Records[1].ID != "third" {
		t.Fatalf("records=%+v, want the two objects in source order", chunk.Records)
	}
	if chunk.Malformed != 1 || len(chunk.Diagnostics) != 1 ||
		chunk.Diagnostics[0].Page != 7 || chunk.Diagnostics[0].Ordinal != 2 {
		t.Fatalf("diagnostics=%+v malformed=%d, want page 7 ordinal 2", chunk.Diagnostics, chunk.Malformed)
	}
}

// Hunting asks for one record past its capacity so one extra proves the window
// overflowed. Streaming stops at that proof instead of decoding whatever a
// source that ignores `take` actually returned, and the overflow decision is
// unchanged: the window is halved and collection continues.
func TestHuntingStopsStreamingAtItsOverflowProbe(t *testing.T) {
	dense := strings.TrimSuffix(huntingOverflowResults(), "]") + ",!!!not-json!!!]"
	var overflowCalls, calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := huntingRequestQuery(t, r)
		calls.Add(1)
		// Only the full window overflows; each half answers with one record.
		if strings.Contains(query, "datetime(2026-10-20T00:00:00Z)") && strings.Contains(query, "datetime(2026-10-20T01:00:00Z)") {
			overflowCalls.Add(1)
			fmt.Fprintf(w, `{"results":%s}`, dense)
			return
		}
		fmt.Fprint(w, `{"results":[{"ReportId":"r","Timestamp":"2026-10-20T00:30:00Z","DeviceId":"device"}]}`)
	}))
	defer server.Close()

	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	dataset := datasets["defender-xdr-device-events"]
	client.tokens[dataset.Scope] = cachedToken{Value: "token", Expires: time.Now().Add(time.Hour)}
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	chunk, err := client.FetchChunk(t.Context(), dataset, start, start.Add(time.Hour), "", nil)
	if err != nil {
		t.Fatalf("a dense hunting response failed instead of overflowing: %v", err)
	}
	if overflowCalls.Load() != 1 {
		t.Fatalf("overflow responses=%d, want exactly 1", overflowCalls.Load())
	}
	if !chunk.Complete || chunk.Continuation != nil {
		t.Fatalf("complete=%t continuation=%+v, want the halved windows to finish", chunk.Complete, chunk.Continuation)
	}
	// Two halves, each delivering its single record.
	if len(chunk.Records) != 2 || calls.Load() != 3 {
		t.Fatalf("records=%d calls=%d, want 2 records from 3 queries", len(chunk.Records), calls.Load())
	}
}
