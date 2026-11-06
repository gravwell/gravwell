package microsoft

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const managementCapacityContentPath = "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/"

type managementCapacityContent struct {
	id      string
	created time.Time
}

func managementDescriptorAt(serverURL, contentType string, content managementCapacityContent) string {
	return fmt.Sprintf(`{"contentType":%q,"contentId":%q,"contentUri":%q,"contentCreated":%q,"contentExpiration":%q}`,
		contentType, content.id, serverURL+managementCapacityContentPath+content.id,
		content.created.Format(time.RFC3339), content.created.Add(managementMaximumHistory).Format(time.RFC3339))
}

// managementCapacityServer lists contents filtered by the requested half-open
// window, the way the source does, and serves one event per content.
func managementCapacityServer(t *testing.T, contents []managementCapacityContent, fetched map[string]*atomic.Int64) *httptest.Server {
	t.Helper()
	for _, content := range contents {
		fetched[content.id] = &atomic.Int64{}
	}
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			start, err := time.Parse(time.RFC3339, r.URL.Query().Get("startTime"))
			if err != nil {
				t.Errorf("startTime=%q: %v", r.URL.Query().Get("startTime"), err)
			}
			end, err := time.Parse(time.RFC3339, r.URL.Query().Get("endTime"))
			if err != nil {
				t.Errorf("endTime=%q: %v", r.URL.Query().Get("endTime"), err)
			}
			listed := make([]string, 0, len(contents))
			for _, content := range contents {
				if content.created.Before(start) || !content.created.Before(end) {
					continue
				}
				listed = append(listed, managementDescriptorAt(server.URL, "Audit.Exchange", content))
			}
			_, _ = fmt.Fprintf(w, "[%s]", strings.Join(listed, ","))
		case strings.Contains(r.URL.Path, managementCapacityContentPath):
			id := strings.TrimPrefix(r.URL.Path, managementCapacityContentPath)
			counter, ok := fetched[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			counter.Add(1)
			_, _ = io.WriteString(w, fmt.Sprintf(
				`[{"Id":%q,"CreationTime":"2026-10-08T11:40:00Z","Operation":"MailItemsAccessed"}]`, id))
		default:
			http.NotFound(w, r)
		}
	})
	return server
}

func drainManagementSelector(t *testing.T, plugin *Microsoft, rt *microsoftTestRuntime) int {
	t.Helper()
	for pass := 1; pass <= 8; pass++ {
		continuation, err := plugin.Handle(t.Context(), rt)
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if continuation == nil {
			t.Fatalf("pass %d returned no continuation", pass)
		}
		if continuation.Delay != 0 {
			return pass
		}
	}
	t.Fatal("selector never finished its bounded catch-up")
	return 0
}

// TestManagementReceiptCapacityRecoversInsteadOfStalling pins the receipt bound
// as recoverable. Overflow used to return a hard error with no state write and
// nothing that could clear it: the next poll resumed at the same content index,
// re-fetched the same content, found it already fully delivered, appended the
// same receipt and overflowed again, so a tenant producing more content blobs
// per retention window than the bound wedged that selector permanently -- and
// because the selector kept reporting pending work it also held the whole
// stanza in drain mode.
func TestManagementReceiptCapacityRecoversInsteadOfStalling(t *testing.T) {
	contents := []managementCapacityContent{
		{id: "content-oldest", created: managementTestNow.Add(-3 * time.Hour)},
		{id: "content-middle", created: managementTestNow.Add(-2 * time.Hour)},
		{id: "content-newest", created: managementTestNow.Add(-1 * time.Hour)},
	}
	fetched := make(map[string]*atomic.Int64)
	server := managementCapacityServer(t, contents, fetched)
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	plugin.managementReceiptLimit = 2

	drainManagementSelector(t, plugin, rt)
	if got := len(rt.Entries()); got != len(contents) {
		t.Fatalf("entries=%d, want one per content blob", got)
	}
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	state, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Receipts) != 2 {
		t.Fatalf("retained receipts=%d, want the capacity bound respected", len(state.Receipts))
	}
	// The oldest receipt by ContentCreated is the one dropped, and the content
	// index has long since advanced past it.
	for _, receipt := range state.Receipts {
		if receipt.ContentID == "content-oldest" {
			t.Fatalf("capacity trimmed a newer receipt: %+v", state.Receipts)
		}
	}
	if got := rt.logsContaining("WARN", "dropped the oldest delivery receipts"); len(got) != 1 {
		t.Fatalf("capacity warnings=%d, want exactly 1 (%+v)", len(got), got)
	}

	// A later poll must neither stall nor re-deliver the trimmed content.
	plugin.now = func() time.Time { return managementTestNow.Add(time.Hour) }
	drainManagementSelector(t, plugin, rt)
	if got := len(rt.Entries()); got != len(contents) {
		t.Fatalf("entries after the following poll=%d, want no redelivery", got)
	}
	for id, counter := range fetched {
		if got := counter.Load(); got != 1 {
			t.Fatalf("content %s fetched %d times, want exactly 1", id, got)
		}
	}
}

// TestManagementDescriptorCapacityNarrowsTheWindowInsteadOfStalling pins the
// descriptor bound as recoverable. A window that published more content than the
// state document may hold used to fail with a capacity error, and the next poll
// planned the same window and failed at the same descriptor forever. The window
// must narrow instead, and the remainder must still be collected exactly once.
func TestManagementDescriptorCapacityNarrowsTheWindowInsteadOfStalling(t *testing.T) {
	contents := []managementCapacityContent{
		{id: "content-early", created: managementTestNow.Add(-20 * time.Hour)},
		{id: "content-middle", created: managementTestNow.Add(-10 * time.Hour)},
		{id: "content-late", created: managementTestNow.Add(-2 * time.Hour)},
	}
	fetched := make(map[string]*atomic.Int64)
	server := managementCapacityServer(t, contents, fetched)
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	plugin.managementDescriptorLimit = 2

	passes := drainManagementSelector(t, plugin, rt)
	if passes < 2 {
		t.Fatalf("catch-up finished in %d passes; the narrowed window must leave a remainder", passes)
	}
	if got := len(rt.Entries()); got != len(contents) {
		t.Fatalf("entries=%d, want one per content blob", got)
	}
	for id, counter := range fetched {
		if got := counter.Load(); got != 1 {
			t.Fatalf("content %s fetched %d times, want exactly 1", id, got)
		}
	}
	warnings := rt.logsContaining("WARN", "listing window narrowed at descriptor capacity")
	if len(warnings) == 0 {
		t.Fatal("the descriptor bound was not reported to the operator")
	}
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	state, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if !state.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s, want the whole planned range %s", state.CompletedThrough, managementTestNow)
	}
	if len(state.Contents) > plugin.descriptorCapacity() {
		t.Fatalf("descriptors=%d exceeded the bound the narrowing exists to hold", len(state.Contents))
	}
}

// TestNarrowManagementWindowHalvesDownToItsFloor pins the arithmetic, including
// the floor that stays a hard error: more than a capacity's worth of content
// blobs inside one minute is not a shape the source produces.
func TestNarrowManagementWindowHalvesDownToItsFloor(t *testing.T) {
	for _, tc := range []struct {
		name     string
		span     time.Duration
		want     time.Duration
		narrowed bool
	}{
		{name: "maximum-window", span: managementMaximumWindow, want: 12 * time.Hour, narrowed: true},
		{name: "above-the-floor", span: 90 * time.Second, want: managementMinimumWindow, narrowed: true},
		{name: "at-the-floor", span: managementMinimumWindow},
		{name: "below-the-floor", span: managementMinimumWindow / 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := managementState{
				Version:     managementStateVersion,
				WindowStart: managementTestNow.Add(-tc.span), WindowEnd: managementTestNow,
				Contents: []managementContent{{ContentType: "Audit.Exchange", ContentID: "a", ContentURI: "https://manage.office.com/a"}},
			}
			if got := narrowManagementWindow(&state); got != tc.narrowed {
				t.Fatalf("narrowed=%t, want %t", got, tc.narrowed)
			}
			if !tc.narrowed {
				return
			}
			if got := state.WindowEnd.Sub(state.WindowStart); got != tc.want {
				t.Fatalf("narrowed span=%s, want %s", got, tc.want)
			}
			if state.ListingComplete || state.NextPage != "" || state.Pages != 0 || len(state.Contents) != 0 || state.ContentIndex != 0 {
				t.Fatalf("narrowing left stale listing progress: %+v", state)
			}
			if err := validateManagementState(state); err != nil {
				t.Fatalf("narrowed window is not persistable: %v", err)
			}
		})
	}
}

// TestTrimManagementReceiptsDropsOnlyTheOldest pins which receipts are dropped
// and that the survivors keep their order.
func TestTrimManagementReceiptsDropsOnlyTheOldest(t *testing.T) {
	receipts := []managementReceipt{
		{ContentID: "c", ContentCreated: managementTestNow.Add(-1 * time.Hour)},
		{ContentID: "a", ContentCreated: managementTestNow.Add(-5 * time.Hour)},
		{ContentID: "d", ContentCreated: managementTestNow},
		{ContentID: "b", ContentCreated: managementTestNow.Add(-3 * time.Hour)},
	}
	kept, dropped := trimManagementReceipts(receipts, 2)
	if len(dropped) != 2 {
		t.Fatalf("dropped=%d, want 2", len(dropped))
	}
	// The dropped receipts are named, not just counted, so the membership index
	// can forget exactly those identifiers instead of being rebuilt.
	if dropped[0].ContentID != "a" || dropped[1].ContentID != "b" {
		t.Fatalf("dropped=%+v, want the two oldest by ContentCreated", dropped)
	}
	if len(kept) != 2 || kept[0].ContentID != "c" || kept[1].ContentID != "d" {
		t.Fatalf("kept=%+v, want the two newest in their original order", kept)
	}
	if kept, dropped := trimManagementReceipts(receipts, len(receipts)); len(dropped) != 0 || len(kept) != len(receipts) {
		t.Fatalf("a set at capacity was trimmed: kept=%d dropped=%d", len(kept), len(dropped))
	}
}

// denseManagementArray builds a JSON array holding elements copies of element
// followed by text no JSON decoder can parse.
//
// The trailing garbage is what makes the bounded-decode regressions
// non-vacuous. A decoder that reads the array whole before its count bound
// applies -- which is what decoding into a []T did -- fails on the garbage, so
// the error names a JSON syntax problem. One that streams and applies the bound
// to each element before retaining it stops at the bound and never reaches the
// garbage, so the error names the bound.
func denseManagementArray(element string, elements int, tail string) string {
	return "[" + strings.Repeat(element+",", elements) + tail + "]"
}

// Every Management Activity array applies its count bound before the element
// that exceeds it is retained, rather than decoding the whole array first. The
// subscription, descriptor and content arrays all decoded whole into a slice,
// so a response that was inside managementMaximumResponseBody but dense in
// elements was fully materialized before any limit was consulted.
func TestManagementArraysApplyTheirBoundBeforeRetention(t *testing.T) {
	t.Run("subscription-list", func(t *testing.T) {
		const limit = 4
		const subscription = `{"contentType":"Audit.Exchange","status":"enabled"}`
		body := denseManagementArray(subscription, limit+1, "!!!not-json!!!")
		subscriptions, err := decodeManagementSubscriptions([]byte(body), limit)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("exceeds %d entries", limit)) {
			t.Fatalf("error=%v, want the count bound: the array was decoded whole first", err)
		}
		if subscriptions != nil {
			t.Fatalf("a refused subscription list was retained: %d entries", len(subscriptions))
		}
		// Exactly the bound is still accepted.
		atBound := "[" + strings.TrimSuffix(strings.Repeat(subscription+",", limit), ",") + "]"
		if got, err := decodeManagementSubscriptions([]byte(atBound), limit); err != nil || len(got) != limit {
			t.Fatalf("a list at the bound was refused: entries=%d error=%v", len(got), err)
		}
	})

	t.Run("content-list", func(t *testing.T) {
		const limit = 3
		descriptor := `{"contentType":"Audit.Exchange","contentId":"c","contentUri":"https://manage.office.com/a","contentCreated":"2026-10-08T11:30:00Z"}`
		body := denseManagementArray(descriptor, limit+1, "!!!not-json!!!")
		wire, err := decodeManagementContentPage([]byte(body), limit)
		if !errors.Is(err, errManagementContentPageOverflow) {
			t.Fatalf("error=%v, want the page overflow sentinel: the array was decoded whole first", err)
		}
		if wire != nil {
			t.Fatalf("a refused content page was retained: %d descriptors", len(wire))
		}
	})

	t.Run("content-events", func(t *testing.T) {
		const limit = 5
		body := denseManagementArray(`{"Id":"e"}`, limit+1, "!!!not-json!!!")
		events, err := decodeManagementEvents([]byte(body), limit)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("exceeds %d events", limit)) {
			t.Fatalf("error=%v, want the count bound: the array was decoded whole first", err)
		}
		if events != nil {
			t.Fatalf("a refused content blob was retained: %d events", len(events))
		}
	})

	// A non-array document, a null document and trailing data stay rejected, and
	// an empty array stays a legitimate empty answer.
	for _, tc := range []struct{ name, body, want string }{
		{"object", `{"Id":"one"}`, "must be a JSON array"},
		{"null", `null`, "must be a JSON array"},
		{"trailing", `[] []`, "trailing data"},
	} {
		t.Run("shape-"+tc.name, func(t *testing.T) {
			if _, err := decodeManagementEvents([]byte(tc.body), managementMaximumEventsPerContent); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want one naming %q", err, tc.want)
			}
		})
	}
	if events, err := decodeManagementEvents([]byte(`[]`), managementMaximumEventsPerContent); err != nil || len(events) != 0 {
		t.Fatalf("an empty content blob was refused: events=%d error=%v", len(events), err)
	}
}

