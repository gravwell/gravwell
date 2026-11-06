package microsoft

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"golang.org/x/time/rate"
)

func fullContinuationHistory(prefix string) []string {
	recent := make([]string, maximumContinuationPages)
	for i := range recent {
		recent[i] = continuationFingerprint(fmt.Sprintf("https://history.invalid/%s/%d", prefix, i))
	}
	return recent
}

func TestEnvelopePacingDeadlinePersistsSuccessfulPagesAndEventuallyCompletes(t *testing.T) {
	var calls int
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		next := ""
		if calls < 6 {
			next = fmt.Sprintf(`,"@odata.nextLink":%q`, server.URL+"/v1.0/users?page="+strconv.Itoa(calls+1))
		}
		fmt.Fprintf(w, `{"value":[{"id":%q}]%s}`, fmt.Sprintf("user-%d", calls), next)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Every(20*time.Millisecond), 1)
	client.fetchDeadline = 35 * time.Millisecond
	dataset := datasets["entra-users"]
	client.tokens[dataset.Scope] = cachedToken{Value: "token", Expires: time.Now().Add(time.Hour)}
	var continuation *FetchContinuation
	var records []FetchedRecord
	chunks := 0
	for ; chunks < 10; chunks++ {
		chunk, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", continuation)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, chunk.Records...)
		if chunk.Complete {
			break
		}
		if chunk.Continuation == nil {
			t.Fatal("paced chunk stopped without a continuation")
		}
		continuation = chunk.Continuation
	}
	if chunks == 0 || calls != 6 || len(records) != 6 {
		t.Fatalf("chunks=%d calls=%d records=%d", chunks+1, calls, len(records))
	}
	for i, record := range records {
		want := fmt.Sprintf("user-%d", i+1)
		if record.ID != want {
			t.Fatalf("record %d ID=%q want %q", i, record.ID, want)
		}
	}
}

func TestEnvelopeTerminalPageCompletesAtHistoryLimit(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		fmt.Fprint(w, `{"value":[{"id":"final"}]}`)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	prior := &FetchContinuation{Version: fetchContinuationVersion, Kind: KindODataGET, NextURL: server.URL + "/final", Recent: fullContinuationHistory("envelope")}
	chunk, err := client.FetchChunk(t.Context(), datasets["entra-users"], time.Time{}, time.Time{}, "", prior)
	if err != nil {
		t.Fatal(err)
	}
	if !chunk.Complete || len(chunk.Records) != 1 {
		t.Fatalf("chunk=%+v", chunk)
	}
}

func TestEnvelopeProgressesBeyondBoundedHistoryLimit(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		if r.URL.Path == "/next" {
			fmt.Fprint(w, `{"value":[{"id":"final"}]}`)
			return
		}
		fmt.Fprintf(w, `{"value":[{"id":"over-limit"}],"@odata.nextLink":%q}`, serverURL(r)+"/next")
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	prior := &FetchContinuation{Version: fetchContinuationVersion, Kind: KindODataGET, NextURL: server.URL + "/over-limit", Recent: fullContinuationHistory("envelope")}
	chunk, err := client.FetchChunk(t.Context(), datasets["entra-users"], time.Time{}, time.Time{}, "", prior)
	if err != nil || !chunk.Complete || len(chunk.Records) != 2 {
		t.Fatalf("chunk=%+v error=%v", chunk, err)
	}
}

