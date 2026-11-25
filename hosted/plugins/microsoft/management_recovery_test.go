/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package microsoft

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// delayedWriteRuntime models a slow ingest link. Delivery uses the standard
// Hosted Runner Write contract and must remain independent of the vendor fetch
// deadline.
type delayedWriteRuntime struct {
	*microsoftTestRuntime
	delay time.Duration
}

func (r *delayedWriteRuntime) Write(ent entry.Entry) error {
	time.Sleep(r.delay)
	return r.microsoftTestRuntime.Write(ent)
}

// managementLossServer serves one enabled subscription and a content listing
// that names every descriptor on the first call and only the survivors
// afterwards, which is what the source does once a blob has aged out: the
// stable contentId is simply absent from the re-list the refresh performs.
func managementLossServer(t *testing.T, dead, live string) (*httptest.Server, *atomic.Int64, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	var lists, deadCalls, liveCalls atomic.Int64
	const base = "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/"
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			descriptors := []string{managementDescriptor(server.URL, "Audit.Exchange", live, base+live)}
			if lists.Add(1) == 1 {
				descriptors = append([]string{managementDescriptor(server.URL, "Audit.Exchange", dead, base+dead)}, descriptors...)
			}
			_, _ = fmt.Fprintf(w, "[%s]", strings.Join(descriptors, ","))
		case strings.HasSuffix(r.URL.Path, base+dead):
			deadCalls.Add(1)
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":"AF20050","message":"expired"}}`)
		case strings.HasSuffix(r.URL.Path, base+live):
			liveCalls.Add(1)
			_, _ = io.WriteString(w, `[{"Id":"survivor","CreationTime":"2026-10-08T11:40:00Z","Operation":"MailItemsAccessed"}]`)
		default:
			http.NotFound(w, r)
		}
	})
	return server, &lists, &deadCalls, &liveCalls
}

// TestManagementUnrefreshableContentIsSkippedAsPermanentLossAcrossRestart pins
// the delivery-phase retention repair. A blob that expires after its window's
// listing completed used to return the refresh error to Handle with no state
// mutation at all: ContentIndex did not advance, Contents was not reset and
// ListingComplete stayed true, so every later poll re-read the same dead
// descriptor and produced the same error forever. A bounded loss of one
// unavailable blob therefore became permanent, total cessation of Management
// Activity collection for that content type, and the selector sat in
// rt.SetError being re-polled every JobErrorDelay indefinitely.
func TestManagementUnrefreshableContentIsSkippedAsPermanentLossAcrossRestart(t *testing.T) {
	server, lists, deadCalls, liveCalls := managementLossServer(t, "content-dead", "content-live")
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	// Crash the process on the first write, after the dead descriptor
	// has already been skipped and that skip has been synchronized.
	rt.failAt.Store(1)
	syncer := &managementSyncController{}
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "write failure") {
		t.Fatalf("error=%v, want the synthetic write failure", err)
	}
	if got := deadCalls.Load(); got != 1 {
		t.Fatalf("expired content fetched %d times before the crash, want 1", got)
	}

	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	crashed, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if crashed.ContentIndex != 1 {
		t.Fatalf("content index=%d, want the permanently lost descriptor durably skipped", crashed.ContentIndex)
	}
	if crashed.SkippedContents != 1 || !crashed.SkippedFrom.Equal(crashed.SkippedThrough) {
		t.Fatalf("permanent-loss accounting=%+v", crashed)
	}
	if !crashed.hasReceipt("content-dead") {
		t.Fatalf("the skipped descriptor has no receipt, so an overlapping listing can resurrect it")
	}

	// Restart against the same state store: the survivor must be delivered and
	// the dead descriptor must never be read again.
	rt.failAt.Store(0)
	plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("restarted poll error=%v, want progress rather than a permanent wedge", err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("entries=%d, want exactly the one surviving content blob", got)
	}
	if got := deadCalls.Load(); got != 1 {
		t.Fatalf("expired content fetched %d times in total, want 1", got)
	}
	// The surviving blob is read again after the crash because its body is never
	// stored, but it is delivered exactly once.
	if got := liveCalls.Load(); got != 2 {
		t.Fatalf("surviving content fetched %d times, want the pre-crash read plus the resumed one", got)
	}
	if got := lists.Load(); got < 2 {
		t.Fatalf("content listings=%d, want the initial listing plus the refresh attempt", got)
	}

	completed, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if !completed.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s, want the whole planned range %s", completed.CompletedThrough, managementTestNow)
	}
	if completed.SkippedContents != 1 {
		t.Fatalf("permanent-loss total=%d, want exactly the one unavailable blob", completed.SkippedContents)
	}

	// The loss is reported exactly once, naming the descriptor, and it is on the
	// status channel the poll loop does not clear.
	lines := rt.logsContaining("ERROR", "no longer available from the source")
	if len(lines) != 1 {
		t.Fatalf("permanent-loss errors=%d, want exactly 1 (%+v)", len(lines), lines)
	}
	if got := lines[0].Params["content_id"]; got != "content-dead" {
		t.Fatalf("content_id=%q", got)
	}
	if got := lines[0].Params["skipped_total"]; got != "1" {
		t.Fatalf("skipped_total=%q, want the exact permanent loss", got)
	}
	if got := lines[0].Params["selector"]; got != "microsoft365-audit-exchange" {
		t.Fatalf("selector=%q", got)
	}
	warn, err := rt.Status()
	if err != nil {
		t.Fatalf("status error=%v, want the loss on the channel the poll loop leaves alone", err)
	}
	if !strings.Contains(warn, "permanently skipped 1 audit content blobs") || !strings.Contains(warn, "microsoft365-audit-exchange") {
		t.Fatalf("operator-visible status=%q", warn)
	}

	// A later poll neither re-reads the dead descriptor nor re-reports the loss.
	plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	plugin.now = func() time.Time { return managementTestNow.Add(time.Hour) }
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := deadCalls.Load(); got != 1 {
		t.Fatalf("expired content fetched %d times after the following poll", got)
	}
	if got := rt.logsContaining("ERROR", "no longer available from the source"); len(got) != 1 {
		t.Fatalf("permanent-loss errors after the following poll=%d, want 1", len(got))
	}
}

func TestManagementLoadClampsTornDescriptorReset(t *testing.T) {
	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	head, err := json.Marshal(managementState{
		Version: managementStateVersion, ContentIndex: 2,
		ActiveContentID: "stale", ActiveDigest: "stale", EventIndex: 7,
		WindowStart: managementTestNow.Add(-time.Hour), WindowEnd: managementTestNow,
		ListingComplete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := json.Marshal(managementContentSet{Version: managementStateVersion})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(key, head); err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(managementContentsKey(key), contents); err != nil {
		t.Fatal(err)
	}
	state, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if state.ContentIndex != 0 || state.EventIndex != 0 || state.ActiveContentID != "" || state.ActiveDigest != "" {
		t.Fatalf("torn state was not clamped: %+v", state)
	}
}

func TestManagementCorruptSplitStateSelfRecovers(t *testing.T) {
	tests := []struct {
		name string
		seed func(t *testing.T, rt *microsoftTestRuntime, key string)
	}{
		{"head", func(t *testing.T, rt *microsoftTestRuntime, key string) {
			t.Helper()
			if err := rt.Put(key, []byte(`{"version":`)); err != nil {
				t.Fatal(err)
			}
		}},
		{"future-head", func(t *testing.T, rt *microsoftTestRuntime, key string) {
			t.Helper()
			if err := rt.Put(key, []byte(`{"version":99,"completed_through":"2026-10-08T10:00:00Z"}`)); err != nil {
				t.Fatal(err)
			}
		}},
		{"contents", func(t *testing.T, rt *microsoftTestRuntime, key string) {
			t.Helper()
			if err := rt.Put(managementContentsKey(key), []byte(`{"version":`)); err != nil {
				t.Fatal(err)
			}
		}},
		{"receipts", func(t *testing.T, rt *microsoftTestRuntime, key string) {
			t.Helper()
			if err := rt.Put(managementReceiptsKey(key), []byte(`{"version":99}`)); err != nil {
				t.Fatal(err)
			}
		}},
		{"progress", func(t *testing.T, rt *microsoftTestRuntime, key string) {
			t.Helper()
			if err := rt.Put(managementProgressKey(key), []byte(`not-json`)); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
			defer server.Close()
			rt := newMicrosoftTestRuntime(t)
			key := managementStateKey("microsoft", "microsoft365-audit-exchange")
			if tc.name != "head" && tc.name != "future-head" {
				head, err := json.Marshal(managementState{Version: managementStateVersion, CompletedThrough: managementTestNow.Add(-time.Hour)})
				if err != nil {
					t.Fatal(err)
				}
				if err := rt.Put(key, head); err != nil {
					t.Fatal(err)
				}
			}
			tc.seed(t, rt, key)
			wantCompletedThrough := time.Time{}
			switch tc.name {
			case "future-head":
				wantCompletedThrough = time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
			case "contents", "receipts", "progress":
				wantCompletedThrough = managementTestNow.Add(-time.Hour)
			}
			plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
			if _, err := plugin.collectManagement(t.Context(), rt, datasets["microsoft365-audit-exchange"]); err != nil {
				t.Fatalf("recovery error=%v", err)
			}
			state, err := loadManagementState(rt, key)
			if err != nil {
				t.Fatalf("replacement state error=%v", err)
			}
			if state.Version != managementStateVersion || state.hasPending() {
				t.Fatalf("replacement state=%+v", state)
			}
			if !state.CompletedThrough.Equal(wantCompletedThrough) {
				t.Fatalf("completed through=%s, want preserved durable boundary %s", state.CompletedThrough, wantCompletedThrough)
			}
		})
	}
}

// TestManagementExpiredWindowDropsUndeliveredContentAndAdvancesTheCheckpoint
// pins the window-level half of the same repair. A window whose own end has
// crossed the retention floor cannot be delivered at all: every blob it points
// at is gone and the re-list the refresh needs is rejected for a startTime
// outside retention. The checkpoint has to move past it, with the exact loss
// reported, instead of the selector spending one dead HTTP round trip per
// descriptor forever.
func TestManagementExpiredWindowDropsUndeliveredContentAndAdvancesTheCheckpoint(t *testing.T) {
	var contentCalls, listed atomic.Int64
	server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			listed.Add(1)
			_, _ = io.WriteString(w, `[]`)
		default:
			contentCalls.Add(1)
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":"AF20050","message":"expired"}}`)
		}
	})
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	windowStart := managementTestNow.Add(-9 * 24 * time.Hour)
	windowEnd := windowStart.Add(managementMaximumWindow)
	encoded, err := json.Marshal(managementState{
		Version:          managementStateVersion,
		CompletedThrough: windowStart,
		TargetThrough:    managementTestNow,
		WindowStart:      windowStart,
		WindowEnd:        windowEnd,
		ListingStarted:   true,
		ListingComplete:  true,
		Pages:            1,
		Contents: []managementContent{
			{ContentType: "Audit.Exchange", ContentID: "gone-one",
				ContentURI:     server.URL + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/gone-one",
				ContentCreated: windowStart.Add(time.Hour)},
			{ContentType: "Audit.Exchange", ContentID: "gone-two",
				ContentURI:     server.URL + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/gone-two",
				ContentCreated: windowStart.Add(2 * time.Hour)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(key, encoded); err != nil {
		t.Fatal(err)
	}

	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	plugin.now = func() time.Time { return managementTestNow }
	drainManagementSelector(t, plugin, rt)

	if got := contentCalls.Load(); got != 0 {
		t.Fatalf("dead content fetched %d times, want the whole aged-out window dropped without a request", got)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries=%d, want none from a window that is entirely beyond retention", got)
	}
	lines := rt.logsContaining("ERROR", "window aged out of source retention")
	if len(lines) != 1 {
		t.Fatalf("window-loss errors=%d, want exactly 1 (%+v)", len(lines), lines)
	}
	if got := lines[0].Params["skipped_contents"]; got != "2" {
		t.Fatalf("skipped_contents=%q, want both undelivered descriptors counted exactly", got)
	}
	if got := lines[0].Params["skipped_total"]; got != "2" {
		t.Fatalf("skipped_total=%q", got)
	}
	final, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if final.SkippedContents != 2 {
		t.Fatalf("permanent-loss total=%d, want 2", final.SkippedContents)
	}
	if len(final.Contents) != 0 || final.ContentIndex != 0 {
		t.Fatalf("the dead descriptors survived the drop: %d descriptors at index %d", len(final.Contents), final.ContentIndex)
	}
	if !final.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s, want the planned target %s", final.CompletedThrough, managementTestNow)
	}
	if got := rt.logsContaining("ERROR", "checkpoint is outside source retention"); len(got) != 1 {
		t.Fatalf("checkpoint-loss errors=%d, want the skipped interval named once (%+v)", len(got), got)
	}
}

// TestManagementDeliveryIsNotBoundedByTheVendorFetchDeadline pins the
// decoupling. The fetch deadline used to wrap the whole of collectManagement,
// including the entry-delivery loop, so on any link where the combined
// write and fsync latency summed past it every pass failed on a write
// -- not on anything the source did -- and the selector was pinned in
// rt.SetError while a backlog it was supposed to be draining aged toward the
// seven-day retention floor.
func TestManagementDeliveryIsNotBoundedByTheVendorFetchDeadline(t *testing.T) {
	const events = 4
	const writeDelay = 25 * time.Millisecond
	server := managementSingleContentServer(t, events)
	defer server.Close()

	rt := &delayedWriteRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(t), delay: writeDelay}
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	// Two writes are enough to exhaust the deadline the source work is
	// allowed; the cumulative vendor span stays generous so only the coupling is
	// under test.
	plugin.client.fetchDeadline = 2 * writeDelay
	plugin.managementFetchSpanLimit = 10 * time.Second

	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("error=%v, want delivery to outlive the vendor fetch deadline", err)
	}
	if got := len(rt.Entries()); got != events {
		t.Fatalf("entries=%d, want %d", got, events)
	}
	state, err := loadManagementState(rt, managementStateKey("microsoft", "microsoft365-audit-exchange"))
	if err != nil {
		t.Fatal(err)
	}
	if !state.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s, want the window finished in one pass", state.CompletedThrough)
	}
}

// TestManagementVendorRequestsStayBoundedByTheFetchDeadline is the other half of
// that decoupling: one request may not run for longer than the deadline just
// because the delivery loop no longer shares it.
func TestManagementVendorRequestsStayBoundedByTheFetchDeadline(t *testing.T) {
	server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/subscriptions/list") {
			time.Sleep(4 * time.Second)
		}
		writeEnabledSubscription(w, "Audit.Exchange")
	})
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	plugin.client.fetchDeadline = 50 * time.Millisecond
	started := time.Now()
	if _, err := plugin.Handle(t.Context(), rt); err == nil {
		t.Fatal("a vendor request that never answers must not succeed")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("poll took %s against a 50ms fetch deadline; the request is unbounded", elapsed)
	}
}

// TestManagementFetchSpanYieldsADurableContinuation pins the bound that replaces
// the single wrapped deadline. One pass may issue one request per content
// descriptor, so the cumulative vendor span is what keeps a selector from
// holding the poll for deadline x descriptors. Exhausting it must yield a
// durable continuation that makes progress on the next pass, not an error and
// not a wedge.
func TestManagementFetchSpanYieldsADurableContinuation(t *testing.T) {
	var calls atomic.Int64
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, "Audit.Exchange", "content-1",
				"/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content-1"))
		case strings.HasSuffix(r.URL.Path, "/audit/content-1"):
			_, _ = io.WriteString(w, `[{"Id":"one","CreationTime":"2026-10-08T11:40:00Z"}]`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	plugin.managementFetchSpanLimit = time.Nanosecond

	continuation, err := plugin.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("error=%v, want a durable continuation when the span is spent", err)
	}
	if continuation == nil || continuation.Delay != 0 {
		t.Fatalf("continuation=%v, want an immediate resume", continuation)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("vendor requests=%d, want the span to stop the pass after the first", got)
	}
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	deferred, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if deferred.ImmediateRuns != 1 || deferred.WindowStart.IsZero() {
		t.Fatalf("deferred state=%+v, want the planned window durably recorded", deferred)
	}

	// A pass with the ordinary span finishes the same window exactly once.
	plugin.managementFetchSpanLimit = 0
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("entries=%d, want exactly one delivery", got)
	}
	completed, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if !completed.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s", completed.CompletedThrough)
	}
}

// managementMultiContentServer serves one content blob per descriptor, each
// holding a single event, inside one window.
func managementMultiContentServer(t *testing.T, contents int) *httptest.Server {
	t.Helper()
	const base = "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/"
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			descriptors := make([]string, 0, contents)
			for index := 0; index < contents; index++ {
				id := fmt.Sprintf("content-%04d", index)
				descriptors = append(descriptors, managementDescriptor(server.URL, "Audit.Exchange", id, base+id))
			}
			_, _ = fmt.Fprintf(w, "[%s]", strings.Join(descriptors, ","))
		case strings.Contains(r.URL.Path, base):
			id := r.URL.Path[strings.Index(r.URL.Path, base)+len(base):]
			_, _ = fmt.Fprintf(w, `[{"Id":%q,"CreationTime":"2026-10-08T11:40:00Z"}]`, id)
		default:
			http.NotFound(w, r)
		}
	})
	return server
}

type managementContentWriteProfile struct {
	headPuts, headBytes, headLargest int
	contentsPuts, contentsLargest    int
	receiptsPuts                     int
	entries                          int
}