// A single listing page carrying more descriptors than the state document may
// hold for a whole window narrows the window and lists again. The bound has to
// be recoverable: failing would repeat on the same page on every poll, and the
// window's content must still be collected exactly once.
func TestManagementDescriptorDensePageNarrowsInsteadOfStalling(t *testing.T) {
	contents := []managementCapacityContent{
		{id: "content-aa", created: managementTestNow.Add(-20 * time.Hour)},
		{id: "content-bb", created: managementTestNow.Add(-14 * time.Hour)},
		{id: "content-cc", created: managementTestNow.Add(-8 * time.Hour)},
		{id: "content-dd", created: managementTestNow.Add(-2 * time.Hour)},
	}
	fetched := make(map[string]*atomic.Int64)
	server := managementCapacityServer(t, contents, fetched)
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	// The decoder admits one descriptor past the capacity, so a 4-descriptor
	// page is refused by the decoder itself rather than by the listing loop.
	plugin.managementDescriptorLimit = 2
	if plugin.contentPageCapacity() != 3 {
		t.Fatalf("page capacity=%d, want the descriptor capacity plus one", plugin.contentPageCapacity())
	}

	if passes := drainManagementSelector(t, plugin, rt); passes < 2 {
		t.Fatalf("catch-up finished in %d passes; the narrowed window must leave a remainder", passes)
	}
	if got := len(rt.Entries()); got != len(contents) {
		t.Fatalf("entries=%d, want one per content blob", got)
	}
	for id, counter := range fetched {
		if got := counter.Load(); got != 1 {
			t.Fatalf("content %s fetched %d times, want exactly 1", id, got)
		}
	}
	if got := rt.logsContaining("WARN", "narrowed at a descriptor-dense page"); len(got) == 0 {
		t.Fatal("the descriptor-dense page was not reported to the operator")
	}
	state, err := loadManagementState(rt, managementStateKey("microsoft", "microsoft365-audit-exchange"))
	if err != nil {
		t.Fatal(err)
	}
	if !state.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s, want the whole planned range %s", state.CompletedThrough, managementTestNow)
	}
}

// managementTruncatingListingServer lists contents filtered by the requested
// half-open window, the way managementCapacityServer does, and publishes a
// NextPageUri on another origin entirely for every listing whose requested
// window is wider than retained. validateManagementURL refuses a continuation
// off the configured Management origin, so the page that carries it is marked
// truncated and the listing ends at it without the continuation ever being
// dispatched -- which is what sink counts.
func managementTruncatingListingServer(t *testing.T, contents []managementCapacityContent,
	fetched map[string]*atomic.Int64, sink *httptest.Server, retained time.Duration) *httptest.Server {
	t.Helper()
	for _, content := range contents {
		fetched[content.id] = &atomic.Int64{}
	}
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			start, err := time.Parse(time.RFC3339, r.URL.Query().Get("startTime"))
			if err != nil {
				t.Errorf("startTime=%q: %v", r.URL.Query().Get("startTime"), err)
			}
			end, err := time.Parse(time.RFC3339, r.URL.Query().Get("endTime"))
			if err != nil {
				t.Errorf("endTime=%q: %v", r.URL.Query().Get("endTime"), err)
			}
			if end.Sub(start) > retained {
				w.Header().Set("NextPageUri", sink.URL+"/capture?contentType=Audit.Exchange")
			}
			listed := make([]string, 0, len(contents))
			for _, content := range contents {
				if content.created.Before(start) || !content.created.Before(end) {
					continue
				}
				listed = append(listed, managementDescriptorAt(server.URL, "Audit.Exchange", content))
			}
			_, _ = fmt.Fprintf(w, "[%s]", strings.Join(listed, ","))
		case strings.Contains(r.URL.Path, managementCapacityContentPath):
			id := strings.TrimPrefix(r.URL.Path, managementCapacityContentPath)
			counter, ok := fetched[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			counter.Add(1)
			_, _ = io.WriteString(w, fmt.Sprintf(
				`[{"Id":%q,"CreationTime":"2026-10-08T11:40:00Z","Operation":"MailItemsAccessed"}]`, id))
		default:
			http.NotFound(w, r)
		}
	})
	return server
}

// TestManagementNarrowedPageDoesNotReportItsDiscardedTruncation pins the
// pass-local truncation report against the page that is discarded by a
// narrowing.
//
// A page publishing a continuation the URL policy refuses sets that report and
// ends the listing at itself. When that same page's descriptors then cross a
// capacity bound, the page is discarded: the window is halved, the descriptors
// and pages of the whole prefix are given back, and the narrowed window lists
// the interval again from its own start. The report survived the discard, so a
// narrowed listing that went on to complete cleanly -- carrying no refused
// continuation anywhere in the state document, and with every descriptor of the
// interval collected -- still warned that the window had been listed only as
// far as a refused page, still published that on the status channel the poll
// loop leaves alone, and still returned it as the pass's error. The poll failed
// having done nothing wrong, and the operator was told an interval had been
// skipped that was in fact collected in full.
//
// The retained-page case is the other half of the same report: a page the
// listing really does end at must still be reported, so the reset cannot be the
// thing that hides a genuine truncation.
func TestManagementNarrowedPageDoesNotReportItsDiscardedTruncation(t *testing.T) {
	// Both halves list the same three descriptors in the first 24-hour window,
	// and the narrowed 12-hour window holds exactly the first two.
	contents := []managementCapacityContent{
		{id: "content-aa", created: managementTestNow.Add(-20 * time.Hour)},
		{id: "content-bb", created: managementTestNow.Add(-18 * time.Hour)},
		{id: "content-cc", created: managementTestNow.Add(-2 * time.Hour)},
	}
	for _, tc := range []struct {
		name string
		// descriptorLimit decides whether the page that carries the refused
		// continuation is discarded by a narrowing or retained.
		descriptorLimit int
		reported        bool
	}{
		{name: "discarded-page", descriptorLimit: 2},
		{name: "retained-page", descriptorLimit: len(contents), reported: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sinkCalls atomic.Int64
			sink := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				sinkCalls.Add(1)
			}))
			defer sink.Close()
			fetched := make(map[string]*atomic.Int64)
			server := managementTruncatingListingServer(t, contents, fetched, sink, 12*time.Hour)
			defer server.Close()

			rt := newMicrosoftTestRuntime(t)
			plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
			plugin.managementDescriptorLimit = tc.descriptorLimit

			if tc.reported {
				// Nothing narrows: the listing ends at the refused continuation
				// and the loss is this pass's error.
				_, err := plugin.Handle(t.Context(), rt)
				if err == nil || !strings.Contains(err.Error(), "a listing continuation") {
					t.Fatalf("error=%v, want the refused continuation returned as the pass error", err)
				}
				if got := rt.logsContaining("WARN", "refused a listing continuation"); len(got) != 1 {
					t.Fatalf("truncation warnings=%d, want exactly 1 (%+v)", len(got), got)
				}
				if got := rt.warnStateContaining("continuation(s) the source published"); len(got) == 0 {
					t.Fatal("a genuinely truncated window was not published on the status channel")
				}
				if sinkCalls.Load() != 0 {
					t.Fatalf("the refused continuation was dispatched %d time(s)", sinkCalls.Load())
				}
				return
			}

			// drainManagementSelector fails the test on any pass error, which is
			// what the false report returned.
			if passes := drainManagementSelector(t, plugin, rt); passes < 2 {
				t.Fatalf("catch-up finished in %d passes; the narrowed window must leave a remainder", passes)
			}
			if got := rt.logsContaining("WARN", "narrowed at descriptor capacity"); len(got) == 0 {
				t.Fatal("the descriptor bound the page crossed was not reported to the operator")
			}
			if got := rt.logsContaining("WARN", "refused a listing continuation"); len(got) != 0 {
				t.Fatalf("a discarded page's truncation was reported %d time(s): %+v", len(got), got)
			}
			if got := rt.warnStateContaining("continuation(s) the source published"); len(got) != 0 {
				t.Fatalf("a discarded page's truncation reached the status channel: %q", got)
			}
			if got := len(rt.Entries()); got != len(contents) {
				t.Fatalf("entries=%d, want one per content blob", got)
			}
			for id, counter := range fetched {
				if got := counter.Load(); got != 1 {
					t.Fatalf("content %s fetched %d times, want exactly 1", id, got)
				}
			}
			if sinkCalls.Load() != 0 {
				t.Fatalf("the refused continuation was dispatched %d time(s)", sinkCalls.Load())
			}
			state, err := loadManagementState(rt, managementStateKey("microsoft", "microsoft365-audit-exchange"))
			if err != nil {
				t.Fatal(err)
			}
			if !state.CompletedThrough.Equal(managementTestNow) {
				t.Fatalf("completed through %s, want the whole planned range %s", state.CompletedThrough, managementTestNow)
			}
			if state.RefusedContents != 0 || state.RefusedUnreported != 0 {
				t.Fatalf("a clean listing charged refusals: %+v", state)
			}
		})
	}
}

// The cumulative descriptor-byte budget narrows the window the same way the
// count bound does. managementMaximumDescriptors bounded how many descriptors a
// window retained but not how large they were, and a descriptor is mostly a
// source-supplied contentUri, so the listing grew the persisted document across
// pages with nothing bounding the sum.
func TestManagementDescriptorByteBudgetNarrowsInsteadOfGrowing(t *testing.T) {
	contents := []managementCapacityContent{
		{id: "content-aa", created: managementTestNow.Add(-20 * time.Hour)},
		{id: "content-bb", created: managementTestNow.Add(-10 * time.Hour)},
		{id: "content-cc", created: managementTestNow.Add(-2 * time.Hour)},
	}
	fetched := make(map[string]*atomic.Int64)
	server := managementCapacityServer(t, contents, fetched)
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	// Every descriptor this server publishes is the same size, so a budget of
	// two admits exactly two and the third narrows the window.
	probe := managementContent{
		ContentType: "Audit.Exchange", ContentID: contents[0].id,
		ContentURI:        server.URL + managementCapacityContentPath + contents[0].id,
		ContentCreated:    contents[0].created,
		ContentExpiration: contents[0].created.Add(managementMaximumHistory),
	}
	plugin.managementDescriptorByteLimit = 2 * managementDescriptorBytes(probe)

	if passes := drainManagementSelector(t, plugin, rt); passes < 2 {
		t.Fatalf("catch-up finished in %d passes; the narrowed window must leave a remainder", passes)
	}
	if got := len(rt.Entries()); got != len(contents) {
		t.Fatalf("entries=%d, want one per content blob", got)
	}
	for id, counter := range fetched {
		if got := counter.Load(); got != 1 {
			t.Fatalf("content %s fetched %d times, want exactly 1", id, got)
		}
	}
	if got := rt.logsContaining("WARN", "narrowed at descriptor byte capacity"); len(got) == 0 {
		t.Fatal("the descriptor byte budget was not reported to the operator")
	}
	state, err := loadManagementState(rt, managementStateKey("microsoft", "microsoft365-audit-exchange"))
	if err != nil {
		t.Fatal(err)
	}
	// The whole planned range still completed: narrowing leaves a remainder for
	// the following window rather than advancing past unfinished work.
	if !state.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s, want the whole planned range %s", state.CompletedThrough, managementTestNow)
	}
	if state.ContentIndex != 0 || len(state.Contents) != 0 {
		t.Fatalf("converged state retained listing progress: %+v", state)
	}
}

// The descriptor-byte accounting has to track the set exactly, because the
// listing loop decides whether to narrow from it rather than re-measuring the
// whole set once per listed descriptor.
func TestManagementDescriptorByteAccountingTracksTheSet(t *testing.T) {
	descriptor := func(id, uri string) managementContent {
		return managementContent{ContentType: "Audit.Exchange", ContentID: id, ContentURI: uri,
			ContentCreated: managementTestNow}
	}
	state := &managementState{Version: managementStateVersion}
	first, second := descriptor("a", "https://manage.office.com/a"), descriptor("b", "https://manage.office.com/b")
	state.appendContent(first)
	state.appendContent(second)
	want := managementDescriptorBytes(first) + managementDescriptorBytes(second)
	if got := state.descriptorBytes(); got != want {
		t.Fatalf("accounted bytes=%d, want %d", got, want)
	}
	// A refreshed descriptor for the same contentId applies its delta.
	longer := descriptor("b", "https://manage.office.com/b"+strings.Repeat("x", 128))
	state.replaceContent(1, longer)
	want = managementDescriptorBytes(first) + managementDescriptorBytes(longer)
	if got := state.descriptorBytes(); got != want {
		t.Fatalf("accounted bytes after replace=%d, want %d", got, want)
	}
	// Rebuilding the index from the decoded set reaches the same total, which is
	// what a reload has to agree with.
	state.indexMembership()
	if got := state.descriptorBytes(); got != want {
		t.Fatalf("reindexed bytes=%d, want %d", got, want)
	}
	state.resetContents()
	if got := state.descriptorBytes(); got != 0 {
		t.Fatalf("reset bytes=%d, want 0", got)
	}

	// The bound is durable, not only a listing-time guard: a descriptor set
	// already over it is refused by the persist path and by every load.
	oversized := make([]managementContent, 0, 4200)
	huge := descriptor("x", "https://manage.office.com/"+strings.Repeat("u", managementMaximumURLLength-32))
	for len(oversized)*managementDescriptorBytes(huge) <= managementMaximumDescriptorBytes {
		oversized = append(oversized, huge)
	}
	if err := validateManagementDescriptors(oversized); err == nil ||
		!strings.Contains(err.Error(), fmt.Sprintf("exceed %d bytes", managementMaximumDescriptorBytes)) {
		t.Fatalf("an oversized descriptor set was accepted: %v", err)
	}
	if err := validateManagementDescriptors(oversized[:len(oversized)-1]); err != nil {
		t.Fatalf("a descriptor set inside the budget was refused: %v", err)
	}
}