func TestEnvelopeRepeatedCurrentPageFailsBeforeRequest(t *testing.T) {
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"value":[]}`)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	current := server.URL + "/repeat"
	prior := &FetchContinuation{Version: fetchContinuationVersion, Kind: KindODataGET, NextURL: current, Recent: []string{continuationFingerprint(current)}}
	if _, err := client.FetchChunk(t.Context(), datasets["entra-users"], time.Time{}, time.Time{}, "", prior); err == nil || !strings.Contains(err.Error(), "repeated continuation") {
		t.Fatalf("error=%v", err)
	}
	if calls != 0 {
		t.Fatalf("repeated page dispatched %d requests", calls)
	}
}

func TestRestartAtHistoryLimitCompletesAndPublishesWindow(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	start, end := now.Add(-time.Hour), now.Add(-sourceConsistencyDelay)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		fmt.Fprint(w, `{"value":[{"id":"final","activityDateTime":"2026-10-07T11:30:00Z"}]}`)
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	state := streamState{WindowStart: start, WindowEnd: end, Continuation: &FetchContinuation{Version: fetchContinuationVersion, Kind: KindODataGET, NextURL: server.URL + "/final", Recent: fullContinuationHistory("restart")}}
	if err := saveState(rt, "microsoft/entra-directory-audits", state); err != nil {
		t.Fatal(err)
	}
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"entra-directory-audits"}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	watermark, err := rt.GetTime("microsoft/entra-directory-audits/timestamp")
	if err != nil || !watermark.Equal(end) || len(rt.Entries()) != 1 {
		t.Fatalf("watermark=%s error=%v entries=%d", watermark, err, len(rt.Entries()))
	}
}

func TestResourceGraphTerminalAndRepeatedCursorsAtHistoryLimit(t *testing.T) {
	var skipToken string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		if skipToken == "" {
			fmt.Fprint(w, `{"data":[{"id":"final"}],"resultTruncated":false}`)
			return
		}
		fmt.Fprintf(w, `{"data":[],"skipToken":%q,"resultTruncated":true}`, skipToken)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	dataset := datasets["azure-resource-inventory"]
	terminal := &FetchContinuation{Version: fetchContinuationVersion, Kind: KindResourceGraph, SkipToken: "resume", Recent: fullContinuationHistory("resource-graph")}
	chunk, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", terminal)
	if err != nil || !chunk.Complete || len(chunk.Records) != 1 {
		t.Fatalf("terminal chunk=%+v error=%v", chunk, err)
	}
	skipToken = "repeat"
	repeated := &FetchContinuation{Version: fetchContinuationVersion, Kind: KindResourceGraph, SkipToken: "resume", Recent: []string{continuationFingerprint(skipToken)}}
	if _, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", repeated); err == nil || !strings.Contains(err.Error(), "repeated skip token") {
		t.Fatalf("repeat error=%v", err)
	}
}

func TestFabricTerminalCursorCompletesAtHistoryLimit(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		fmt.Fprint(w, `{"activityEventEntities":[]}`)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	prior := &FetchContinuation{Version: fetchContinuationVersion, Kind: KindFabricActivity, NextURL: server.URL + "/fabric-final", FabricCursor: start, FabricDayEnd: end, Recent: fullContinuationHistory("fabric")}
	chunk, err := client.FetchChunk(t.Context(), datasets["fabric-activity"], start, end, "", prior)
	if err != nil || !chunk.Complete {
		t.Fatalf("terminal chunk=%+v error=%v", chunk, err)
	}
}

func TestFabricContinuationRollsBoundedHistoryAndDetectsRepeat(t *testing.T) {
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		calls++
		if r.URL.Path == "/first" {
			fmt.Fprintf(w, `{"activityEventEntities":[],"continuationUri":%q}`, serverURL(r)+"/second")
			return
		}
		fmt.Fprint(w, `{"activityEventEntities":[]}`)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	prior := &FetchContinuation{Version: fetchContinuationVersion, Kind: KindFabricActivity, NextURL: server.URL + "/first", FabricCursor: start, FabricDayEnd: end, Recent: fullContinuationHistory("fabric-roll")}
	chunk, err := client.FetchChunk(t.Context(), datasets["fabric-activity"], start, end, "", prior)
	if err != nil || !chunk.Complete || calls != 2 {
		t.Fatalf("rolling chunk=%+v calls=%d error=%v", chunk, calls, err)
	}
	repeatedURL := server.URL + "/first"
	repeated := &FetchContinuation{Version: fetchContinuationVersion, Kind: KindFabricActivity, NextURL: repeatedURL, FabricCursor: start, FabricDayEnd: end, Recent: []string{continuationFingerprint(repeatedURL)}}
	if _, err := client.FetchChunk(t.Context(), datasets["fabric-activity"], start, end, "", repeated); err == nil || !strings.Contains(err.Error(), "repeated continuation") {
		t.Fatalf("repeat error=%v", err)
	}
	if calls != 2 {
		t.Fatalf("repeated Fabric cursor dispatched a request: calls=%d", calls)
	}
}

func TestParentAndChildTerminalCursorsCompleteAtHistoryLimit(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		fmt.Fprint(w, `{"value":[]}`)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	dataset := datasets["defender-users"]
	parent := &FetchContinuation{Version: fetchContinuationVersion, Kind: KindParentChild, ParentNextURL: server.URL + "/parent-final", ParentRecent: fullContinuationHistory("parent")}
	chunk, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", parent)
	if err != nil || !chunk.Complete {
		t.Fatalf("parent terminal chunk=%+v error=%v", chunk, err)
	}
	child := &FetchContinuation{Version: fetchContinuationVersion, Kind: KindParentChild, ParentIDs: []string{"parent"}, ChildParentID: "parent", ChildNextURL: server.URL + "/child-final", ChildRecent: fullContinuationHistory("child")}
	chunk, err = client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", child)
	if err != nil || !chunk.Complete {
		t.Fatalf("child terminal chunk=%+v error=%v", chunk, err)
	}
}

// defaultMaxPages bounds one Handle and the continuation history remembers the
// newest maximumContinuationHistory cursors. Neither bounds the window: a
// source that cycles through more distinct URLs or skip tokens than the history
// holds publishes a cursor this plugin has never seen on every page, so the
// per-Handle bound reset on every pass and the window never ended. The page
// count is now cumulative and persisted with the cursor, so the window
// terminates across passes and the truncation is reported rather than silent.
func TestNonManagementPaginationBoundsPagesAcrossPasses(t *testing.T) {
	const bound = 150
	var pages atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		page := pages.Add(1)
		// Every page names a cursor this window has never seen, so neither the
		// repeated-cursor guard nor the bounded history ever fires.
		fmt.Fprintf(w, `{"value":[{"id":%q}],"@odata.nextLink":%q}`,
			fmt.Sprintf("user-%d", page), serverURL(r)+"/page/"+strconv.FormatInt(page+1, 10))
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	p.client.continuationPageLimit = bound

	bounded := false
	for pass := 1; pass <= 4 && !bounded; pass++ {
		if _, err := p.Handle(t.Context(), rt); err != nil {
			t.Fatalf("pass %d error=%v", pass, err)
		}
		state, err := loadState(rt, "microsoft/entra-users")
		if err != nil {
			t.Fatal(err)
		}
		bounded = state.Continuation == nil
	}
	if !bounded {
		t.Fatalf("read %d pages across four passes without reaching the %d-page cumulative bound", pages.Load(), bound)
	}
	if got := pages.Load(); got > bound {
		t.Fatalf("read %d pages, want at most the cumulative bound of %d", got, bound)
	}
	if got := rt.logsContaining("ERROR", "cumulative page bound"); len(got) != 1 {
		t.Fatalf("page-bound reports=%d, want exactly 1 (%+v)", len(got), got)
	}
	if got := rt.warnStateContaining("page bound"); len(got) == 0 {
		t.Fatal("no operator-visible warning state was published for a truncated window")
	}
	// The cursor is not advanced in silence: the window it could not finish is
	// closed against its own end, and every record it did read was delivered.
	watermark, err := rt.GetTime("microsoft/entra-users/timestamp")
	if err != nil || watermark.IsZero() {
		t.Fatalf("watermark=%s error=%v, want the truncated window to publish its boundary", watermark, err)
	}
	if got := int64(len(rt.Entries())); got != pages.Load() {
		t.Fatalf("entries=%d, want one for each of the %d pages read", got, pages.Load())
	}
}

// Older cursor documents carry no cumulative page count. They default to zero
// rather than becoming unreadable, and a count no growth path could have
// produced is refused like every other structural bound.
func TestFetchContinuationPageCountValidatesAndDefaults(t *testing.T) {
	older := FetchContinuation{}
	document := fmt.Sprintf(`{"version":%d,"kind":%q,"next_url":"https://example.invalid/next"}`, fetchContinuationVersion, string(KindODataGET))
	if err := json.Unmarshal([]byte(document), &older); err != nil {
		t.Fatal(err)
	}
	if older.Pages != 0 {
		t.Fatalf("older document page count=%d, want the zero default", older.Pages)
	}
	if err := older.validate(); err != nil {
		t.Fatalf("older document rejected: %v", err)
	}
	resumed, err := continuationFor(datasets["entra-users"], &FetchContinuation{Version: fetchContinuationVersion, Kind: KindODataGET, Pages: 7})
	if err != nil || resumed.Pages != 7 {
		t.Fatalf("resumed page count=%d error=%v, want 7", resumed.Pages, err)
	}
	for name, pages := range map[string]int{"negative": -1, "over-bound": maximumContinuationPages + 1} {
		t.Run(name, func(t *testing.T) {
			invalid := FetchContinuation{Version: fetchContinuationVersion, Kind: KindODataGET, Pages: pages}
			if err := invalid.validate(); err == nil || !errors.Is(err, errIncompatibleContinuation) {
				t.Fatalf("page count %d accepted: %v", pages, err)
			}
		})
	}
}

// parentChildFanOutServer answers the defender-users shape: a paginated device
// list, and one terminal page of logon users per device whose account name
// names the device it came from.
func parentChildFanOutServer(parentPages, devicesPerPage int, parentCalls, childCalls *atomic.Int64) *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
		case strings.HasSuffix(r.URL.Path, "/logonusers"):
			childCalls.Add(1)
			segments := strings.Split(strings.TrimSuffix(r.URL.Path, "/logonusers"), "/")
			fmt.Fprintf(w, `{"value":[{"accountName":%q}]}`, "user-on-"+segments[len(segments)-1])
		case strings.HasSuffix(r.URL.Path, "/api/machines"):
			page := 1
			if raw := r.URL.Query().Get("page"); raw != "" {
				parsed, err := strconv.Atoi(raw)
				if err != nil {
					http.Error(w, "unreadable page", http.StatusBadRequest)
					return
				}
				page = parsed
			}
			parentCalls.Add(1)
			devices := make([]string, 0, devicesPerPage)
			for i := 0; i < devicesPerPage; i++ {
				devices = append(devices, fmt.Sprintf(`{"id":"machine-%d"}`, (page-1)*devicesPerPage+i))
			}
			next := ""
			if page < parentPages {
				next = fmt.Sprintf(`,"@odata.nextLink":%q`, fmt.Sprintf("%s/api/machines?page=%d", serverURL(r), page+1))
			}
			fmt.Fprintf(w, `{"value":[%s]%s}`, strings.Join(devices, ","), next)
		default:
			http.NotFound(w, r)
		}
	}))
}

// deliveredAccountNames returns the accountName of every entry written so far.
func deliveredAccountNames(t *testing.T, rt *microsoftTestRuntime) map[string]int {
	t.Helper()
	names := make(map[string]int)
	for _, ent := range rt.Entries() {
		var record struct {
			AccountName string `json:"accountName"`
		}
		if err := json.Unmarshal(ent.Data, &record); err != nil {
			t.Fatalf("decode delivered entry %q: %v", ent.Data, err)
		}
		if record.AccountName == "" {
			t.Fatalf("delivered entry carries no account name: %s", ent.Data)
		}
		names[record.AccountName]++
	}
	return names
}

// A parent-child fan-out issues one source request per parent, so charging the
// cumulative window page budget once per child made the ordinary width of a
// tenant the thing that exhausted it. defender-users spends at least one page
// per device, so a tenant with more devices than the 4096-page budget reached
// PageBudgetExhausted partway down its device list, closed the window, and
// recollected the same prefix on the next poll without ever reaching the
// devices behind it.
//
// The cumulative budget counts parent-list pages only. A fan-out far wider
// than that budget now completes across Handle passes, with every child
// delivered exactly once and no truncation reported.
func TestParentChildFanOutCompletesBeyondTheCumulativePageBound(t *testing.T) {
	const (
		parentPages    = 3
		devicesPerPage = 50
		// One more than the parent-list pages this fan-out reads, and far fewer
		// than its devices: the window budget must bound the former only.
		pageBound = parentPages + 1
		devices   = parentPages * devicesPerPage
	)
	if devices <= pageBound {
		t.Fatalf("fan-out of %d devices does not exceed the %d-page window bound it has to survive", devices, pageBound)
	}
	var parentCalls, childCalls atomic.Int64
	server := parentChildFanOutServer(parentPages, devicesPerPage, &parentCalls, &childCalls)
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newSelectorTestPlugin(t, server, "defender-users")
	plugin.client.continuationPageLimit = pageBound
	key := stateKey("microsoft", "defender-users", "")

	passes, highWaterPages := 0, 0
	for passes < 8 {
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("pass %d error=%v", passes+1, err)
		}
		passes++
		state, err := loadState(rt, key)
		if err != nil {
			t.Fatal(err)
		}
		if state.Continuation == nil {
			break
		}
		if state.Continuation.Pages > highWaterPages {
			highWaterPages = state.Continuation.Pages
		}
	}

	if state, err := loadState(rt, key); err != nil {
		t.Fatal(err)
	} else if state.Continuation != nil {
		t.Fatalf("fan-out did not finish in %d passes: parents=%d children=%d, still at parent %d of %d after %d cumulative pages",
			passes, parentCalls.Load(), childCalls.Load(),
			state.Continuation.ParentIndex, len(state.Continuation.ParentIDs), state.Continuation.Pages)
	}
	if got := parentCalls.Load(); got != parentPages {
		t.Fatalf("parent-list requests=%d, want %d", got, parentPages)
	}
	if got := childCalls.Load(); got != devices {
		t.Fatalf("child requests=%d, want one per device (%d)", got, devices)
	}
	names := deliveredAccountNames(t, rt)
	if len(names) != devices {
		t.Fatalf("distinct delivered children=%d, want %d", len(names), devices)
	}
	for device := 0; device < devices; device++ {
		want := fmt.Sprintf("user-on-machine-%d", device)
		if got := names[want]; got != 1 {
			t.Fatalf("child %q delivered %d times, want exactly once", want, got)
		}
	}
	if got := rt.logsContaining("ERROR", "cumulative page bound"); len(got) != 0 {
		t.Fatalf("a complete fan-out reported %d truncations: %+v", len(got), got)
	}
	if got := rt.warnStateContaining("page bound"); len(got) != 0 {
		t.Fatalf("a complete fan-out published %d page-bound warnings: %+v", len(got), got)
	}
	watermark, err := rt.GetTime("microsoft/defender-users/timestamp")
	if err != nil || watermark.IsZero() {
		t.Fatalf("watermark=%s error=%v, want the completed window published", watermark, err)
	}
	// Non-vacuity: the fan-out really did outlive a single Handle, and the
	// persisted cursor really did carry a cumulative page count that stayed
	// inside the window bound the fan-out itself used to exhaust.
	if passes < 2 {
		t.Fatalf("fan-out completed in %d pass; the regression has to span Handle passes", passes)
	}
	if highWaterPages < 1 || highWaterPages > pageBound {
		t.Fatalf("persisted cumulative page count high-water=%d, want between 1 and the %d-page bound", highWaterPages, pageBound)
	}
}

// Counting parent-list pages only leaves one parent able to publish an endless
// sequence of distinct child cursors: the per-Handle page bound and the bounded
// child history are both re-armed by every pass. The per-parent child page
// counter is what refuses that, and the loss it takes is bounded to the
// offending parent's child list and reported, rather than the parent being
// skipped in silence or the whole window being discarded.
func TestParentChildSingleParentChildPageBoundIsReportedAndBounded(t *testing.T) {
	const childBound = 5
	var childCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
		case strings.HasSuffix(r.URL.Path, "/logonusers"):
			// Every child page names a cursor this parent has never served, so
			// neither the repeated-cursor guard nor the bounded child history
			// ever fires: only the per-parent counter can stop this.
			serial := childCalls.Add(1)
			fmt.Fprintf(w, `{"value":[{"accountName":%q}],"@odata.nextLink":%q}`,
				fmt.Sprintf("user-%d", serial),
				fmt.Sprintf("%s/api/machines/machine-1/logonusers?cursor=%d", serverURL(r), serial+1))
		case strings.HasSuffix(r.URL.Path, "/api/machines"):
			fmt.Fprint(w, `{"value":[{"id":"machine-1"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newSelectorTestPlugin(t, server, "defender-users")
	plugin.client.parentChildPageLimit = childBound
	key := stateKey("microsoft", "defender-users", "")

	passes := 0
	for passes < 4 {
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("pass %d error=%v", passes+1, err)
		}
		passes++
		state, err := loadState(rt, key)
		if err != nil {
			t.Fatal(err)
		}
		if state.Continuation == nil {
			break
		}
	}
	state, err := loadState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if state.Continuation != nil {
		t.Fatalf("one parent held the window open for %d passes and %d child pages, still resuming at parent %q child page %d",
			passes, childCalls.Load(), state.Continuation.ChildParentID, state.Continuation.ChildPages)
	}
	if got := childCalls.Load(); got != childBound {
		t.Fatalf("child pages read=%d, want exactly the %d-page per-parent bound", got, childBound)
	}
	if got := len(rt.Entries()); got != childBound {
		t.Fatalf("entries=%d, want the %d child pages this pass did read delivered", got, childBound)
	}
	reports := rt.logsContaining("ERROR", "per-parent child page bound")
	if len(reports) != 1 {
		t.Fatalf("bounded-loss reports=%d, want exactly 1 (%+v)", len(reports), reports)
	}
	if got := reports[0].Params["parents"]; got != "1" {
		t.Fatalf("reported parents=%q, want 1", got)
	}
	if got := reports[0].Params["first_parent"]; got != "machine-1" {
		t.Fatalf("reported first_parent=%q, want machine-1", got)
	}
	if got := reports[0].Params["child_bound"]; got != strconv.Itoa(childBound) {
		t.Fatalf("reported child_bound=%q, want %d", got, childBound)
	}
	// The per-parent bound is not the cumulative window bound and must not be
	// reported as one: this window was not truncated, its only parent was.
	if got := rt.logsContaining("ERROR", "cumulative page bound"); len(got) != 0 {
		t.Fatalf("per-parent truncation reported %d window truncations: %+v", len(got), got)
	}
	if got := rt.warnStateContaining("per-parent child page bound"); len(got) == 0 {
		t.Fatal("no operator-visible warning state was published for the truncated parent")
	}
	watermark, err := rt.GetTime("microsoft/defender-users/timestamp")
	if err != nil || watermark.IsZero() {
		t.Fatalf("watermark=%s error=%v, want the window closed against its own end", watermark, err)
	}
}

// The per-parent child-page bound used to close the whole window: it attached
// the cursor as a cumulative-bound truncation, so collect discarded every
// parent behind the offending one together with the parent-list cursor that
// would have reached them, advanced the watermark past the window anyway, and
// met the same parent again on the next window -- permanent loss of every later
// parent, repeated on every window, from one pathological parent.
//
// The loss is bounded to that parent now. Its child continuation, history and
// count are dropped, the fan-out advances, and the window completes with every
// later parent delivered exactly once, including the parents behind a parent-list
// page the truncated window never used to reach.
func TestParentChildBoundTruncatesOneParentAndDeliversTheRest(t *testing.T) {
	const childBound = 4
	goodParents := []string{"machine-good-1", "machine-good-2", "machine-good-3"}
	var badCalls, parentCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
		case strings.Contains(r.URL.Path, "/machine-bad/logonusers"):
			// Every page names a child cursor this parent has never served, so
			// only the per-parent counter can stop it.
			serial := badCalls.Add(1)
			fmt.Fprintf(w, `{"value":[{"accountName":%q}],"@odata.nextLink":%q}`,
				fmt.Sprintf("user-bad-%d", serial),
				fmt.Sprintf("%s/api/machines/machine-bad/logonusers?cursor=%d", serverURL(r), serial+1))
		case strings.HasSuffix(r.URL.Path, "/logonusers"):
			segments := strings.Split(strings.TrimSuffix(r.URL.Path, "/logonusers"), "/")
			fmt.Fprintf(w, `{"value":[{"accountName":%q}]}`, "user-on-"+segments[len(segments)-1])
		case strings.HasSuffix(r.URL.Path, "/api/machines"):
			parentCalls.Add(1)
			// The bad parent comes first, and the parents behind it are split
			// across a parent-list page the old bounded-loss path threw away
			// with the rest of the window.
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprint(w, `{"value":[{"id":"machine-good-2"},{"id":"machine-good-3"}]}`)
				return
			}
			fmt.Fprintf(w, `{"value":[{"id":"machine-bad"},{"id":"machine-good-1"}],"@odata.nextLink":%q}`,
				serverURL(r)+"/api/machines?page=2")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newSelectorTestPlugin(t, server, "defender-users")
	plugin.client.parentChildPageLimit = childBound
	key := stateKey("microsoft", "defender-users", "")

	passes := 0
	for passes < 4 {
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("pass %d error=%v", passes+1, err)
		}
		passes++
		state, err := loadState(rt, key)
		if err != nil {
			t.Fatal(err)
		}
		if state.Continuation == nil {
			break
		}
	}
	state, err := loadState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if state.Continuation != nil {
		t.Fatalf("fan-out did not finish in %d passes: resuming at parent %q child page %d of %d parents",
			passes, state.Continuation.ChildParentID, state.Continuation.ChildPages, len(state.Continuation.ParentIDs))
	}

	// The bad parent is truncated at its bound and never read again.
	if got := badCalls.Load(); got != childBound {
		t.Fatalf("bad parent supplied %d child pages, want exactly the %d-page per-parent bound", got, childBound)
	}
	// The parent-list cursor survived the truncation, so the parents behind the
	// first parent-list page were reached at all.
	if got := parentCalls.Load(); got != 2 {
		t.Fatalf("parent-list requests=%d, want both pages (2)", got)
	}
	names := deliveredAccountNames(t, rt)
	for _, parent := range goodParents {
		if got := names["user-on-"+parent]; got != 1 {
			t.Fatalf("child of %s delivered %d times, want exactly once", parent, got)
		}
	}
	// What the bad parent did supply before the bound is still delivered, once.
	for page := 1; page <= childBound; page++ {
		if got := names[fmt.Sprintf("user-bad-%d", page)]; got != 1 {
			t.Fatalf("truncated parent's child page %d delivered %d times, want exactly once", page, got)
		}
	}
	if got := len(names); got != childBound+len(goodParents) {
		t.Fatalf("distinct delivered children=%d, want %d", got, childBound+len(goodParents))
	}

	// The truncation is stated, exactly once, and names the parent it bounded.
	reports := rt.logsContaining("ERROR", "per-parent child page bound")
	if len(reports) != 1 {
		t.Fatalf("truncation reports=%d, want exactly 1 (%+v)", len(reports), reports)
	}
	if got := reports[0].Params["parents"]; got != "1" {
		t.Fatalf("reported parents=%q, want 1", got)
	}
	if got := reports[0].Params["first_parent"]; got != "machine-bad" {
		t.Fatalf("reported first_parent=%q, want machine-bad", got)
	}
	if got := rt.warnStateContaining("per-parent child page bound"); len(got) != 1 {
		t.Fatalf("operator-visible warnings=%d, want exactly 1 (%+v)", len(got), got)
	}
	// One parent's child list was truncated; the window itself was not, so it
	// must not be reported as a window truncation.
	if got := rt.logsContaining("ERROR", "cumulative page bound"); len(got) != 0 {
		t.Fatalf("a bounded parent reported %d window truncations: %+v", len(got), got)
	}
	if got := rt.logsContaining("ERROR", "selector poll failed"); len(got) != 0 {
		t.Fatalf("a bounded parent reported %d poll failures: %+v", len(got), got)
	}
	// The window completed against its own end rather than being abandoned.
	watermark, err := rt.GetTime("microsoft/defender-users/timestamp")
	if err != nil || watermark.IsZero() {
		t.Fatalf("watermark=%s error=%v, want the completed window published", watermark, err)
	}
}