func collectManagementContentWriteProfile(t *testing.T, contents int) managementContentWriteProfile {
	t.Helper()
	server := managementMultiContentServer(t, contents)
	defer server.Close()
	rt := newCountingManagementRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	headPuts, headBytes := rt.count(key)
	contentsPuts, _ := rt.count(managementContentsKey(key))
	receiptsPuts, _ := rt.count(managementReceiptsKey(key))
	return managementContentWriteProfile{
		headPuts: headPuts, headBytes: headBytes, headLargest: rt.largestPut(key),
		contentsPuts: contentsPuts, contentsLargest: rt.largestPut(managementContentsKey(key)),
		receiptsPuts: receiptsPuts,
		entries:      len(rt.Entries()),
	}
}

// TestManagementPerContentDurabilityDoesNotRewriteTheDescriptorOrReceiptSets
// pins the write-amplification repair one level above the per-event checkpoint.
// Every content completion used to re-read, re-validate, re-marshal and re-fsync
// the whole state document, which is bounded only by
// managementMaximumDescriptors and managementMaximumReceipts and reaches tens of
// megabytes at those bounds. Finishing one more content blob must cost one more
// small write, not another copy of both sets.
func TestManagementPerContentDurabilityDoesNotRewriteTheDescriptorOrReceiptSets(t *testing.T) {
	const few, many = 4, 32
	small := collectManagementContentWriteProfile(t, few)
	large := collectManagementContentWriteProfile(t, many)

	if small.entries != few || large.entries != many {
		t.Fatalf("delivered entries small=%d large=%d", small.entries, large.entries)
	}
	// The descriptor set is written by the listing loop and at the window
	// boundary only, and the receipt set only when the window closes.
	if small.contentsPuts != large.contentsPuts {
		t.Fatalf("descriptor-set writes scale with content count: %d contents=%d writes, %d contents=%d writes",
			few, small.contentsPuts, many, large.contentsPuts)
	}
	if small.receiptsPuts != large.receiptsPuts {
		t.Fatalf("receipt-set writes scale with content count: %d contents=%d writes, %d contents=%d writes",
			few, small.receiptsPuts, many, large.receiptsPuts)
	}
	// The head document is written per content, so it is the one that has to
	// stay small whatever the descriptor set is doing.
	if large.headPuts <= small.headPuts {
		t.Fatalf("head writes=%d did not grow with content count (%d at %d contents)", large.headPuts, small.headPuts, few)
	}
	if large.headLargest > small.headLargest+256 {
		t.Fatalf("largest head document grew from %d to %d bytes with content count", small.headLargest, large.headLargest)
	}
	if perWrite := large.headBytes / large.headPuts; perWrite > 1024 {
		t.Fatalf("head document averages %d bytes per write, want a small fixed document", perWrite)
	}
	// Non-vacuous: the descriptor set really is large and really is excluded.
	if large.contentsLargest <= small.contentsLargest {
		t.Fatalf("descriptor document did not grow with content count: %d then %d bytes", small.contentsLargest, large.contentsLargest)
	}
	if large.contentsLargest <= large.headLargest {
		t.Fatalf("descriptor document=%d bytes is not larger than the head=%d, so nothing was excluded",
			large.contentsLargest, large.headLargest)
	}
}

// TestManagementMembershipIsIndexedNotScanned pins that both dedup checks are
// answered from an index built once per pass. The delivery loop asks once per
// content and the listing loop once per listed descriptor, so the linear
// helpers this replaced cost O(descriptors x receipts) string comparisons per
// window. A descriptor appended straight to the slice, bypassing the mutator,
// must be invisible: a scan would find it.
func TestManagementMembershipIsIndexedNotScanned(t *testing.T) {
	state := managementState{
		Version:  managementStateVersion,
		Contents: []managementContent{{ContentType: "Audit.Exchange", ContentID: "indexed", ContentURI: "https://manage.office.com/a"}},
		Receipts: []managementReceipt{{ContentID: "receipted", ContentCreated: managementTestNow}},
	}
	state.indexMembership()
	if !state.hasContent("indexed") || !state.hasReceipt("receipted") {
		t.Fatal("the index does not reflect the decoded sets")
	}

	state.Contents = append(state.Contents, managementContent{ContentType: "Audit.Exchange", ContentID: "unindexed", ContentURI: "https://manage.office.com/b"})
	state.Receipts = append(state.Receipts, managementReceipt{ContentID: "unreceipted", ContentCreated: managementTestNow})
	if state.hasContent("unindexed") || state.hasReceipt("unreceipted") {
		t.Fatal("membership was answered by a linear scan of the set")
	}

	state.appendContent(managementContent{ContentType: "Audit.Exchange", ContentID: "added", ContentURI: "https://manage.office.com/c"})
	state.appendReceipt(managementReceipt{ContentID: "acknowledged", ContentCreated: managementTestNow})
	if !state.hasContent("added") || !state.hasReceipt("acknowledged") {
		t.Fatal("the mutators did not update the index")
	}

	// Bounding the receipt set forgets exactly the identifiers that were dropped.
	kept, dropped := trimManagementReceipts(state.Receipts, 1)
	state.retainReceipts(kept, dropped)
	if len(dropped) == 0 {
		t.Fatal("nothing was trimmed")
	}
	for _, receipt := range dropped {
		if state.hasReceipt(receipt.ContentID) {
			t.Fatalf("trimmed receipt %q is still indexed", receipt.ContentID)
		}
	}
	for _, receipt := range kept {
		if !state.hasReceipt(receipt.ContentID) {
			t.Fatalf("retained receipt %q left the index", receipt.ContentID)
		}
	}

	state.resetContents()
	if state.hasContent("indexed") || state.hasContent("added") || len(state.Contents) != 0 {
		t.Fatalf("resetting the descriptor set left membership behind: %+v", state.Contents)
	}
}

// TestManagementNarrowedWindowsStayAtWireWindowPrecision pins the boundary
// repair. managementContentListURL formats startTime with time.RFC3339, which
// drops fractional seconds, so a window boundary carrying a fraction asks the
// source for more than the state holds. Halving without truncation produced
// exactly that after eight narrowings (24h down to 5m37.5s), and the completion
// path carried the fraction into the next window's start through
// CompletedThrough.
func TestManagementNarrowedWindowsStayAtWireWindowPrecision(t *testing.T) {
	state := managementState{
		Version:     managementStateVersion,
		WindowStart: managementTestNow.Add(-managementMaximumWindow),
		WindowEnd:   managementTestNow,
	}
	spans := []time.Duration{}
	for narrowings := 0; narrowings < 64; narrowings++ {
		if !narrowManagementWindow(&state) {
			break
		}
		span := state.WindowEnd.Sub(state.WindowStart)
		spans = append(spans, span)
		if span%managementWireWindowPrecision != 0 {
			t.Fatalf("narrowing %d produced span %s, which time.RFC3339 cannot express", narrowings, span)
		}
		if !state.WindowEnd.Equal(state.WindowEnd.Truncate(managementWireWindowPrecision)) {
			t.Fatalf("narrowing %d produced window end %s below wire precision", narrowings, state.WindowEnd)
		}
		if err := validateManagementState(state); err != nil {
			t.Fatalf("narrowing %d is not persistable: %v", narrowings, err)
		}
	}
	if len(spans) < 9 {
		t.Fatalf("narrowed %d times before reaching the floor, want the sub-second depth covered", len(spans))
	}
	// The step that used to introduce the fraction: 11m15s halves to 5m37.5s.
	if spans[6] != 11*time.Minute+15*time.Second || spans[7] != 5*time.Minute+37*time.Second {
		t.Fatalf("spans[6]=%s spans[7]=%s, want 11m15s then a truncated 5m37s", spans[6], spans[7])
	}
	if spans[len(spans)-1] != managementMinimumWindow {
		t.Fatalf("final span=%s, want the one-minute floor", spans[len(spans)-1])
	}
}

// TestManagementBoundaryDescriptorsAreCollectedOrReportedNotFatal pins the other
// half of the boundary repair. The half-open assertion ran ahead of the dedup
// skip and returned a hard error, so a descriptor in the sub-second gap the
// truncated wire query legitimately exposes -- or any boundary fuzziness from
// the source -- failed the whole listing. The failing page was reached from the
// persisted NextPage on every later poll, so the selector stalled until the
// window crossed the retention floor and the entire interval was lost.
func TestManagementBoundaryDescriptorsAreCollectedOrReportedNotFatal(t *testing.T) {
	const base = "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/"
	// A legacy fractional boundary, exactly what an untruncated halving left
	// behind in CompletedThrough.
	windowStart := managementTestNow.Add(-time.Hour).Add(500 * time.Millisecond)
	windowEnd := managementTestNow.Add(-30 * time.Minute)
	gapCreated := windowStart.Add(-200 * time.Millisecond)

	var delivered atomic.Int64
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			descriptor := func(id string, created time.Time) string {
				return fmt.Sprintf(`{"contentType":"Audit.Exchange","contentId":%q,"contentUri":%q,"contentCreated":%q}`,
					id, server.URL+base+id, created.UTC().Format(time.RFC3339Nano))
			}
			_, _ = fmt.Fprintf(w, "[%s,%s,%s]",
				descriptor("in-the-gap", gapCreated),
				descriptor("past-the-end", windowEnd),
				descriptor("already-acknowledged", windowStart.Add(time.Minute)))
		case strings.Contains(r.URL.Path, base):
			delivered.Add(1)
			_, _ = io.WriteString(w, `[{"Id":"boundary","CreationTime":"2026-10-08T11:35:00Z"}]`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	encoded, err := json.Marshal(managementState{
		Version:          managementStateVersion,
		CompletedThrough: windowStart,
		TargetThrough:    windowEnd,
		WindowStart:      windowStart,
		WindowEnd:        windowEnd,
		Receipts:         []managementReceipt{{ContentID: "already-acknowledged", ContentCreated: windowStart.Add(time.Minute)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(key, encoded); err != nil {
		t.Fatal(err)
	}

	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	plugin.now = func() time.Time { return managementTestNow }
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("error=%v, want a boundary descriptor handled rather than fatal", err)
	}

	// The descriptor inside the truncated request range is collected, not lost.
	if got := delivered.Load(); got != 1 {
		t.Fatalf("content fetches=%d, want the sub-second gap descriptor collected exactly once", got)
	}
	entries := rt.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries=%d, want the gap descriptor delivered", len(entries))
	}
	// The descriptor past the end is reported, not silently dropped; the window
	// that starts at this WindowEnd is the one that covers it.
	warnings := rt.logsContaining("WARN", "outside the requested window")
	if len(warnings) != 1 {
		t.Fatalf("boundary warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
	}
	if got := warnings[0].Params["count"]; got != "1" {
		t.Fatalf("count=%q, want only the descriptor past the window end", got)
	}
	if got := warnings[0].Params["content_id"]; got != "past-the-end" {
		t.Fatalf("content_id=%q", got)
	}
	state, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if !state.CompletedThrough.Equal(windowEnd) {
		t.Fatalf("completed through %s, want the listing to finish at %s", state.CompletedThrough, windowEnd)
	}
	if !state.hasReceipt("in-the-gap") {
		t.Fatal("the collected boundary descriptor has no delivery receipt")
	}
}

// TestManagementEquivalentSerializationKeepsDurableProgress proves that
// insignificant response changes cannot reset an in-progress ordinal. A
// contentId served with alternating whitespace and object-member order must
// finish across restarts instead of replaying its first batch forever.
func TestManagementEquivalentSerializationKeepsDurableProgress(t *testing.T) {
	const (
		selector    = "microsoft365-audit-exchange"
		contentType = "Audit.Exchange"
		contentID   = "content-equivalent"
	)

	var first, second strings.Builder
	first.WriteByte('[')
	second.WriteString("[ ")
	for i := 0; i < managementMaximumEntriesPerHandle+5; i++ {
		if i > 0 {
			first.WriteByte(',')
			second.WriteString(",\n")
		}
		_, _ = fmt.Fprintf(&first, `{"Id":"event-%d","CreationTime":"2026-10-08T11:40:00Z","Operation":"MailItemsAccessed"}`, i)
		_, _ = fmt.Fprintf(&second, `{"Operation":"MailItemsAccessed","Id":"event-%d","CreationTime":"2026-10-08T11:40:00Z"}`, i)
	}
	first.WriteByte(']')
	second.WriteString(" ]")

	var contentCalls atomic.Int64
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, contentType, contentID,
				"/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/"+contentID))
		case strings.HasSuffix(r.URL.Path, "/audit/"+contentID):
			if contentCalls.Add(1)%2 == 1 {
				_, _ = io.WriteString(w, first.String())
				return
			}
			_, _ = io.WriteString(w, second.String())
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	runtime := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", selector)
	syncer := &managementSyncController{}
	plugin := newManagementTestPlugin(t, server, selector, syncer.Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatalf("initial partial delivery: %v", err)
	}
	if got := len(runtime.Entries()); got != managementMaximumEntriesPerHandle {
		t.Fatalf("initial entries=%d, want %d", got, managementMaximumEntriesPerHandle)
	}

	plugin = newManagementTestPlugin(t, server, selector, syncer.Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatalf("resume equivalent serialization: %v", err)
	}
	if got := len(runtime.Entries()); got != managementMaximumEntriesPerHandle+5 {
		t.Fatalf("final entries=%d, want %d", got, managementMaximumEntriesPerHandle+5)
	}
	if got := contentCalls.Load(); got != 2 {
		t.Fatalf("content requests=%d, want 2", got)
	}
	if warnings := runtime.logsContaining("WARN", "content changed while partially delivered"); len(warnings) != 0 {
		t.Fatalf("equivalent serialization produced %d change warnings", len(warnings))
	}
	state, err := loadManagementState(runtime, key)
	if err != nil {
		t.Fatal(err)
	}
	if !state.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s, want %s", state.CompletedThrough, managementTestNow)
	}
}

// TestManagementRepeatedSemanticChangesRemainBounded covers an upstream
// contract violation without allowing it to become a collection hot loop. The
// plugin replays a bounded number of changed representations, then keeps its
// durable ordinal so the content type can finish with an explicit warning.
func TestManagementRepeatedSemanticChangesRemainBounded(t *testing.T) {
	const (
		selector              = "microsoft365-audit-exchange"
		contentType           = "Audit.Exchange"
		contentID             = "content-unstable"
		expectedMaximumReplay = 3
	)
	if managementMaximumContentReplays != expectedMaximumReplay {
		t.Fatalf("maximum content replays=%d, want reviewed bound %d", managementMaximumContentReplays, expectedMaximumReplay)
	}

	buildBody := func(operation string) string {
		var body strings.Builder
		body.WriteByte('[')
		for i := 0; i < managementMaximumEntriesPerHandle+5; i++ {
			if i > 0 {
				body.WriteByte(',')
			}
			_, _ = fmt.Fprintf(&body, `{"Id":"event-%d","CreationTime":"2026-10-08T11:40:00Z","Operation":"%s"}`, i, operation)
		}
		body.WriteByte(']')
		return body.String()
	}
	firstBody := buildBody("FirstRepresentation")
	secondBody := buildBody("SecondRepresentation")

	var contentCalls atomic.Int64
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, contentType, contentID,
				"/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/"+contentID))
		case strings.HasSuffix(r.URL.Path, "/audit/"+contentID):
			if contentCalls.Add(1)%2 == 1 {
				_, _ = io.WriteString(w, firstBody)
				return
			}
			_, _ = io.WriteString(w, secondBody)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	runtime := newMicrosoftTestRuntime(t)
	syncer := &managementSyncController{}
	key := managementStateKey("microsoft", selector)
	for pass := 0; pass < expectedMaximumReplay+2; pass++ {
		plugin := newManagementTestPlugin(t, server, selector, syncer.Sync)
		if _, err := plugin.Handle(t.Context(), runtime); err != nil {
			t.Fatalf("pass %d: %v", pass+1, err)
		}
	}
	if got, want := len(runtime.Entries()), (expectedMaximumReplay+1)*managementMaximumEntriesPerHandle+5; got != want {
		t.Fatalf("entries=%d, want bounded replay total %d", got, want)
	}
	if got := contentCalls.Load(); got != expectedMaximumReplay+2 {
		t.Fatalf("content requests=%d, want %d", got, expectedMaximumReplay+2)
	}
	if got := len(runtime.logsContaining("WARN", "replaying it from the beginning")); got != expectedMaximumReplay {
		t.Fatalf("replay warnings=%d, want %d", got, expectedMaximumReplay)
	}
	if got := len(runtime.logsContaining("WARN", "continuing from the durable ordinal")); got != 1 {
		t.Fatalf("bounded-continuation warnings=%d, want 1", got)
	}
	state, err := loadManagementState(runtime, key)
	if err != nil {
		t.Fatal(err)
	}
	if !state.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s, want %s", state.CompletedThrough, managementTestNow)
	}
}