// A contentUri and the descriptor timestamps are source-controlled and
// persisted, so both carry explicit bounds. The URL bound applies through the
// one validator every Management Activity URL passes.
func TestManagementDescriptorFieldsAreBounded(t *testing.T) {
	server := managementTestServer(t, func(http.ResponseWriter, *http.Request) {})
	defer server.Close()
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	dataset := datasets["microsoft365-audit-exchange"]
	base := server.URL + managementCapacityContentPath

	long := managementContentWire{
		ContentType: dataset.ContentType, ContentID: "content-1",
		ContentURI:     base + strings.Repeat("u", managementMaximumURLLength),
		ContentCreated: "2026-10-08T11:30:00Z",
	}
	if _, err := plugin.decodeManagementContent(dataset, long); err == nil ||
		!strings.Contains(err.Error(), fmt.Sprintf("exceeds %d bytes", managementMaximumURLLength)) {
		t.Fatalf("an unbounded contentUri was accepted: %v", err)
	}
	for _, field := range []string{"created", "expiration"} {
		item := managementContentWire{
			ContentType: dataset.ContentType, ContentID: "content-1",
			ContentURI: base + "content-1", ContentCreated: "2026-10-08T11:30:00Z",
		}
		oversized := "2026-10-08T11:30:00Z" + strings.Repeat("0", managementMaximumTimestampLength)
		if field == "created" {
			item.ContentCreated = oversized
		} else {
			item.ContentExpiration = oversized
		}
		if _, err := plugin.decodeManagementContent(dataset, item); err == nil ||
			!strings.Contains(err.Error(), "oversized timestamp") {
			t.Fatalf("an unbounded content%s was accepted: %v", field, err)
		}
	}
	// The ordinary descriptor this server publishes still decodes.
	ordinary := managementContentWire{
		ContentType: dataset.ContentType, ContentID: "content-1",
		ContentURI: base + "content-1", ContentCreated: "2026-10-08T11:30:00Z",
		ContentExpiration: "2026-10-15T11:30:00Z",
	}
	if _, err := plugin.decodeManagementContent(dataset, ordinary); err != nil {
		t.Fatalf("an ordinary descriptor was refused: %v", err)
	}
}

// A skip-dense blob holds more malformed events than one pass may charge, so
// the bound has to stop the pass part way through that one blob at least twice
// before any event in it is deliverable.
const (
	managementSkipDenseMalformed = 2*managementMaximumEntriesPerHandle + 400
	managementSkipDenseValid     = 100
	managementSkipDrainBound     = 16
	// The progress key is republished alongside the head document by every
	// checkpoint as well as by the per-event delivery write, so a pass costs a
	// few writes of it that the per-event bound does not govern. The slack
	// covers those and nothing like a per-event run: an uncharged skip path
	// writes it once per malformed event, which is thousands.
	managementSkipCheckpointSlack = 64
)

// managementSkipAccountingRuntime counts the durable work one Handle call
// performs. The per-event delivery checkpoint is the expensive part of a skip:
// each one writes and synchronizes the progress key, logs, and republishes the
// status warning, so those are what a per-pass bound has to cap. resetPassCounters
// zeroes them between passes; the delivery loop is sequential, so the counters
// need no lock of their own.
type managementSkipAccountingRuntime struct {
	*microsoftTestRuntime
	progressKey  string
	progressPuts int
	statusWarns  int
}

func (r *managementSkipAccountingRuntime) Put(key string, value []byte) error {
	if key == r.progressKey {
		r.progressPuts++
	}
	return r.microsoftTestRuntime.Put(key, value)
}

func (r *managementSkipAccountingRuntime) SetWarn(message string) {
	r.statusWarns++
	r.microsoftTestRuntime.SetWarn(message)
}

func (r *managementSkipAccountingRuntime) resetPassCounters() {
	r.progressPuts, r.statusWarns = 0, 0
}

// managementSkipDenseBody serves a blob whose leading events are JSON numbers.
// decodeManagementEvents retains them -- the wire array is well formed -- and
// prepareManagementEvent then refuses each one as not a JSON object, which is
// exactly the bounded per-event skip. A number costs a handful of bytes, so the
// blob carries thousands of them without the test depending on the
// oversized-entry bound and its megabyte-per-event cost.
func managementSkipDenseBody() string {
	var body strings.Builder
	body.WriteByte('[')
	for ordinal := 0; ordinal < managementSkipDenseMalformed; ordinal++ {
		if ordinal != 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, "%d", ordinal)
	}
	for ordinal := 0; ordinal < managementSkipDenseValid; ordinal++ {
		fmt.Fprintf(&body, `,{"Id":"dense-%d","CreationTime":"2026-10-08T11:40:00Z","Operation":"MailItemsAccessed"}`, ordinal)
	}
	body.WriteByte(']')
	return body.String()
}

// managementSkipDenseServer lists two descriptors in one window. The first
// carries the skip-dense blob; the second is an ordinary single-event blob and
// exists only so a pass that ignores the bound can be caught running on past
// the descriptor it was supposed to stop inside.
func managementSkipDenseServer(t *testing.T, fetched map[string]*atomic.Int64) *httptest.Server {
	t.Helper()
	contents := []managementCapacityContent{
		{id: "content-dense", created: managementTestNow.Add(-time.Hour)},
		{id: "content-second", created: managementTestNow.Add(-30 * time.Minute)},
	}
	for _, content := range contents {
		fetched[content.id] = &atomic.Int64{}
	}
	dense := managementSkipDenseBody()
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			start, err := time.Parse(time.RFC3339, r.URL.Query().Get("startTime"))
			if err != nil {
				t.Errorf("startTime=%q: %v", r.URL.Query().Get("startTime"), err)
			}
			end, err := time.Parse(time.RFC3339, r.URL.Query().Get("endTime"))
			if err != nil {
				t.Errorf("endTime=%q: %v", r.URL.Query().Get("endTime"), err)
			}
			listed := make([]string, 0, len(contents))
			for _, content := range contents {
				if content.created.Before(start) || !content.created.Before(end) {
					continue
				}
				listed = append(listed, managementDescriptorAt(server.URL, "Audit.Exchange", content))
			}
			_, _ = fmt.Fprintf(w, "[%s]", strings.Join(listed, ","))
		case strings.Contains(r.URL.Path, managementCapacityContentPath):
			id := strings.TrimPrefix(r.URL.Path, managementCapacityContentPath)
			counter, ok := fetched[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			counter.Add(1)
			if id == "content-dense" {
				_, _ = io.WriteString(w, dense)
				return
			}
			_, _ = fmt.Fprintf(w, `[{"Id":%q,"CreationTime":"2026-10-08T11:45:00Z","Operation":"MailItemsAccessed"}]`, id)
		default:
			http.NotFound(w, r)
		}
	})
	return server
}

// TestManagementSkipDenseContentChargesEveryAttemptAgainstThePassBound pins the
// per-pass processing bound as a bound on attempted events rather than on
// accepted ones.
//
// The delivery loop charged only the events it wrote. A malformed event took
// the skip branch, which advances the ordinal, writes and synchronizes the
// delivery checkpoint, logs, and republishes the status warning -- and then
// resumed the loop without charging anything at all. A blob of malformed events
// therefore ran to its own end inside one Handle call: up to
// managementMaximumEventsPerContent durable synchronizations and status
// warnings, with cancellation observed nowhere between them, and because the
// shared counter was still zero when the blob finished, that same pass went on
// into every remaining descriptor.
//
// Charging every attempt against the same counter makes a skip cost exactly
// what a delivered event costs. The ordinal the skip advanced is already
// durable, so the pass yields at the bound and the next one resumes from it.
func TestManagementSkipDenseContentChargesEveryAttemptAgainstThePassBound(t *testing.T) {
	fetched := make(map[string]*atomic.Int64)
	server := managementSkipDenseServer(t, fetched)
	defer server.Close()

	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	rt := &managementSkipAccountingRuntime{
		microsoftTestRuntime: newMicrosoftTestRuntime(t),
		progressKey:          managementProgressKey(key),
	}
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)

	// Pass one lands entirely inside the malformed run, so every unit of work it
	// performs is a skip and the bound is the only thing that can stop it.
	rt.resetPassCounters()
	continuation, err := plugin.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if rt.progressPuts > managementMaximumEntriesPerHandle+managementSkipCheckpointSlack {
		t.Fatalf("delivery checkpoint writes in one pass=%d, want at most the per-pass bound of %d",
			rt.progressPuts, managementMaximumEntriesPerHandle)
	}
	// Only the skip path publishes a status warning on this pass, so the count
	// is exactly how many events the pass charged.
	if rt.statusWarns != managementMaximumEntriesPerHandle {
		t.Fatalf("status warnings in one pass=%d, want exactly the per-pass bound of %d, one per attempted event",
			rt.statusWarns, managementMaximumEntriesPerHandle)
	}
	if rt.progressPuts == 0 {
		t.Fatal("the pass charged nothing at all, so the bound claim is vacuous")
	}
	if continuation == nil || continuation.Delay != 0 {
		t.Fatalf("first continuation=%v, want the pass to yield with immediate pending work", continuation)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries after the first pass=%d, want none: the bound is reached inside the malformed run", got)
	}
	if got := fetched["content-second"].Load(); got != 0 {
		t.Fatalf("second descriptor requests=%d, want 0: a pass stopped by the bound may not run on into more descriptors", got)
	}

	// The ordinal that pass reached is durable, and it is exactly the bound: the
	// skip path advances one ordinal per charge.
	afterFirst, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatalf("state after the first pass: %v", err)
	}
	if afterFirst.EventIndex != managementMaximumEntriesPerHandle {
		t.Fatalf("persisted event ordinal=%d, want the per-pass bound of %d charged one skip at a time",
			afterFirst.EventIndex, managementMaximumEntriesPerHandle)
	}
	if afterFirst.ContentIndex != 0 || afterFirst.ActiveContentID != "content-dense" {
		t.Fatalf("persisted continuation=%+v, want the pass resumable inside the skip-dense blob", afterFirst)
	}
	if afterFirst.SkippedEvents != managementMaximumEntriesPerHandle {
		t.Fatalf("skipped accounting=%d, want %d", afterFirst.SkippedEvents, managementMaximumEntriesPerHandle)
	}

	// Every later pass is bounded the same way and strictly advances that
	// ordinal, so the blob drains in a bounded number of passes and terminates.
	previous, passes := afterFirst, 1
	for ; passes <= managementSkipDrainBound; passes++ {
		rt.resetPassCounters()
		continuation, err = plugin.Handle(t.Context(), rt)
		if err != nil {
			t.Fatalf("pass %d: %v", passes+1, err)
		}
		if continuation == nil {
			t.Fatalf("pass %d returned no continuation", passes+1)
		}
		if rt.progressPuts > managementMaximumEntriesPerHandle+managementSkipCheckpointSlack {
			t.Fatalf("pass %d delivery checkpoint writes=%d, want at most %d",
				passes+1, rt.progressPuts, managementMaximumEntriesPerHandle)
		}
		if rt.statusWarns > managementMaximumEntriesPerHandle {
			t.Fatalf("pass %d status warnings=%d, want at most %d", passes+1, rt.statusWarns, managementMaximumEntriesPerHandle)
		}
		current, err := loadManagementState(rt, key)
		if err != nil {
			t.Fatalf("state after pass %d: %v", passes+1, err)
		}
		if current.ContentIndex == previous.ContentIndex && current.ActiveContentID == previous.ActiveContentID &&
			current.EventIndex <= previous.EventIndex && current.CompletedThrough.Equal(previous.CompletedThrough) {
			t.Fatalf("pass %d made no progress: %+v then %+v", passes+1, previous, current)
		}
		previous = current
		if continuation.Delay != 0 {
			break
		}
	}
	if passes > managementSkipDrainBound {
		t.Fatal("the skip-dense selector never finished its bounded catch-up")
	}

	// Termination leaves exactly the deliverable events, every malformed one
	// accounted for once, and the window closed.
	if got := len(rt.Entries()); got != managementSkipDenseValid+1 {
		t.Fatalf("entries=%d, want %d from the dense blob plus 1 from the second descriptor",
			got, managementSkipDenseValid)
	}
	final, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if final.SkippedEvents != managementSkipDenseMalformed {
		t.Fatalf("skipped events=%d, want each malformed event charged exactly once (%d)",
			final.SkippedEvents, managementSkipDenseMalformed)
	}
	if final.hasPending() || final.CompletedThrough.IsZero() {
		t.Fatalf("final state=%+v, want the window completed with nothing pending", final)
	}
	if got := fetched["content-second"].Load(); got != 1 {
		t.Fatalf("second descriptor requests=%d, want exactly 1 once the dense blob drained", got)
	}
}