// Bounding one parent must not have unbound the window. A parent-list that
// never stops publishing fresh pages still reaches the cumulative window page
// bound, and that bound still closes the window through the reported
// bounded-loss path: it is the per-parent bound that stops closing windows, not
// the cumulative one.
func TestParentChildCumulativeParentPageBoundStillClosesTheWindow(t *testing.T) {
	const pageBound = 3
	var parentCalls, childCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
		case strings.HasSuffix(r.URL.Path, "/logonusers"):
			childCalls.Add(1)
			segments := strings.Split(strings.TrimSuffix(r.URL.Path, "/logonusers"), "/")
			fmt.Fprintf(w, `{"value":[{"accountName":%q}]}`, "user-on-"+segments[len(segments)-1])
		case strings.HasSuffix(r.URL.Path, "/api/machines"):
			// One device per parent-list page, and a parent-list page this window
			// has never seen behind every one of them.
			serial := parentCalls.Add(1)
			fmt.Fprintf(w, `{"value":[{"id":%q}],"@odata.nextLink":%q}`,
				fmt.Sprintf("machine-%d", serial),
				fmt.Sprintf("%s/api/machines?page=%d", serverURL(r), serial+1))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newSelectorTestPlugin(t, server, "defender-users")
	plugin.client.continuationPageLimit = pageBound
	key := stateKey("microsoft", "defender-users", "")

	for pass := 0; pass < 4; pass++ {
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("pass %d error=%v", pass+1, err)
		}
		state, err := loadState(rt, key)
		if err != nil {
			t.Fatal(err)
		}
		if state.Continuation == nil {
			break
		}
	}
	state, err := loadState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if state.Continuation != nil {
		t.Fatalf("an endless parent list held the window open after %d parent-list pages", parentCalls.Load())
	}
	if got := parentCalls.Load(); got != pageBound {
		t.Fatalf("parent-list requests=%d, want exactly the %d-page cumulative bound", got, pageBound)
	}
	// Every parent the window did list was fanned out before it closed: the
	// cumulative bound counts parent-list pages, not children.
	if got := childCalls.Load(); got != pageBound {
		t.Fatalf("child requests=%d, want one per listed parent (%d)", got, pageBound)
	}
	reports := rt.logsContaining("ERROR", "cumulative page bound")
	if len(reports) != 1 {
		t.Fatalf("window-bound reports=%d, want exactly 1 (%+v)", len(reports), reports)
	}
	if got := reports[0].Params["pages"]; got != strconv.Itoa(pageBound) {
		t.Fatalf("reported pages=%q, want %d", got, pageBound)
	}
	// No parent exceeded its own child bound, so nothing was truncated per parent.
	if got := rt.logsContaining("ERROR", "per-parent child page bound"); len(got) != 0 {
		t.Fatalf("window truncation reported %d per-parent truncations: %+v", len(got), got)
	}
	if got := rt.warnStateContaining("cumulative page bound"); len(got) == 0 {
		t.Fatal("no operator-visible warning state was published for the truncated window")
	}
	watermark, err := rt.GetTime("microsoft/defender-users/timestamp")
	if err != nil || watermark.IsZero() {
		t.Fatalf("watermark=%s error=%v, want the truncated window closed against its own end", watermark, err)
	}
}