// TestManagementChangedContentReplaysFromDurableBoundary pins recovery when
// the source returns semantically changed content for a stable contentId after
// part of the blob was already accepted. The old behavior returned the same
// error on every poll until the entire window aged out of retention.
func TestManagementChangedContentReplaysFromDurableBoundary(t *testing.T) {
	const (
		selector    = "microsoft365-audit-exchange"
		contentType = "Audit.Exchange"
		contentID   = "content-changing"
	)

	var body strings.Builder
	body.WriteByte('[')
	for i := 0; i < managementMaximumEntriesPerHandle+5; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		_, _ = fmt.Fprintf(&body, `{"Id":"event-%d","CreationTime":"2026-10-08T11:40:00Z","Operation":"MailItemsAccessed"}`, i)
	}
	body.WriteByte(']')
	firstBody := body.String()
	changedBody := strings.Replace(firstBody, `"Operation":"MailItemsAccessed"`, `"Operation":"ContentChanged"`, 1)

	var contentCalls atomic.Int64
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, contentType, contentID,
				"/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/"+contentID))
		case strings.HasSuffix(r.URL.Path, "/audit/"+contentID):
			if contentCalls.Add(1) == 1 {
				_, _ = io.WriteString(w, firstBody)
				return
			}
			_, _ = io.WriteString(w, changedBody)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	syncer := &managementSyncController{}
	runtime := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", selector)

	plugin := newManagementTestPlugin(t, server, selector, syncer.Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatalf("initial partial delivery: %v", err)
	}
	if got := len(runtime.Entries()); got != managementMaximumEntriesPerHandle {
		t.Fatalf("initial entries=%d, want %d", got, managementMaximumEntriesPerHandle)
	}
	initial, err := loadManagementState(runtime, key)
	if err != nil {
		t.Fatal(err)
	}
	if initial.EventIndex != managementMaximumEntriesPerHandle || initial.ActiveDigest == "" {
		t.Fatalf("initial progress index=%d digest=%q", initial.EventIndex, initial.ActiveDigest)
	}

	// The reset must be synchronized before the first replayed write. Force that
	// synchronization to fail and verify that neither delivery nor durable
	// progress moved.
	// The next synchronization persists the already-enabled subscription. Fail
	// the following synchronization, which is the changed-content replay reset
	// this phase is intended to exercise.
	syncer.failAt.Store(syncer.calls.Load() + 2)
	plugin = newManagementTestPlugin(t, server, selector, syncer.Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err == nil {
		t.Fatal("changed-content replay succeeded despite failed state synchronization")
	}
	if got := len(runtime.Entries()); got != managementMaximumEntriesPerHandle {
		t.Fatalf("entries after failed replay checkpoint=%d, want %d", got, managementMaximumEntriesPerHandle)
	}
	afterFailure, err := loadManagementState(runtime, key)
	if err != nil {
		t.Fatal(err)
	}
	if afterFailure.EventIndex != initial.EventIndex || afterFailure.ActiveDigest != initial.ActiveDigest {
		t.Fatalf("failed replay checkpoint advanced to index=%d digest_changed=%v",
			afterFailure.EventIndex, afterFailure.ActiveDigest != initial.ActiveDigest)
	}
	if warnings := runtime.logsContaining("WARN", "content changed while partially delivered"); len(warnings) != 0 {
		t.Fatalf("failed replay checkpoint published %d warnings before it was durable", len(warnings))
	}

	// Recreate the plugin again to prove that the successfully written replay
	// boundary is durable across the same restart that previously resumed the
	// terminal mismatch forever.
	syncer.failAt.Store(0)
	plugin = newManagementTestPlugin(t, server, selector, syncer.Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatalf("changed-content replay: %v", err)
	}
	if got := len(runtime.Entries()); got != 2*managementMaximumEntriesPerHandle {
		t.Fatalf("entries after replay=%d, want %d", got, 2*managementMaximumEntriesPerHandle)
	}
	replayed, err := loadManagementState(runtime, key)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.EventIndex != managementMaximumEntriesPerHandle || replayed.ActiveDigest == initial.ActiveDigest {
		t.Fatalf("replay progress index=%d digest_changed=%v", replayed.EventIndex, replayed.ActiveDigest != initial.ActiveDigest)
	}
	warnings := runtime.logsContaining("WARN", "content changed while partially delivered")
	if len(warnings) != 1 {
		t.Fatalf("replay warnings=%d, want 1 (%+v)", len(warnings), warnings)
	}
	if got := warnings[0].Params["previous_event_index"]; got != "1000" {
		t.Fatalf("previous_event_index=%q, want 1000", got)
	}

	plugin = newManagementTestPlugin(t, server, selector, syncer.Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatalf("resume replayed content: %v", err)
	}
	if got := len(runtime.Entries()); got != 2*managementMaximumEntriesPerHandle+5 {
		t.Fatalf("final entries=%d, want %d", got, 2*managementMaximumEntriesPerHandle+5)
	}
	final, err := loadManagementState(runtime, key)
	if err != nil {
		t.Fatal(err)
	}
	if !final.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s, want %s", final.CompletedThrough, managementTestNow)
	}

	counts := make(map[string]int, managementMaximumEntriesPerHandle+5)
	for _, ent := range runtime.Entries() {
		var event struct {
			ID string `json:"Id"`
		}
		if err := json.Unmarshal(ent.Data, &event); err != nil {
			t.Fatal(err)
		}
		counts[event.ID]++
	}
	for i := 0; i < managementMaximumEntriesPerHandle+5; i++ {
		want := 1
		if i < managementMaximumEntriesPerHandle {
			want = 2
		}
		if got := counts[fmt.Sprintf("event-%d", i)]; got != want {
			t.Fatalf("event-%d deliveries=%d, want %d", i, got, want)
		}
	}
}

// TestManagementContentDigestPreservesLargeIntegerIdentity pins UseNumber in
// the semantic digest. Decoding through float64 would collapse these adjacent
// integers and treat two meaningfully different source records as equivalent.
func TestManagementContentDigestPreservesLargeIntegerIdentity(t *testing.T) {
	first, err := managementContentDigest([]json.RawMessage{json.RawMessage(`{"value":9007199254740992}`)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := managementContentDigest([]json.RawMessage{json.RawMessage(`{"value":9007199254740993}`)})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("semantic digest collapsed distinct integers above the exact float64 range")
	}
}

// TestManagementRepeatedChangeClampsOrdinalToShorterBody pins the final
// liveness guard after the replay budget is exhausted. If a contract-violating
// contentId shrinks, its durable ordinal must clamp to the new length so the
// content type can finish instead of failing forever.
func TestManagementRepeatedChangeClampsOrdinalToShorterBody(t *testing.T) {
	const (
		selector    = "microsoft365-audit-exchange"
		contentType = "Audit.Exchange"
		contentID   = "content-shortened"
	)

	buildBody := func(count int, operation string) string {
		var body strings.Builder
		body.WriteByte('[')
		for i := 0; i < count; i++ {
			if i > 0 {
				body.WriteByte(',')
			}
			_, _ = fmt.Fprintf(&body, `{"Id":"event-%d","CreationTime":"2026-10-08T11:40:00Z","Operation":"%s"}`, i, operation)
		}
		body.WriteByte(']')
		return body.String()
	}
	longBody := buildBody(managementMaximumEntriesPerHandle+5, "Initial")
	shortBody := buildBody(5, "Shortened")

	var contentCalls atomic.Int64
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, contentType, contentID,
				"/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/"+contentID))
		case strings.HasSuffix(r.URL.Path, "/audit/"+contentID):
			if contentCalls.Add(1) == 1 {
				_, _ = io.WriteString(w, longBody)
				return
			}
			_, _ = io.WriteString(w, shortBody)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	runtime := newMicrosoftTestRuntime(t)
	syncer := &managementSyncController{}
	key := managementStateKey("microsoft", selector)
	plugin := newManagementTestPlugin(t, server, selector, syncer.Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatalf("initial partial delivery: %v", err)
	}
	state, err := loadManagementState(runtime, key)
	if err != nil {
		t.Fatal(err)
	}
	if state.EventIndex != managementMaximumEntriesPerHandle {
		t.Fatalf("initial event index=%d, want %d", state.EventIndex, managementMaximumEntriesPerHandle)
	}
	state.ActiveDigestChanges = managementMaximumContentReplays
	if err := plugin.persistManagementState(runtime, key, &state); err != nil {
		t.Fatalf("seed exhausted replay budget: %v", err)
	}

	plugin = newManagementTestPlugin(t, server, selector, syncer.Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatalf("shortened content after replay exhaustion: %v", err)
	}
	if got := len(runtime.Entries()); got != managementMaximumEntriesPerHandle {
		t.Fatalf("entries=%d, want no redelivery after clamping to the shorter body", got)
	}
	final, err := loadManagementState(runtime, key)
	if err != nil {
		t.Fatal(err)
	}
	if !final.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed through %s, want %s", final.CompletedThrough, managementTestNow)
	}
	if got := len(runtime.logsContaining("WARN", "continuing from the durable ordinal")); got != 1 {
		t.Fatalf("bounded-continuation warnings=%d, want 1", got)
	}
}

// managementRepeatedContinuationServer models a source whose content listing
// publishes a NextPageUri it has already served. The second page's own URL is
// what both pages publish, so the plugin reads page one, reads page two, and is
// then handed page two again. Clearing repeat ends the defect, which is what the
// window after the truncated one has to be able to collect.
func managementRepeatedContinuationServer(t *testing.T, repeat *atomic.Bool, listCalls *atomic.Int64) *httptest.Server {
	t.Helper()
	const contentType = "Audit.Exchange"
	first := managementCapacityContent{id: "content-first", created: managementTestNow.Add(-2 * time.Hour)}
	second := managementCapacityContent{id: "content-second", created: managementTestNow.Add(-1 * time.Hour)}
	third := managementCapacityContent{id: "content-third", created: managementTestNow.Add(30 * time.Minute)}
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			listCalls.Add(1)
			if !repeat.Load() {
				_, _ = fmt.Fprintf(w, "[%s]", managementDescriptorAt(server.URL, contentType, third))
				return
			}
			w.Header().Set("NextPageUri", server.URL+
				"/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/subscriptions/content?contentType="+contentType+"&page=2")
			listed := first
			if r.URL.Query().Get("page") == "2" {
				listed = second
			}
			_, _ = fmt.Fprintf(w, "[%s]", managementDescriptorAt(server.URL, contentType, listed))
		case strings.Contains(r.URL.Path, managementCapacityContentPath):
			_, _ = fmt.Fprintf(w, `[{"Id":%q,"CreationTime":"2026-10-08T11:40:00Z","Operation":"MailItemsAccessed"}]`,
				strings.TrimPrefix(r.URL.Path, managementCapacityContentPath))
		default:
			http.NotFound(w, r)
		}
	})
	return server
}

// TestManagementRepeatedListingContinuationEndsTheWindowInsteadOfWedging pins
// the listing-continuation repair. Both halves of the repeat detection are
// durable -- the page history and the continuation itself are persisted by the
// page that published them -- so returning the repeat as this pass's error left
// the next poll reading back the same pair, refusing the same URI before it
// issued a request, and producing the same error until the window crossed the
// seven-day retention floor, at which point the interval it covers was lost
// with it. The listing has to end at the repeat the way a policy-refused
// continuation does: what was listed is delivered, the loss is reported, and
// the window that follows starts at this WindowEnd.
func TestManagementRepeatedListingContinuationEndsTheWindowInsteadOfWedging(t *testing.T) {
	var repeat atomic.Bool
	var listCalls atomic.Int64
	repeat.Store(true)
	server := managementRepeatedContinuationServer(t, &repeat, &listCalls)
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	syncer := &managementSyncController{}
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)

	if _, err := plugin.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "repeated a NextPageUri") {
		t.Fatalf("error=%v, want the repeat reported as this pass's error", err)
	}
	if got := listCalls.Load(); got != 2 {
		t.Fatalf("listing requests=%d, want the two distinct pages and no dispatch of the repeat", got)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries=%d, want delivery of the listed pages left to the next pass", got)
	}
	warnings := rt.logsContaining("WARN", "refused a listing continuation")
	if len(warnings) != 1 {
		t.Fatalf("continuation warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
	}
	if got := warnings[0].Params["reason"]; !strings.Contains(got, "repeated a NextPageUri") {
		t.Fatalf("reason=%q, want the repeat named for the operator", got)
	}
	if got := rt.warnStateContaining("refused 0 content descriptor(s) and 1 continuation(s)"); len(got) != 1 {
		t.Fatalf("published status warnings=%v, want the bounded loss on the channel the poll loop leaves alone", got)
	}
	truncated, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if truncated.NextPage != "" || !truncated.ListingComplete || len(truncated.Contents) != 2 {
		t.Fatalf("durable state after the repeat=%+v, want the repeat cleared and both listed descriptors kept", truncated)
	}

	// The next poll delivers what the truncated listing did reach, without
	// re-listing anything, which is the progress the permanent error could never
	// make.
	plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("resumed poll error=%v, want progress rather than a permanent wedge", err)
	}
	if got := listCalls.Load(); got != 2 {
		t.Fatalf("listing requests=%d, want the completed listing resumed from durable state", got)
	}
	if got := len(rt.Entries()); got != 2 {
		t.Fatalf("entries=%d, want one per listed descriptor", got)
	}
	delivered, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if !delivered.CompletedThrough.Equal(managementTestNow) || delivered.hasPending() {
		t.Fatalf("state after delivery=%+v, want the truncated window closed at its own end", delivered)
	}

	// A later poll collects the window that follows, so the selector was never
	// pinned behind the repeat.
	repeat.Store(false)
	plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	plugin.now = func() time.Time { return managementTestNow.Add(time.Hour) }
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("following poll error=%v", err)
	}
	if got := len(rt.Entries()); got != 3 {
		t.Fatalf("entries=%d, want the following window's content collected exactly once", got)
	}
	final, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if !final.CompletedThrough.Equal(managementTestNow.Add(time.Hour)) || final.hasPending() {
		t.Fatalf("final state=%+v, want the checkpoint advanced through the following window", final)
	}
}