// The three descriptor-disposal branches of the delivery loop -- an
// already-receipted descriptor, a stored contentUri this build's URL policy
// refuses, and a blob the source can no longer serve -- each advance
// ContentIndex and synchronize, and two of them also append a receipt, report
// permanent loss and republish a status warning. None of them charged the
// shared per-pass counter, so one Handle call could dispose of the entire
// descriptor set: up to managementMaximumDescriptors durable synchronizations
// and operator warnings with cancellation observed nowhere in between. A
// Tenant-ID or Management-Host change makes that the ordinary case rather than
// the rare one, because it invalidates every stored descriptor at once.
const (
	// A descriptor set a few past the per-pass bound, so the bound has to stop
	// the first pass part way through it and a second pass has to finish it.
	managementDisposalDescriptors = managementMaximumEntriesPerHandle + 5
	managementDisposalRemainder   = managementDisposalDescriptors - managementMaximumEntriesPerHandle
	// The head document and the state synchronization are also charged by the
	// subscription check and by the checkpoint that ends a pass, which the
	// per-disposal charge does not govern. The slack covers those few and
	// nothing like a per-descriptor run, which is a thousand.
	managementDisposalSlack       = 16
	managementDisposalSelector    = "microsoft365-audit-exchange"
	managementDisposalContentType = "Audit.Exchange"
)

// managementDisposalRuntime counts the state-document writes one Handle call
// performs. Every disposal republishes the head document, so the count is what
// a per-pass bound has to cap. The delivery loop is sequential, so the counter
// needs no lock of its own.
type managementDisposalRuntime struct {
	*microsoftTestRuntime
	headKey  string
	headPuts int
}

func (r *managementDisposalRuntime) Put(key string, value []byte) error {
	if key == r.headKey {
		r.headPuts++
	}
	return r.microsoftTestRuntime.Put(key, value)
}

// statusWarns reports how many operator status warnings have been published so
// far. Two of the three disposal branches publish one each, so this is the
// operator-visible half of the same amplification.
func (r *managementDisposalRuntime) statusWarns() int {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	return len(r.warnState)
}

// seedManagementDisposalWindow publishes a durable state document whose listing
// is already complete over contents, so the pass under test starts in the
// delivery loop. receipted additionally records a delivery receipt for every
// descriptor, which is what an overlapping listing leaves behind.
func seedManagementDisposalWindow(t *testing.T, plugin *Microsoft, rt *managementDisposalRuntime, key string, contents []managementContent, receipted bool) {
	t.Helper()
	stored := managementState{
		Version:        managementStateVersion,
		WindowStart:    managementTestNow.Add(-time.Hour),
		WindowEnd:      managementTestNow,
		TargetThrough:  managementTestNow,
		ListingStarted: true, ListingComplete: true,
	}
	for _, content := range contents {
		stored.appendContent(content)
		if receipted {
			stored.appendReceipt(managementReceipt{ContentID: content.ContentID, ContentCreated: content.ContentCreated})
		}
	}
	if err := plugin.persistManagementCheckpoint(rt, key, &stored); err != nil {
		t.Fatalf("seed the durable descriptor set: %v", err)
	}
	// The seeded document has to be the one the pass actually reads back, or
	// every bound this test asserts would be measured against something else.
	reloaded, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatalf("seeded state does not load: %v", err)
	}
	if len(reloaded.Contents) != len(contents) || reloaded.ContentIndex != 0 || !reloaded.ListingComplete {
		t.Fatalf("seeded descriptors=%d index=%d listed=%v, want %d, 0 and true",
			len(reloaded.Contents), reloaded.ContentIndex, reloaded.ListingComplete, len(contents))
	}
}

// managementDisposalContents builds managementDisposalDescriptors descriptors
// whose contentUri comes from uri, so one generator serves the receipted run
// and both configuration-overlay refusals.
func managementDisposalContents(uri func(id string) string) []managementContent {
	contents := make([]managementContent, 0, managementDisposalDescriptors)
	for ordinal := 0; ordinal < managementDisposalDescriptors; ordinal++ {
		id := fmt.Sprintf("disposal-%05d", ordinal)
		contents = append(contents, managementContent{
			ContentType:       managementDisposalContentType,
			ContentID:         id,
			ContentURI:        uri(id),
			ContentCreated:    managementTestNow.Add(-time.Hour).Add(time.Duration(ordinal) * time.Millisecond),
			ContentExpiration: managementTestNow.Add(managementMaximumHistory),
		})
	}
	return contents
}

// managementDisposalServer serves the subscription listing and an empty content
// listing. Every other request is counted: a receipted or refused descriptor is
// disposed of without being read at all, so any content request at all is a
// failure of the branch under test.
func managementDisposalServer(t *testing.T, contentRequests *atomic.Int64) *httptest.Server {
	t.Helper()
	return managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, managementDisposalContentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = io.WriteString(w, "[]")
		default:
			contentRequests.Add(1)
			http.NotFound(w, r)
		}
	})
}

// TestManagementReceiptedDescriptorsChargeThePassBound pins the
// already-receipted disposal branch against the shared per-pass bound.
//
// An overlapping listing re-lists content this selector has already delivered,
// so a whole window can be nothing but receipted descriptors. Each one advanced
// ContentIndex and wrote and synchronized the head document while charging
// nothing, so the pass ran to the end of the descriptor set in one
// uncancellable call. Charging it costs the same as a delivered event: the pass
// yields at managementMaximumEntriesPerHandle and the next one resumes from the
// ContentIndex this one published.
func TestManagementReceiptedDescriptorsChargeThePassBound(t *testing.T) {
	var contentRequests atomic.Int64
	server := managementDisposalServer(t, &contentRequests)
	defer server.Close()

	key := managementStateKey("microsoft", managementDisposalSelector)
	rt := &managementDisposalRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(t), headKey: key}
	syncer := &managementSyncController{}
	plugin := newManagementTestPlugin(t, server, managementDisposalSelector, syncer.Sync)
	contents := managementDisposalContents(func(id string) string {
		return server.URL + managementCapacityContentPath + id
	})
	// The contentUri of every descriptor is one this build's URL policy accepts,
	// so the receipt is the only reason any of them is disposed of unread.
	if err := plugin.client.validateManagementURL(contents[0].ContentURI); err != nil {
		t.Fatalf("the seeded contentUri is refused (%v), so this test would exercise the refusal branch instead", err)
	}
	seedManagementDisposalWindow(t, plugin, rt, key, contents, true)

	// Pass one lands entirely inside the receipted run, so every unit of work it
	// performs is a receipted disposal and the bound is the only thing that can
	// stop it.
	rt.headPuts = 0
	syncsBefore := syncer.calls.Load()
	continuation, err := plugin.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if continuation == nil || continuation.Delay != 0 {
		t.Fatalf("first continuation=%v, want the pass to yield with immediate pending work", continuation)
	}
	if rt.headPuts > managementMaximumEntriesPerHandle+managementDisposalSlack {
		t.Fatalf("state document writes in one pass=%d, want at most the per-pass bound of %d",
			rt.headPuts, managementMaximumEntriesPerHandle)
	}
	if rt.headPuts == 0 {
		t.Fatal("the pass performed no durable disposal at all, so the bound claim is vacuous")
	}
	if syncs := syncer.calls.Load() - syncsBefore; syncs > managementMaximumEntriesPerHandle+managementDisposalSlack {
		t.Fatalf("state synchronizations in one pass=%d, want at most the per-pass bound of %d",
			syncs, managementMaximumEntriesPerHandle)
	}
	if got := contentRequests.Load(); got != 0 {
		t.Fatalf("content requests=%d, want none: a receipted descriptor is disposed of without being read", got)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries=%d, want none from an entirely receipted descriptor set", got)
	}
	if got := rt.statusWarns(); got != 0 {
		t.Fatalf("status warnings=%d, want none: a receipted descriptor is ordinary progress, not loss", got)
	}

	// The index that pass reached is durable and it is exactly the bound: the
	// receipted branch advances one descriptor per charge.
	afterFirst, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatalf("state after the first pass: %v", err)
	}
	if afterFirst.ContentIndex != managementMaximumEntriesPerHandle {
		t.Fatalf("persisted content index=%d, want exactly the per-pass bound of %d charged one descriptor at a time",
			afterFirst.ContentIndex, managementMaximumEntriesPerHandle)
	}
	if !afterFirst.CompletedThrough.IsZero() {
		t.Fatalf("the first pass completed the window at %s; a pass stopped by the bound may not run on through the whole descriptor set",
			afterFirst.CompletedThrough)
	}
	if !afterFirst.hasPending() {
		t.Fatal("the yielded pass left nothing pending, so the remainder would never be collected")
	}

	// A fresh plugin against the same state store is a process restart: the
	// remainder is finished from the durable index and the first thousand
	// descriptors are not disposed of a second time.
	rt.headPuts = 0
	syncsBefore = syncer.calls.Load()
	plugin = newManagementTestPlugin(t, server, managementDisposalSelector, syncer.Sync)
	continuation, err = plugin.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("resumed pass: %v", err)
	}
	if continuation == nil || continuation.Delay == 0 {
		t.Fatalf("resumed continuation=%v, want the window finished", continuation)
	}
	if rt.headPuts > managementDisposalRemainder+managementDisposalSlack {
		t.Fatalf("resumed state document writes=%d, want at most the %d remaining descriptors; the pass re-disposed of work the first one published",
			rt.headPuts, managementDisposalRemainder)
	}
	if syncs := syncer.calls.Load() - syncsBefore; syncs > managementDisposalRemainder+managementDisposalSlack {
		t.Fatalf("resumed state synchronizations=%d, want at most the %d remaining descriptors", syncs, managementDisposalRemainder)
	}
	if got := contentRequests.Load(); got != 0 {
		t.Fatalf("content requests=%d after the resumed pass, want none", got)
	}

	final, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatalf("state after the resumed pass: %v", err)
	}
	if !final.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s, want the seeded window end %s", final.CompletedThrough, managementTestNow)
	}
	if final.hasPending() || len(final.Contents) != 0 || final.ContentIndex != 0 {
		t.Fatalf("finished window retained %d descriptors at index %d (pending=%v)",
			len(final.Contents), final.ContentIndex, final.hasPending())
	}
	if final.SkippedContents != 0 || final.SkippedEvents != 0 {
		t.Fatalf("permanent-loss accounting=%+v, want none: nothing was lost", final)
	}
}