// The truncation has to be bounded in the durable cursor as well as in the
// chunk. A pass that spends its last page on the parent it truncates ends on
// that very iteration, so the cursor it persists is the one the next pass
// resumes: it must carry no child continuation, no child history and no child
// page count, and it must point at the next parent rather than at the truncated
// one. Charging the truncating iteration against the per-Handle page bound is
// what makes the pass end exactly there.
func TestParentChildTruncationPersistsACleanCursorAtTheNextParent(t *testing.T) {
	// One page short of the per-Handle bound, so the iteration that truncates
	// the parent is the last one this pass may run.
	const childBound = defaultMaxPages - 1
	var badCalls, goodCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
		case strings.Contains(r.URL.Path, "/machine-bad/logonusers"):
			serial := badCalls.Add(1)
			fmt.Fprintf(w, `{"value":[{"accountName":%q}],"@odata.nextLink":%q}`,
				fmt.Sprintf("user-bad-%d", serial),
				fmt.Sprintf("%s/api/machines/machine-bad/logonusers?cursor=%d", serverURL(r), serial+1))
		case strings.Contains(r.URL.Path, "/machine-good/logonusers"):
			goodCalls.Add(1)
			fmt.Fprint(w, `{"value":[{"accountName":"user-on-machine-good"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.parentChildPageLimit = childBound
	dataset := datasets["defender-users"]
	chunk, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", &FetchContinuation{
		Version: fetchContinuationVersion, Kind: KindParentChild,
		ParentIDs: []string{"machine-bad", "machine-good"}, ChildParentID: "machine-bad",
		ChildNextURL: server.URL + "/api/machines/machine-bad/logonusers?cursor=1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := badCalls.Load(); got != childBound {
		t.Fatalf("bad parent supplied %d child pages, want the %d-page bound", got, childBound)
	}
	if got := goodCalls.Load(); got != 0 {
		t.Fatalf("next parent was read %d times; this pass has to end on the truncating iteration", got)
	}
	if chunk.ChildBudgetExhausted != 1 || len(chunk.TruncatedParents) != 1 || chunk.TruncatedParents[0] != "machine-bad" {
		t.Fatalf("truncation report=%d parents=%v, want exactly machine-bad", chunk.ChildBudgetExhausted, chunk.TruncatedParents)
	}
	if chunk.PageBudgetExhausted || chunk.Complete {
		t.Fatalf("truncated parent reported window truncation=%t complete=%t; the window is neither", chunk.PageBudgetExhausted, chunk.Complete)
	}
	if got := len(chunk.Records); got != childBound {
		t.Fatalf("records=%d, want the %d pages this pass did read", got, childBound)
	}
	resume := chunk.Continuation
	if resume == nil {
		t.Fatal("a truncated parent left no cursor to resume the fan-out with")
	}
	if resume.ParentIndex != 1 || resume.ChildNextURL != "" || resume.ChildParentID != "" ||
		resume.ChildPages != 0 || len(resume.ChildRecent) != 0 {
		t.Fatalf("persisted cursor=%+v, want the child state cleared and the index on the next parent", resume)
	}
	if err := resume.validate(); err != nil {
		t.Fatalf("persisted cursor is not resumable: %v", err)
	}

	// The next pass collects the parent behind the truncated one and finishes.
	next, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", resume)
	if err != nil || !next.Complete {
		t.Fatalf("resumed chunk=%+v error=%v, want the fan-out completed", next, err)
	}
	if got := goodCalls.Load(); got != 1 {
		t.Fatalf("next parent child requests=%d, want exactly 1", got)
	}
	if got := badCalls.Load(); got != childBound {
		t.Fatalf("truncated parent was read again: %d pages total", got)
	}
	if next.ChildBudgetExhausted != 0 || len(next.Records) != 1 {
		t.Fatalf("resumed chunk truncations=%d records=%d, want 0 and 1", next.ChildBudgetExhausted, len(next.Records))
	}
}

// The per-parent counter is reset exactly when the fan-out advances to another
// parent. Two parents that each spend one page short of the bound both finish,
// where a counter carried across parents would have truncated the window on the
// second one.
func TestParentChildChildPageBudgetResetsOnEachParent(t *testing.T) {
	const (
		childBound    = 3
		pagesPerChild = childBound - 1
		parents       = 2
	)
	var childCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
		case strings.HasSuffix(r.URL.Path, "/logonusers"):
			childCalls.Add(1)
			segments := strings.Split(strings.TrimSuffix(r.URL.Path, "/logonusers"), "/")
			device := segments[len(segments)-1]
			page := 1
			if raw := r.URL.Query().Get("cursor"); raw != "" {
				parsed, err := strconv.Atoi(raw)
				if err != nil {
					http.Error(w, "unreadable cursor", http.StatusBadRequest)
					return
				}
				page = parsed
			}
			next := ""
			if page < pagesPerChild {
				next = fmt.Sprintf(`,"@odata.nextLink":%q`,
					fmt.Sprintf("%s/api/machines/%s/logonusers?cursor=%d", serverURL(r), device, page+1))
			}
			fmt.Fprintf(w, `{"value":[{"accountName":%q}]%s}`, fmt.Sprintf("user-on-%s-%d", device, page), next)
		case strings.HasSuffix(r.URL.Path, "/api/machines"):
			fmt.Fprint(w, `{"value":[{"id":"machine-1"},{"id":"machine-2"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newSelectorTestPlugin(t, server, "defender-users")
	plugin.client.parentChildPageLimit = childBound
	key := stateKey("microsoft", "defender-users", "")

	// Non-vacuity: the two parents together read more child pages than the
	// per-parent bound, so only a counter that resets per parent completes.
	if parents*pagesPerChild <= childBound {
		t.Fatalf("%d total child pages do not exceed the %d-page per-parent bound", parents*pagesPerChild, childBound)
	}
	for pass := 0; pass < 4; pass++ {
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("pass %d error=%v", pass+1, err)
		}
		state, err := loadState(rt, key)
		if err != nil {
			t.Fatal(err)
		}
		if state.Continuation == nil {
			break
		}
	}
	if got := childCalls.Load(); got != parents*pagesPerChild {
		t.Fatalf("child pages read=%d, want %d", got, parents*pagesPerChild)
	}
	names := deliveredAccountNames(t, rt)
	for _, device := range []string{"machine-1", "machine-2"} {
		for page := 1; page <= pagesPerChild; page++ {
			want := fmt.Sprintf("user-on-%s-%d", device, page)
			if got := names[want]; got != 1 {
				t.Fatalf("child %q delivered %d times, want exactly once", want, got)
			}
		}
	}
	if got := rt.logsContaining("ERROR", "cumulative page bound"); len(got) != 0 {
		t.Fatalf("a fan-out within the per-parent bound reported %d truncations: %+v", len(got), got)
	}
}

// The per-parent child page count is durable cursor state: it has to survive a
// restart, be refused when it holds a value no growth path could have produced,
// and default to zero for a cursor written before the field existed.
func TestChildPageCountPersistsValidatesAndDefaults(t *testing.T) {
	dataset := datasets["defender-users"]
	older := FetchContinuation{}
	document := fmt.Sprintf(`{"version":%d,"kind":%q,"parent_ids":["machine-1"],"child_parent_id":"machine-1","child_next_url":"https://example.invalid/child"}`,
		fetchContinuationVersion, string(KindParentChild))
	if err := json.Unmarshal([]byte(document), &older); err != nil {
		t.Fatal(err)
	}
	if older.ChildPages != 0 {
		t.Fatalf("older document child page count=%d, want the zero default", older.ChildPages)
	}
	if err := older.validate(); err != nil {
		t.Fatalf("older document rejected: %v", err)
	}

	stored := &FetchContinuation{
		Version: fetchContinuationVersion, Kind: KindParentChild,
		ParentIDs: []string{"machine-1"}, ChildParentID: "machine-1",
		ChildNextURL: "https://example.invalid/child", ChildPages: 11,
	}
	rt := newMicrosoftTestRuntime(t)
	key := stateKey("microsoft", "defender-users", "")
	if err := saveState(rt, key, streamState{Continuation: stored}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Continuation == nil || reloaded.Continuation.ChildPages != 11 {
		t.Fatalf("reloaded cursor=%+v, want a persisted child page count of 11", reloaded.Continuation)
	}
	resumed, err := continuationFor(dataset, reloaded.Continuation)
	if err != nil || resumed.ChildPages != 11 {
		t.Fatalf("resumed child page count=%d error=%v, want 11", resumed.ChildPages, err)
	}
	for name, pages := range map[string]int{"negative": -1, "over-bound": maximumParentChildPages + 1} {
		t.Run(name, func(t *testing.T) {
			invalid := *stored
			invalid.ChildPages = pages
			if _, err := continuationFor(dataset, &invalid); err == nil || !errors.Is(err, errIncompatibleContinuation) {
				t.Fatalf("child page count %d accepted: %v", pages, err)
			}
		})
	}
}

// Both cumulative counters are charged for pages a window actually read. Every
// paginated shape used to charge its counter immediately before the request
// that reads the page -- the ordinary envelope, Resource Graph, the Fabric day
// walk, the parent list, Advanced Hunting, and the child side of a fan-out --
// so a pass that yielded on pacing or on the request budget persisted a cursor
// with an unread page already spent against the 4096-page window bound or the
// 1024-page per-parent bound. A paced tenant, a short fetch deadline or a busy
// poll deferred often enough walked either bound up without reading the pages
// it charged, and the bound then closed the window or truncated the parent over
// history no pass had ever seen.
const (
	// The shapes below resume with a seeded count rather than from a fresh
	// cursor: a counter charged at the wrong moment shows up as a charge for a
	// page the window never read, and a counter reset at the wrong moment shows
	// up as the loss of the history the cursor already carried.
	seededWindowPages      = 5
	seededParentChildPages = 3
)

var (
	pageChargeWindowStart = time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	pageChargeWindowEnd   = pageChargeWindowStart.Add(time.Hour)
)

// paginationShape is one paginated fetch shape, the cursor it resumes, and what
// one successful page of it costs.
type paginationShape struct {
	name    string
	dataset Dataset
	// handler answers this shape's source requests and counts the ones that
	// charge the counter under test.
	handler func(calls *atomic.Int64) http.HandlerFunc
	cursor  func(base string) *FetchContinuation
	// recordsPerPage is what one page contributes to the chunk. A parent-list
	// page contributes parent identities to the cursor instead.
	recordsPerPage int
	// childBudget marks the shape whose request site charges the per-parent
	// child page counter rather than the cumulative window one.
	childBudget bool
}

// seededChildPages is the per-parent count this shape resumes with.
func (s paginationShape) seededChildPages() int {
	if s.childBudget {
		return seededParentChildPages
	}
	return 0
}

func paginationShapes() []paginationShape {
	return []paginationShape{
		{
			name:    "envelope",
			dataset: datasets["entra-users"],
			handler: func(calls *atomic.Int64) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					serial := calls.Add(1)
					fmt.Fprintf(w, `{"value":[{"id":%q}],"@odata.nextLink":%q}`,
						fmt.Sprintf("user-%d", serial),
						fmt.Sprintf("%s/v1.0/users?page=%d", serverURL(r), serial+1))
				}
			},
			cursor: func(base string) *FetchContinuation {
				return &FetchContinuation{Version: fetchContinuationVersion, Kind: KindODataGET,
					NextURL: base + "/v1.0/users?page=1", Pages: seededWindowPages}
			},
			recordsPerPage: 1,
		},
		{
			name:    "resource graph",
			dataset: datasets["azure-resource-inventory"],
			handler: func(calls *atomic.Int64) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					serial := calls.Add(1)
					fmt.Fprintf(w, `{"data":[{"id":%q}],"skipToken":%q}`,
						fmt.Sprintf("/subscriptions/synthetic/resource-%d", serial),
						fmt.Sprintf("skip-%d", serial+1))
				}
			},
			cursor: func(string) *FetchContinuation {
				return &FetchContinuation{Version: fetchContinuationVersion, Kind: KindResourceGraph,
					SkipToken: "skip-1", Pages: seededWindowPages}
			},
			recordsPerPage: 1,
		},
		{
			name:    "fabric day walk",
			dataset: datasets["fabric-activity"],
			handler: func(calls *atomic.Int64) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					serial := calls.Add(1)
					fmt.Fprintf(w, `{"activityEventEntities":[{"Id":%q,"CreationTime":%q}],"continuationUri":%q}`,
						fmt.Sprintf("event-%d", serial),
						pageChargeWindowStart.Format(time.RFC3339Nano),
						fmt.Sprintf("%s/v1.0/myorg/admin/activityevents/continue?page=%d", serverURL(r), serial+1))
				}
			},
			cursor: func(base string) *FetchContinuation {
				return &FetchContinuation{Version: fetchContinuationVersion, Kind: KindFabricActivity,
					NextURL:      base + "/v1.0/myorg/admin/activityevents/continue?page=1",
					FabricCursor: pageChargeWindowStart, FabricDayEnd: pageChargeWindowEnd,
					Pages: seededWindowPages}
			},
			recordsPerPage: 1,
		},
		{
			name:    "parent list",
			dataset: datasets["defender-users"],
			handler: func(calls *atomic.Int64) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					switch {
					case strings.HasSuffix(r.URL.Path, "/api/machines"):
						serial := calls.Add(1)
						fmt.Fprintf(w, `{"value":[{"id":%q}],"@odata.nextLink":%q}`,
							fmt.Sprintf("machine-%d", serial),
							fmt.Sprintf("%s/api/machines?page=%d", serverURL(r), serial+1))
					case strings.HasSuffix(r.URL.Path, "/logonusers"):
						// Only the parent-list site charges the window counter,
						// so this shape's children are terminal and uncounted.
						fmt.Fprint(w, `{"value":[{"accountName":"user-on-machine"}]}`)
					default:
						http.NotFound(w, r)
					}
				}
			},
			cursor: func(base string) *FetchContinuation {
				return &FetchContinuation{Version: fetchContinuationVersion, Kind: KindParentChild,
					ParentNextURL: base + "/api/machines", Pages: seededWindowPages}
			},
			recordsPerPage: 0,
		},
		{
			name:    "advanced hunting",
			dataset: datasets["defender-xdr-device-events"],
			handler: func(calls *atomic.Int64) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					serial := calls.Add(1)
					fmt.Fprintf(w, `{"results":[{"ReportId":%q,"Timestamp":%q,"DeviceId":"device-1"}]}`,
						fmt.Sprintf("report-%d", serial), pageChargeWindowStart.Format(time.RFC3339Nano))
				}
			},
			cursor: func(string) *FetchContinuation {
				return &FetchContinuation{Version: fetchContinuationVersion, Kind: KindAdvancedHunting,
					HuntingWindows: []FetchWindow{
						{Start: pageChargeWindowStart, End: pageChargeWindowStart.Add(30 * time.Minute)},
						{Start: pageChargeWindowStart.Add(30 * time.Minute), End: pageChargeWindowEnd},
					},
					Pages: seededWindowPages}
			},
			recordsPerPage: 1,
		},
		{
			name:    "parent-child child pages",
			dataset: datasets["defender-users"],
			handler: func(calls *atomic.Int64) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					serial := calls.Add(1)
					fmt.Fprintf(w, `{"value":[{"accountName":%q}],"@odata.nextLink":%q}`,
						fmt.Sprintf("user-%d", serial),
						fmt.Sprintf("%s/api/machines/machine-1/logonusers?cursor=%d", serverURL(r), serial+1))
				}
			},
			cursor: func(base string) *FetchContinuation {
				return &FetchContinuation{Version: fetchContinuationVersion, Kind: KindParentChild,
					ParentIDs: []string{"machine-1"}, ChildParentID: "machine-1",
					ChildNextURL: base + "/api/machines/machine-1/logonusers?cursor=1",
					Pages:        seededWindowPages, ChildPages: seededParentChildPages}
			},
			recordsPerPage: 1,
			childBudget:    true,
		},
	}
}