// TestManagementPersistedRepeatedContinuationNeedsNoRequestAndStillProgresses
// is the durable half of the same repair: the state a repeat leaves behind is
// exactly what the old error read back on every later poll. A pass that loads a
// NextPage its own page history already holds issues no listing request at all,
// and the selector still finishes the window it had already listed.
func TestManagementPersistedRepeatedContinuationNeedsNoRequestAndStillProgresses(t *testing.T) {
	const contentType = "Audit.Exchange"
	var listCalls, contentCalls atomic.Int64
	server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			listCalls.Add(1)
			_, _ = io.WriteString(w, `[]`)
		case strings.Contains(r.URL.Path, managementCapacityContentPath):
			contentCalls.Add(1)
			_, _ = io.WriteString(w, `[{"Id":"listed","CreationTime":"2026-10-08T11:40:00Z","Operation":"MailItemsAccessed"}]`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	syncer := &managementSyncController{}
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	windowStart := managementTestNow.Add(-time.Hour)
	repeated := server.URL +
		"/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/subscriptions/content?contentType=" + contentType + "&page=2"
	encoded, err := json.Marshal(managementState{
		Version:          managementStateVersion,
		CompletedThrough: windowStart,
		TargetThrough:    managementTestNow,
		WindowStart:      windowStart,
		WindowEnd:        managementTestNow,
		ListingStarted:   true,
		Pages:            2,
		NextPage:         repeated,
		RecentPages: []string{
			plugin.managementContentListURL(contentType, windowStart, managementTestNow),
			repeated,
		},
		Contents: []managementContent{{
			ContentType: contentType, ContentID: "content-listed",
			ContentURI:     server.URL + managementCapacityContentPath + "content-listed",
			ContentCreated: windowStart.Add(30 * time.Minute),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(key, encoded); err != nil {
		t.Fatal(err)
	}

	if _, err := plugin.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "repeated a NextPageUri") {
		t.Fatalf("error=%v, want the persisted repeat refused before dispatch", err)
	}
	if got := listCalls.Load(); got != 0 {
		t.Fatalf("listing requests=%d, want the persisted repeat refused without one", got)
	}
	if got := contentCalls.Load(); got != 0 {
		t.Fatalf("content requests=%d, want delivery left to the next pass", got)
	}

	plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("resumed poll error=%v, want the already listed descriptor delivered", err)
	}
	if got := listCalls.Load(); got != 0 {
		t.Fatalf("listing requests=%d, want the durable listing honoured", got)
	}
	if got, want := len(rt.Entries()), 1; got != want {
		t.Fatalf("entries=%d, want %d", got, want)
	}
	final, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if !final.CompletedThrough.Equal(managementTestNow) || final.hasPending() {
		t.Fatalf("final state=%+v, want the window closed at its own end", final)
	}
}

// managementTestOrdinalDigest is the semantic digest of the body
// managementCapacityServer serves for contentID, so a seeded state document can
// name the content it is part way through without tripping the changed-content
// replay path.
func managementTestOrdinalDigest(t *testing.T, contentID string) string {
	t.Helper()
	event := fmt.Sprintf(`{"Id":%q,"CreationTime":"2026-10-08T11:40:00Z","Operation":"MailItemsAccessed"}`, contentID)
	digest, err := managementContentDigest([]json.RawMessage{json.RawMessage(event)})
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// seedManagementDeliveryState writes the durable documents of a selector that
// has listed one content blob and is part way through delivering it, with the
// head ordinal and the delivery checkpoint ordinal set independently so either
// one can carry the corruption.
func seedManagementDeliveryState(t *testing.T, rt *microsoftTestRuntime, key, serverURL string, content managementCapacityContent, headOrdinal, progressOrdinal int) {
	t.Helper()
	digest := managementTestOrdinalDigest(t, content.id)
	head, err := json.Marshal(managementState{
		Version:          managementStateVersion,
		CompletedThrough: managementTestNow.Add(-time.Hour),
		TargetThrough:    managementTestNow,
		WindowStart:      managementTestNow.Add(-time.Hour),
		WindowEnd:        managementTestNow,
		ListingStarted:   true,
		ListingComplete:  true,
		Pages:            1,
		ActiveContentID:  content.id,
		ActiveDigest:     digest,
		EventIndex:       headOrdinal,
		Contents: []managementContent{{
			ContentType: "Audit.Exchange", ContentID: content.id,
			ContentURI:     serverURL + managementCapacityContentPath + content.id,
			ContentCreated: content.created,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(key, head); err != nil {
		t.Fatal(err)
	}
	progress, err := json.Marshal(managementProgress{
		Version: managementProgressVersion, ContentIndex: 0,
		ActiveContentID: content.id, ActiveDigest: digest, EventIndex: progressOrdinal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(managementProgressKey(key), progress); err != nil {
		t.Fatal(err)
	}
}

// TestManagementCorruptEventOrdinalRecoversFromTheDurableCheckpoint pins the
// structural bound on the delivery ordinal. managementMaximumEventsPerContent is
// the absolute bound: decodeManagementEvents refuses the event past it before it
// is retained, so no blob the plugin can fetch holds more and no delivery loop
// can count past it. Bounding only the sign of the field let a corrupt ordinal
// through load validation in either document, and the delivery loop then
// compared it against the fetched event count, failed, and mutated nothing at
// all -- so every later poll fetched the same blob and failed identically until
// the window aged out of the source's seven-day retention. Both documents have
// to route through recoverManagementState instead, keeping the completed
// boundary and delivering nothing until that reset is durable.
func TestManagementCorruptEventOrdinalRecoversFromTheDurableCheckpoint(t *testing.T) {
	for _, tc := range []struct {
		name           string
		head, progress int
	}{
		{"head", managementMaximumEventsPerContent + 1, 0},
		{"progress", 0, managementMaximumEventsPerContent + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := managementCapacityContent{id: "content-ordinal", created: managementTestNow.Add(-30 * time.Minute)}
			fetched := make(map[string]*atomic.Int64)
			server := managementCapacityServer(t, []managementCapacityContent{content}, fetched)
			defer server.Close()

			rt := newMicrosoftTestRuntime(t)
			key := managementStateKey("microsoft", "microsoft365-audit-exchange")
			seedManagementDeliveryState(t, rt, key, server.URL, content, tc.head, tc.progress)

			plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatalf("recovery pass error=%v, want the corrupt ordinal replaced rather than returned forever", err)
			}
			if got := len(rt.Entries()); got != 0 {
				t.Fatalf("entries=%d, want no delivery from the pass that resets the state", got)
			}
			if got := fetched[content.id].Load(); got != 0 {
				t.Fatalf("content requests=%d, want the corrupt document refused before any fetch", got)
			}
			resets := rt.logsContaining("WARN", "stored state was reset")
			if len(resets) != 1 {
				t.Fatalf("reset warnings=%d, want exactly 1 (%+v)", len(resets), resets)
			}
			if got := resets[0].Params["reason"]; !strings.Contains(got, "invalid progress") {
				t.Fatalf("reason=%q, want the structural bound named for the operator", got)
			}
			reset, err := loadManagementState(rt, key)
			if err != nil {
				t.Fatalf("replacement state error=%v", err)
			}
			if reset.EventIndex != 0 || reset.ActiveContentID != "" || reset.hasPending() {
				t.Fatalf("replacement state=%+v, want every in-flight delivery field discarded", reset)
			}
			if !reset.CompletedThrough.Equal(managementTestNow.Add(-time.Hour)) {
				t.Fatalf("completed through=%s, want the durable boundary preserved", reset.CompletedThrough)
			}

			// The next poll collects the window that boundary names, exactly once.
			plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatalf("resumed poll error=%v", err)
			}
			if got := len(rt.Entries()); got != 1 {
				t.Fatalf("entries=%d, want the blob collected normally after the reset", got)
			}
			if got := fetched[content.id].Load(); got != 1 {
				t.Fatalf("content requests=%d, want exactly one", got)
			}
			final, err := loadManagementState(rt, key)
			if err != nil {
				t.Fatal(err)
			}
			if !final.CompletedThrough.Equal(managementTestNow) || final.hasPending() {
				t.Fatalf("final state=%+v, want the window completed", final)
			}
		})
	}
}

// managementResetWitnessRuntime records what the state store held at the moment
// the first entry was delivered. The whole split document is loaded rather than
// the head key alone, because that is the claim: the state the first delivery
// sees has to be state this build will load. Writes are issued sequentially by
// the delivery loop, so the capture needs no lock of its own.
type managementResetWitnessRuntime struct {
	*microsoftTestRuntime
	key               string
	captured          bool
	stateAtFirstWrite managementState
	loadAtFirstWrite  error
}

func (r *managementResetWitnessRuntime) Write(ent entry.Entry) error {
	if !r.captured {
		r.captured = true
		r.stateAtFirstWrite, r.loadAtFirstWrite = loadManagementState(r, r.key)
	}
	return r.microsoftTestRuntime.Write(ent)
}

// TestManagementCorruptEventOrdinalIssuesNoDeliveryBeforeTheResetIsDurable pins
// the ordering of the same repair. The replacement document is the only thing
// that keeps the corrupt ordinal from being read back, so nothing may be
// delivered until it has been synchronized: a pass whose reset cannot be made
// durable has to deliver nothing at all, and the first entry that is delivered
// must see a head document this build will load.
func TestManagementCorruptEventOrdinalIssuesNoDeliveryBeforeTheResetIsDurable(t *testing.T) {
	content := managementCapacityContent{id: "content-ordinal", created: managementTestNow.Add(-30 * time.Minute)}
	fetched := make(map[string]*atomic.Int64)
	server := managementCapacityServer(t, []managementCapacityContent{content}, fetched)
	defer server.Close()

	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	rt := &managementResetWitnessRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(t), key: key}
	seedManagementDeliveryState(t, rt.microsoftTestRuntime, key, server.URL, content, managementMaximumEventsPerContent+1, 0)

	syncer := &managementSyncController{}
	syncer.failAt.Store(1)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "reset") {
		t.Fatalf("error=%v, want the failed reset reported", err)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries=%d, want nothing delivered while the reset is not durable", got)
	}
	if got := fetched[content.id].Load(); got != 0 {
		t.Fatalf("content requests=%d, want no source work before the reset is durable", got)
	}

	syncer.failAt.Store(0)
	for pass := 0; pass < 2; pass++ {
		plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("pass %d after the reset: %v", pass, err)
		}
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("entries=%d, want the blob collected exactly once once the reset is durable", got)
	}
	if !rt.captured {
		t.Fatal("no entry was delivered, so the ordering claim is vacuous")
	}
	if rt.loadAtFirstWrite != nil {
		t.Fatalf("the state document at the first delivery was still invalid: %v", rt.loadAtFirstWrite)
	}
	if rt.stateAtFirstWrite.EventIndex > managementMaximumEventsPerContent {
		t.Fatalf("the corrupt ordinal %d was still durable when the first entry was delivered", rt.stateAtFirstWrite.EventIndex)
	}
}

// TestManagementEventOrdinalBoundKeepsValidMaximaAndOldDocuments is the other
// side of that bound: it has to refuse only what no fetched blob can contain. A
// maximum-length blob that was fully delivered stores exactly
// managementMaximumEventsPerContent, in the head document and in the delivery
// checkpoint that may be adopted in its place, and a document written before
// the field existed carries no ordinal at all.
func TestManagementEventOrdinalBoundKeepsValidMaximaAndOldDocuments(t *testing.T) {
	state := managementState{Version: managementStateVersion, EventIndex: managementMaximumEventsPerContent}
	if err := validateManagementProgressFields(state); err != nil {
		t.Fatalf("a fully delivered maximum-length blob was refused: %v", err)
	}
	state.EventIndex = managementMaximumEventsPerContent + 1
	if err := validateManagementProgressFields(state); err == nil {
		t.Fatal("an ordinal past the absolute event bound was accepted")
	}

	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	if err := rt.Put(key, []byte(`{"version":1,"completed_through":"2026-10-08T10:00:00Z"}`)); err != nil {
		t.Fatal(err)
	}
	old, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatalf("a state document written before the bound existed was refused: %v", err)
	}
	if old.EventIndex != 0 || !old.CompletedThrough.Equal(time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("old state document=%+v", old)
	}

	head, err := json.Marshal(managementState{Version: managementStateVersion, EventIndex: managementMaximumEventsPerContent - 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(key, head); err != nil {
		t.Fatal(err)
	}
	maximum, err := json.Marshal(managementProgress{Version: managementProgressVersion, EventIndex: managementMaximumEventsPerContent})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(managementProgressKey(key), maximum); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatalf("a maximum delivery checkpoint was refused: %v", err)
	}
	if loaded.EventIndex != managementMaximumEventsPerContent {
		t.Fatalf("resumed ordinal=%d, want the maximum adopted", loaded.EventIndex)
	}

	corrupt, err := json.Marshal(managementProgress{Version: managementProgressVersion, EventIndex: managementMaximumEventsPerContent + 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(managementProgressKey(key), corrupt); err != nil {
		t.Fatal(err)
	}
	if _, err := loadManagementState(rt, key); err == nil {
		t.Fatal("a delivery checkpoint past the absolute event bound was accepted")
	}
}

// TestManagementInRangeEventOrdinalReplaysInsteadOfFailingForever covers the
// corruption load validation cannot see: only the fetched blob states how many
// events it holds, so an ordinal inside the absolute bound and past this body
// reached the delivery loop, which returned it and mutated nothing -- the next
// poll fetched the same blob at the same ordinal and failed the same way for as
// long as the window stayed in retention. The blob is replayed from its own
// beginning instead, under the at-least-once contract, and the corrected
// ordinal is durable before any event is written or reported.
func TestManagementInRangeEventOrdinalReplaysInsteadOfFailingForever(t *testing.T) {
	content := managementCapacityContent{id: "content-ordinal", created: managementTestNow.Add(-30 * time.Minute)}
	fetched := make(map[string]*atomic.Int64)
	server := managementCapacityServer(t, []managementCapacityContent{content}, fetched)
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	// Five is inside the absolute bound load validation can check and past the
	// single event this blob holds.
	seedManagementDeliveryState(t, rt, key, server.URL, content, 5, 5)

	syncer := &managementSyncController{}
	syncer.failAt.Store(2)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "synchronize") {
		t.Fatalf("error=%v, want the corrected ordinal published durably before anything else", err)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries=%d, want nothing delivered against an ordinal that is not durable", got)
	}
	if got := rt.logsContaining("WARN", "exceeded the content it was recorded against"); len(got) != 0 {
		t.Fatalf("the reset was reported %d times before it was durable", len(got))
	}
	stalled, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if stalled.EventIndex != 5 {
		t.Fatalf("ordinal after the failed reset=%d, want the durable document unchanged", stalled.EventIndex)
	}

	syncer.failAt.Store(0)
	plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("resumed poll error=%v, want the blob replayed rather than the same failure forever", err)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("entries=%d, want the blob replayed from its own beginning", got)
	}
	warnings := rt.logsContaining("WARN", "exceeded the content it was recorded against")
	if len(warnings) != 1 {
		t.Fatalf("ordinal warnings=%d, want exactly 1 (%+v)", len(warnings), warnings)
	}
	if got := warnings[0].Params["stored_event_index"]; got != "5" {
		t.Fatalf("stored_event_index=%q, want the exact corrupt ordinal", got)
	}
	if got := warnings[0].Params["events"]; got != "1" {
		t.Fatalf("events=%q, want the fetched event count", got)
	}
	if got := rt.warnStateContaining("reset a corrupt delivery ordinal"); len(got) != 1 {
		t.Fatalf("published status warnings=%v, want the reset on the channel the poll loop leaves alone", got)
	}
	final, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if !final.CompletedThrough.Equal(managementTestNow) || final.hasPending() {
		t.Fatalf("final state=%+v, want the window completed", final)
	}
}

// errManagementStateStoreUnavailable is an operational state-store failure: the
// store could not answer. It is deliberately not a not-found, because not-found
// is an answer.
var errManagementStateStoreUnavailable = errors.New("synthetic state-store unavailable")

// managementStorageFaultRuntime fails reads of one key once allowReads of them
// have succeeded, which is what lets a test place the fault on the first read
// (the load) or the second (the recovery's read of the durable head). faults
// bounds how many consecutive reads fail, so a test can model the transient
// fault an operational error actually is: the read that follows succeeds. It
// also counts every write, so a pass that is supposed to change nothing can be
// shown to have changed nothing.
type managementStorageFaultRuntime struct {
	*microsoftTestRuntime
	failKey    string
	allowReads int
	faults     int
	reads      int
	failed     int
	puts       int
}

func (r *managementStorageFaultRuntime) Get(key string) ([]byte, error) {
	if key == r.failKey {
		r.reads++
		if r.reads > r.allowReads && (r.faults == 0 || r.failed < r.faults) {
			r.failed++
			return nil, errManagementStateStoreUnavailable
		}
	}
	return r.microsoftTestRuntime.Get(key)
}

func (r *managementStorageFaultRuntime) Put(key string, value []byte) error {
	r.puts++
	return r.microsoftTestRuntime.Put(key, value)
}

// managementRequestCountingServer answers an ordinary empty poll and counts
// every request that reaches it, the OAuth token exchange included, so a pass
// that must not touch the source can be shown not to have touched it.
func managementRequestCountingServer(t *testing.T, requests *atomic.Int64) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			_, _ = io.WriteString(w, `{"access_token":"management-token","expires_in":3600}`)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
}

// seedManagementCompletedHead writes the head document of a selector that has
// completed everything through boundary and has nothing in flight.
func seedManagementCompletedHead(t *testing.T, rt *microsoftTestRuntime, key string, boundary time.Time) {
	t.Helper()
	head, err := json.Marshal(managementState{Version: managementStateVersion, CompletedThrough: boundary})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(key, head); err != nil {
		t.Fatal(err)
	}
}

// storedManagementCompletedThrough reads the durable boundary straight out of
// the state store, bypassing any injected fault.
func storedManagementCompletedThrough(t *testing.T, rt *microsoftTestRuntime, key string) time.Time {
	t.Helper()
	encoded, err := rt.Get(key)
	if err != nil {
		t.Fatalf("stored head: %v", err)
	}
	var head managementState
	if err := json.Unmarshal(encoded, &head); err != nil {
		t.Fatalf("stored head is not decodable: %v", err)
	}
	return head.CompletedThrough
}

// TestManagementStorageReadFailureIsNotTreatedAsCorruption separates the two
// conditions that used to arrive at the same destructive reset.
//
// loadManagementState returns both "the state store could not answer" and "the
// state store answered with bytes this build cannot use", and the caller
// recovered from either one: it replaced every split document and published a
// checkpoint built from whatever the head happened to hold. A transient
// state-store fault therefore discarded a usable durable boundary and restarted
// the selector from the configured lookback, which re-collects or permanently
// skips everything after the boundary it threw away -- for a condition the next
// poll may not even see.
//
// An operational read failure now has to come back as an error, with no state
// written, nothing published, and no request issued to the source.
func TestManagementStorageReadFailureIsNotTreatedAsCorruption(t *testing.T) {
	var requests atomic.Int64
	server := managementRequestCountingServer(t, &requests)
	defer server.Close()

	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	base := newMicrosoftTestRuntime(t)
	boundary := managementTestNow.Add(-2 * time.Hour)
	seedManagementCompletedHead(t, base, key, boundary)
	// One read fails and the next succeeds, which is what an operational fault
	// looks like. A reset performed on it would therefore have completed, and
	// would have been permanent.
	rt := &managementStorageFaultRuntime{microsoftTestRuntime: base, failKey: key, faults: 1}

	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	_, err := plugin.Handle(t.Context(), rt)
	if err == nil || !errors.Is(err, errManagementStateStoreUnavailable) {
		t.Fatalf("error=%v, want the operational state-store failure returned rather than repaired", err)
	}
	if rt.reads == 0 {
		t.Fatal("the fault was never reached, so the claim is vacuous")
	}
	if rt.puts != 0 {
		t.Fatalf("state writes=%d, want none: a store that cannot be read must not be overwritten", rt.puts)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("vendor requests=%d, want none before the state store answers", got)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries=%d, want none", got)
	}
	if got := rt.logsContaining("WARN", "stored state was reset"); len(got) != 0 {
		t.Fatalf("reset warnings=%d, want none: nothing was reset (%+v)", len(got), got)
	}
	if got := storedManagementCompletedThrough(t, base, key); !got.Equal(boundary) {
		t.Fatalf("durable boundary=%s, want the stored %s left exactly as it was", got, boundary)
	}

	// The fault has cleared. The next poll reads the same boundary it would have
	// destroyed and continues from it, with no reset ever published.
	plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("poll after the transient fault cleared: %v", err)
	}
	if got := rt.logsContaining("WARN", "stored state was reset"); len(got) != 0 {
		t.Fatalf("reset warnings=%d, want none across both polls (%+v)", len(got), got)
	}
	resumed, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.CompletedThrough.Before(boundary) {
		t.Fatalf("resumed boundary=%s, want collection to continue at or after the retained %s", resumed.CompletedThrough, boundary)
	}
}

// TestManagementTrueCorruptionStillRecoversAndKeepsTheDurableBoundary is the
// other side of that split: state the store did hand back and this build cannot
// use is still repaired in place, and the repair still retains the completed
// boundary, which is the only head field advanced solely by accepted delivery.
func TestManagementTrueCorruptionStillRecoversAndKeepsTheDurableBoundary(t *testing.T) {
	var requests atomic.Int64
	server := managementRequestCountingServer(t, &requests)
	defer server.Close()

	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	rt := newMicrosoftTestRuntime(t)
	boundary := managementTestNow.Add(-2 * time.Hour)
	seedManagementCompletedHead(t, rt, key, boundary)
	if err := rt.Put(managementProgressKey(key), []byte(`{"version":97}`)); err != nil {
		t.Fatal(err)
	}

	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("corruption was reported as an operational error: %v", err)
	}
	resets := rt.logsContaining("WARN", "stored state was reset")
	if len(resets) != 1 {
		t.Fatalf("reset warnings=%d, want exactly 1 (%+v)", len(resets), resets)
	}
	if got := resets[0].Params["reason"]; !strings.Contains(got, "unsupported version") {
		t.Fatalf("reason=%q, want the corruption named for the operator", got)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries=%d, want none from the pass that resets the state", got)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("vendor requests=%d, want none from the reset pass", got)
	}
	recovered, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatalf("replacement state error=%v", err)
	}
	if !recovered.CompletedThrough.Equal(boundary) {
		t.Fatalf("recovered boundary=%s, want the durable %s preserved", recovered.CompletedThrough, boundary)
	}
	if recovered.hasPending() || recovered.EventIndex != 0 || recovered.ActiveContentID != "" {
		t.Fatalf("recovered state=%+v, want every in-flight field discarded", recovered)
	}
}