// TestManagementRefusedStoredContentURLsChargeThePassBound pins the
// stored-contentUri refusal branch against the shared per-pass bound, in the
// configuration-overlay shape that makes it a whole descriptor set at once
// rather than one descriptor.
//
// A Tenant-ID or Management-Host change invalidates every descriptor a previous
// configuration persisted: none of them can ever be fetched again, so all of
// them take the refusal branch on the next poll. Each refusal appends a
// receipt, reports permanent loss, republishes the operator status warning, and
// writes and synchronizes the head document -- and charged nothing, so one
// Handle call performed the whole set of them. Charging each one yields the pass
// at managementMaximumEntriesPerHandle with the loss reported exactly once per
// descriptor and the rest left for the next pass.
func TestManagementRefusedStoredContentURLsChargeThePassBound(t *testing.T) {
	const refusalMessage = "refused by the Microsoft Management Activity URL policy"
	for _, tc := range []struct {
		name string
		uri  func(serverURL, id string) string
	}{
		{
			// Written under a previous Tenant-ID: the origin still matches but
			// the feed path addresses another tenant's subtree.
			name: "tenant-id-overlay",
			uri: func(serverURL, id string) string {
				return serverURL + "/api/v1.0/99999999-9999-9999-9999-999999999999/activity/feed/audit/" + id
			},
		},
		{
			// Written under a previous Management-Host: this tenant's feed path
			// on an origin the configuration no longer names. Nothing is
			// dispatched to it, which is the point of the branch.
			name: "management-host-overlay",
			uri: func(_, id string) string {
				return "https://previous-management-host.invalid" + managementCapacityContentPath + id
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var contentRequests atomic.Int64
			server := managementDisposalServer(t, &contentRequests)
			defer server.Close()

			key := managementStateKey("microsoft", managementDisposalSelector)
			rt := &managementDisposalRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(t), headKey: key}
			syncer := &managementSyncController{}
			plugin := newManagementTestPlugin(t, server, managementDisposalSelector, syncer.Sync)
			contents := managementDisposalContents(func(id string) string { return tc.uri(server.URL, id) })
			// The overlay has to be one this build's policy actually refuses, or
			// the descriptors would be fetched and this test would bound another
			// branch entirely.
			if err := plugin.client.validateManagementURL(contents[0].ContentURI); err == nil {
				t.Fatalf("the configuration overlay %q is accepted, so the refusal branch is never reached", contents[0].ContentURI)
			}
			seedManagementDisposalWindow(t, plugin, rt, key, contents, false)

			rt.headPuts = 0
			syncsBefore := syncer.calls.Load()
			continuation, err := plugin.Handle(t.Context(), rt)
			if err != nil {
				t.Fatalf("first pass: %v", err)
			}
			if continuation == nil || continuation.Delay != 0 {
				t.Fatalf("first continuation=%v, want the pass to yield with immediate pending work", continuation)
			}
			if rt.headPuts > managementMaximumEntriesPerHandle+managementDisposalSlack {
				t.Fatalf("state document writes in one pass=%d, want at most the per-pass bound of %d",
					rt.headPuts, managementMaximumEntriesPerHandle)
			}
			if rt.headPuts == 0 {
				t.Fatal("the pass performed no durable disposal at all, so the bound claim is vacuous")
			}
			if syncs := syncer.calls.Load() - syncsBefore; syncs > managementMaximumEntriesPerHandle+managementDisposalSlack {
				t.Fatalf("state synchronizations in one pass=%d, want at most the per-pass bound of %d",
					syncs, managementMaximumEntriesPerHandle)
			}
			// The loss report and the status warning are the operator-visible
			// half of the amplification, and the bound caps them exactly.
			if got := len(rt.logsContaining("ERROR", refusalMessage)); got != managementMaximumEntriesPerHandle {
				t.Fatalf("permanent-loss reports in one pass=%d, want exactly the per-pass bound of %d, one per refused descriptor",
					got, managementMaximumEntriesPerHandle)
			}
			if got := rt.statusWarns(); got != managementMaximumEntriesPerHandle {
				t.Fatalf("status warnings in one pass=%d, want exactly the per-pass bound of %d",
					got, managementMaximumEntriesPerHandle)
			}
			if got := contentRequests.Load(); got != 0 {
				t.Fatalf("content requests=%d, want none: a refused contentUri is never dispatched", got)
			}
			if got := len(rt.Entries()); got != 0 {
				t.Fatalf("entries=%d, want none from a refused descriptor set", got)
			}

			afterFirst, err := loadManagementState(rt, key)
			if err != nil {
				t.Fatalf("state after the first pass: %v", err)
			}
			if afterFirst.ContentIndex != managementMaximumEntriesPerHandle {
				t.Fatalf("persisted content index=%d, want exactly the per-pass bound of %d charged one descriptor at a time",
					afterFirst.ContentIndex, managementMaximumEntriesPerHandle)
			}
			if afterFirst.SkippedContents != managementMaximumEntriesPerHandle {
				t.Fatalf("permanent-loss accounting=%d, want %d", afterFirst.SkippedContents, managementMaximumEntriesPerHandle)
			}
			if !afterFirst.CompletedThrough.IsZero() {
				t.Fatalf("the first pass completed the window at %s; a pass stopped by the bound may not run on through the whole descriptor set",
					afterFirst.CompletedThrough)
			}
			if !afterFirst.hasPending() {
				t.Fatal("the yielded pass left nothing pending, so the remainder would never be collected")
			}

			// A process restart finishes the remainder from the durable index,
			// and the refusals the first pass already reported are not reported
			// again.
			rt.headPuts = 0
			syncsBefore = syncer.calls.Load()
			plugin = newManagementTestPlugin(t, server, managementDisposalSelector, syncer.Sync)
			continuation, err = plugin.Handle(t.Context(), rt)
			if err != nil {
				t.Fatalf("resumed pass: %v", err)
			}
			if continuation == nil || continuation.Delay == 0 {
				t.Fatalf("resumed continuation=%v, want the window finished", continuation)
			}
			if rt.headPuts > managementDisposalRemainder+managementDisposalSlack {
				t.Fatalf("resumed state document writes=%d, want at most the %d remaining descriptors; the pass re-disposed of work the first one published",
					rt.headPuts, managementDisposalRemainder)
			}
			if syncs := syncer.calls.Load() - syncsBefore; syncs > managementDisposalRemainder+managementDisposalSlack {
				t.Fatalf("resumed state synchronizations=%d, want at most the %d remaining descriptors", syncs, managementDisposalRemainder)
			}
			if got := len(rt.logsContaining("ERROR", refusalMessage)); got != managementDisposalDescriptors {
				t.Fatalf("permanent-loss reports in total=%d, want exactly one per refused descriptor (%d)",
					got, managementDisposalDescriptors)
			}
			if got := contentRequests.Load(); got != 0 {
				t.Fatalf("content requests=%d after the resumed pass, want none", got)
			}

			final, err := loadManagementState(rt, key)
			if err != nil {
				t.Fatalf("state after the resumed pass: %v", err)
			}
			if !final.CompletedThrough.Equal(managementTestNow) {
				t.Fatalf("completed through %s, want the seeded window end %s", final.CompletedThrough, managementTestNow)
			}
			if final.hasPending() || len(final.Contents) != 0 || final.ContentIndex != 0 {
				t.Fatalf("finished window retained %d descriptors at index %d (pending=%v)",
					len(final.Contents), final.ContentIndex, final.hasPending())
			}
			if final.SkippedContents != managementDisposalDescriptors {
				t.Fatalf("permanent-loss total=%d, want exactly one per refused descriptor (%d)",
					final.SkippedContents, managementDisposalDescriptors)
			}
			// Every refusal leaves a receipt, so an overlapping listing cannot
			// resurrect a descriptor this selector has already given up on.
			if len(final.Receipts) != managementDisposalDescriptors {
				t.Fatalf("receipts=%d, want one per refused descriptor (%d)", len(final.Receipts), managementDisposalDescriptors)
			}
			for _, content := range contents {
				if !final.hasReceipt(content.ContentID) {
					t.Fatalf("descriptor %s was skipped without a receipt", content.ContentID)
				}
			}
		})
	}
}

// The unavailable branch costs two vendor requests per descriptor -- the refused
// read and the refresh that cannot find the identifier again -- so the per-pass
// request budget of maximumFetchRequests, not managementMaximumEntriesPerHandle,
// is what stops a pass made of nothing but those. The bound under test is
// therefore approached by first delivering a blob of ordinary events, which is
// the charge the loop always made, and the unavailable descriptors that follow
// have to carry the pass the rest of the way to it. That keeps the request
// budget far from binding: one pass issues the subscription check, one content
// read, and two requests per unavailable descriptor.
const (
	managementUnavailablePrecharge = managementMaximumEntriesPerHandle - 50
	managementUnavailableDisposals = 100
	managementUnavailableFirstPass = managementMaximumEntriesPerHandle - managementUnavailablePrecharge
)