// pacedFetchClient answers allowed source requests from server and defers every
// later one. rate.Limiter refuses a wait it cannot satisfy inside the fetch
// deadline without sleeping, and do() maps that refusal onto errFetchPacing --
// the deferral every page loop turns into a durable continuation -- so the
// yield is immediate and deterministic. A negative allowance paces nothing.
func pacedFetchClient(t *testing.T, server *httptest.Server, dataset Dataset, allowed int) *Client {
	t.Helper()
	conf := testClientConfig(t, server.URL)
	client, err := NewClient(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	client.http = server.Client()
	// A token request would spend the same allowance a page does, so the scope
	// this dataset's pages resolve to is seeded instead.
	client.tokens[conf.Scope(dataset.Service, dataset.Scope)] = cachedToken{Value: "paced", Expires: time.Now().Add(time.Hour)}
	if allowed < 0 {
		client.limiter = rate.NewLimiter(rate.Inf, 1)
	} else {
		client.limiter = rate.NewLimiter(rate.Every(time.Hour), allowed)
	}
	return client
}

// persistedCursor round-trips a cursor through the durable document saveState
// writes, so a count asserted through it is the count a restart resumes with.
func persistedCursor(t *testing.T, cursor *FetchContinuation) *FetchContinuation {
	t.Helper()
	if cursor == nil {
		t.Fatal("yielded chunk carried no continuation to persist")
	}
	encoded, err := json.Marshal(cursor)
	if err != nil {
		t.Fatal(err)
	}
	var restored FetchContinuation
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.validate(); err != nil {
		t.Fatalf("persisted cursor %s is unreadable: %v", encoded, err)
	}
	return &restored
}

// A pass deferred before its first request reads no page, so it may charge no
// page: the cursor it persists has to carry exactly the count it resumed with.
func TestDeferredFirstRequestLeavesCumulativePageCountersUnchanged(t *testing.T) {
	for _, shape := range paginationShapes() {
		t.Run(shape.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewTLSServer(shape.handler(&calls))
			defer server.Close()
			client := pacedFetchClient(t, server, shape.dataset, 0)
			chunk, err := client.FetchChunk(t.Context(), shape.dataset,
				pageChargeWindowStart, pageChargeWindowEnd, "", shape.cursor(server.URL))
			if err != nil {
				t.Fatalf("paced first request failed instead of yielding a continuation: %v", err)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("source requests=%d, want none: the pacing yield precedes the page", got)
			}
			if chunk.Complete || chunk.PageBudgetExhausted {
				t.Fatalf("chunk=%+v, want an incomplete chunk that is not reported as bounded", chunk)
			}
			if len(chunk.Records) != 0 {
				t.Fatalf("records=%d, want none from a request that never ran", len(chunk.Records))
			}
			resumed := persistedCursor(t, chunk.Continuation)
			if resumed.Pages != seededWindowPages {
				t.Fatalf("cumulative window pages=%d, want the seeded %d: a page the window never read was charged",
					resumed.Pages, seededWindowPages)
			}
			if want := shape.seededChildPages(); resumed.ChildPages != want {
				t.Fatalf("per-parent child pages=%d, want the seeded %d: a child page the parent never read was charged",
					resumed.ChildPages, want)
			}
		})
	}
}

// One page read charges exactly one page, against exactly one of the two
// counters, and the deferral that follows it charges nothing more.
func TestSuccessfulPageChargesItsCumulativeCounterExactlyOnce(t *testing.T) {
	for _, shape := range paginationShapes() {
		t.Run(shape.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewTLSServer(shape.handler(&calls))
			defer server.Close()
			client := pacedFetchClient(t, server, shape.dataset, 1)
			chunk, err := client.FetchChunk(t.Context(), shape.dataset,
				pageChargeWindowStart, pageChargeWindowEnd, "", shape.cursor(server.URL))
			if err != nil {
				t.Fatalf("paced pass failed instead of yielding a continuation: %v", err)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("source requests=%d, want exactly the one page the allowance covers", got)
			}
			if chunk.Complete || chunk.PageBudgetExhausted {
				t.Fatalf("chunk=%+v, want an incomplete chunk that is not reported as bounded", chunk)
			}
			if len(chunk.Records) != shape.recordsPerPage {
				t.Fatalf("records=%d, want %d from one page", len(chunk.Records), shape.recordsPerPage)
			}
			wantPages, wantChildPages := seededWindowPages, shape.seededChildPages()
			if shape.childBudget {
				wantChildPages++
			} else {
				wantPages++
			}
			resumed := persistedCursor(t, chunk.Continuation)
			if resumed.Pages != wantPages {
				t.Fatalf("cumulative window pages=%d, want %d: one charge for the one page read", resumed.Pages, wantPages)
			}
			if resumed.ChildPages != wantChildPages {
				t.Fatalf("per-parent child pages=%d, want %d: one charge for the one page read", resumed.ChildPages, wantChildPages)
			}
		})
	}
}

// Charging after the page rather than before it leaves the bound exact: the
// loop header re-reads the count before the next page, so a window still reads
// its whole budget and no more, and the page that reaches the bound is still
// the one that closes the window as truncated.
func TestCumulativePageBoundClosesTheWindowOnTheExactPageItReads(t *testing.T) {
	for _, shape := range paginationShapes() {
		if shape.childBudget {
			continue
		}
		t.Run(shape.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewTLSServer(shape.handler(&calls))
			defer server.Close()
			client := pacedFetchClient(t, server, shape.dataset, -1)
			// Exactly one page of the cumulative budget is left unspent.
			client.continuationPageLimit = seededWindowPages + 1
			chunk, err := client.FetchChunk(t.Context(), shape.dataset,
				pageChargeWindowStart, pageChargeWindowEnd, "", shape.cursor(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("pages read=%d, want exactly the one page left inside the %d-page bound",
					got, seededWindowPages+1)
			}
			if chunk.Complete || !chunk.PageBudgetExhausted {
				t.Fatalf("chunk=%+v, want the window reported as truncated on its exact bound", chunk)
			}
			if chunk.Continuation == nil || chunk.Continuation.Pages != seededWindowPages+1 {
				t.Fatalf("cursor=%+v, want the cumulative count at exactly the %d-page bound",
					chunk.Continuation, seededWindowPages+1)
			}
		})
	}
}

// The window bound is cumulative across passes, so charging an unread page was
// permanent loss rather than a slow start: a source with exactly as many pages
// as the bound allows, deferred once per pass, used to spend two pages per pass
// and close itself as truncated with real pages still unread. Every pass now
// charges the one page it read, the count survives the durable document, and
// the window completes.
func TestWindowBudgetSurvivesDeferralsAcrossRestarts(t *testing.T) {
	const pages = 3
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serial := calls.Add(1)
		next := ""
		if serial < pages {
			next = fmt.Sprintf(`,"@odata.nextLink":%q`, fmt.Sprintf("%s/v1.0/users?page=%d", serverURL(r), serial+1))
		}
		fmt.Fprintf(w, `{"value":[{"id":%q}]%s}`, fmt.Sprintf("user-%d", serial), next)
	}))
	defer server.Close()

	dataset := datasets["entra-users"]
	client := pacedFetchClient(t, server, dataset, 0)
	// The window may read exactly the pages this source has, so one page
	// charged for a request that never ran is one real page permanently lost.
	client.continuationPageLimit = pages

	var cursor *FetchContinuation
	records, passes := 0, 0
	for passes < pages {
		// Each pass answers one page and defers the next, the way a paced
		// tenant hands back a continuation mid-window.
		client.limiter = rate.NewLimiter(rate.Every(time.Hour), 1)
		chunk, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", cursor)
		if err != nil {
			t.Fatalf("pass %d error=%v", passes+1, err)
		}
		passes++
		records += len(chunk.Records)
		if chunk.PageBudgetExhausted {
			t.Fatalf("pass %d closed the window as truncated after %d of %d source pages", passes, calls.Load(), pages)
		}
		if chunk.Complete {
			break
		}
		cursor = persistedCursor(t, chunk.Continuation)
		if cursor.Pages != passes {
			t.Fatalf("pass %d persisted a cumulative page count of %d, want %d: one charge per page read",
				passes, cursor.Pages, passes)
		}
	}
	if passes != pages || records != pages || calls.Load() != int64(pages) {
		t.Fatalf("passes=%d records=%d source requests=%d, want %d of each", passes, records, calls.Load(), pages)
	}
}

// The per-parent bound is charged the same way, and still stops a parent on the
// exact child page that reaches it: the parent is truncated, the window keeps
// going, and the child page that reached the bound is still delivered.
func TestChildPageBoundStopsOnTheExactChildPageItReads(t *testing.T) {
	const childBound = seededParentChildPages + 1
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
		case strings.HasSuffix(r.URL.Path, "/logonusers"):
			serial := calls.Add(1)
			fmt.Fprintf(w, `{"value":[{"accountName":%q}],"@odata.nextLink":%q}`,
				fmt.Sprintf("user-%d", serial),
				fmt.Sprintf("%s/api/machines/machine-1/logonusers?cursor=%d", serverURL(r), serial+1))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.parentChildPageLimit = childBound
	chunk, err := client.FetchChunk(t.Context(), datasets["defender-users"], time.Time{}, time.Time{}, "",
		&FetchContinuation{
			Version: fetchContinuationVersion, Kind: KindParentChild,
			ParentIDs: []string{"machine-1"}, ChildParentID: "machine-1",
			ChildNextURL: server.URL + "/api/machines/machine-1/logonusers?cursor=1",
			ChildPages:   seededParentChildPages,
		})
	if err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("child pages read=%d, want exactly the one page left inside the %d-page per-parent bound", got, childBound)
	}
	if !chunk.Complete || chunk.ChildBudgetExhausted != 1 {
		t.Fatalf("chunk=%+v, want the offending parent truncated and the window complete", chunk)
	}
	if len(chunk.TruncatedParents) != 1 || chunk.TruncatedParents[0] != "machine-1" {
		t.Fatalf("truncated parents=%v, want exactly machine-1", chunk.TruncatedParents)
	}
	if len(chunk.Records) != 1 {
		t.Fatalf("records=%d, want the one child page this parent did supply", len(chunk.Records))
	}
}

// A parent identifier of a parent-child fan-out is decoded verbatim from the
// identity field of a parent-list record, so the vendor controls its bytes
// completely: nothing in the catalog or the decode path constrains them, and it
// can carry CR, LF or any other control character. Both operator-visible
// insertions of it -- first_parent and reported_parents in the per-parent
// child-page bound report -- used to write it through unchanged, so an interior
// CR LF ended the physical RFC5424 record there and the remainder of the
// identifier was read as a further complete record this plugin never wrote
// (CWE-117).
//
// After the forged record the identifier carries the remaining shapes a
// terminal or a log parser can be driven with: a tab, a NUL, an ESC, a NEL, and
// a backslash that must not be able to imitate an escape this presentation
// produces.
const parentInjectionID = "machine-1\r\n<13>1 2026-10-07T12:00:00Z runner microsoft - - - forged record\r\n" +
	"\ttab\x00nul\x1besc\u0085nel\\slash"

// parentInjectionDisplayed is the only presentation of that identifier an
// operator may be shown: one physical line, every control character visible.
const parentInjectionDisplayed = `machine-1\r\n<13>1 2026-10-07T12:00:00Z runner microsoft - - - forged record\r\n\ttab\x00nul\x1besc\x85nel\\slash`