// TestManagementRecoveryWithoutADurableHeadRefusesToPublishZero pins the
// ordering inside the repair itself. Recovery retains the completed boundary by
// reading the head document back, and that read used to be best effort: when it
// failed, recovery published a zero CompletedThrough as though the selector had
// never completed anything. Zero is not a safe stand-in for an unknown
// boundary -- it restarts the selector at the configured lookback -- so the read
// failure has to come back as an error and the stored state has to survive it.
func TestManagementRecoveryWithoutADurableHeadRefusesToPublishZero(t *testing.T) {
	var requests atomic.Int64
	server := managementRequestCountingServer(t, &requests)
	defer server.Close()

	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	base := newMicrosoftTestRuntime(t)
	boundary := managementTestNow.Add(-2 * time.Hour)
	seedManagementCompletedHead(t, base, key, boundary)
	if err := base.Put(managementProgressKey(key), []byte(`{"version":97}`)); err != nil {
		t.Fatal(err)
	}
	// The load reads the head once and succeeds, which is what classifies the
	// corrupt delivery checkpoint as recoverable. The recovery's own read of the
	// head is the one that fails.
	rt := &managementStorageFaultRuntime{microsoftTestRuntime: base, failKey: key, allowReads: 1}

	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	_, err := plugin.Handle(t.Context(), rt)
	if err == nil || !errors.Is(err, errManagementStateStoreUnavailable) {
		t.Fatalf("error=%v, want the unreadable durable head reported", err)
	}
	if !strings.Contains(err.Error(), "reset") {
		t.Fatalf("error=%q, want the failed reset named", err)
	}
	if rt.reads < 2 {
		t.Fatalf("head reads=%d, want the recovery to have attempted its own read", rt.reads)
	}
	if rt.puts != 0 {
		t.Fatalf("state writes=%d, want nothing published when the durable boundary is unknown", rt.puts)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("vendor requests=%d, want none", got)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries=%d, want none", got)
	}
	if got := storedManagementCompletedThrough(t, base, key); !got.Equal(boundary) {
		t.Fatalf("durable boundary=%s, want the stored %s untouched rather than a published zero", got, boundary)
	}
}

// TestManagementStoredStateErrorsAreClassified enumerates what each stored-state
// failure means. Decode failures, unsupported versions, structural validation
// and invalid progress are all bytes the store handed back, in any of the four
// documents, and all of them are recoverable corruption. A read that failed is
// not, in any of them. The messages are asserted alongside the classification so
// the operator-facing text stays exactly what it was.
func TestManagementStoredStateErrorsAreClassified(t *testing.T) {
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	for _, tc := range []struct {
		name     string
		seed     func(t *testing.T, rt *microsoftTestRuntime)
		fragment string
	}{
		{"head-decode", func(t *testing.T, rt *microsoftTestRuntime) {
			mustPutManagementBytes(t, rt, key, `{"version":1`)
		}, "decode Microsoft Management Activity state"},
		{"head-version", func(t *testing.T, rt *microsoftTestRuntime) {
			mustPutManagementBytes(t, rt, key, `{"version":97}`)
		}, "unsupported version 97"},
		{"head-structural", func(t *testing.T, rt *microsoftTestRuntime) {
			mustPutManagementBytes(t, rt, key, `{"version":1,"contents":[{"content_type":"Audit.Exchange","content_uri":"https://example.invalid/blob"}]}`)
		}, "invalid content descriptor"},
		{"head-invalid-progress", func(t *testing.T, rt *microsoftTestRuntime) {
			mustPutManagementBytes(t, rt, key, fmt.Sprintf(`{"version":1,"event_index":%d}`, managementMaximumEventsPerContent+1))
		}, "invalid progress"},
		{"contents-decode", func(t *testing.T, rt *microsoftTestRuntime) {
			mustPutManagementBytes(t, rt, managementContentsKey(key), `{"version":1`)
		}, "decode Microsoft Management Activity content descriptors"},
		{"contents-version", func(t *testing.T, rt *microsoftTestRuntime) {
			mustPutManagementBytes(t, rt, managementContentsKey(key), `{"version":97}`)
		}, "content descriptors have unsupported version 97"},
		{"receipts-decode", func(t *testing.T, rt *microsoftTestRuntime) {
			mustPutManagementBytes(t, rt, managementReceiptsKey(key), `{"version":1`)
		}, "decode Microsoft Management Activity delivery receipts"},
		{"receipts-version", func(t *testing.T, rt *microsoftTestRuntime) {
			mustPutManagementBytes(t, rt, managementReceiptsKey(key), `{"version":97}`)
		}, "delivery receipts have unsupported version 97"},
		{"progress-decode", func(t *testing.T, rt *microsoftTestRuntime) {
			mustPutManagementBytes(t, rt, managementProgressKey(key), `{"version":1`)
		}, "decode Microsoft Management Activity delivery progress"},
		{"progress-version", func(t *testing.T, rt *microsoftTestRuntime) {
			mustPutManagementBytes(t, rt, managementProgressKey(key), `{"version":97}`)
		}, "delivery progress has unsupported version 97"},
		{"progress-invalid-progress", func(t *testing.T, rt *microsoftTestRuntime) {
			mustPutManagementBytes(t, rt, managementProgressKey(key),
				fmt.Sprintf(`{"version":1,"event_index":%d}`, managementMaximumEventsPerContent+1))
		}, "delivery progress contains invalid progress"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := newMicrosoftTestRuntime(t)
			tc.seed(t, rt)
			_, err := loadManagementState(rt, key)
			if err == nil {
				t.Fatal("the corrupt document was accepted")
			}
			if !recoverableManagementCorruption(err) {
				t.Fatalf("error=%v, want it classified as recoverable corruption", err)
			}
			if !strings.Contains(err.Error(), tc.fragment) {
				t.Fatalf("error=%q, want one naming %q", err, tc.fragment)
			}
		})
	}

	for _, failKey := range []string{key, managementContentsKey(key), managementReceiptsKey(key), managementProgressKey(key)} {
		t.Run("storage-"+failKey, func(t *testing.T) {
			rt := &managementStorageFaultRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(t), failKey: failKey}
			_, err := loadManagementState(rt, key)
			if err == nil || !errors.Is(err, errManagementStateStoreUnavailable) {
				t.Fatalf("error=%v, want the state-store failure returned", err)
			}
			if recoverableManagementCorruption(err) {
				t.Fatalf("error=%v was classified as corruption; a store that cannot answer says nothing about its bytes", err)
			}
		})
	}
}

func mustPutManagementBytes(t *testing.T, rt *microsoftTestRuntime, key, document string) {
	t.Helper()
	if err := rt.Put(key, []byte(document)); err != nil {
		t.Fatal(err)
	}
}

// managementRecoveryHead is a minimal, valid head document carrying nothing but
// the durable completed boundary, which is the only field a reset retains.
const managementRecoveryHead = `{"version":1,"completed_through":"2026-10-08T11:00:00Z"}`

// managementRecoveryBoundary is the boundary that head names.
var managementRecoveryBoundary = managementTestNow.Add(-time.Hour)

// managementRelationshipSource serves one Audit.Exchange descriptor and the one
// event its blob holds, and counts every request the plugin issues -- including
// the token lookup -- so a pass that recovers stored state can be shown to reach
// the source not at all.
func managementRelationshipSource(t *testing.T, content managementCapacityContent, requests, fetches *atomic.Int64) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			_, _ = io.WriteString(w, `{"access_token":"management-token","expires_in":3600}`)
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
			if content.created.Before(start) || !content.created.Before(end) {
				_, _ = io.WriteString(w, "[]")
				return
			}
			_, _ = io.WriteString(w, "["+managementDescriptorAt(server.URL, "Audit.Exchange", content)+"]")
		case strings.Contains(r.URL.Path, managementCapacityContentPath):
			fetches.Add(1)
			_, _ = fmt.Fprintf(w, `[{"Id":%q,"CreationTime":%q,"Operation":"MailItemsAccessed"}]`,
				content.id, content.created.Format(time.RFC3339))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// managementPaddedDocument grows a JSON object to exactly total bytes with a
// member this build does not know. That member is what makes a pre-decode
// ceiling load-bearing rather than redundant: the decoder discards it, so
// nothing downstream of the decode -- not the descriptor budget, not the receipt
// budget, not any count bound -- can ever charge for it.
func managementPaddedDocument(t *testing.T, body string, total int) string {
	t.Helper()
	trimmed := strings.TrimSuffix(body, "}")
	if trimmed == body {
		t.Fatalf("document %q is not a JSON object", body)
	}
	prefix, suffix := trimmed+`,"unknown_member":"`, `"}`
	pad := total - len(prefix) - len(suffix)
	if pad < 0 {
		t.Fatalf("document is already %d bytes, over the %d-byte target", len(prefix)+len(suffix), total)
	}
	return prefix + strings.Repeat("a", pad) + suffix
}

// managementStateDocuments names every key one logical Management state
// document is split across, beside the ceiling each one is measured against
// before it is decoded.
func managementStateDocuments(key string) []struct {
	name    string
	key     string
	ceiling int
} {
	return []struct {
		name    string
		key     string
		ceiling int
	}{
		{"head", key, managementMaximumStateBytes},
		{"contents", managementContentsKey(key), managementMaximumContentSetBytes},
		{"receipts", managementReceiptsKey(key), managementMaximumReceiptSetBytes},
		{"progress", managementProgressKey(key), managementMaximumProgressBytes},
	}
}

// assertManagementDocumentsAreBounded fails unless every document the state
// store now holds for this selector is inside its own ceiling, which is what
// makes a recovery a repair rather than a document the next poll refuses again.
func assertManagementDocumentsAreBounded(t *testing.T, rt *microsoftTestRuntime, key string) {
	t.Helper()
	for _, document := range managementStateDocuments(key) {
		encoded, err := rt.Get(document.key)
		if err != nil {
			t.Fatalf("the %s document was not written by the recovery: %v", document.name, err)
		}
		if len(encoded) > document.ceiling {
			t.Fatalf("the persisted %s document is %d bytes, over the %d-byte ceiling", document.name, len(encoded), document.ceiling)
		}
	}
}

// Every Management state document was unmarshaled before any encoded-size
// ceiling applied. The per-field and aggregate bounds elsewhere in the file are
// charged against what a decoder retained, and a JSON member this build does not
// know is discarded and charged against nothing at all, so the only bound on
// reading a stored document was whatever the state store happened to hold.
//
// TestManagementStateDocumentCeilingsAreExactAndRecoverAtOneByteOver pins each
// ceiling at its exact boundary in both directions, and pins what crossing it
// does: the existing recoverable-corruption handling, with the durable completed
// boundary retained, no request issued to the source, and every replacement
// document inside its own ceiling and loadable again.
func TestManagementStateDocumentCeilingsAreExactAndRecoverAtOneByteOver(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  func(string) string
		body string
		// retainsBoundary is false only for the head itself. A head past the
		// ceiling is not a document any build of this plugin wrote, and the
		// repair declines to decode one rather than being the single path that
		// reads an unbounded document, so the boundary it might have named is
		// treated as absent.
		retainsBoundary bool
		ceiling         int
	}{
		{"head", func(key string) string { return key }, managementRecoveryHead, false, managementMaximumStateBytes},
		{"contents", managementContentsKey, `{"version":1}`, true, managementMaximumContentSetBytes},
		{"receipts", managementReceiptsKey, `{"version":1}`, true, managementMaximumReceiptSetBytes},
		{"progress", managementProgressKey, `{"version":1,"content_index":0,"event_index":0}`, true, managementMaximumProgressBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := managementStateKey("microsoft", "microsoft365-audit-exchange")

			// Exactly at the ceiling is accepted, and the boundary the head
			// carries survives, so the bound is a boundary rather than a
			// rejection of the shape.
			atCeiling := newMicrosoftTestRuntime(t)
			mustPutManagementBytes(t, atCeiling, key, managementRecoveryHead)
			mustPutManagementBytes(t, atCeiling, tc.key(key), managementPaddedDocument(t, tc.body, tc.ceiling))
			loaded, err := loadManagementState(atCeiling, key)
			if err != nil {
				t.Fatalf("a %s document at exactly the %d-byte ceiling was refused: %v", tc.name, tc.ceiling, err)
			}
			if !loaded.CompletedThrough.Equal(managementRecoveryBoundary) {
				t.Fatalf("completed through=%s, want the durable %s", loaded.CompletedThrough, managementRecoveryBoundary)
			}

			// One byte over is recoverable corruption: the store answered and
			// these bytes cannot be used, which is the only condition a
			// destructive replacement repairs.
			rt := newMicrosoftTestRuntime(t)
			mustPutManagementBytes(t, rt, key, managementRecoveryHead)
			mustPutManagementBytes(t, rt, tc.key(key), managementPaddedDocument(t, tc.body, tc.ceiling+1))
			_, err = loadManagementState(rt, key)
			if err == nil {
				t.Fatalf("a %s document one byte over the %d-byte ceiling was accepted", tc.name, tc.ceiling)
			}
			if !recoverableManagementCorruption(err) {
				t.Fatalf("error=%v, want it classified as recoverable corruption", err)
			}
			if !strings.Contains(err.Error(), "ceiling") {
				t.Fatalf("error=%v, want the ceiling named for the operator", err)
			}

			// The pass that repairs it issues nothing at all to the source, not
			// even a token lookup, and delivers nothing.
			content := managementCapacityContent{id: "content-ceiling", created: managementTestNow.Add(-30 * time.Minute)}
			var requests, fetches atomic.Int64
			server := managementRelationshipSource(t, content, &requests, &fetches)
			plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatalf("recovery pass error=%v, want the oversized document replaced rather than returned forever", err)
			}
			if got := requests.Load(); got != 0 {
				t.Fatalf("the recovery pass issued %d requests to the source", got)
			}
			if got := len(rt.Entries()); got != 0 {
				t.Fatalf("entries=%d, want nothing delivered by the pass that resets the state", got)
			}
			resets := rt.logsContaining("WARN", "stored state was reset")
			if len(resets) != 1 {
				t.Fatalf("reset warnings=%d, want exactly 1 (%+v)", len(resets), resets)
			}
			if got := resets[0].Params["reason"]; !strings.Contains(got, "ceiling") {
				t.Fatalf("reason=%q, want the ceiling named for the operator", got)
			}

			// Every replacement document is inside its own ceiling and loads.
			assertManagementDocumentsAreBounded(t, rt, key)
			reset, err := loadManagementState(rt, key)
			if err != nil {
				t.Fatalf("the replacement state was refused on reload: %v", err)
			}
			if reset.hasPending() || reset.ActiveContentID != "" || reset.EventIndex != 0 {
				t.Fatalf("replacement state=%+v, want every in-flight field discarded", reset)
			}
			if tc.retainsBoundary && !reset.CompletedThrough.Equal(managementRecoveryBoundary) {
				t.Fatalf("completed through=%s, want the durable %s retained", reset.CompletedThrough, managementRecoveryBoundary)
			}

			// The next poll collects normally from whatever boundary survived.
			plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatalf("resumed poll error=%v", err)
			}
			if got := len(rt.Entries()); got != 1 {
				t.Fatalf("entries=%d, want the blob collected once after the reset", got)
			}
			if got := fetches.Load(); got != 1 {
				t.Fatalf("content requests=%d, want exactly one", got)
			}
			assertManagementDocumentsAreBounded(t, rt, key)
		})
	}
}