// managementUnavailablePrechargeBody serves one ordinary blob of deliverable
// events, which charges the pass up to just short of the bound.
func managementUnavailablePrechargeBody() string {
	var body strings.Builder
	body.WriteByte('[')
	for ordinal := 0; ordinal < managementUnavailablePrecharge; ordinal++ {
		if ordinal != 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"Id":"bulk-%04d","CreationTime":"2026-10-08T11:40:00Z","Operation":"MailItemsAccessed"}`, ordinal)
	}
	body.WriteByte(']')
	return body.String()
}

// TestManagementUnavailableContentChargesThePassBound pins the
// content-unavailable disposal branch against the shared per-pass bound.
//
// A window whose blobs have aged out of the source's seven-day retention is
// reported as bounded permanent loss, descriptor by descriptor. Each loss
// appended a receipt, reported the loss, republished the status warning, and
// wrote and synchronized the head document while charging nothing, so the pass
// carried straight on into every remaining descriptor after a delivered blob
// had already reached the bound. Charging each one yields the pass there
// instead and the next one resumes from the durable ContentIndex.
func TestManagementUnavailableContentChargesThePassBound(t *testing.T) {
	const lossMessage = "no longer available from the source"
	var bulkRequests, goneRequests, refreshes atomic.Int64
	bulkBody := managementUnavailablePrechargeBody()
	server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, managementDisposalContentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			// The refresh each unavailable descriptor attempts. The identifier
			// is simply absent, which is what the source does once a blob has
			// aged out of its retention.
			refreshes.Add(1)
			_, _ = io.WriteString(w, "[]")
		case strings.HasSuffix(r.URL.Path, managementCapacityContentPath+"content-bulk"):
			bulkRequests.Add(1)
			_, _ = io.WriteString(w, bulkBody)
		case strings.Contains(r.URL.Path, managementCapacityContentPath+"content-gone-"):
			goneRequests.Add(1)
			w.WriteHeader(http.StatusGone)
			_, _ = io.WriteString(w, `{"error":{"code":"AF20051","message":"expired"}}`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	key := managementStateKey("microsoft", managementDisposalSelector)
	rt := &managementDisposalRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(t), headKey: key}
	syncer := &managementSyncController{}
	plugin := newManagementTestPlugin(t, server, managementDisposalSelector, syncer.Sync)

	contents := []managementContent{{
		ContentType:       managementDisposalContentType,
		ContentID:         "content-bulk",
		ContentURI:        server.URL + managementCapacityContentPath + "content-bulk",
		ContentCreated:    managementTestNow.Add(-time.Hour),
		ContentExpiration: managementTestNow.Add(managementMaximumHistory),
	}}
	for ordinal := 0; ordinal < managementUnavailableDisposals; ordinal++ {
		id := fmt.Sprintf("content-gone-%03d", ordinal)
		contents = append(contents, managementContent{
			ContentType:       managementDisposalContentType,
			ContentID:         id,
			ContentURI:        server.URL + managementCapacityContentPath + id,
			ContentCreated:    managementTestNow.Add(-time.Hour).Add(time.Duration(ordinal+1) * time.Millisecond),
			ContentExpiration: managementTestNow.Add(managementMaximumHistory),
		})
	}
	seedManagementDisposalWindow(t, plugin, rt, key, contents, false)

	rt.headPuts = 0
	syncsBefore := syncer.calls.Load()
	continuation, err := plugin.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if continuation == nil || continuation.Delay != 0 {
		t.Fatalf("first continuation=%v, want the pass to yield with immediate pending work", continuation)
	}
	if syncs := syncer.calls.Load() - syncsBefore; syncs > managementMaximumEntriesPerHandle+managementDisposalSlack {
		t.Fatalf("state synchronizations in one pass=%d, want at most the per-pass bound of %d",
			syncs, managementMaximumEntriesPerHandle)
	}
	// The precharge is the deliverable blob, and the unavailable descriptors
	// carry the pass exactly the rest of the way to the bound and no further.
	if got := len(rt.Entries()); got != managementUnavailablePrecharge {
		t.Fatalf("entries=%d, want the %d deliverable events that charge the pass toward the bound",
			got, managementUnavailablePrecharge)
	}
	if got := len(rt.logsContaining("ERROR", lossMessage)); got != managementUnavailableFirstPass {
		t.Fatalf("permanent-loss reports in one pass=%d, want the %d that fit under the bound after the delivered blob",
			got, managementUnavailableFirstPass)
	}
	if got := goneRequests.Load(); got != int64(managementUnavailableFirstPass) {
		t.Fatalf("unavailable content reads=%d, want %d: a pass stopped by the bound may not run on into more descriptors",
			got, managementUnavailableFirstPass)
	}
	if got := refreshes.Load(); got != int64(managementUnavailableFirstPass) {
		t.Fatalf("refresh listings=%d, want one per disposed descriptor (%d)", got, managementUnavailableFirstPass)
	}
	if got := bulkRequests.Load(); got != 1 {
		t.Fatalf("deliverable blob reads=%d, want exactly 1", got)
	}
	if got := rt.statusWarns(); got != managementUnavailableFirstPass {
		t.Fatalf("status warnings=%d, want one per reported loss (%d)", got, managementUnavailableFirstPass)
	}

	afterFirst, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatalf("state after the first pass: %v", err)
	}
	// One descriptor for the delivered blob plus exactly the disposals the bound
	// still had room for.
	if afterFirst.ContentIndex != managementUnavailableFirstPass+1 {
		t.Fatalf("persisted content index=%d, want the delivered blob plus the %d charged disposals",
			afterFirst.ContentIndex, managementUnavailableFirstPass)
	}
	if afterFirst.SkippedContents != managementUnavailableFirstPass {
		t.Fatalf("permanent-loss accounting=%d, want %d", afterFirst.SkippedContents, managementUnavailableFirstPass)
	}
	if !afterFirst.CompletedThrough.IsZero() {
		t.Fatalf("the first pass completed the window at %s; a pass stopped by the bound may not run on through the whole descriptor set",
			afterFirst.CompletedThrough)
	}
	if !afterFirst.hasPending() {
		t.Fatal("the yielded pass left nothing pending, so the remainder would never be collected")
	}

	// A process restart finishes the remainder from the durable index. The blob
	// already delivered is not read again, so nothing is redelivered.
	rt.headPuts = 0
	syncsBefore = syncer.calls.Load()
	plugin = newManagementTestPlugin(t, server, managementDisposalSelector, syncer.Sync)
	continuation, err = plugin.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("resumed pass: %v", err)
	}
	if continuation == nil || continuation.Delay == 0 {
		t.Fatalf("resumed continuation=%v, want the window finished", continuation)
	}
	const remaining = managementUnavailableDisposals - managementUnavailableFirstPass
	if syncs := syncer.calls.Load() - syncsBefore; syncs > remaining+managementDisposalSlack {
		t.Fatalf("resumed state synchronizations=%d, want at most the %d remaining descriptors", syncs, remaining)
	}
	if got := bulkRequests.Load(); got != 1 {
		t.Fatalf("deliverable blob reads=%d in total, want the one the first pass made", got)
	}
	if got := len(rt.Entries()); got != managementUnavailablePrecharge {
		t.Fatalf("entries=%d in total, want the %d the first pass delivered", got, managementUnavailablePrecharge)
	}
	if got := goneRequests.Load(); got != managementUnavailableDisposals {
		t.Fatalf("unavailable content reads=%d in total, want exactly one per descriptor (%d)", got, managementUnavailableDisposals)
	}
	if got := len(rt.logsContaining("ERROR", lossMessage)); got != managementUnavailableDisposals {
		t.Fatalf("permanent-loss reports in total=%d, want exactly one per unavailable descriptor (%d)",
			got, managementUnavailableDisposals)
	}

	final, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatalf("state after the resumed pass: %v", err)
	}
	if !final.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s, want the seeded window end %s", final.CompletedThrough, managementTestNow)
	}
	if final.hasPending() || len(final.Contents) != 0 || final.ContentIndex != 0 {
		t.Fatalf("finished window retained %d descriptors at index %d (pending=%v)",
			len(final.Contents), final.ContentIndex, final.hasPending())
	}
	if final.SkippedContents != managementUnavailableDisposals {
		t.Fatalf("permanent-loss total=%d, want exactly one per unavailable descriptor (%d)",
			final.SkippedContents, managementUnavailableDisposals)
	}
	for _, content := range contents {
		if !final.hasReceipt(content.ContentID) {
			t.Fatalf("descriptor %s finished without a receipt", content.ContentID)
		}
	}
}

// managementReceiptCapacityWarning is the operator-visible half of receipt
// maintenance. It is published once per maintenance pass, not once per evicted
// receipt, which is what the batching regressions below measure.
const managementReceiptCapacityWarning = "dropped the oldest delivery receipts"

// newReceiptBudgetPlugin builds a plugin with no transport: the regressions
// below drive the receipt set and the checkpoint path directly, so both bounds
// are measured where they are enforced rather than through a server fixture.
func newReceiptBudgetPlugin(t *testing.T, receipts, receiptBytes int) *Microsoft {
	t.Helper()
	plugin := NewWithStateSync(&Config{}, func() error { return nil })
	plugin.now = func() time.Time { return managementTestNow }
	plugin.managementReceiptLimit = receipts
	plugin.managementReceiptByteLimit = receiptBytes
	return plugin
}

// appendReceiptsAt records count receipts through the production insertion path,
// so every regression here exercises what the delivery loop actually calls.
func appendReceiptsAt(state *managementState, prefix string, count int, at func(ordinal int) time.Time) {
	for ordinal := range count {
		recordManagementReceipt(state, managementContent{
			ContentID:      fmt.Sprintf("%s-%05d", prefix, ordinal),
			ContentCreated: at(ordinal),
		})
	}
}

func receiptIDs(receipts []managementReceipt) []string {
	ids := make([]string, 0, len(receipts))
	for _, receipt := range receipts {
		ids = append(ids, receipt.ContentID)
	}
	return ids
}

// sumManagementReceiptBytes measures the set the slow way, so the accounting the
// store maintains incrementally cannot drift from it unnoticed.
func sumManagementReceiptBytes(receipts []managementReceipt) int {
	total := 0
	for _, receipt := range receipts {
		total += managementReceiptBytes(receipt)
	}
	return total
}

// assertReceiptSetIsConsistent checks the three things every maintenance pass
// has to leave true: the surviving receipts are exactly the expected ones in
// their original order, the membership index names exactly those, and the byte
// accounting matches them.
func assertReceiptSetIsConsistent(t *testing.T, state *managementState, wantIDs []string) {
	t.Helper()
	if got := receiptIDs(state.Receipts); !slices.Equal(got, wantIDs) {
		t.Fatalf("receipts=%v, want %v", got, wantIDs)
	}
	store := state.ensureStore()
	if len(store.receiptIDs) != len(state.Receipts) {
		t.Fatalf("membership index holds %d identifiers for %d receipts", len(store.receiptIDs), len(state.Receipts))
	}
	for _, id := range wantIDs {
		if !state.hasReceipt(id) {
			t.Fatalf("surviving receipt %s is absent from the membership index", id)
		}
	}
	if got, want := state.receiptBytes(), sumManagementReceiptBytes(state.Receipts); got != want {
		t.Fatalf("receipt byte accounting=%d, want %d", got, want)
	}
}

// TestManagementReceiptMaintenanceIsBatchedPerPass pins the repair. Insertion
// used to prune the whole set and, on every overflow, stable-sort and copy it:
// a selector growing toward managementMaximumReceipts paid O(n) per receipt and
// O(n log n) per overflow, so one window cost O(n^2), and every overflow
// published its own operator warning. Insertion is now O(1) and the set is
// pruned and bounded exactly once, immediately before the checkpoint that
// persists it.
func TestManagementReceiptMaintenanceIsBatchedPerPass(t *testing.T) {
	const capacity = 4
	const recorded = 12
	rt := newMicrosoftTestRuntime(t)
	plugin := newReceiptBudgetPlugin(t, capacity, managementMaximumReceiptBytes)
	key := managementStateKey("microsoft", "batched")
	state := &managementState{Version: managementStateVersion}
	// Half of these are already outside the source retention horizon, so the
	// pass has both pruning and count eviction to do.
	appendReceiptsAt(state, "receipt", recorded, func(ordinal int) time.Time {
		if ordinal < recorded/2 {
			return managementTestNow.Add(-managementMaximumHistory - time.Hour)
		}
		return managementTestNow.Add(-time.Duration(recorded-ordinal) * time.Minute)
	})

	// Insertion neither prunes nor trims. A set three times its capacity and
	// half expired is still intact here; the per-receipt maintenance this
	// replaced would have left exactly `capacity` receipts and warned seven
	// times before the checkpoint was ever reached.
	if len(state.Receipts) != recorded {
		t.Fatalf("receipts after %d insertions=%d, want all of them: maintenance still runs per insertion", recorded, len(state.Receipts))
	}
	if got := len(rt.logsContaining("WARN", managementReceiptCapacityWarning)); got != 0 {
		t.Fatalf("capacity warnings before the checkpoint=%d, want none", got)
	}

	if err := plugin.persistManagementCheckpoint(rt, key, state); err != nil {
		t.Fatal(err)
	}
	// The six expired receipts are pruned, then the count bound evicts the two
	// oldest survivors. The newest four remain, in the order they were recorded.
	assertReceiptSetIsConsistent(t, state, []string{"receipt-00008", "receipt-00009", "receipt-00010", "receipt-00011"})
	for _, gone := range []string{"receipt-00000", "receipt-00005", "receipt-00006", "receipt-00007"} {
		if state.hasReceipt(gone) {
			t.Fatalf("evicted receipt %s is still in the membership index", gone)
		}
	}

	// One pass, one warning, naming the whole eviction rather than one receipt
	// of it.
	warnings := rt.logsContaining("WARN", managementReceiptCapacityWarning)
	if len(warnings) != 1 {
		t.Fatalf("capacity warnings=%d, want exactly 1: maintenance warned per receipt (%+v)", len(warnings), warnings)
	}
	if got := warnings[0].Params["dropped"]; got != "2" {
		t.Fatalf("dropped=%q, want 2 reported by a single pass", got)
	}
	if got := warnings[0].Params["selector"]; got != "batched" {
		t.Fatalf("selector=%q, want the selector the state key names", got)
	}
}

// TestManagementReceiptEvictionIsStableAcrossEqualTimestamps pins the eviction
// key as (ContentCreated, original insertion index). The source stamps content
// to the second, so a busy tenant produces runs of receipts sharing one
// ContentCreated: without the insertion-index tie-break, which member of such a
// run survives would depend on the selection algorithm rather than on age.
func TestManagementReceiptEvictionIsStableAcrossEqualTimestamps(t *testing.T) {
	t.Run("through-the-checkpoint", func(t *testing.T) {
		const capacity = 3
		rt := newMicrosoftTestRuntime(t)
		plugin := newReceiptBudgetPlugin(t, capacity, managementMaximumReceiptBytes)
		state := &managementState{Version: managementStateVersion}
		appendReceiptsAt(state, "tie", 7, func(int) time.Time { return managementTestNow.Add(-time.Hour) })
		if err := plugin.persistManagementCheckpoint(rt, managementStateKey("microsoft", "ties"), state); err != nil {
			t.Fatal(err)
		}
		assertReceiptSetIsConsistent(t, state, []string{"tie-00004", "tie-00005", "tie-00006"})
	})

	t.Run("mixed-with-distinct-timestamps", func(t *testing.T) {
		// Two pairs share a timestamp and one receipt is older than all of
		// them. The oldest goes first, then the earlier-recorded member of the
		// older pair, and the survivors keep their original relative order.
		receipts := []managementReceipt{
			{ContentID: "pair-late-first", ContentCreated: managementTestNow.Add(-time.Hour)},
			{ContentID: "oldest", ContentCreated: managementTestNow.Add(-5 * time.Hour)},
			{ContentID: "pair-early-first", ContentCreated: managementTestNow.Add(-3 * time.Hour)},
			{ContentID: "pair-late-second", ContentCreated: managementTestNow.Add(-time.Hour)},
			{ContentID: "pair-early-second", ContentCreated: managementTestNow.Add(-3 * time.Hour)},
		}
		kept, dropped := trimManagementReceipts(receipts, 3)
		if got := receiptIDs(dropped); !slices.Equal(got, []string{"oldest", "pair-early-first"}) {
			t.Fatalf("dropped=%v, want the oldest then the earlier-recorded member of the older pair", got)
		}
		if got := receiptIDs(kept); !slices.Equal(got, []string{"pair-late-first", "pair-late-second", "pair-early-second"}) {
			t.Fatalf("kept=%v, want the survivors in their original order", got)
		}
	})
}

// TestManagementReceiptByteBudgetIsEnforcedBeforePersistence pins the aggregate
// byte bound. managementMaximumReceipts bounds how many receipts the document
// holds but not how large they are: decodeManagementContent accepts a contentId
// up to 512 bytes, so the count bound alone admits roughly 116 MB of receipt
// JSON that every flushing pass re-marshals and re-fsyncs and every poll
// re-reads. The budget has to be met by the bytes that are actually written.
func TestManagementReceiptByteBudgetIsEnforcedBeforePersistence(t *testing.T) {
	const recorded = 10
	const admitted = 4
	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", "bytes")
	state := &managementState{Version: managementStateVersion}
	// Long identifiers: the shape the count bound does not describe.
	filler := strings.Repeat("c", 400)
	for ordinal := range recorded {
		recordManagementReceipt(state, managementContent{
			ContentID:      fmt.Sprintf("%s-%05d", filler, ordinal),
			ContentCreated: managementTestNow.Add(-time.Duration(recorded-ordinal) * time.Minute),
		})
	}
	budget := admitted * managementReceiptBytes(state.Receipts[0])
	// The count bound is left wide open, so only the byte budget can evict.
	plugin := newReceiptBudgetPlugin(t, managementMaximumReceipts, budget)
	if err := plugin.persistManagementCheckpoint(rt, key, state); err != nil {
		t.Fatal(err)
	}
	if len(state.Receipts) != admitted {
		t.Fatalf("receipts=%d, want %d: the aggregate byte budget was not enforced", len(state.Receipts), admitted)
	}
	wantIDs := make([]string, 0, admitted)
	for ordinal := recorded - admitted; ordinal < recorded; ordinal++ {
		wantIDs = append(wantIDs, fmt.Sprintf("%s-%05d", filler, ordinal))
	}
	assertReceiptSetIsConsistent(t, state, wantIDs)
	if got := state.receiptBytes(); got > budget {
		t.Fatalf("receipt bytes=%d, want at most the %d byte budget", got, budget)
	}

	// The budget bounds the document that was written rather than an estimate
	// of it: the charge per receipt covers its serialized identifier and the
	// separator the document needs to hold it, so the only bytes it does not
	// account for are the fixed envelope of an empty receipt set.
	encoded, err := rt.Get(managementReceiptsKey(key))
	if err != nil {
		t.Fatal(err)
	}
	single, err := json.Marshal(managementReceiptSet{Version: managementStateVersion, Receipts: state.Receipts[:1]})
	if err != nil {
		t.Fatal(err)
	}
	envelope := len(single) - managementReceiptBytes(state.Receipts[0])
	if len(encoded) > budget+envelope {
		t.Fatalf("persisted receipt document is %d bytes against a %d byte budget and a %d byte envelope",
			len(encoded), budget, envelope)
	}
	// The on-disk schema is unchanged.
	var document managementReceiptSet
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != managementStateVersion || !slices.Equal(receiptIDs(document.Receipts), wantIDs) {
		t.Fatalf("persisted document version=%d receipts=%v", document.Version, receiptIDs(document.Receipts))
	}
	if got := len(rt.logsContaining("WARN", managementReceiptCapacityWarning)); got != 1 {
		t.Fatalf("byte-budget warnings=%d, want exactly 1", got)
	}
}

// TestManagementReceiptPruningRunsOncePerPass pins the retention prune at the
// checkpoint: expired receipts go even when neither budget binds, and a set with
// nothing expired is handed to the persist untouched rather than reallocated.
func TestManagementReceiptPruningRunsOncePerPass(t *testing.T) {
	t.Run("expired-receipts-are-pruned", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		plugin := newReceiptBudgetPlugin(t, managementMaximumReceipts, managementMaximumReceiptBytes)
		state := &managementState{Version: managementStateVersion}
		appendReceiptsAt(state, "aged", 6, func(ordinal int) time.Time {
			if ordinal%2 == 0 {
				return managementTestNow.Add(-managementMaximumHistory - time.Minute)
			}
			return managementTestNow.Add(-time.Hour)
		})
		if err := plugin.persistManagementCheckpoint(rt, managementStateKey("microsoft", "aged"), state); err != nil {
			t.Fatal(err)
		}
		assertReceiptSetIsConsistent(t, state, []string{"aged-00001", "aged-00003", "aged-00005"})
		// Pruning is retention maintenance, not capacity loss, so it is not
		// reported as an eviction.
		if got := len(rt.logsContaining("WARN", managementReceiptCapacityWarning)); got != 0 {
			t.Fatalf("capacity warnings=%d, want none for retention pruning", got)
		}
	})

	t.Run("a-bounded-set-is-not-recopied", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		plugin := newReceiptBudgetPlugin(t, managementMaximumReceipts, managementMaximumReceiptBytes)
		state := &managementState{Version: managementStateVersion}
		appendReceiptsAt(state, "fresh", 32, func(int) time.Time { return managementTestNow.Add(-time.Hour) })
		before := &state.Receipts[0]
		if err := plugin.persistManagementCheckpoint(rt, managementStateKey("microsoft", "fresh"), state); err != nil {
			t.Fatal(err)
		}
		if len(state.Receipts) != 32 || &state.Receipts[0] != before {
			t.Fatalf("a set inside every bound was reallocated: receipts=%d", len(state.Receipts))
		}
	})
}

// TestManagementReceiptChurnNearCapacityStaysBounded pins the steady state a
// high-volume tenant actually lives in: every pass records more receipts than
// the cap admits, and every pass has to leave the newest `capacity` of them, in
// order, with the membership index and the byte accounting agreeing.
func TestManagementReceiptChurnNearCapacityStaysBounded(t *testing.T) {
	const capacity = 8
	const perPass = 5
	const passes = 10
	rt := newMicrosoftTestRuntime(t)
	plugin := newReceiptBudgetPlugin(t, capacity, managementMaximumReceiptBytes)
	key := managementStateKey("microsoft", "churn")
	state := &managementState{Version: managementStateVersion}
	var recorded []string
	evictingPasses := 0
	for pass := range passes {
		survivors := len(state.Receipts)
		for ordinal := range perPass {
			id := fmt.Sprintf("churn-%02d-%02d", pass, ordinal)
			recordManagementReceipt(state, managementContent{
				ContentID:      id,
				ContentCreated: managementTestNow.Add(-time.Duration(passes*perPass-len(recorded)) * time.Minute),
			})
			recorded = append(recorded, id)
		}
		// Insertion is unconditional, so between checkpoints the set is exactly
		// what was recorded into it.
		if len(state.Receipts) != survivors+perPass {
			t.Fatalf("pass %d: insertion bounded the set before the checkpoint: receipts=%d, want %d",
				pass, len(state.Receipts), survivors+perPass)
		}
		if len(state.Receipts) > capacity {
			evictingPasses++
		}
		if err := plugin.persistManagementCheckpoint(rt, key, state); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		want := recorded
		if len(want) > capacity {
			want = want[len(want)-capacity:]
		}
		assertReceiptSetIsConsistent(t, state, want)
	}
	if got := len(rt.logsContaining("WARN", managementReceiptCapacityWarning)); got != evictingPasses {
		t.Fatalf("capacity warnings=%d, want one per evicting pass (%d)", got, evictingPasses)
	}
}

// TestManagementReceiptMaintenanceSurvivesRestart pins restart compatibility.
// Neither the membership index nor the byte accounting is serialized, so both
// are rebuilt from the decoded set on load; a pass after a restart has to
// enforce the same budgets as the pass that wrote the document.
func TestManagementReceiptMaintenanceSurvivesRestart(t *testing.T) {
	const capacity = 6
	rt := newMicrosoftTestRuntime(t)
	plugin := newReceiptBudgetPlugin(t, capacity, managementMaximumReceiptBytes)
	key := managementStateKey("microsoft", "restart")
	state := &managementState{Version: managementStateVersion}
	appendReceiptsAt(state, "first", 9, func(ordinal int) time.Time {
		return managementTestNow.Add(-time.Duration(20-ordinal) * time.Minute)
	})
	if err := plugin.persistManagementCheckpoint(rt, key, state); err != nil {
		t.Fatal(err)
	}
	wantFirst := receiptIDs(state.Receipts)
	if len(wantFirst) != capacity {
		t.Fatalf("receipts=%d, want the count bound enforced before the restart", len(wantFirst))
	}

	loaded, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	assertReceiptSetIsConsistent(t, &loaded, wantFirst)

	// The reloaded document is already bounded, so a checkpoint that records
	// nothing new leaves it alone and publishes no eviction.
	if err := plugin.persistManagementCheckpoint(rt, key, &loaded); err != nil {
		t.Fatal(err)
	}
	assertReceiptSetIsConsistent(t, &loaded, wantFirst)

	// A pass after the restart enforces the same bound against the rebuilt
	// accounting.
	appendReceiptsAt(&loaded, "second", 4, func(ordinal int) time.Time {
		return managementTestNow.Add(-time.Duration(4-ordinal) * time.Minute)
	})
	if err := plugin.persistManagementCheckpoint(rt, key, &loaded); err != nil {
		t.Fatal(err)
	}
	want := slices.Concat(wantFirst[len(wantFirst)-(capacity-4):],
		[]string{"second-00000", "second-00001", "second-00002", "second-00003"})
	assertReceiptSetIsConsistent(t, &loaded, want)
	reloaded, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	assertReceiptSetIsConsistent(t, &reloaded, want)
	if got := len(rt.logsContaining("WARN", managementReceiptCapacityWarning)); got != 2 {
		t.Fatalf("capacity warnings=%d, want one per evicting pass", got)
	}
}

// TestManagementReceiptInsertionCostDoesNotGrowWithTheSet is the performance
// shape regression. Pruning and trimming once per receipt copied the whole set
// on every insertion, so recording n receipts into a set already at capacity
// allocated on the order of n^2 receipts -- hundreds of megabytes at the size
// used here. Insertion is now O(1), so the same work allocates two orders of
// magnitude less than the ceiling asserted below.
func TestManagementReceiptInsertionCostDoesNotGrowWithTheSet(t *testing.T) {
	const capacity = 4000
	// A quadratic insertion path allocates roughly capacity^2 receipts, several
	// hundred megabytes here. A linear one allocates a few.
	const ceiling = 48 << 20
	rt := newMicrosoftTestRuntime(t)
	plugin := newReceiptBudgetPlugin(t, capacity, managementMaximumReceiptBytes)
	state := &managementState{Version: managementStateVersion}
	appendReceiptsAt(state, "seed", capacity, func(ordinal int) time.Time {
		return managementTestNow.Add(-time.Duration(2*capacity-ordinal) * time.Second)
	})

	// Every insertion below leaves the set over its capacity, which is exactly
	// when the trim this replaced sorted and copied all of it.
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	appendReceiptsAt(state, "churn", capacity, func(ordinal int) time.Time {
		return managementTestNow.Add(-time.Duration(capacity-ordinal) * time.Second)
	})
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > ceiling {
		t.Fatalf("recording %d receipts into a set at capacity allocated %d bytes, want under %d: insertion scales with the set",
			capacity, allocated, uint64(ceiling))
	}
	if len(state.Receipts) != 2*capacity {
		t.Fatalf("receipts=%d, want every insertion retained until maintenance runs", len(state.Receipts))
	}

	// One pass bounds the whole set.
	if err := plugin.persistManagementCheckpoint(rt, managementStateKey("microsoft", "shape"), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Receipts) != capacity {
		t.Fatalf("receipts after one maintenance pass=%d, want %d", len(state.Receipts), capacity)
	}
	if got := state.Receipts[0].ContentID; got != "churn-00000" {
		t.Fatalf("oldest survivor=%s, want the newest %d receipts", got, capacity)
	}
}

// TestManagementReceiptCapacityWarnsOncePerPass pins the batching end to end. A
// window whose content count exceeds the receipt capacity used to publish one
// operator warning per receipt past the bound; it now publishes one per pass,
// naming the whole eviction, and the content is still collected exactly once.
func TestManagementReceiptCapacityWarnsOncePerPass(t *testing.T) {
	contents := make([]managementCapacityContent, 0, 6)
	for ordinal := range 6 {
		contents = append(contents, managementCapacityContent{
			id:      fmt.Sprintf("content-%d", ordinal),
			created: managementTestNow.Add(-time.Duration(6-ordinal) * time.Hour),
		})
	}
	fetched := make(map[string]*atomic.Int64)
	server := managementCapacityServer(t, contents, fetched)
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	plugin.managementReceiptLimit = 2

	drainManagementSelector(t, plugin, rt)
	if got := len(rt.Entries()); got != len(contents) {
		t.Fatalf("entries=%d, want one per content blob", got)
	}
	warnings := rt.logsContaining("WARN", managementReceiptCapacityWarning)
	if len(warnings) != 1 {
		t.Fatalf("capacity warnings=%d, want exactly 1 for a pass that evicted four receipts (%+v)", len(warnings), warnings)
	}
	if got := warnings[0].Params["dropped"]; got != "4" {
		t.Fatalf("dropped=%q, want 4 reported by a single pass", got)
	}
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	state, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if got := receiptIDs(state.Receipts); !slices.Equal(got, []string{"content-4", "content-5"}) {
		t.Fatalf("receipts=%v, want the two newest", got)
	}

	// A later poll must neither stall nor re-deliver what the eviction dropped.
	plugin.now = func() time.Time { return managementTestNow.Add(time.Hour) }
	drainManagementSelector(t, plugin, rt)
	if got := len(rt.Entries()); got != len(contents) {
		t.Fatalf("entries after the following poll=%d, want no redelivery", got)
	}
	for id, counter := range fetched {
		if got := counter.Load(); got != 1 {
			t.Fatalf("content %s fetched %d times, want exactly 1", id, got)
		}
	}
}

// seedManagementReceiptsAtCapacity publishes a durable state document holding
// exactly managementMaximumReceipts retained receipts beside one undelivered
// descriptor. The receipts are all inside the source retention horizon, so the
// retention prune has nothing to take and the count bound is the only thing the
// next completion can be refused by.
func seedManagementReceiptsAtCapacity(t *testing.T, plugin *Microsoft, rt *microsoftTestRuntime, key, contentType string,
	content managementCapacityContent, serverURL string) {
	t.Helper()
	state := managementState{
		Version:          managementStateVersion,
		CompletedThrough: managementTestNow.Add(-time.Hour),
		TargetThrough:    managementTestNow,
		WindowStart:      managementTestNow.Add(-time.Hour),
		WindowEnd:        managementTestNow,
		ListingStarted:   true,
		ListingComplete:  true,
		Pages:            1,
	}
	state.appendContent(managementContent{
		ContentType: contentType, ContentID: content.id,
		ContentURI:     serverURL + managementCapacityContentPath + content.id,
		ContentCreated: content.created,
	})
	for ordinal := range managementMaximumReceipts {
		state.appendReceipt(managementReceipt{
			ContentID:      fmt.Sprintf("seed-%06d", ordinal),
			ContentCreated: managementTestNow.Add(-time.Duration(managementMaximumReceipts-ordinal) * time.Second),
		})
	}
	if err := plugin.persistManagementCheckpoint(rt, key, &state); err != nil {
		t.Fatal(err)
	}
}

// Exactly managementMaximumReceipts retained receipts is a valid state
// document, and completing one more content appended receipt 200,001. The
// per-content persist validates the hard count bound and cannot evict anything
// -- the deterministic maintenance pass runs at the checkpoint, which is after
// it -- so the whole state transaction was refused, nothing was written, and
// the next poll read the same 200,000 receipts back, resumed at the same
// ContentIndex, re-fetched the same blob, found it already fully delivered,
// appended the same receipt and was refused identically. Collection stopped at
// exactly the capacity the eviction exists to recover from, and the selector
// kept reporting pending work while it did.
//
// TestManagementReceiptCapacityAtTheProductionBoundCompletesAnotherContent runs
// that at the real 200,000 cap, with a restart between every pass.
func TestManagementReceiptCapacityAtTheProductionBoundCompletesAnotherContent(t *testing.T) {
	const contentType = "Audit.Exchange"
	const selector = "microsoft365-audit-exchange"

	content := managementCapacityContent{id: "content-at-capacity", created: managementTestNow.Add(-30 * time.Minute)}
	fetched := make(map[string]*atomic.Int64)
	server := managementCapacityServer(t, []managementCapacityContent{content}, fetched)
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", selector)
	seed := newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
	seedManagementReceiptsAtCapacity(t, seed, rt, key, contentType, content, server.URL)

	// Non-vacuous: the durable set really is at the production bound, under the
	// production limits, so the next completion really is receipt 200,001.
	seeded, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(seeded.Receipts) != managementMaximumReceipts {
		t.Fatalf("seeded receipts=%d, want exactly the production cap %d", len(seeded.Receipts), managementMaximumReceipts)
	}
	if seed.receiptCapacity() != managementMaximumReceipts || seed.receiptByteCapacity() != managementMaximumReceiptBytes {
		t.Fatalf("capacities=%d/%d, want the production bounds", seed.receiptCapacity(), seed.receiptByteCapacity())
	}
	if err := validateManagementProgressFields(seeded); err != nil {
		t.Fatalf("a set at exactly the cap is not a valid document: %v", err)
	}
	overflowing := seeded
	overflowing.Receipts = append(slices.Clone(seeded.Receipts), managementReceipt{ContentID: content.id, ContentCreated: content.created})
	if err := validateManagementProgressFields(overflowing); err == nil {
		t.Fatal("receipt 200,001 is persistable, so the stall this repairs cannot happen and the test is vacuous")
	}

	// Pass one completes the content. The receipt has to be recorded, the
	// progress has to be persisted, and the pass must not fail.
	first := newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
	if _, err := first.Handle(t.Context(), rt); err != nil {
		t.Fatalf("the pass that completed one more content failed: %v", err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("entries=%d, want the one blob delivered", got)
	}

	completed, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if !completed.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through=%s, want the window's end %s: the completion was never persisted",
			completed.CompletedThrough, managementTestNow)
	}
	if len(completed.Receipts) > managementMaximumReceipts {
		t.Fatalf("retained receipts=%d, over the hard bound %d", len(completed.Receipts), managementMaximumReceipts)
	}
	if got := completed.receiptBytes(); got > managementMaximumReceiptBytes {
		t.Fatalf("retained receipt bytes=%d, over the hard budget %d", got, managementMaximumReceiptBytes)
	}
	// The identity whose delivery this transaction acknowledged is the one
	// identity the eviction may not drop.
	if !completed.hasReceipt(content.id) {
		t.Fatal("the completed content's own receipt was evicted, so an unacknowledged identity was dropped")
	}
	// What went is the oldest, and the eviction is operator-visible rather than
	// silent -- once for the pass, not once per content.
	if completed.hasReceipt("seed-000000") {
		t.Fatal("the oldest seeded receipt survived, so the eviction was not oldest-first")
	}
	if !completed.hasReceipt(fmt.Sprintf("seed-%06d", managementMaximumReceipts-1)) {
		t.Fatal("the newest seeded receipt was evicted ahead of older ones")
	}
	warnings := rt.logsContaining("WARN", managementReceiptCapacityWarning)
	if len(warnings) != 1 {
		t.Fatalf("capacity warnings=%d, want exactly 1 naming the whole eviction (%+v)", len(warnings), warnings)
	}

	// The next poll must neither repeat the content nor stall. Every pass is a
	// fresh plugin, which is the restart the stall used to survive.
	for pass := 2; pass <= 4; pass++ {
		later := newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
		continuation, err := later.Handle(t.Context(), rt)
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if continuation != nil && continuation.Delay == 0 {
			t.Fatalf("pass %d still reports immediate pending work, so the selector is stalled", pass)
		}
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("entries after three further polls=%d, want no redelivery", got)
	}
	if got := fetched[content.id].Load(); got != 1 {
		t.Fatalf("content fetched %d times, want exactly 1: the completion repeated", got)
	}
	if got := len(rt.logsContaining("WARN", managementReceiptCapacityWarning)); got != 1 {
		t.Fatalf("capacity warnings after the further polls=%d, want the one the evicting pass published", got)
	}
	assertManagementDocumentsAreBounded(t, rt, key)
}

// managementLongestDispatchableURLLength is the longest a Management Activity
// URL may be as the source writes it and still fit managementMaximumURLLength
// once managementRequest applies the mandatory PublisherIdentifier, which adds
// "?PublisherIdentifier=" and the 36-character identifier.
const managementLongestDispatchableURLLength = managementMaximumURLLength - len("?PublisherIdentifier=") - 36

// managementRefreshBudgetSource answers the stored content URI the way a blob
// past its URI's lifetime does and re-lists the same contentId at the longest
// URI the Management URL policy admits -- an individually valid replacement
// that is much larger than the descriptor it replaces.
type managementRefreshBudgetSource struct {
	server      *httptest.Server
	contentType string
	contentID   string
	created     time.Time
	lists       atomic.Int64
	stale       atomic.Int64
	fresh       atomic.Int64
}

const managementRefreshedContentPrefix = "refreshed-"

func (s *managementRefreshBudgetSource) stalePath() string {
	return managementCapacityContentPath + s.contentID
}

func (s *managementRefreshBudgetSource) staleURI() string {
	return s.server.URL + s.stalePath()
}

func (s *managementRefreshBudgetSource) freshURI() string {
	base := s.server.URL + managementCapacityContentPath + managementRefreshedContentPrefix
	return base + strings.Repeat("u", managementLongestDispatchableURLLength-len(base))
}

func (s *managementRefreshBudgetSource) start(t *testing.T) *httptest.Server {
	t.Helper()
	s.server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, s.contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			s.lists.Add(1)
			_, _ = fmt.Fprintf(w, `[{"contentType":%q,"contentId":%q,"contentUri":%q,"contentCreated":%q}]`,
				s.contentType, s.contentID, s.freshURI(), s.created.UTC().Format(time.RFC3339))
		case r.URL.Path == s.stalePath():
			s.stale.Add(1)
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":"AF20050","message":"expired"}}`)
		case strings.HasPrefix(r.URL.Path, managementCapacityContentPath+managementRefreshedContentPrefix):
			s.fresh.Add(1)
			_, _ = fmt.Fprintf(w, `[{"Id":%q,"CreationTime":%q,"Operation":"MailItemsAccessed"}]`,
				s.contentID, s.created.UTC().Format(time.RFC3339))
		default:
			http.NotFound(w, r)
		}
	})
	t.Cleanup(s.server.Close)
	return s.server
}