// parentInjectionServer publishes one Defender machine whose identity field is
// operator-hostile, and then answers that machine's logon-user list with an
// endless sequence of distinct child cursors so only the per-parent child-page
// bound can stop it -- which is the report that names the parent.
//
// childPaths records the request path of every child page, which is what proves
// the canonical identifier, not the escaped presentation, is what the fan-out
// dispatched against.
func parentInjectionServer(t *testing.T, childPaths *parentInjectionPaths) *httptest.Server {
	t.Helper()
	var childCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
		case strings.HasSuffix(r.URL.Path, "/logonusers"):
			childPaths.add(r.URL.Path)
			serial := childCalls.Add(1)
			// The next cursor deliberately does not carry the hostile identifier:
			// the per-parent bound, not a repeated cursor, has to be what stops
			// this parent.
			fmt.Fprintf(w, `{"value":[{"accountName":%q}],"@odata.nextLink":%q}`,
				fmt.Sprintf("user-%d", serial),
				fmt.Sprintf("%s/api/machines/hostile/logonusers?cursor=%d", serverURL(r), serial+1))
		case strings.HasSuffix(r.URL.Path, "/api/machines"):
			fmt.Fprintf(w, `{"value":[{"id":%s}]}`, mustJSONString(t, parentInjectionID))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// parentInjectionPaths collects the child request paths the fan-out dispatched.
type parentInjectionPaths struct {
	mu    sync.Mutex
	paths []string
}

func (p *parentInjectionPaths) add(path string) {
	p.mu.Lock()
	p.paths = append(p.paths, path)
	p.mu.Unlock()
}

func (p *parentInjectionPaths) all() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.paths...)
}

func mustJSONString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// parentInjectionSinkRuntime forwards every operator-visible line to the
// default RFC5424 file sink, which is the sink the forged physical records
// appeared in.
type parentInjectionSinkRuntime struct {
	*microsoftTestRuntime
	t         *testing.T
	sink      *log.Logger
	forwarded atomic.Int64
}

func (r *parentInjectionSinkRuntime) Warn(message string, params ...rfc5424.SDParam) {
	r.microsoftTestRuntime.Warn(message, params...)
	r.forward(r.sink.Warn(message, params...))
}

func (r *parentInjectionSinkRuntime) Error(message string, params ...rfc5424.SDParam) {
	r.microsoftTestRuntime.Error(message, params...)
	r.forward(r.sink.Error(message, params...))
}

func (r *parentInjectionSinkRuntime) forward(err error) {
	r.forwarded.Add(1)
	if err != nil {
		r.t.Errorf("sink write failed: %v", err)
	}
}

// TestHostileParentIDIsOneRecordInTheRFC5424FileSink is the end-to-end proof
// for the parent-identifier insertions. A parent-list record whose id carries
// CR, LF and other control bytes drives a real fan-out through the real
// per-parent bound report into the real file sink, and what lands on disk has
// to be exactly one physical record per reported line.
func TestHostileParentIDIsOneRecordInTheRFC5424FileSink(t *testing.T) {
	const childBound = 3
	var childPaths parentInjectionPaths
	server := parentInjectionServer(t, &childPaths)

	path := t.TempDir() + "/runner.log"
	sink, err := log.NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sink.SetAppname("microsoft")
	sink.SetHostname("runner")
	rt := &parentInjectionSinkRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(t), t: t, sink: sink}

	plugin := newSelectorTestPlugin(t, server, "defender-users")
	plugin.client.parentChildPageLimit = childBound
	key := stateKey("microsoft", "defender-users", "")
	for pass := 1; pass <= 4; pass++ {
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("pass %d error=%v, want the hostile parent truncated and reported", pass, err)
		}
		state, err := loadState(rt.microsoftTestRuntime, key)
		if err != nil {
			t.Fatal(err)
		}
		if state.Continuation == nil {
			break
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}

	reports := rt.logsContaining("ERROR", "per-parent child page bound")
	if len(reports) != 1 {
		t.Fatalf("bounded-loss reports=%d, want exactly 1 (%+v)", len(reports), reports)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatalf("sink bytes %q do not end a record", raw)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if got, want := len(lines), int(rt.forwarded.Load()); got != want {
		t.Fatalf("the sink holds %d physical lines for %d reported lines:\n%s", got, want, raw)
	}
	carrying := ""
	for _, line := range lines {
		if !strings.HasPrefix(line, "<") {
			t.Fatalf("physical line %q is not a complete record; a reported value ended the line before it", line)
		}
		if strings.ContainsAny(line, "\r") {
			t.Fatalf("physical line %q carries a raw carriage return", line)
		}
		if !strings.Contains(line, "first_parent=") {
			if strings.Contains(line, "forged record") {
				t.Fatalf("physical line %q is the forged record the identifier carried", line)
			}
			continue
		}
		if carrying != "" {
			t.Fatalf("the identifier was written across more than one physical record:\n%s", raw)
		}
		carrying = line
	}
	if carrying == "" {
		t.Fatalf("no record named the truncated parent:\n%s", raw)
	}
	// The identifier appears twice in that one record, once per insertion:
	// first_parent and reported_parents. Neither may have escaped the record.
	if got := strings.Count(string(raw), "forged record"); got != 2 {
		t.Fatalf("the forged record text appears %d times, want twice inside the one escaped record:\n%s", got, raw)
	}
	// The sink escapes a backslash in a parameter value, so each escape this
	// presentation produced reaches disk with its backslash doubled; what
	// matters is that the escape is there and visible rather than acted on.
	for _, escape := range []string{`\r`, `\n`, `\t`, `\x00`, `\x1b`, `\x85`, `\\slash`} {
		if !strings.Contains(carrying, escape) {
			t.Fatalf("record %q is missing the visible escape %s", carrying, escape)
		}
	}

	// Both insertions carry the escaped presentation and nothing else.
	if got := reports[0].Params["first_parent"]; got != parentInjectionDisplayed {
		t.Fatalf("first_parent=%q, want the escaped presentation %q", got, parentInjectionDisplayed)
	}
	if got := reports[0].Params["reported_parents"]; got != parentInjectionDisplayed {
		t.Fatalf("reported_parents=%q, want the escaped presentation %q", got, parentInjectionDisplayed)
	}
	for name, value := range reports[0].Params {
		if strings.ContainsFunc(value, unicode.IsControl) {
			t.Fatalf("reported %s=%q retains a control character an operator log cannot carry", name, value)
		}
	}
	for _, published := range rt.warnStateContaining("per-parent child page bound") {
		if strings.ContainsFunc(published, unicode.IsControl) {
			t.Fatalf("published status=%q retains a control character", published)
		}
	}

	// Canonical behavior is unchanged: the fan-out dispatched against the
	// canonical identifier, percent-escaped into the child path, and never
	// against the escaped presentation.
	paths := childPaths.all()
	if len(paths) != childBound {
		t.Fatalf("child pages read=%d, want exactly the %d-page per-parent bound", len(paths), childBound)
	}
	// The first child page is the one the fan-out builds from the parent
	// identifier; the pages after it follow the cursors the source published.
	wantPath := "/api/machines/" + parentInjectionID + "/logonusers"
	if paths[0] != wantPath {
		t.Fatalf("first child request path=%q, want the canonical identifier %q", paths[0], wantPath)
	}
	for index, got := range paths {
		if strings.Contains(got, parentInjectionDisplayed) {
			t.Fatalf("child request %d path=%q dispatched against the escaped presentation", index, got)
		}
	}
	// The window closed against its own end with the loss stated, so the
	// identifier's bytes never changed what the collection did.
	watermark, err := rt.GetTime("microsoft/defender-users/timestamp")
	if err != nil || watermark.IsZero() {
		t.Fatalf("watermark=%s error=%v, want the window closed against its own end", watermark, err)
	}
	if got := len(rt.Entries()); got != childBound {
		t.Fatalf("entries=%d, want the %d child pages this parent did supply", got, childBound)
	}
}

// TestRawParentIDWouldForgeARecordInTheSameSink is the mutation proof that the
// sink the test above measures is the sink that was vulnerable. It writes the
// canonical identifier into the same default file sink with no presentation
// applied, which is exactly what reverting either insertion would do, and
// requires that it really does forge further physical records.
//
// Without this, a sink that happened to neutralize control characters itself
// would let the test above pass whether or not the plugin escapes anything.
func TestRawParentIDWouldForgeARecordInTheSameSink(t *testing.T) {
	path := t.TempDir() + "/raw.log"
	sink, err := log.NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sink.SetAppname("microsoft")
	sink.SetHostname("runner")
	if err := sink.Error("Microsoft API parents reached their per-parent child page bound",
		log.KV("first_parent", parentInjectionID)); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("the unescaped identifier produced %d physical record(s); the sink neutralizes control characters, so the escaping test proves nothing:\n%s", len(lines), raw)
	}
	forged := false
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "<") && strings.Contains(line, "forged record") {
			forged = true
		}
	}
	if !forged {
		t.Fatalf("the unescaped identifier did not forge a complete record, so the regression above is not measuring one:\n%s", raw)
	}
	// And the escaped presentation of the same bytes is one record in that
	// same sink, which is the property the repair relies on.
	escapedPath := t.TempDir() + "/escaped.log"
	escapedSink, err := log.NewFile(escapedPath)
	if err != nil {
		t.Fatal(err)
	}
	escapedSink.SetAppname("microsoft")
	escapedSink.SetHostname("runner")
	if err := escapedSink.Error("Microsoft API parents reached their per-parent child page bound",
		log.KV("first_parent", displayVendorValue(parentInjectionID, maximumDisplayedVendorValue))); err != nil {
		t.Fatal(err)
	}
	if err := escapedSink.Close(); err != nil {
		t.Fatal(err)
	}
	escapedRaw, err := os.ReadFile(escapedPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Split(strings.TrimSuffix(string(escapedRaw), "\n"), "\n")); got != 1 {
		t.Fatalf("the escaped presentation produced %d physical records:\n%s", got, escapedRaw)
	}
}