// Restored receipt sets were count-bounded and nothing else. A receipt is not
// inert: it goes straight into the membership index that suppresses redelivery,
// it is charged against the aggregate byte budget, and it is re-marshaled into
// the document the next checkpoint persists, so an identifier no listing would
// have admitted and a creation time no prune can order were both adopted clean.
func TestManagementRestoredReceiptsMustCarryTheCanonicalShape(t *testing.T) {
	identifier := strings.Repeat("c", managementMaximumContentIDLength)
	for _, tc := range []struct {
		name, receipts, want string
	}{
		{"empty-identifier", `[{"content_id":"","content_created":"2026-10-08T11:30:00Z"}]`, "empty or over"},
		{"blank-identifier", `[{"content_id":"   ","content_created":"2026-10-08T11:30:00Z"}]`, "empty or over"},
		{"identifier-one-byte-over-the-bound", `[{"content_id":"` + identifier + `c","content_created":"2026-10-08T11:30:00Z"}]`, "empty or over"},
		{"missing-creation-time", `[{"content_id":"content-1"}]`, "no creation time"},
		{"zero-creation-time", `[{"content_id":"content-1","content_created":"0001-01-01T00:00:00Z"}]`, "no creation time"},
		{"invalid-receipt-among-valid-ones", `[{"content_id":"content-1","content_created":"2026-10-08T11:30:00Z"},` +
			`{"content_id":"content-2","content_created":"0001-01-01T00:00:00Z"}]`, "no creation time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := newMicrosoftTestRuntime(t)
			key := managementStateKey("microsoft", "microsoft365-audit-exchange")
			mustPutManagementBytes(t, rt, key, managementRecoveryHead)
			mustPutManagementBytes(t, rt, managementReceiptsKey(key), `{"version":1,"receipts":`+tc.receipts+`}`)
			_, err := loadManagementState(rt, key)
			if err == nil {
				t.Fatal("an invalid delivery receipt was adopted clean")
			}
			if !recoverableManagementCorruption(err) {
				t.Fatalf("error=%v, want it classified as recoverable corruption", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want it to name %q", err, tc.want)
			}
			// Vendor bytes stay out of the diagnostic.
			if strings.Contains(err.Error(), identifier) {
				t.Fatalf("error=%v names the stored identifier", err)
			}
		})
	}

	// The exact per-identifier bound is accepted, which is what the listing
	// decode admits, so the two agree and a legitimate receipt is never refused.
	t.Run("identifier-at-the-bound", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		key := managementStateKey("microsoft", "microsoft365-audit-exchange")
		mustPutManagementBytes(t, rt, key, managementRecoveryHead)
		mustPutManagementBytes(t, rt, managementReceiptsKey(key),
			`{"version":1,"receipts":[{"content_id":"`+identifier+`","content_created":"2026-10-08T11:30:00Z"}]}`)
		state, err := loadManagementState(rt, key)
		if err != nil {
			t.Fatalf("a receipt at exactly the identifier bound was refused: %v", err)
		}
		if len(state.Receipts) != 1 || state.Receipts[0].ContentID != identifier {
			t.Fatalf("receipts=%d, want the canonical identifier retained verbatim", len(state.Receipts))
		}
		if !state.hasReceipt(identifier) {
			t.Fatal("receipt membership does not answer for the restored identifier")
		}
	})

	// One receipt past the count bound. The count is a bound on how many
	// receipts the document may carry; the two bounds below are what it does
	// not say anything about.
	t.Run("count-one-over-the-bound", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		key := managementStateKey("microsoft", "microsoft365-audit-exchange")
		member := `{"content_id":"c","content_created":"2026-10-08T11:30:00Z"}`
		document := `{"version":1,"receipts":[` +
			strings.TrimSuffix(strings.Repeat(member+",", managementMaximumReceipts+1), ",") + `]}`
		if len(document) > managementMaximumReceiptSetBytes {
			t.Fatalf("the document is %d bytes, so the count claim is vacuous", len(document))
		}
		mustPutManagementBytes(t, rt, key, managementRecoveryHead)
		mustPutManagementBytes(t, rt, managementReceiptsKey(key), document)
		_, err := loadManagementState(rt, key)
		if err == nil || !recoverableManagementCorruption(err) {
			t.Fatalf("error=%v, want a receipt set past the count bound classified as recoverable corruption", err)
		}
		if !strings.Contains(err.Error(), "capacity") {
			t.Fatalf("error=%v, want the count bound named", err)
		}
		// The same bound answers for a set carried inline by an earlier build's
		// head, where there is no side document to measure first.
		inline := managementState{Version: managementStateVersion, CompletedThrough: managementRecoveryBoundary}
		inline.Receipts = make([]managementReceipt, managementMaximumReceipts+1)
		for index := range inline.Receipts {
			inline.Receipts[index] = managementReceipt{ContentID: "c", ContentCreated: managementTestNow}
		}
		if err := validateManagementProgressFields(inline); err == nil || !strings.Contains(err.Error(), "safety bound") {
			t.Fatalf("error=%v, want an inline set past the count bound refused", err)
		}
	})

	// The aggregate retained-byte budget, in the region where it is the only
	// bound that answers: a receipt set inside the count bound and inside the
	// document ceiling that would still cost more to retain and re-marshal than
	// the budget allows. The identifiers are '<', which the canonical encoding
	// escapes to six bytes each, so what the document costs to keep is six times
	// what it cost to read.
	t.Run("aggregate-over-the-budget-inside-the-document-ceiling", func(t *testing.T) {
		identity := strings.Repeat("<", 500)
		charge := managementReceiptBytes(managementReceipt{ContentID: identity, ContentCreated: managementTestNow})
		count := managementMaximumReceiptBytes/charge + 1
		if count > managementMaximumReceipts {
			t.Fatalf("the budget needs %d receipts, past the count bound, so the claim is vacuous", count)
		}
		member := fmt.Sprintf(`{"content_id":%q,"content_created":%q}`, identity, managementTestNow.Format(time.RFC3339))
		document := `{"version":1,"receipts":[` + strings.TrimSuffix(strings.Repeat(member+",", count), ",") + `]}`
		if len(document) > managementMaximumReceiptSetBytes {
			t.Fatalf("the document is %d bytes, over the ceiling, so the aggregate claim is vacuous", len(document))
		}

		rt := newMicrosoftTestRuntime(t)
		key := managementStateKey("microsoft", "microsoft365-audit-exchange")
		mustPutManagementBytes(t, rt, key, managementRecoveryHead)
		mustPutManagementBytes(t, rt, managementReceiptsKey(key), document)
		_, err := loadManagementState(rt, key)
		if err == nil || !recoverableManagementCorruption(err) {
			t.Fatalf("error=%v, want a receipt set over the byte budget classified as recoverable corruption", err)
		}
		if !strings.Contains(err.Error(), "exceed") {
			t.Fatalf("error=%v, want the byte budget named", err)
		}

		// One receipt fewer is inside the budget and is adopted, so the budget
		// is a boundary and not a refusal of the shape.
		within := `{"version":1,"receipts":[` + strings.TrimSuffix(strings.Repeat(member+",", count-1), ",") + `]}`
		mustPutManagementBytes(t, rt, managementReceiptsKey(key), within)
		state, err := loadManagementState(rt, key)
		if err != nil {
			t.Fatalf("a receipt set inside the byte budget was refused: %v", err)
		}
		if got := state.receiptBytes(); got > managementMaximumReceiptBytes {
			t.Fatalf("the adopted set is accounted at %d bytes, over the %d-byte budget", got, managementMaximumReceiptBytes)
		}
	})
}

// A head document written by an earlier build carries both sets inline, under no
// aggregate budget at all. loadManagementState adopts the receipts so none are
// lost and marks them dirty; the first persist then migrates them to the receipt
// side key -- whether or not it is a checkpoint, because the head this build
// writes no longer carries them -- and marks them clean. Clean is the state the
// maintenance pass returns from immediately, so a legacy set over the budget was
// written once to the side key and then never visited again.
func TestManagementLegacyInlineReceiptsArePrunedBeforeTheirMigrationPersist(t *testing.T) {
	const recorded = 10
	const admitted = 4
	content := managementCapacityContent{id: "content-legacy", created: managementTestNow.Add(-30 * time.Minute)}
	var requests, fetches atomic.Int64
	server := managementRelationshipSource(t, content, &requests, &fetches)
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	rt := &managementMigrationWitnessRuntime{
		microsoftTestRuntime: newMicrosoftTestRuntime(t),
		receiptsKey:          managementReceiptsKey(key),
	}
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)

	// A legacy head part way through a window, which is the ordering that
	// matters: no new window starts, so nothing checkpoints before the delivery
	// loop, and the migration write is the first thing that would ever have
	// touched the receipt set.
	legacy := managementState{
		Version:          managementStateVersion,
		CompletedThrough: managementRecoveryBoundary,
		TargetThrough:    managementTestNow,
		WindowStart:      managementRecoveryBoundary,
		WindowEnd:        managementTestNow,
		ListingStarted:   true,
		ListingComplete:  true,
		Pages:            1,
		Contents: []managementContent{{
			ContentType: "Audit.Exchange", ContentID: content.id,
			ContentURI:     server.URL + managementCapacityContentPath + content.id,
			ContentCreated: content.created,
		}},
	}
	for ordinal := range recorded {
		legacy.Receipts = append(legacy.Receipts, managementReceipt{
			ContentID:      fmt.Sprintf("legacy-%04d", ordinal),
			ContentCreated: managementTestNow.Add(-time.Duration(recorded-ordinal) * time.Minute),
		})
	}
	// An override may only tighten the durable bound, which is what makes the
	// aggregate budget reachable without a 32 MiB document.
	plugin.managementReceiptByteLimit = admitted * managementReceiptBytes(legacy.Receipts[0])
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	mustPutManagementBytes(t, rt.microsoftTestRuntime, key, string(encoded))

	// The inline set is valid, so it is adopted rather than discarded: this is
	// preservation, not recovery.
	adopted, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatalf("a valid legacy head was refused: %v", err)
	}
	if len(adopted.Receipts) != recorded {
		t.Fatalf("adopted receipts=%d, want all %d from the inline head", len(adopted.Receipts), recorded)
	}

	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("migration pass error=%v", err)
	}

	// The very first bytes written to the receipt side key are already bounded.
	// That write is the migration, and it is the last moment anything would have
	// visited the set: it marks the document clean, and clean is the state the
	// maintenance pass returns from immediately.
	if !rt.captured {
		t.Fatal("the inline receipts were never migrated to the side key, so the claim is vacuous")
	}
	var migrated managementReceiptSet
	if err := json.Unmarshal(rt.firstWrite, &migrated); err != nil {
		t.Fatal(err)
	}
	if len(migrated.Receipts) != admitted {
		t.Fatalf("the migration persisted %d receipts, want the %d the budget admits", len(migrated.Receipts), admitted)
	}
	if err := validateManagementReceiptBytes(migrated.Receipts); err != nil {
		t.Fatalf("the migrated document is over the durable budget: %v", err)
	}
	charged := 0
	for _, receipt := range migrated.Receipts {
		charged += managementReceiptBytes(receipt)
	}
	if charged > plugin.receiptByteCapacity() {
		t.Fatalf("the migrated document is accounted at %d bytes, over the %d-byte budget", charged, plugin.receiptByteCapacity())
	}
	// Oldest-first, so the receipts an overlapping listing can still use are the
	// ones that survive.
	for index, receipt := range migrated.Receipts {
		if want := fmt.Sprintf("legacy-%04d", recorded-admitted+index); receipt.ContentID != want {
			t.Fatalf("retained receipt %d=%q, want %q", index, receipt.ContentID, want)
		}
	}
	// The migration ran before anything in the pass appended to the set. Writing
	// the bounded set plus what the pass had already recorded on top of it is a
	// document the next load would refuse.
	if len(rt.firstWrite) != 0 && strings.Contains(string(rt.firstWrite), content.id) {
		t.Fatalf("the migration document already carries this pass's own receipt: %s", rt.firstWrite)
	}
	// And the pass did deliver, so the ordering claim is not vacuous.
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("entries=%d, want the window's one blob delivered", got)
	}
	if got := fetches.Load(); got != 1 {
		t.Fatalf("content requests=%d, want exactly one", got)
	}

	// The head no longer carries them, the reload is bounded, and the durable
	// boundary was never lost.
	head, err := rt.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(head), "legacy-") {
		t.Fatalf("the head document still carries the inline receipt set: %s", head)
	}
	reloaded, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatalf("the migrated state was refused on reload: %v", err)
	}
	if got := reloaded.receiptBytes(); got > plugin.receiptByteCapacity() {
		t.Fatalf("the reloaded set is accounted at %d bytes, over the %d-byte budget", got, plugin.receiptByteCapacity())
	}
	if reloaded.CompletedThrough.Before(managementRecoveryBoundary) {
		t.Fatalf("completed through=%s, want at least the durable %s", reloaded.CompletedThrough, managementRecoveryBoundary)
	}
	assertManagementDocumentsAreBounded(t, rt.microsoftTestRuntime, key)
	// The eviction is operator-visible rather than silent.
	if got := rt.logsContaining("WARN", "dropped the oldest delivery receipts at capacity"); len(got) == 0 {
		t.Fatal("the budget evicted receipts with nothing published for the operator")
	}
}

// managementMigrationWitnessRuntime records the first bytes ever written to the
// receipt side key, which is the document the inline-to-side-key migration
// publishes and marks clean.
type managementMigrationWitnessRuntime struct {
	*microsoftTestRuntime
	receiptsKey string
	captured    bool
	firstWrite  []byte
}

func (r *managementMigrationWitnessRuntime) Put(key string, value []byte) error {
	if key == r.receiptsKey && !r.captured {
		r.captured = true
		r.firstWrite = append([]byte(nil), value...)
	}
	return r.microsoftTestRuntime.Put(key, value)
}

// A loaded head whose nonempty ActiveContentID is not the identifier of the
// descriptor at ContentIndex passed every field bound there was. The delivery
// loop then fetched the blob at ContentIndex, compared the identifiers, and
// returned a plain error having mutated nothing at all, so every later poll read
// the same document back and failed identically until the window crossed the
// source's seven-day retention floor.
//
// The relationship is one O(1) invariant over fields the same document carries.
// This covers the valid active states and every invalid index/identifier
// relationship there is.
func TestManagementActiveContentMustMatchTheDescriptorItWasRecordedAgainst(t *testing.T) {
	descriptors := []managementContent{
		{ContentType: "Audit.Exchange", ContentID: "content-1", ContentURI: "https://manage.office.com/one", ContentCreated: managementTestNow.Add(-40 * time.Minute)},
		{ContentType: "Audit.Exchange", ContentID: "content-2", ContentURI: "https://manage.office.com/two", ContentCreated: managementTestNow.Add(-30 * time.Minute)},
	}
	for _, tc := range []struct {
		name     string
		contents []managementContent
		index    int
		active   string
		valid    bool
	}{
		{"active-matches-the-first-descriptor", descriptors, 0, "content-1", true},
		{"active-matches-the-second-descriptor", descriptors, 1, "content-2", true},
		{"no-active-content-mid-set", descriptors, 1, "", true},
		{"no-active-content-at-the-end-of-the-set", descriptors, len(descriptors), "", true},
		{"no-active-content-and-no-descriptors", nil, 0, "", true},
		{"active-names-a-later-descriptor", descriptors, 0, "content-2", false},
		{"active-names-an-earlier-descriptor", descriptors, 1, "content-1", false},
		{"active-names-a-descriptor-that-was-never-listed", descriptors, 0, "content-never-listed", false},
		{"active-past-the-last-descriptor", descriptors, len(descriptors), "content-2", false},
		{"active-with-no-descriptors-at-all", nil, 0, "content-1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := managementState{
				Version:         managementStateVersion,
				WindowStart:     managementTestNow.Add(-time.Hour),
				WindowEnd:       managementTestNow,
				Contents:        tc.contents,
				ContentIndex:    tc.index,
				ActiveContentID: tc.active,
			}
			err := validateManagementProgressFields(state)
			if tc.valid {
				if err != nil {
					t.Fatalf("a valid active state was refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("in-flight content that is not the descriptor at ContentIndex was accepted")
			}
			if !strings.Contains(err.Error(), "in-flight content") {
				t.Fatalf("error=%v, want the relationship named for the operator", err)
			}
			// The same answer reaches the load path, classified so the existing
			// recovery replaces the document.
			rt := newMicrosoftTestRuntime(t)
			key := managementStateKey("microsoft", "microsoft365-audit-exchange")
			encoded, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			mustPutManagementBytes(t, rt, key, string(encoded))
			if _, err := loadManagementState(rt, key); err == nil || !recoverableManagementCorruption(err) {
				t.Fatalf("load error=%v, want it classified as recoverable corruption", err)
			}
			// And the persist path refuses to write it, so the invariant cannot
			// be introduced by this build either.
			plugin := NewWithStateSync(&Config{}, func() error { return nil })
			if err := plugin.persistManagementState(rt, key, &state); err == nil {
				t.Fatal("a state document violating the relationship was persisted")
			}
		})
	}
}