// managementContentsWitnessRuntime records every value ever written to the
// descriptor side key, so a test can state that an over-budget contentUri was
// never durably retained however the window it belonged to ended.
type managementContentsWitnessRuntime struct {
	*microsoftTestRuntime
	contentsKey string
	mu          sync.Mutex
	writes      [][]byte
}

func (r *managementContentsWitnessRuntime) Put(key string, value []byte) error {
	if key == r.contentsKey {
		r.mu.Lock()
		r.writes = append(r.writes, append([]byte(nil), value...))
		r.mu.Unlock()
	}
	return r.microsoftTestRuntime.Put(key, value)
}

func (r *managementContentsWitnessRuntime) contentsWrites() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.writes)
}

// seedManagementNearLimitDescriptorSet publishes a descriptor set whose
// aggregate cost is just under managementMaximumDescriptorBytes, with target as
// the only undelivered descriptor. The slack it leaves is deliberately smaller
// than growth, so replacing target by a descriptor that much larger crosses the
// aggregate budget -- which is the shape a refreshed contentUri produces.
func seedManagementNearLimitDescriptorSet(t *testing.T, contentType string, target managementContent, growth int) managementState {
	t.Helper()
	state := managementState{
		Version:          managementStateVersion,
		CompletedThrough: managementTestNow.Add(-time.Hour),
		TargetThrough:    managementTestNow,
		WindowStart:      managementTestNow.Add(-time.Hour),
		WindowEnd:        managementTestNow,
		ListingStarted:   true,
		ListingComplete:  true,
		Pages:            1,
	}
	filler := func(prefix string, ordinal, length int) managementContent {
		id := fmt.Sprintf("%s-%05d", prefix, ordinal)
		uri := "https://manage.office.com/api/v1.0/t/activity/feed/audit/" + id
		if pad := length - len(uri); pad > 0 {
			uri += strings.Repeat("u", pad)
		}
		return managementContent{
			ContentType: contentType, ContentID: id, ContentURI: uri,
			ContentCreated: managementTestNow.Add(-time.Duration(ordinal+1) * time.Second),
		}
	}
	slack := func() int {
		return managementMaximumDescriptorBytes - state.descriptorBytes() - managementDescriptorBytes(target)
	}
	// Long descriptors first, which is what fills the budget at the shape the
	// source really publishes, then short ones to bring the remaining slack
	// below the growth a replacement would add.
	for ordinal := 0; ; ordinal++ {
		candidate := filler("fill", ordinal, managementLongestDispatchableURLLength)
		if managementDescriptorBytes(candidate) > slack() {
			break
		}
		state.appendContent(candidate)
	}
	for ordinal := 0; ; ordinal++ {
		candidate := filler("tight", ordinal, 0)
		charge := managementDescriptorBytes(candidate)
		if charge > slack() || slack() < growth {
			break
		}
		state.appendContent(candidate)
	}
	state.appendContent(target)
	state.ContentIndex = len(state.Contents) - 1
	return state
}