// TestDisplayVendorValueEscapesEveryControlCharacter pins the shared
// presentation contract every operator-visible vendor value goes through.
func TestDisplayVendorValueEscapesEveryControlCharacter(t *testing.T) {
	if got := displayVendorValue(parentInjectionID, maximumDisplayedVendorValue); got != parentInjectionDisplayed {
		t.Fatalf("displayed=%q, want %q", got, parentInjectionDisplayed)
	}
	// Unicode Cc is U+0000-U+001F and U+007F-U+009F. Every one of them has to
	// become visible text, because the sink writes all of them through.
	for r := rune(0); r <= 0x9f; r++ {
		if !unicode.IsControl(r) {
			continue
		}
		got := displayVendorValue(string(r), maximumDisplayedVendorValue)
		if strings.ContainsFunc(got, unicode.IsControl) {
			t.Fatalf("U+%04X displayed as %q, which still holds a control character", r, got)
		}
		if len(got) < 2 || !strings.HasPrefix(got, `\`) {
			t.Fatalf("U+%04X displayed as %q, want a visible escape", r, got)
		}
	}
	// Nothing an operator can act on is rewritten.
	for _, clean := range []string{"machine-1", "0123456789abcdef0123456789abcdef01234567", "a.b-c_d", "dévice"} {
		if got := displayVendorValue(clean, maximumDisplayedVendorValue); got != clean {
			t.Fatalf("clean value %q displayed as %q", clean, got)
		}
	}
	// The presentation is bounded on its own, and the bound is marked.
	displayed := displayVendorValue(strings.Repeat("\n", maximumDisplayedVendorValue*4), maximumDisplayedVendorValue)
	if want := maximumDisplayedVendorValue*2 + len(displayTruncationMarker); len(displayed) != want {
		t.Fatalf("displayed length=%d, want the bounded %d", len(displayed), want)
	}
	if !strings.HasSuffix(displayed, displayTruncationMarker) {
		t.Fatalf("displayed=%q, want the bound marked", displayed)
	}
	// A non-positive bound falls back to the per-value bound rather than
	// rendering nothing.
	if got := displayVendorValue("machine-1", 0); got != "machine-1" {
		t.Fatalf("zero bound displayed %q", got)
	}
}

// TestDisplayVendorListBoundsTheWholeParameter pins the list presentation. The
// count beside it in a report is exact, so the list itself has to be bounded
// and say when it did not fit.
func TestDisplayVendorListBoundsTheWholeParameter(t *testing.T) {
	if got := displayVendorList(nil); got != "" {
		t.Fatalf("empty list displayed as %q", got)
	}
	if got, want := displayVendorList([]string{"a", "b\r\nc"}), `a,b\r\nc`; got != want {
		t.Fatalf("list displayed as %q, want %q", got, want)
	}
	// maximumDiagnostics values at the per-value bound is the widest list a
	// report can carry, and it has to come back bounded and marked.
	wide := make([]string, maximumDiagnostics)
	for i := range wide {
		wide[i] = strings.Repeat("\n", maximumDisplayedVendorValue)
	}
	displayed := displayVendorList(wide)
	if len(displayed) > maximumDisplayedVendorList+len(displayTruncationMarker) {
		t.Fatalf("list presentation is %d bytes, over the %d-byte bound", len(displayed), maximumDisplayedVendorList)
	}
	if !strings.HasSuffix(displayed, displayTruncationMarker) {
		t.Fatalf("list presentation %q did not state that it was bounded", displayed)
	}
	if strings.ContainsFunc(displayed, unicode.IsControl) {
		t.Fatal("list presentation retains a control character")
	}
	// A single element at the per-value bound always fits, so the presentation
	// is never reduced to the marker alone.
	one := displayVendorList([]string{strings.Repeat("\x01", maximumDisplayedVendorValue)})
	if one == displayTruncationMarker || !strings.HasPrefix(one, `\x01`) {
		t.Fatalf("single bounded element displayed as %q", one)
	}
}

// storedCursorAtSize returns a persisted cursor whose named field holds exactly
// size bytes, with everything else a shape the structural bounds accept.
func storedCursorAtSize(field string, size int) FetchContinuation {
	cursor := FetchContinuation{Version: fetchContinuationVersion, Kind: KindParentChild}
	filler := strings.Repeat("a", size)
	switch field {
	case "next_url":
		cursor.NextURL = "https://host.invalid/" + filler
	case "parent_next_url":
		cursor.ParentNextURL = "https://host.invalid/" + filler
	case "child_next_url":
		cursor.ParentIDs = []string{"machine-1"}
		cursor.ChildNextURL = "https://host.invalid/" + filler
	case "skip_token":
		cursor.SkipToken = filler
	case "child_parent_id":
		cursor.ParentIDs = []string{filler}
		cursor.ChildParentID = filler
	case "parent_id":
		cursor.ParentIDs = []string{filler}
	case "history_entry":
		cursor.Recent = []string{filler}
	}
	// The URL fields carry a scheme and host of their own, so the field is
	// trimmed back to exactly size bytes.
	switch field {
	case "next_url":
		cursor.NextURL = cursor.NextURL[:size]
	case "parent_next_url":
		cursor.ParentNextURL = cursor.ParentNextURL[:size]
	case "child_next_url":
		cursor.ChildNextURL = cursor.ChildNextURL[:size]
	}
	return cursor
}

// TestStoredCursorFieldSizeBoundsAcceptTheBoundAndRefuseOneOver is the
// exact-bound and one-over proof for every bounded cursor field. Each case is
// its own mutation proof: removing the bound for that field makes the one-over
// case resumable and fails exactly this subtest.
func TestStoredCursorFieldSizeBoundsAcceptTheBoundAndRefuseOneOver(t *testing.T) {
	dataset := datasets["defender-users"]
	for _, tc := range []struct {
		field string
		bound int
	}{
		{"next_url", maximumContinuationURLLength},
		{"parent_next_url", maximumContinuationURLLength},
		{"child_next_url", maximumContinuationURLLength},
		{"skip_token", maximumContinuationSkipTokenLength},
		{"child_parent_id", maximumContinuationParentIDLength},
		{"parent_id", maximumContinuationParentIDLength},
		{"history_entry", maximumContinuationURLLength},
	} {
		t.Run(tc.field, func(t *testing.T) {
			atBound := storedCursorAtSize(tc.field, tc.bound)
			if _, err := continuationFor(dataset, &atBound); err != nil {
				t.Fatalf("a %s of exactly %d bytes was refused: %v", tc.field, tc.bound, err)
			}
			oneOver := storedCursorAtSize(tc.field, tc.bound+1)
			_, err := continuationFor(dataset, &oneOver)
			if !errors.Is(err, errIncompatibleContinuation) {
				t.Fatalf("a %s of %d bytes error=%v, want an incompatible-continuation error so the cursor resets", tc.field, tc.bound+1, err)
			}
			// The reason reaches the operator's log, so it may name the field
			// and the bound but never the stored bytes.
			if strings.Contains(err.Error(), "aaaa") {
				t.Fatalf("refusal message %q carries the stored value", err)
			}
		})
	}
}

// parentsCostingExactly builds a parent list whose persisted cost is exactly
// total bytes, using identifiers no longer than the per-identifier bound.
func parentsCostingExactly(t *testing.T, total int) []string {
	t.Helper()
	wide := strings.Repeat("a", maximumContinuationParentIDLength)
	wideCost := continuationParentIDBytes(wide)
	count, remainder := total/wideCost, total%wideCost
	if remainder != 0 && remainder < continuationParentIDBytes("") {
		count--
		remainder += wideCost
	}
	ids := make([]string, 0, count+1)
	for i := 0; i < count; i++ {
		ids = append(ids, wide)
	}
	if remainder != 0 {
		filler := remainder - continuationParentIDBytes("")
		if filler < 0 || filler > maximumContinuationParentIDLength {
			t.Fatalf("cannot land exactly on %d persisted bytes with %d-byte identifiers", total, maximumContinuationParentIDLength)
		}
		ids = append(ids, strings.Repeat("a", filler))
	}
	if got := continuationParentBytes(ids); got != total {
		t.Fatalf("built a parent list costing %d bytes, want exactly %d", got, total)
	}
	return ids
}

// TestStoredCursorParentCountAndByteBoundsAcceptTheBoundAndRefuseOneOver is the
// same proof for the two aggregate parent bounds. The count bound and the byte
// bound are separate because neither implies the other: a tenant with many
// short machine IDs reaches the count first, and a source publishing long ones
// reaches the bytes first.
func TestStoredCursorParentCountAndByteBoundsAcceptTheBoundAndRefuseOneOver(t *testing.T) {
	dataset := datasets["defender-users"]

	t.Run("count", func(t *testing.T) {
		atBound := FetchContinuation{Version: fetchContinuationVersion, Kind: dataset.Kind}
		atBound.ParentIDs = make([]string, maximumContinuationParents)
		for i := range atBound.ParentIDs {
			atBound.ParentIDs[i] = "a"
		}
		if _, err := continuationFor(dataset, &atBound); err != nil {
			t.Fatalf("exactly %d parents were refused: %v", maximumContinuationParents, err)
		}
		oneOver := atBound
		oneOver.ParentIDs = append(append([]string(nil), atBound.ParentIDs...), "a")
		if _, err := continuationFor(dataset, &oneOver); !errors.Is(err, errIncompatibleContinuation) {
			t.Fatalf("%d parents error=%v, want an incompatible-continuation error", len(oneOver.ParentIDs), err)
		}
	})

	t.Run("bytes", func(t *testing.T) {
		atBound := FetchContinuation{Version: fetchContinuationVersion, Kind: dataset.Kind}
		atBound.ParentIDs = parentsCostingExactly(t, maximumContinuationParentBytes)
		if len(atBound.ParentIDs) > maximumContinuationParents {
			t.Fatalf("the byte bound needs %d parents, over the %d-parent bound", len(atBound.ParentIDs), maximumContinuationParents)
		}
		if _, err := continuationFor(dataset, &atBound); err != nil {
			t.Fatalf("a parent list costing exactly %d bytes was refused: %v", maximumContinuationParentBytes, err)
		}
		oneOver := atBound
		oneOver.ParentIDs = append([]string(nil), atBound.ParentIDs...)
		last := len(oneOver.ParentIDs) - 1
		oneOver.ParentIDs[last] += "a"
		if got := continuationParentBytes(oneOver.ParentIDs); got != maximumContinuationParentBytes+1 {
			t.Fatalf("one-over list costs %d bytes, want %d", got, maximumContinuationParentBytes+1)
		}
		if _, err := continuationFor(dataset, &oneOver); !errors.Is(err, errIncompatibleContinuation) {
			t.Fatalf("a parent list costing %d bytes error=%v, want an incompatible-continuation error",
				maximumContinuationParentBytes+1, err)
		}
	})
}

// TestParentListAdmissionBoundsTheCursorAsItIsBuilt proves the same three
// parent bounds are enforced where the source value is accepted, not only
// where a stored document is loaded, and that the aggregate budget is charged
// in persisted bytes rather than raw ones.
//
// That distinction is the whole amplification concern: a parent identifier of
// control characters costs six persisted bytes per raw byte, so a list that
// looks like 1.3 MB of identifiers is already over an 8 MiB document budget.
func TestParentListAdmissionBoundsTheCursorAsItIsBuilt(t *testing.T) {
	dataset := datasets["defender-users"]

	t.Run("identifier-length", func(t *testing.T) {
		cursor := &FetchContinuation{Version: fetchContinuationVersion, Kind: dataset.Kind}
		if err := admitParentID(cursor, dataset, strings.Repeat("a", maximumContinuationParentIDLength)); err != nil {
			t.Fatalf("an identifier of exactly %d bytes was refused: %v", maximumContinuationParentIDLength, err)
		}
		if err := admitParentID(cursor, dataset, strings.Repeat("a", maximumContinuationParentIDLength+1)); err == nil {
			t.Fatalf("an identifier of %d bytes was accepted", maximumContinuationParentIDLength+1)
		} else if strings.Contains(err.Error(), "aaaa") {
			t.Fatalf("refusal message %q carries the vendor identifier", err)
		}
		if len(cursor.ParentIDs) != 1 {
			t.Fatalf("parents retained=%d, want only the one inside the bound", len(cursor.ParentIDs))
		}
	})

	t.Run("count", func(t *testing.T) {
		cursor := &FetchContinuation{Version: fetchContinuationVersion, Kind: dataset.Kind}
		cursor.ParentIDs = make([]string, maximumContinuationParents-1)
		for i := range cursor.ParentIDs {
			cursor.ParentIDs[i] = "a"
		}
		cursor.parentBytes = continuationParentBytes(cursor.ParentIDs)
		if err := admitParentID(cursor, dataset, "a"); err != nil {
			t.Fatalf("the parent that reaches exactly the %d-parent bound was refused: %v", maximumContinuationParents, err)
		}
		if err := admitParentID(cursor, dataset, "a"); err == nil {
			t.Fatalf("parent %d was accepted over the %d-parent bound", maximumContinuationParents+1, maximumContinuationParents)
		}
		if len(cursor.ParentIDs) != maximumContinuationParents {
			t.Fatalf("parents retained=%d, want exactly the bound", len(cursor.ParentIDs))
		}
	})

	t.Run("persisted-bytes", func(t *testing.T) {
		cursor := &FetchContinuation{Version: fetchContinuationVersion, Kind: dataset.Kind}
		cursor.ParentIDs = parentsCostingExactly(t, maximumContinuationParentBytes)
		cursor.parentBytes = continuationParentBytes(cursor.ParentIDs)
		if err := admitParentID(cursor, dataset, "a"); err == nil {
			t.Fatalf("a parent was accepted past the %d-byte budget", maximumContinuationParentBytes)
		}
		if got := continuationParentBytes(cursor.ParentIDs); got != maximumContinuationParentBytes {
			t.Fatalf("the refused parent was retained: list costs %d bytes", got)
		}
	})

	t.Run("json-escaping-amplification", func(t *testing.T) {
		hostile := strings.Repeat("\x01", maximumContinuationParentIDLength)
		encoded := continuationParentIDBytes(hostile)
		if encoded <= maximumContinuationParentIDLength {
			t.Fatalf("a control-character identifier costs %d persisted bytes for %d raw ones; the budget is not measuring the document",
				encoded, maximumContinuationParentIDLength)
		}
		cursor := &FetchContinuation{Version: fetchContinuationVersion, Kind: dataset.Kind}
		rawBytes := 0
		refused := false
		for i := 0; i < maximumContinuationParents; i++ {
			if err := admitParentID(cursor, dataset, hostile); err != nil {
				refused = true
				break
			}
			rawBytes += len(hostile)
		}
		if !refused {
			t.Fatalf("%d control-character parents were all accepted", len(cursor.ParentIDs))
		}
		if rawBytes >= maximumContinuationParentBytes {
			t.Fatalf("the budget only bound the raw bytes: %d raw bytes were accepted against a %d-byte budget",
				rawBytes, maximumContinuationParentBytes)
		}
		// The list the cursor kept is inside the budget it is persisted under.
		if got := continuationParentBytes(cursor.ParentIDs); got > maximumContinuationParentBytes {
			t.Fatalf("retained parent list costs %d bytes, over the %d-byte budget", got, maximumContinuationParentBytes)
		}
		// And a cursor holding it decodes and re-validates, which is what makes
		// the accept-time budget and the load-time one the same statement.
		stored := FetchContinuation{Version: fetchContinuationVersion, Kind: dataset.Kind, ParentIDs: cursor.ParentIDs}
		if _, err := continuationFor(dataset, &stored); err != nil {
			t.Fatalf("the list the accept path kept was refused on load: %v", err)
		}
	})
}

// TestPathEscapingAmplificationStaysInsideTheDispatchedURLBound checks the
// other escaping the cursor pays for. A parent identifier is percent-escaped
// into the child URL path, which costs three bytes per control byte, so the
// per-identifier bound and the URL bound have to be compatible: a full-length
// hostile identifier must still produce a child URL this plugin will dispatch.
func TestPathEscapingAmplificationStaysInsideTheDispatchedURLBound(t *testing.T) {
	conf := testClientConfig(t, "https://api.securitycenter.microsoft.com")
	client, err := NewClient(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	dataset := datasets["defender-users"]
	hostile := strings.Repeat("\x01", maximumContinuationParentIDLength)
	escaped := url.PathEscape(hostile)
	if len(escaped) <= len(hostile) {
		t.Fatalf("path escaping of a control-character identifier is %d bytes for %d raw ones", len(escaped), len(hostile))
	}
	child := dataset
	child.Kind = KindODataGET
	child.Path = strings.ReplaceAll(dataset.ChildPath, "{parentId}", escaped)
	first, err := client.envelopeStartURL(child, time.Time{}, time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) > maximumContinuationURLLength {
		t.Fatalf("a full-length parent identifier produces a %d-byte child URL, over the %d-byte bound; the two bounds are incompatible",
			len(first), maximumContinuationURLLength)
	}
	if err := client.validateDatasetURL(dataset, first); err != nil {
		t.Fatalf("the child URL a bounded identifier produces was refused: %v", err)
	}
	// One byte over the dispatch bound is refused, before a token is acquired.
	tooLong := "https://api.securitycenter.microsoft.com/api/machines?pad=" +
		strings.Repeat("a", maximumContinuationURLLength+1-len("https://api.securitycenter.microsoft.com/api/machines?pad="))
	if len(tooLong) != maximumContinuationURLLength+1 {
		t.Fatalf("built a %d-byte URL, want %d", len(tooLong), maximumContinuationURLLength+1)
	}
	if err := client.validateDatasetURL(dataset, tooLong); !errors.Is(err, errIncompatibleContinuation) {
		t.Fatalf("a %d-byte dispatch URL error=%v, want an incompatible-continuation error", len(tooLong), err)
	}
}

// TestOversizedSourceContinuationFailsWithoutPersistingTheCursor covers the
// accept side of the URL bound end to end. The source publishes a nextLink one
// byte over it, which must fail the poll with the previous checkpoint intact --
// no oversized cursor on disk, no watermark advanced past pages that were never
// delivered -- and the window has to collect normally once the source behaves.
func TestOversizedSourceContinuationFailsWithoutPersistingTheCursor(t *testing.T) {
	oversized := atomic.Bool{}
	oversized.Store(true)
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		if !oversized.Load() {
			fmt.Fprint(w, `{"value":[{"id":"user-2"}]}`)
			return
		}
		prefix := server.URL + "/v1.0/users?pad="
		fmt.Fprintf(w, `{"value":[{"id":"user-1"}],"@odata.nextLink":%q}`,
			prefix+strings.Repeat("a", maximumContinuationURLLength+1-len(prefix)))
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newSelectorTestPlugin(t, server, "entra-users")
	key := stateKey("microsoft", "entra-users", "")

	err := func() error { _, err := plugin.Handle(t.Context(), rt); return err }()
	if err == nil {
		t.Fatal("an oversized source continuation was accepted")
	}
	if !strings.Contains(err.Error(), "continuation URL exceeds") {
		t.Fatalf("error=%v, want the URL bound named", err)
	}
	if strings.Contains(err.Error(), "aaaa") {
		t.Fatalf("error %v carries the vendor URL", err)
	}
	// Nothing was persisted: no cursor document, no watermark, no entries.
	if encoded, err := rt.Get(key + "/cursor"); err == nil {
		t.Fatalf("an oversized continuation persisted a cursor document: %s", encoded)
	}
	if _, err := rt.GetTime(key + "/timestamp"); err == nil {
		t.Fatal("an oversized continuation advanced the durable watermark")
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries=%d, want the chunk discarded whole", got)
	}

	// The window is not wedged: once the source publishes a continuation
	// inside the bound, the same window collects and completes.
	oversized.Store(false)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("recovered entries=%d, want 1", got)
	}
	if watermark, err := rt.GetTime(key + "/timestamp"); err != nil || watermark.IsZero() {
		t.Fatalf("watermark=%s error=%v, want the completed window published", watermark, err)
	}
}

// TestOversizedSourceSkipTokenFailsWithoutPersistingTheCursor covers the accept
// side of the skip-token bound. A Resource Graph skip token is published in the
// response body rather than as a URL, so it is the one continuation value the
// URL bound says nothing about, and it is both re-sent in every paged request
// body and persisted in the cursor.
func TestOversizedSourceSkipTokenFailsWithoutPersistingTheCursor(t *testing.T) {
	oversized := atomic.Bool{}
	oversized.Store(true)
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		calls.Add(1)
		if !oversized.Load() {
			fmt.Fprint(w, `{"data":[{"id":"resource-1"}],"resultTruncated":false}`)
			return
		}
		fmt.Fprintf(w, `{"data":[{"id":"resource-1"}],"skipToken":%q,"resultTruncated":true}`,
			strings.Repeat("a", maximumContinuationSkipTokenLength+1))
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	const subscription = "33333333-3333-3333-3333-333333333333"
	plugin := repeatedCursorPlugin(t, server, "azure-resource-inventory", []string{subscription})
	key := stateKey("microsoft", "azure-resource-inventory", subscription)

	if _, err := plugin.Handle(t.Context(), rt); err == nil {
		t.Fatal("an oversized skip token was accepted")
	} else if !strings.Contains(err.Error(), "skip token exceeds") {
		t.Fatalf("error=%v, want the skip-token bound named", err)
	} else if strings.Contains(err.Error(), "aaaa") {
		t.Fatalf("error %v carries the vendor token", err)
	}
	if encoded, err := rt.Get(key + "/cursor"); err == nil {
		t.Fatalf("an oversized skip token persisted a cursor document: %s", encoded)
	}
	if _, err := rt.GetTime(key + "/timestamp"); err == nil {
		t.Fatal("an oversized skip token advanced the durable watermark")
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries=%d, want the chunk discarded whole", got)
	}

	oversized.Store(false)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("recovered entries=%d, want 1", got)
	}
}

// TestOversizedSourceParentIDFailsWithoutPersistingTheCursor covers the accept
// side of the per-identifier bound. The parent list publishes an identifier one
// byte over it, which must fail the poll rather than be silently dropped from
// the fan-out or retained in the cursor.
func TestOversizedSourceParentIDFailsWithoutPersistingTheCursor(t *testing.T) {
	oversized := atomic.Bool{}
	oversized.Store(true)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
		case strings.HasSuffix(r.URL.Path, "/logonusers"):
			fmt.Fprint(w, `{"value":[{"accountName":"alice"}]}`)
		case strings.HasSuffix(r.URL.Path, "/api/machines"):
			if !oversized.Load() {
				fmt.Fprint(w, `{"value":[{"id":"machine-1"}]}`)
				return
			}
			fmt.Fprintf(w, `{"value":[{"id":%q}]}`, strings.Repeat("a", maximumContinuationParentIDLength+1))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newSelectorTestPlugin(t, server, "defender-users")
	key := stateKey("microsoft", "defender-users", "")

	if _, err := plugin.Handle(t.Context(), rt); err == nil {
		t.Fatal("an oversized parent identifier was accepted")
	} else if !strings.Contains(err.Error(), "parent identifier exceeds") {
		t.Fatalf("error=%v, want the per-identifier bound named", err)
	} else if strings.Contains(err.Error(), "aaaa") {
		t.Fatalf("error %v carries the vendor identifier", err)
	}
	if encoded, err := rt.Get(key + "/cursor"); err == nil {
		t.Fatalf("an oversized parent identifier persisted a cursor document: %s", encoded)
	}
	if _, err := rt.GetTime(key + "/timestamp"); err == nil {
		t.Fatal("an oversized parent identifier advanced the durable watermark")
	}

	oversized.Store(false)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("recovered entries=%d, want the one child record", got)
	}
}

// TestCursorDocumentCeilingRefusesPersistenceBeforeAnyKeyIsWritten is the
// backstop proof. The per-field bounds are what a well-behaved path stays
// inside; this is what guarantees the document itself, and it has to fail
// before the first Put so a partially written checkpoint cannot replace a
// usable one.
func TestCursorDocumentCeilingRefusesPersistenceBeforeAnyKeyIsWritten(t *testing.T) {
	rt := newMicrosoftTestRuntime(t)
	key := stateKey("microsoft", "defender-users", "")
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	// A cursor the per-field bounds admit persists.
	inside := streamState{
		WindowStart: now.Add(-time.Hour), WindowEnd: now,
		Overlap:      &temporalOverlap{Version: temporalOverlapVersion},
		Continuation: &FetchContinuation{Version: fetchContinuationVersion, Kind: KindParentChild, ParentNextURL: "https://host.invalid/api/machines"},
	}
	if err := saveState(rt, key, inside); err != nil {
		t.Fatalf("an ordinary cursor was refused: %v", err)
	}
	baseline, err := rt.Get(key + "/cursor")
	if err != nil {
		t.Fatal(err)
	}

	// A document over the ceiling is refused, and nothing it would have
	// written reaches the store.
	parents := make([]string, 0, maximumContinuationParents)
	for encoded := 0; encoded <= maximumCursorDocumentBytes; encoded += continuationParentIDBytes(strings.Repeat("a", maximumContinuationParentIDLength)) {
		parents = append(parents, strings.Repeat("a", maximumContinuationParentIDLength))
	}
	oversized := inside
	oversized.Continuation = &FetchContinuation{Version: fetchContinuationVersion, Kind: KindParentChild, ParentIDs: parents}
	oversized.Watermark = now
	err = saveState(rt, key, oversized)
	if err == nil {
		t.Fatalf("a cursor document over the %d-byte ceiling was persisted", maximumCursorDocumentBytes)
	}
	if !strings.Contains(err.Error(), "ceiling") {
		t.Fatalf("error=%v, want the document ceiling named", err)
	}
	after, err := rt.Get(key + "/cursor")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, baseline) {
		t.Fatalf("the refused document replaced the previous cursor: %d bytes", len(after))
	}
	if _, err := rt.GetTime(key + "/timestamp"); err == nil {
		t.Fatal("a refused cursor document still advanced the durable watermark")
	}
}