// TestManagementMismatchedActiveContentRecoversAndResumesWithoutDuplicateDelivery
// is the end-to-end half of that repair: the mismatch is classified as
// recoverable corruption, the durable completed boundary survives, only the
// in-flight work is cleared, the recovery reaches the source not at all, and the
// next valid poll resumes and delivers the blob exactly once.
func TestManagementMismatchedActiveContentRecoversAndResumesWithoutDuplicateDelivery(t *testing.T) {
	content := managementCapacityContent{id: "content-active", created: managementTestNow.Add(-30 * time.Minute)}
	var requests, fetches atomic.Int64
	server := managementRelationshipSource(t, content, &requests, &fetches)
	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")

	head, err := json.Marshal(managementState{
		Version:          managementStateVersion,
		CompletedThrough: managementRecoveryBoundary,
		TargetThrough:    managementTestNow,
		WindowStart:      managementRecoveryBoundary,
		WindowEnd:        managementTestNow,
		ListingStarted:   true,
		ListingComplete:  true,
		Pages:            1,
		ActiveContentID:  "content-that-was-never-listed",
		ActiveDigest:     "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		EventIndex:       3,
		Contents: []managementContent{{
			ContentType: "Audit.Exchange", ContentID: content.id,
			ContentURI:     server.URL + managementCapacityContentPath + content.id,
			ContentCreated: content.created,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustPutManagementBytes(t, rt, key, string(head))

	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("recovery pass error=%v, want the mismatch repaired rather than returned forever", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("the recovery pass issued %d requests to the source, including token lookups", got)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries=%d, want nothing delivered by the pass that resets the state", got)
	}
	resets := rt.logsContaining("WARN", "stored state was reset")
	if len(resets) != 1 {
		t.Fatalf("reset warnings=%d, want exactly 1 (%+v)", len(resets), resets)
	}
	if got := resets[0].Params["reason"]; !strings.Contains(got, "in-flight content") {
		t.Fatalf("reason=%q, want the relationship named for the operator", got)
	}

	reset, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatalf("the replacement state was refused on reload: %v", err)
	}
	if !reset.CompletedThrough.Equal(managementRecoveryBoundary) {
		t.Fatalf("completed through=%s, want the durable %s preserved", reset.CompletedThrough, managementRecoveryBoundary)
	}
	if reset.ActiveContentID != "" || reset.ActiveDigest != "" || reset.EventIndex != 0 || reset.hasPending() {
		t.Fatalf("replacement state=%+v, want only the in-flight work cleared", reset)
	}

	// The next valid poll resumes from that boundary, lists the window it names
	// and delivers the blob exactly once. The acknowledged delivery is not
	// repeated by the poll after it.
	for pass := range 2 {
		plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
		if _, err := plugin.Handle(t.Context(), rt); err != nil {
			t.Fatalf("resumed poll %d error=%v", pass, err)
		}
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("entries=%d, want the blob delivered exactly once across both polls", got)
	}
	if got := fetches.Load(); got != 1 {
		t.Fatalf("content requests=%d, want exactly one", got)
	}
	final, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if !final.CompletedThrough.Equal(managementTestNow) || final.hasPending() {
		t.Fatalf("final state=%+v, want the window completed", final)
	}
	if !final.hasReceipt(content.id) {
		t.Fatal("the delivered blob left no receipt, so an overlapping listing could resurrect it")
	}
	assertManagementDocumentsAreBounded(t, rt, key)
}

// The retained-element sizes the stored arrays decode to. A dense JSON member is
// as short as the grammar allows -- `{}` is two bytes -- while the struct it
// decodes to is fixed-size, and these are those sizes: three string headers and
// two time.Time for a descriptor, one string header and one time.Time for a
// receipt. They are what makes the amplification, and they are what the bounds
// below have to be charged before rather than after.
const (
	managementRetainedDescriptorBytes = 96
	managementRetainedReceiptBytes    = 40
)

// managementDenseArrayDocument builds a stored side document whose named array
// holds members copies of one JSON member.
func managementDenseArrayDocument(array, member string, members int) string {
	var document strings.Builder
	document.Grow(len(member)*members + members + 32)
	document.WriteString(`{"version":1,"`)
	document.WriteString(array)
	document.WriteString(`":[`)
	for ordinal := range members {
		if ordinal != 0 {
			document.WriteString(",")
		}
		document.WriteString(member)
	}
	document.WriteString(`]}`)
	return document.String()
}

// managementDenseMemberCount is how many copies of member, plus the separator
// each one needs, fit inside a document of ceiling bytes.
func managementDenseMemberCount(array, member string, ceiling int) int {
	frame := len(managementDenseArrayDocument(array, member, 1)) - len(member)
	return (ceiling - frame + 1) / (len(member) + 1)
}

// managementDecodeAllocation measures what one decode allocates. The document is
// built before the measurement starts, so what is counted is the cost of
// reading it rather than the cost of being handed it.
func managementDecodeAllocation(t *testing.T, decode func()) uint64 {
	t.Helper()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	decode()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// assertManagementDenseRejectionIsBounded fails unless a document far denser in
// members than its bound admits was both refused and refused cheaply: the cost
// has to scale with the bound rather than with the member count, which is
// exactly what decoding the array whole before consulting the bound did not do.
func assertManagementDenseRejectionIsBounded(t *testing.T, what string, members, bound, retainedBytes int, allocated uint64) {
	t.Helper()
	// Non-vacuous: the document really does carry far more members than the
	// bound admits, so a decode that materialized it first had to allocate an
	// element for every one of them.
	if members < 10*bound {
		t.Fatalf("the dense %s document carries %d members against a bound of %d, which is not dense enough for the claim", what, members, bound)
	}
	// What materializing the whole array costs. Growing a slice to n elements
	// by append allocates every intermediate backing array too, which is about
	// five times the final one, and that factor applies to the bounded decode
	// below in exactly the same way -- so both sides of the comparison carry it
	// and what is left is the ratio between the member count and the bound.
	const appendGrowth = 5
	unbounded := uint64(members) * uint64(retainedBytes) * appendGrowth
	// What the bound itself permits to be retained, grown the same way, with a
	// factor of three to spare for the per-member read and for the extra
	// bookkeeping the race detector allocates.
	ceiling := uint64(3 * appendGrowth * bound * retainedBytes)
	if ceiling >= unbounded/4 {
		t.Fatalf("the %s ceiling %d is not meaningfully below the %d bytes materializing every member costs", what, ceiling, unbounded)
	}
	if allocated > ceiling {
		t.Fatalf("refusing a dense %s document allocated %d bytes against a %d-byte ceiling; materializing its %d members costs about %d: the array was decoded whole before the bound applied",
			what, allocated, ceiling, members, unbounded)
	}
}

// managementStoredFiller is one descriptor whose contentUri is padded to length
// bytes, so a set can be built to an exact aggregate charge.
func managementStoredFiller(contentType string, ordinal, length int) managementContent {
	id := fmt.Sprintf("stored-%06d", ordinal)
	uri := "https://manage.office.com/api/v1.0/t/activity/feed/audit/" + id
	if pad := length - len(uri); pad > 0 {
		uri += strings.Repeat("u", pad)
	}
	return managementContent{
		ContentType: contentType, ContentID: id, ContentURI: uri,
		ContentCreated: managementTestNow.Add(-time.Duration(ordinal+1) * time.Second),
	}
}

// managementDescriptorsCharging builds a descriptor set whose charges, as
// managementDescriptorBytes sums them, come to exactly total. The last
// descriptor's contentUri is sized by the byte, which is what makes the
// aggregate boundary testable at its exact value in both directions.
func managementDescriptorsCharging(t *testing.T, contentType string, total int) []managementContent {
	t.Helper()
	var contents []managementContent
	charged := 0
	for ordinal := 0; ; ordinal++ {
		candidate := managementStoredFiller(contentType, ordinal, managementLongestDispatchableURLLength)
		charge := managementDescriptorBytes(candidate)
		if charged+charge > total {
			break
		}
		contents = append(contents, candidate)
		charged += charge
	}
	last := managementStoredFiller(contentType, len(contents), 0)
	minimum := managementDescriptorBytes(last)
	if remaining := total - charged; remaining < minimum {
		t.Fatalf("%d bytes are left for a descriptor that costs at least %d", remaining, minimum)
	} else {
		last.ContentURI += strings.Repeat("u", remaining-minimum)
	}
	contents = append(contents, last)
	charged = 0
	for _, content := range contents {
		charged += managementDescriptorBytes(content)
	}
	if charged != total {
		t.Fatalf("the set charges %d bytes, want exactly %d", charged, total)
	}
	return contents
}

// managementValidStoredDescriptor and managementValidStoredReceipt are the
// shortest members the shape checks accept, so a document at exactly the count
// bound is adopted rather than refused for some other reason and the boundary is
// a boundary rather than a blanket refusal.
const (
	managementValidStoredDescriptor = `{"content_type":"Audit.Exchange","content_id":"a","content_uri":"https://manage.office.com/a","content_created":"2026-10-08T11:30:00Z"}`
	managementValidStoredReceipt    = `{"content_id":"a","content_created":"2026-10-08T11:30:00Z"}`
)

// Both stored Management arrays were fully unmarshaled before any element-count
// check ran. json.Unmarshal materialized every member into a
// []managementContent or []managementReceipt, and only then did the count bound
// in validateManagementProgressFields, loadManagementReceiptSet's own count
// check and the aggregate byte budgets look at what had already been allocated.
//
// The pre-decode document ceilings bound the bytes that are read and say nothing
// about what retaining them costs. A member of a dense array is two bytes while
// the struct it decodes to is forty or ninety-six, so a compact document inside
// its ceiling expanded to hundreds of megabytes of retained elements, with an
// element count an order of magnitude past the bound that then refused the whole
// document (CWE-770).
//
// TestManagementStoredArraysApplyTheirBoundsBeforeRetention pins each array at
// its exact count boundary in both directions and pins the dense case the
// boundary alone cannot describe: a document far past the bound has to be
// refused without the allocation.
func TestManagementStoredArraysApplyTheirBoundsBeforeRetention(t *testing.T) {
	key := managementStateKey("microsoft", "dense-arrays")

	t.Run("content-side-document", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		contentsKey := managementContentsKey(key)

		atBound := managementDenseArrayDocument("contents", managementValidStoredDescriptor, managementMaximumDescriptors)
		mustPutManagementBytes(t, rt, contentsKey, atBound)
		contents, found, _, err := loadManagementContentSet(rt, key)
		if err != nil || !found {
			t.Fatalf("a document at exactly the count bound was refused: found=%t error=%v", found, err)
		}
		if len(contents) != managementMaximumDescriptors {
			t.Fatalf("descriptors=%d, want the %d the document carries", len(contents), managementMaximumDescriptors)
		}
		if err := validateManagementDescriptors(contents); err != nil {
			t.Fatalf("the adopted set is not one the persist path accepts: %v", err)
		}

		overBound := managementDenseArrayDocument("contents", managementValidStoredDescriptor, managementMaximumDescriptors+1)
		mustPutManagementBytes(t, rt, contentsKey, overBound)
		if _, _, _, err := loadManagementContentSet(rt, key); err == nil {
			t.Fatal("a document one member over the count bound was adopted")
		} else if !strings.Contains(err.Error(), fmt.Sprintf("exceed capacity %d", managementMaximumDescriptors)) {
			t.Fatalf("error=%v, want the count bound named for the operator", err)
		} else if !recoverableManagementCorruption(err) {
			t.Fatalf("error=%v, want it classified as recoverable corruption", err)
		}

		members := managementDenseMemberCount("contents", "{}", managementMaximumContentSetBytes)
		dense := managementDenseArrayDocument("contents", "{}", members)
		if len(dense) > managementMaximumContentSetBytes {
			t.Fatalf("the dense document is %d bytes, over the %d-byte ceiling, so the ceiling would catch it instead",
				len(dense), managementMaximumContentSetBytes)
		}
		mustPutManagementBytes(t, rt, contentsKey, dense)
		var denseErr error
		allocated := managementDecodeAllocation(t, func() {
			_, _, _, denseErr = loadManagementContentSet(rt, key)
		})
		if denseErr == nil {
			t.Fatal("a document dense past the count bound was adopted")
		}
		if !strings.Contains(denseErr.Error(), fmt.Sprintf("exceed capacity %d", managementMaximumDescriptors)) {
			t.Fatalf("error=%v, want the count bound to be what refused it", denseErr)
		}
		assertManagementDenseRejectionIsBounded(t, "descriptor", members, managementMaximumDescriptors,
			managementRetainedDescriptorBytes, allocated)
	})

	t.Run("receipt-side-document", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		receiptsKey := managementReceiptsKey(key)

		atBound := managementDenseArrayDocument("receipts", managementValidStoredReceipt, managementMaximumReceipts)
		mustPutManagementBytes(t, rt, receiptsKey, atBound)
		receipts, found, _, err := loadManagementReceiptSet(rt, key)
		if err != nil || !found {
			t.Fatalf("a document at exactly the count bound was refused: found=%t error=%v", found, err)
		}
		if len(receipts) != managementMaximumReceipts {
			t.Fatalf("receipts=%d, want the %d the document carries", len(receipts), managementMaximumReceipts)
		}
		if err := validateManagementReceipts(receipts); err != nil {
			t.Fatalf("the adopted set is not one load validation accepts: %v", err)
		}

		overBound := managementDenseArrayDocument("receipts", managementValidStoredReceipt, managementMaximumReceipts+1)
		mustPutManagementBytes(t, rt, receiptsKey, overBound)
		if _, _, _, err := loadManagementReceiptSet(rt, key); err == nil {
			t.Fatal("a document one member over the count bound was adopted")
		} else if !strings.Contains(err.Error(), fmt.Sprintf("exceed capacity %d", managementMaximumReceipts)) {
			t.Fatalf("error=%v, want the count bound named for the operator", err)
		} else if !recoverableManagementCorruption(err) {
			t.Fatalf("error=%v, want it classified as recoverable corruption", err)
		}

		members := managementDenseMemberCount("receipts", "{}", managementMaximumReceiptSetBytes)
		dense := managementDenseArrayDocument("receipts", "{}", members)
		if len(dense) > managementMaximumReceiptSetBytes {
			t.Fatalf("the dense document is %d bytes, over the %d-byte ceiling, so the ceiling would catch it instead",
				len(dense), managementMaximumReceiptSetBytes)
		}
		mustPutManagementBytes(t, rt, receiptsKey, dense)
		var denseErr error
		allocated := managementDecodeAllocation(t, func() {
			_, _, _, denseErr = loadManagementReceiptSet(rt, key)
		})
		if denseErr == nil {
			t.Fatal("a document dense past the count bound was adopted")
		}
		if !strings.Contains(denseErr.Error(), fmt.Sprintf("exceed capacity %d", managementMaximumReceipts)) {
			t.Fatalf("error=%v, want the count bound to be what refused it", denseErr)
		}
		assertManagementDenseRejectionIsBounded(t, "receipt", members, managementMaximumReceipts,
			managementRetainedReceiptBytes, allocated)
	})

	// A head document written by an earlier build carries both sets inline, so
	// the same two arrays arrive through the head's own decode. That path had
	// nothing but the head ceiling in front of it.
	t.Run("legacy-inline-head", func(t *testing.T) {
		for _, tc := range []struct {
			name          string
			array         string
			member        string
			bound         int
			retainedBytes int
		}{
			{"contents", "contents", managementValidStoredDescriptor, managementMaximumDescriptors, managementRetainedDescriptorBytes},
			{"receipts", "receipts", managementValidStoredReceipt, managementMaximumReceipts, managementRetainedReceiptBytes},
		} {
			t.Run(tc.name, func(t *testing.T) {
				rt := newMicrosoftTestRuntime(t)

				atBound := managementDenseArrayDocument(tc.array, tc.member, tc.bound)
				mustPutManagementBytes(t, rt, key, atBound)
				adopted, err := loadManagementState(rt, key)
				if err != nil {
					t.Fatalf("an inline set at exactly the count bound was refused: %v", err)
				}
				inline := len(adopted.Receipts)
				if tc.array == "contents" {
					inline = len(adopted.Contents)
				}
				if inline != tc.bound {
					t.Fatalf("adopted inline %s=%d, want all %d from the head", tc.array, inline, tc.bound)
				}

				overBound := managementDenseArrayDocument(tc.array, tc.member, tc.bound+1)
				mustPutManagementBytes(t, rt, key, overBound)
				if _, err := loadManagementState(rt, key); err == nil {
					t.Fatal("an inline set one member over the count bound was adopted")
				} else if !strings.Contains(err.Error(), fmt.Sprintf("exceed capacity %d", tc.bound)) {
					t.Fatalf("error=%v, want the count bound named for the operator", err)
				} else if !recoverableManagementCorruption(err) {
					t.Fatalf("error=%v, want it classified as recoverable corruption", err)
				}

				members := managementDenseMemberCount(tc.array, "{}", managementMaximumStateBytes)
				dense := managementDenseArrayDocument(tc.array, "{}", members)
				if len(dense) > managementMaximumStateBytes {
					t.Fatalf("the dense head is %d bytes, over the %d-byte ceiling", len(dense), managementMaximumStateBytes)
				}
				mustPutManagementBytes(t, rt, key, dense)
				var denseErr error
				allocated := managementDecodeAllocation(t, func() {
					_, denseErr = loadManagementState(rt, key)
				})
				if denseErr == nil {
					t.Fatal("a head dense past the count bound was adopted")
				}
				if !strings.Contains(denseErr.Error(), fmt.Sprintf("exceed capacity %d", tc.bound)) {
					t.Fatalf("error=%v, want the count bound to be what refused it", denseErr)
				}
				assertManagementDenseRejectionIsBounded(t, tc.array, members, tc.bound, tc.retainedBytes, allocated)

				// The repair reads the durable boundary out of that same head
				// without decoding either set, so the bound that refuses the
				// head cannot also cost the checkpoint the reset restarts from.
				// It names only the boundary, so both arrays are members it does
				// not know and the decoder skips them at the scanner: the whole
				// read allocates a few kilobytes however many members they hold.
				const boundaryMember = `,"completed_through":"2026-10-08T11:00:00Z"}`
				boundaryMembers := managementDenseMemberCount(tc.array, "{}", managementMaximumStateBytes-len(boundaryMember))
				boundary := managementDenseArrayDocument(tc.array, "{}", boundaryMembers)
				boundary = boundary[:len(boundary)-1] + boundaryMember
				if len(boundary) > managementMaximumStateBytes {
					t.Fatalf("the boundary head is %d bytes, over the %d-byte ceiling, so the repair would treat it as absent",
						len(boundary), managementMaximumStateBytes)
				}
				mustPutManagementBytes(t, rt, key, boundary)
				var recovered managementState
				recoveredAlloc := managementDecodeAllocation(t, func() {
					recovered, denseErr = recoverManagementState(rt, key)
				})
				if denseErr != nil {
					t.Fatalf("the repair could not read a head it has to replace: %v", denseErr)
				}
				if !recovered.CompletedThrough.Equal(managementRecoveryBoundary) {
					t.Fatalf("recovered boundary=%s, want the durable %s the head names",
						recovered.CompletedThrough, managementRecoveryBoundary)
				}
				if len(recovered.Contents) != 0 || len(recovered.Receipts) != 0 {
					t.Fatalf("the repair retained %d descriptors and %d receipts from a head it discards",
						len(recovered.Contents), len(recovered.Receipts))
				}
				const recoveryCeiling = 1 << 20
				if recoveredAlloc > recoveryCeiling {
					t.Fatalf("reading the durable boundary out of a head carrying %d dense members allocated %d bytes against a %d-byte ceiling: the set was decoded to reach it",
						boundaryMembers, recoveredAlloc, recoveryCeiling)
				}
			})
		}
	})

	// The aggregate charge has to be exactly what the persist path sums, or a
	// set this build wrote would be refused by the load that follows it.
	t.Run("aggregate-byte-boundary", func(t *testing.T) {
		t.Run("charge-matches-the-persist-path", func(t *testing.T) {
			contents := []managementContent{
				managementStoredFiller("Audit.Exchange", 0, 0),
				managementStoredFiller("Audit.Exchange", 1, 400),
				managementStoredFiller("Audit.Exchange", 2, managementLongestDispatchableURLLength),
			}
			charged := 0
			for _, content := range contents {
				charged += managementDescriptorBytes(content)
			}
			array, err := json.Marshal(contents)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decodeBoundedManagementArray[managementContent](array, "content descriptors", len(contents), charged)
			if err != nil {
				t.Fatalf("a set charging exactly its budget was refused: %v", err)
			}
			if len(decoded) != len(contents) {
				t.Fatalf("descriptors=%d, want %d", len(decoded), len(contents))
			}
			if _, err := decodeBoundedManagementArray[managementContent](array, "content descriptors", len(contents), charged-1); err == nil {
				t.Fatal("a set one byte over its budget was adopted")
			} else if !strings.Contains(err.Error(), fmt.Sprintf("exceed %d bytes", charged-1)) {
				t.Fatalf("error=%v, want the aggregate budget named for the operator", err)
			}

			receipts := []managementReceipt{
				{ContentID: "a", ContentCreated: managementTestNow},
				{ContentID: strings.Repeat("b", managementMaximumContentIDLength), ContentCreated: managementTestNow},
			}
			charged = 0
			for _, receipt := range receipts {
				charged += managementReceiptBytes(receipt)
			}
			if array, err = json.Marshal(receipts); err != nil {
				t.Fatal(err)
			}
			if _, err := decodeBoundedManagementArray[managementReceipt](array, "delivery receipts", len(receipts), charged); err != nil {
				t.Fatalf("a receipt set charging exactly its budget was refused: %v", err)
			}
			if _, err := decodeBoundedManagementArray[managementReceipt](array, "delivery receipts", len(receipts), charged-1); err == nil {
				t.Fatal("a receipt set one byte over its budget was adopted")
			}
		})

		// And at the real budget, through the real load, which is where the
		// bound a window's listing narrows to keep has to agree with it.
		t.Run("at-the-production-descriptor-budget", func(t *testing.T) {
			rt := newMicrosoftTestRuntime(t)
			contentsKey := managementContentsKey(key)
			atBudget := managementDescriptorsCharging(t, "Audit.Exchange", managementMaximumDescriptorBytes)
			document, err := json.Marshal(managementContentSet{Version: managementStateVersion, Contents: atBudget})
			if err != nil {
				t.Fatal(err)
			}
			if len(document) > managementMaximumContentSetBytes {
				t.Fatalf("a document at exactly the budget is %d bytes, over the %d-byte ceiling",
					len(document), managementMaximumContentSetBytes)
			}
			mustPutManagementBytes(t, rt, contentsKey, string(document))
			contents, found, _, err := loadManagementContentSet(rt, key)
			if err != nil || !found || len(contents) != len(atBudget) {
				t.Fatalf("a set at exactly the %d-byte budget was refused: contents=%d found=%t error=%v",
					managementMaximumDescriptorBytes, len(contents), found, err)
			}
			if err := validateManagementDescriptors(contents); err != nil {
				t.Fatalf("the adopted set is not one the persist path accepts: %v", err)
			}

			overBudget := managementDescriptorsCharging(t, "Audit.Exchange", managementMaximumDescriptorBytes+1)
			if document, err = json.Marshal(managementContentSet{Version: managementStateVersion, Contents: overBudget}); err != nil {
				t.Fatal(err)
			}
			if len(document) > managementMaximumContentSetBytes {
				t.Fatalf("a document one byte over the budget is %d bytes, over the %d-byte ceiling, so the ceiling would catch it instead",
					len(document), managementMaximumContentSetBytes)
			}
			mustPutManagementBytes(t, rt, contentsKey, string(document))
			if _, _, _, err := loadManagementContentSet(rt, key); err == nil {
				t.Fatal("a set one byte over the aggregate budget was adopted")
			} else if !strings.Contains(err.Error(), fmt.Sprintf("exceed %d bytes", managementMaximumDescriptorBytes)) {
				t.Fatalf("error=%v, want the aggregate budget named for the operator", err)
			}
		})
	})

	// Preservation: the properties these loads already had are unchanged.
	t.Run("shape-and-trailing-data", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		contentsKey := managementContentsKey(key)
		for _, tc := range []struct{ name, document, want string }{
			{"trailing-data", `{"version":1,"contents":[]} {}`, "decode"},
			{"array-is-an-object", `{"version":1,"contents":{"a":1}}`, "must be a JSON array"},
			{"member-of-the-wrong-type", `{"version":1,"contents":[1]}`, "decode"},
			{"unsupported-version", `{"version":2,"contents":[]}`, "unsupported version 2"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				mustPutManagementBytes(t, rt, contentsKey, tc.document)
				_, _, _, err := loadManagementContentSet(rt, key)
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error=%v, want one naming %q", err, tc.want)
				}
			})
		}
		// An absent array, an explicit null and an empty one all stay the empty
		// answer a document this build writes for a set it holds nothing in.
		for _, document := range []string{`{"version":1}`, `{"version":1,"contents":null}`, `{"version":1,"contents":[]}`} {
			mustPutManagementBytes(t, rt, contentsKey, document)
			contents, found, _, err := loadManagementContentSet(rt, key)
			if err != nil || !found || len(contents) != 0 {
				t.Fatalf("document %s: contents=%d found=%t error=%v", document, len(contents), found, err)
			}
		}
		// A member this build does not know is still ignored, which is what the
		// pre-decode ceilings exist for and what forward compatibility needs.
		mustPutManagementBytes(t, rt, contentsKey,
			`{"version":1,"unknown_member":"x","contents":[`+managementValidStoredDescriptor+`]}`)
		contents, found, _, err := loadManagementContentSet(rt, key)
		if err != nil || !found || len(contents) != 1 {
			t.Fatalf("contents=%d found=%t error=%v, want the known member adopted", len(contents), found, err)
		}
	})
}