// A refreshed descriptor is individually valid -- decodeManagementContent holds
// its contentUri to the Management URL policy, its origin, its path prefix and
// managementMaximumURLLength -- and can still be far longer than the expired one
// it replaces. The replacement was applied to the descriptor set before the
// persist that validates the aggregate, so the persist refused the whole state
// transaction on the byte budget, nothing was written, and the next poll read
// the same expired descriptor back, refreshed it to the same individually valid
// over-budget URI and failed identically. A recoverable budget became permanent
// cessation of collection for the content type, on every poll, until the window
// crossed the source's seven-day retention floor and the whole interval went
// with it.
//
// TestManagementExpiredDescriptorRefreshProgressesInsideTheAggregateBudget runs
// that against a descriptor set at the real 8 MiB budget and the longest
// replacement the real URL policy admits, across repeated passes with a restart
// between each one.
func TestManagementExpiredDescriptorRefreshProgressesInsideTheAggregateBudget(t *testing.T) {
	const contentType = "Audit.Exchange"
	const selector = "microsoft365-audit-exchange"

	source := &managementRefreshBudgetSource{
		contentType: contentType,
		contentID:   "content-expired",
		created:     managementTestNow.Add(-30 * time.Minute),
	}
	server := source.start(t)
	key := managementStateKey("microsoft", selector)
	rt := &managementContentsWitnessRuntime{
		microsoftTestRuntime: newMicrosoftTestRuntime(t),
		contentsKey:          managementContentsKey(key),
	}
	plugin := newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)

	// Non-vacuous on the policy side: the replacement is the longest URI the
	// real policy admits, and one byte more is refused, so it is valid rather
	// than merely large.
	if err := plugin.client.validateManagementURL(source.freshURI()); err != nil {
		t.Fatalf("the refreshed URI is not individually valid: %v", err)
	}
	if err := plugin.client.validateManagementURL(source.freshURI() + "u"); err == nil {
		t.Fatal("the refreshed URI is not at the policy limit, so it is not the largest valid replacement")
	}
	if plugin.descriptorByteCapacity() != managementMaximumDescriptorBytes {
		t.Fatalf("descriptor byte capacity=%d, want the production budget", plugin.descriptorByteCapacity())
	}

	target := managementContent{
		ContentType: contentType, ContentID: source.contentID,
		ContentURI: source.staleURI(), ContentCreated: source.created,
	}
	refreshed := target
	refreshed.ContentURI = source.freshURI()
	growth := managementDescriptorBytes(refreshed) - managementDescriptorBytes(target)
	if growth <= 0 {
		t.Fatalf("the replacement grows the set by %d bytes, so it cannot cross the budget", growth)
	}
	state := seedManagementNearLimitDescriptorSet(t, contentType, target, growth)

	// Non-vacuous on the budget side: the set fits, the replacement does not,
	// and the set really is at the production budget rather than a token one.
	retained := state.descriptorBytes()
	if retained > managementMaximumDescriptorBytes {
		t.Fatalf("the seeded set is %d bytes, already over the %d-byte budget", retained, managementMaximumDescriptorBytes)
	}
	if retained+growth <= managementMaximumDescriptorBytes {
		t.Fatalf("the replacement leaves the set at %d bytes, inside the %d-byte budget: nothing is under test",
			retained+growth, managementMaximumDescriptorBytes)
	}
	if retained < managementMaximumDescriptorBytes-managementMaximumURLLength {
		t.Fatalf("the seeded set is %d bytes, not near the %d-byte budget", retained, managementMaximumDescriptorBytes)
	}
	if err := plugin.persistManagementCheckpoint(rt, key, &state); err != nil {
		t.Fatalf("a descriptor set inside the budget was refused: %v", err)
	}

	for pass := 1; pass <= 3; pass++ {
		// A fresh plugin every pass: the expired descriptor is durable, so a
		// restart used to meet exactly the same failure.
		later := newManagementTestPlugin(t, server, selector, (&managementSyncController{}).Sync)
		if _, err := later.Handle(t.Context(), rt); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}

	// The blob was collected, once, and the expired URI was dispatched once:
	// under the stall it was re-dispatched and re-refreshed on every pass.
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("entries=%d, want the refreshed blob delivered exactly once", got)
	}
	if got := source.stale.Load(); got != 1 {
		t.Fatalf("expired content requests=%d, want exactly 1: the pass repeated", got)
	}
	if got := source.fresh.Load(); got != 1 {
		t.Fatalf("refreshed content requests=%d, want the re-listed URI fetched once", got)
	}
	if got := source.lists.Load(); got != 1 {
		t.Fatalf("content listings=%d, want the single refresh re-list", got)
	}
	if got := rt.logsContaining("WARN", "refreshed content URI is longer than the descriptor byte budget"); len(got) != 1 {
		t.Fatalf("budget reports=%d, want exactly 1 (%+v)", len(got), got)
	}

	// Nothing over budget ever became durable: no write to the descriptor side
	// key carried the refreshed URI, whatever the window did afterwards.
	writes := rt.contentsWrites()
	if len(writes) == 0 {
		t.Fatal("the descriptor side key was never written, so the retention claim is vacuous")
	}
	for index, written := range writes {
		if strings.Contains(string(written), source.freshURI()) {
			t.Fatalf("descriptor write %d durably retained the over-budget refreshed URI", index)
		}
		if len(written) > managementMaximumContentSetBytes {
			t.Fatalf("descriptor write %d is %d bytes, over the %d-byte ceiling", index, len(written), managementMaximumContentSetBytes)
		}
	}

	// Delivery semantics are unchanged: the window completed, the receipt names
	// the same canonical contentId, and every document is inside its ceiling.
	final, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if !final.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through=%s, want the window's end %s", final.CompletedThrough, managementTestNow)
	}
	if !final.hasReceipt(source.contentID) {
		t.Fatalf("receipts=%+v, want one naming the delivered contentId", final.Receipts)
	}
	if got := final.descriptorBytes(); got > managementMaximumDescriptorBytes {
		t.Fatalf("retained descriptor bytes=%d, over the %d-byte budget", got, managementMaximumDescriptorBytes)
	}
	assertManagementDocumentsAreBounded(t, rt.microsoftTestRuntime, key)
}