// managementStoredRecentPage is one stored listing continuation of the shape the
// source publishes and validateManagementURL accepts, as the head stores it.
const managementStoredRecentPage = `"https://manage.office.com/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/subscriptions/content?contentType=Audit.Exchange&nextPage=0000000000"`

// TestManagementStoredRecentPagesAppliesItsBoundsBeforeRetention pins the head
// document's continuation history at its bounds.
//
// managementStoredHead held the two inline sets to their bounds as they were
// read and left recent_pages on the outer json.Unmarshal, so the whole array was
// materialized -- one retained string header per member, plus every byte behind
// it -- before the count bound in validateManagementProgressFields looked at
// what had already been allocated. managementMaximumStateBytes bounds the bytes
// that are read and says nothing about what retaining them costs: a member of a
// dense array is three bytes while the element it decodes to is a 16-byte
// header, so a valid-size head inside the roughly 41 MiB ceiling carried
// millions of members and allocated hundreds of megabytes before the bound that
// then refused the whole document was consulted (CWE-770).
//
// The count boundary is pinned in both directions, the aggregate budget is
// pinned in both directions at its exact value, and the dense case the
// boundaries alone cannot describe is pinned too: a head far past the count has
// to be refused without the allocation.
func TestManagementStoredRecentPagesAppliesItsBoundsBeforeRetention(t *testing.T) {
	key := managementStateKey("microsoft", "dense-recent-pages")

	t.Run("exact-maximum", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		mustPutManagementBytes(t, rt, key,
			managementDenseArrayDocument("recent_pages", managementStoredRecentPage, managementMaximumContinuationRecent))
		state, err := loadManagementState(rt, key)
		if err != nil {
			t.Fatalf("a head at exactly the count bound was refused: %v", err)
		}
		if len(state.RecentPages) != managementMaximumContinuationRecent {
			t.Fatalf("recent pages=%d, want the %d the head carries", len(state.RecentPages), managementMaximumContinuationRecent)
		}
		if err := validateManagementState(state); err != nil {
			t.Fatalf("the adopted history is not one the persist path accepts: %v", err)
		}
		// The history is what makes a repeated continuation detectable, so the
		// elements have to arrive intact rather than merely be counted.
		if !stringSliceContains(state.RecentPages, strings.Trim(managementStoredRecentPage, `"`)) {
			t.Fatalf("the adopted history does not hold the stored continuation: %+v", state.RecentPages)
		}
	})

	t.Run("maximum-plus-one", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		mustPutManagementBytes(t, rt, key,
			managementDenseArrayDocument("recent_pages", managementStoredRecentPage, managementMaximumContinuationRecent+1))
		_, err := loadManagementState(rt, key)
		if err == nil {
			t.Fatal("a head one member over the count bound was adopted")
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("exceed capacity %d", managementMaximumContinuationRecent)) {
			t.Fatalf("error=%v, want the count bound named for the operator", err)
		}
		if !recoverableManagementCorruption(err) {
			t.Fatalf("error=%v, want it classified as recoverable corruption", err)
		}
		if strings.Contains(err.Error(), "manage.office.com") {
			t.Fatalf("error=%q names a stored continuation", err)
		}
	})

	// The budget in both directions at its exact value. A member of
	// managementMaximumURLLength*6 characters is the widest escape
	// managementMaximumContinuationRecentBytes is derived from, and the charge is
	// the member bytes plus the separator each one needs, so the whole count at
	// that width is exactly the budget.
	t.Run("aggregate-byte-boundary", func(t *testing.T) {
		member := func(characters int) string { return `"` + strings.Repeat("u", characters) + `"` }
		atBudget := member(managementMaximumURLLength * 6)
		if charged := managementMaximumContinuationRecent * (len(atBudget) + 1); charged != managementMaximumContinuationRecentBytes {
			t.Fatalf("the full count at the widest entry charges %d bytes, want exactly the %d-byte budget",
				charged, managementMaximumContinuationRecentBytes)
		}
		rt := newMicrosoftTestRuntime(t)
		document := managementDenseArrayDocument("recent_pages", atBudget, managementMaximumContinuationRecent)
		if len(document) > managementMaximumStateBytes {
			t.Fatalf("a head at exactly the budget is %d bytes, over the %d-byte ceiling", len(document), managementMaximumStateBytes)
		}
		mustPutManagementBytes(t, rt, key, document)
		state, err := loadManagementState(rt, key)
		if err != nil || len(state.RecentPages) != managementMaximumContinuationRecent {
			t.Fatalf("a history at exactly the %d-byte budget was refused: pages=%d error=%v",
				managementMaximumContinuationRecentBytes, len(state.RecentPages), err)
		}

		over := managementDenseArrayDocument("recent_pages", member(managementMaximumURLLength*6+1), managementMaximumContinuationRecent)
		if len(over) > managementMaximumStateBytes {
			t.Fatalf("a head one byte over the budget is %d bytes, over the %d-byte ceiling, so the ceiling would catch it instead",
				len(over), managementMaximumStateBytes)
		}
		mustPutManagementBytes(t, rt, key, over)
		if _, err := loadManagementState(rt, key); err == nil {
			t.Fatal("a history over its aggregate budget was adopted")
		} else if !strings.Contains(err.Error(), fmt.Sprintf("exceed %d bytes", managementMaximumContinuationRecentBytes)) {
			t.Fatalf("error=%v, want the aggregate budget named for the operator", err)
		} else if !recoverableManagementCorruption(err) {
			t.Fatalf("error=%v, want it classified as recoverable corruption", err)
		}
	})

	t.Run("dense-array", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		members := managementDenseMemberCount("recent_pages", `"a"`, managementMaximumStateBytes)
		// Non-vacuous: the head really does carry orders of magnitude more
		// members than the bound admits, so a decode that materialized the array
		// first had to retain a string header for every one of them.
		if members < 100*managementMaximumContinuationRecent {
			t.Fatalf("the dense head carries %d members against a bound of %d, which is not dense enough for the claim",
				members, managementMaximumContinuationRecent)
		}
		dense := managementDenseArrayDocument("recent_pages", `"a"`, members)
		if len(dense) > managementMaximumStateBytes {
			t.Fatalf("the dense head is %d bytes, over the %d-byte ceiling, so the ceiling would catch it instead",
				len(dense), managementMaximumStateBytes)
		}
		mustPutManagementBytes(t, rt, key, dense)
		var denseErr error
		allocated := managementDecodeAllocation(t, func() {
			_, denseErr = loadManagementState(rt, key)
		})
		if denseErr == nil {
			t.Fatal("a head dense past the count bound was adopted")
		}
		// The cost of the refusal is asserted before its message, so a decode
		// that refuses the document only after materializing it fails here
		// rather than on the wording. What materializing the array costs is one
		// 16-byte string header per member, with append allocating every
		// intermediate backing array on the way there as well; the bound admits
		// 32 of them, so a megabyte is ample headroom for the read itself and
		// still orders of magnitude below the unbounded cost.
		const denseCeiling = 1 << 20
		if unbounded := uint64(members) * 16; allocated > denseCeiling {
			t.Fatalf("refusing a head carrying %d dense members allocated %d bytes against a %d-byte ceiling; retaining them all costs at least %d: the array was decoded whole before the bound applied",
				members, allocated, denseCeiling, unbounded)
		}
		if !strings.Contains(denseErr.Error(), fmt.Sprintf("exceed capacity %d", managementMaximumContinuationRecent)) {
			t.Fatalf("error=%v, want the count bound to be what refused it", denseErr)
		}

		// The repair reads the durable boundary out of that same head without
		// decoding the history, so the bound that refuses the head cannot also
		// cost the checkpoint the reset restarts from.
		const boundaryMember = `,"completed_through":"2026-10-08T11:00:00Z"}`
		boundaryMembers := managementDenseMemberCount("recent_pages", `"a"`, managementMaximumStateBytes-len(boundaryMember))
		boundary := managementDenseArrayDocument("recent_pages", `"a"`, boundaryMembers)
		boundary = boundary[:len(boundary)-1] + boundaryMember
		if len(boundary) > managementMaximumStateBytes {
			t.Fatalf("the boundary head is %d bytes, over the %d-byte ceiling, so the repair would treat it as absent",
				len(boundary), managementMaximumStateBytes)
		}
		mustPutManagementBytes(t, rt, key, boundary)
		var recovered managementState
		recoveredAlloc := managementDecodeAllocation(t, func() {
			recovered, denseErr = recoverManagementState(rt, key)
		})
		if denseErr != nil {
			t.Fatalf("the repair could not read a head it has to replace: %v", denseErr)
		}
		if !recovered.CompletedThrough.Equal(managementRecoveryBoundary) {
			t.Fatalf("recovered boundary=%s, want the durable %s the head names", recovered.CompletedThrough, managementRecoveryBoundary)
		}
		if len(recovered.RecentPages) != 0 || recovered.NextPage != "" {
			t.Fatalf("the repair retained %d continuation(s) from a head it discards", len(recovered.RecentPages))
		}
		const recoveryCeiling = 1 << 20
		if recoveredAlloc > recoveryCeiling {
			t.Fatalf("reading the durable boundary out of a head carrying %d dense members allocated %d bytes against a %d-byte ceiling: the history was decoded to reach it",
				boundaryMembers, recoveredAlloc, recoveryCeiling)
		}
	})

	// Preservation: the properties this load already had are unchanged.
	t.Run("shape-and-trailing-data", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		for _, tc := range []struct{ name, document, want string }{
			{"trailing-data", `{"version":1,"recent_pages":[]} {}`, "decode Microsoft Management Activity state"},
			{"array-is-an-object", `{"version":1,"recent_pages":{"a":1}}`, "must be a JSON array"},
			{"member-of-the-wrong-type", `{"version":1,"recent_pages":[1]}`, "decode Microsoft Management Activity state"},
			{"member-of-the-wrong-shape", `{"version":1,"recent_pages":[["a"]]}`, "decode Microsoft Management Activity state"},
			{"unsupported-version", `{"version":2,"recent_pages":[]}`, "unsupported version 2"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				mustPutManagementBytes(t, rt, key, tc.document)
				_, err := loadManagementState(rt, key)
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error=%v, want one naming %q", err, tc.want)
				}
				if !recoverableManagementCorruption(err) {
					t.Fatalf("error=%v, want it classified as recoverable corruption", err)
				}
			})
		}

		// An absent array, an explicit null and an empty one all stay the empty
		// history a head this build writes for a window with no page behind it.
		for _, document := range []string{`{"version":1}`, `{"version":1,"recent_pages":null}`, `{"version":1,"recent_pages":[]}`} {
			mustPutManagementBytes(t, rt, key, document)
			state, err := loadManagementState(rt, key)
			if err != nil || len(state.RecentPages) != 0 {
				t.Fatalf("document %s: recent pages=%d error=%v", document, len(state.RecentPages), err)
			}
		}

		// A repeated bounded member is corrupt state. Accepting both occurrences
		// gives each one a fresh count and byte budget.
		mustPutManagementBytes(t, rt, key,
			`{"version":1,"recent_pages":["https://manage.office.com/one"],"recent_pages":["https://manage.office.com/two","https://manage.office.com/three"]}`)
		if _, err := loadManagementState(rt, key); err == nil || !strings.Contains(err.Error(), "repeats bounded member recent_pages") {
			t.Fatalf("error=%v, want the duplicate bounded member refused", err)
		}
		over := managementDenseArrayDocument("recent_pages", managementStoredRecentPage, managementMaximumContinuationRecent+1)
		mustPutManagementBytes(t, rt, key, over[:len(over)-1]+`,"recent_pages":[]}`)
		if _, err := loadManagementState(rt, key); err == nil ||
			!strings.Contains(err.Error(), fmt.Sprintf("exceed capacity %d", managementMaximumContinuationRecent)) {
			t.Fatalf("error=%v, want the over-bound first occurrence refused on its own", err)
		}
	})

	// Compatibility: a head an earlier build wrote carries both sets inline
	// beside its continuation history, and all three are still adopted together.
	t.Run("legacy-inline-head", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		mustPutManagementBytes(t, rt, key, `{"version":1,"completed_through":"2026-10-08T11:00:00Z",`+
			`"window_start":"2026-10-08T11:00:00Z","window_end":"2026-10-08T11:30:00Z","listing_started":true,"pages":1,`+
			`"next_page":"https://manage.office.com/api/v1.0/next",`+
			`"recent_pages":[`+managementStoredRecentPage+`],`+
			`"contents":[`+managementValidStoredDescriptor+`],`+
			`"receipts":[`+managementValidStoredReceipt+`]}`)
		state, err := loadManagementState(rt, key)
		if err != nil {
			t.Fatalf("a head written by an earlier build was refused: %v", err)
		}
		if len(state.RecentPages) != 1 || len(state.Contents) != 1 || len(state.Receipts) != 1 {
			t.Fatalf("adopted pages=%d contents=%d receipts=%d, want all three from the head",
				len(state.RecentPages), len(state.Contents), len(state.Receipts))
		}
		if !state.CompletedThrough.Equal(managementRecoveryBoundary) || !state.hasPending() {
			t.Fatalf("state=%+v, want the durable boundary and the in-flight listing both kept", state)
		}
	})
}
