package microsoft

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
)

// countingManagementRuntime records every state-store write so a test can
// distinguish per-event writes from full-document writes.
type countingManagementRuntime struct {
	*microsoftTestRuntime
	mu      sync.Mutex
	puts    map[string]int
	bytes   map[string]int
	largest map[string]int
}

func newCountingManagementRuntime(t *testing.T) *countingManagementRuntime {
	t.Helper()
	return &countingManagementRuntime{
		microsoftTestRuntime: newMicrosoftTestRuntime(t),
		puts:                 make(map[string]int),
		bytes:                make(map[string]int),
		largest:              make(map[string]int),
	}
}

func (r *countingManagementRuntime) Put(key string, value []byte) error {
	r.mu.Lock()
	r.puts[key]++
	r.bytes[key] += len(value)
	if len(value) > r.largest[key] {
		r.largest[key] = len(value)
	}
	r.mu.Unlock()
	return r.microsoftTestRuntime.Put(key, value)
}

func (r *countingManagementRuntime) count(key string) (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.puts[key], r.bytes[key]
}

// largestPut reports the biggest single document written to key, which is what
// bounds the cost of one synchronized state write.
func (r *countingManagementRuntime) largestPut(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.largest[key]
}

func managementEventArray(count int) string {
	events := make([]string, 0, count)
	for i := 0; i < count; i++ {
		events = append(events, fmt.Sprintf(`{"Id":"event-%d","CreationTime":"2026-10-08T11:40:00Z","Operation":"MailItemsAccessed"}`, i))
	}
	return "[" + strings.Join(events, ",") + "]"
}

// managementSingleContentServer serves one enabled subscription, one content
// descriptor, and one content body holding eventCount events.
func managementSingleContentServer(t *testing.T, eventCount int) *httptest.Server {
	t.Helper()
	const contentPath = "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content-1"
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, "Audit.Exchange", "content-1", contentPath))
		case strings.HasSuffix(r.URL.Path, "/audit/content-1"):
			_, _ = io.WriteString(w, managementEventArray(eventCount))
		default:
			http.NotFound(w, r)
		}
	})
	return server
}

type managementWriteProfile struct {
	fullPuts, fullBytes         int
	progressPuts, progressBytes int
	syncs                       int
	entries                     int
}

func collectManagementWriteProfile(t *testing.T, eventCount int) managementWriteProfile {
	t.Helper()
	server := managementSingleContentServer(t, eventCount)
	defer server.Close()
	rt := newCountingManagementRuntime(t)
	syncer := &managementSyncController{}
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	fullPuts, fullBytes := rt.count(key)
	progressPuts, progressBytes := rt.count(managementProgressKey(key))
	return managementWriteProfile{
		fullPuts: fullPuts, fullBytes: fullBytes,
		progressPuts: progressPuts, progressBytes: progressBytes,
		syncs:   int(syncer.calls.Load()),
		entries: len(rt.Entries()),
	}
}

// TestManagementPerEventDurabilityDoesNotRewriteTheFullStateDocument pins the
// write-amplification repair. Accepting one more event must cost one more
// small checkpoint write and one more synchronization, not another full
// re-marshal of a document bounded only by managementMaximumDescriptors and
// managementMaximumReceipts.
func TestManagementPerEventDurabilityDoesNotRewriteTheFullStateDocument(t *testing.T) {
	const few, many = 3, 50
	small := collectManagementWriteProfile(t, few)
	large := collectManagementWriteProfile(t, many)

	if small.entries != few || large.entries != many {
		t.Fatalf("delivered entries small=%d large=%d", small.entries, large.entries)
	}
	if small.fullPuts != large.fullPuts {
		t.Fatalf("full state writes scale with event count: %d events=%d writes, %d events=%d writes",
			few, small.fullPuts, many, large.fullPuts)
	}
	if small.fullBytes != large.fullBytes {
		t.Fatalf("full state bytes scale with event count: %d events=%d bytes, %d events=%d bytes",
			few, small.fullBytes, many, large.fullBytes)
	}
	// Per-event durability is unchanged: exactly one checkpoint write and one
	// state-store synchronization per accepted event.
	if got, want := large.progressPuts-small.progressPuts, many-few; got != want {
		t.Fatalf("delivery checkpoint writes grew by %d, want %d", got, want)
	}
	if got, want := large.syncs-small.syncs, many-few; got != want {
		t.Fatalf("state synchronizations grew by %d, want one per accepted event (%d)", got, want)
	}
	if small.syncs != small.fullPuts+few || large.syncs != large.fullPuts+many {
		t.Fatalf("synchronizations small=%d (full=%d) large=%d (full=%d)", small.syncs, small.fullPuts, large.syncs, large.fullPuts)
	}
	if perWrite := large.progressBytes / large.progressPuts; perWrite > 512 {
		t.Fatalf("delivery checkpoint averages %d bytes per write, want a small fixed document", perWrite)
	}
}

// TestManagementRestartResumesFromTheDurableDeliveryProgressKey proves the
// resumable ordinal survives a crash mid-content even though the full document
// is no longer rewritten per event.
func TestManagementRestartResumesFromTheDurableDeliveryProgressKey(t *testing.T) {
	server := managementSingleContentServer(t, 2)
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	rt.failAt.Store(2)
	syncer := &managementSyncController{}
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "write failure") {
		t.Fatalf("error=%v", err)
	}

	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	encoded, err := rt.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	var persisted managementState
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.EventIndex != 0 {
		t.Fatalf("full state document was rewritten per event: EventIndex=%d", persisted.EventIndex)
	}
	encodedProgress, err := rt.Get(managementProgressKey(key))
	if err != nil {
		t.Fatal(err)
	}
	var progress managementProgress
	if err := json.Unmarshal(encodedProgress, &progress); err != nil {
		t.Fatal(err)
	}
	if progress.EventIndex != 1 || progress.ActiveContentID != "content-1" || progress.ContentIndex != 0 {
		t.Fatalf("delivery checkpoint=%+v", progress)
	}
	if progress.ActiveDigest != persisted.ActiveDigest || progress.ActiveDigest == "" {
		t.Fatalf("checkpoint digest=%q state digest=%q", progress.ActiveDigest, persisted.ActiveDigest)
	}

	loaded, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EventIndex != 1 {
		t.Fatalf("resumed event ordinal=%d, want 1", loaded.EventIndex)
	}

	// A restarted process must deliver the remaining event exactly once.
	rt.failAt.Store(0)
	plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.Entries()) != 2 {
		t.Fatalf("entries after restart=%d, want 2", len(rt.Entries()))
	}
	completed, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(completed.Receipts) != 1 || !completed.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed state=%+v", completed)
	}
}

// TestManagementStaleDeliveryProgressIsIgnored pins that the full document stays
// authoritative. A checkpoint recorded against different content must never be
// mistaken for progress through the content now being delivered.
func TestManagementStaleDeliveryProgressIsIgnored(t *testing.T) {
	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	state := managementState{
		Version:     managementStateVersion,
		WindowStart: managementTestNow.Add(-time.Hour), WindowEnd: managementTestNow,
		Contents: []managementContent{
			{ContentType: "Audit.Exchange", ContentID: "content-1", ContentURI: "https://manage.office.com/one", ContentCreated: managementTestNow.Add(-30 * time.Minute)},
			{ContentType: "Audit.Exchange", ContentID: "content-2", ContentURI: "https://manage.office.com/two", ContentCreated: managementTestNow.Add(-20 * time.Minute)},
		},
		ContentIndex: 1, ActiveContentID: "content-2", ActiveDigest: "digest-2", EventIndex: 2,
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(key, encoded); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		progress managementProgress
		want     int
	}{
		{name: "different-content", progress: managementProgress{Version: managementProgressVersion, ContentIndex: 0, ActiveContentID: "content-1", ActiveDigest: "digest-1", EventIndex: 900}, want: 2},
		{name: "same-content-different-digest", progress: managementProgress{Version: managementProgressVersion, ContentIndex: 1, ActiveContentID: "content-2", ActiveDigest: "digest-stale", EventIndex: 900}, want: 2},
		{name: "behind-the-document", progress: managementProgress{Version: managementProgressVersion, ContentIndex: 1, ActiveContentID: "content-2", ActiveDigest: "digest-2", EventIndex: 1}, want: 2},
		{name: "matching-content", progress: managementProgress{Version: managementProgressVersion, ContentIndex: 1, ActiveContentID: "content-2", ActiveDigest: "digest-2", EventIndex: 5}, want: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encodedProgress, err := json.Marshal(tc.progress)
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.Put(managementProgressKey(key), encodedProgress); err != nil {
				t.Fatal(err)
			}
			loaded, err := loadManagementState(rt, key)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.EventIndex != tc.want {
				t.Fatalf("resumed event ordinal=%d, want %d", loaded.EventIndex, tc.want)
			}
		})
	}

	if err := rt.Put(managementProgressKey(key), []byte(`{"version":99,"event_index":4}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := loadManagementState(rt, key); err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("unsupported checkpoint version error=%v", err)
	}
}

// TestManagementBacklogDrainIsNotThrottledAtTheImmediateRunCap pins the
// throughput repair. While a catch-up target is outstanding, reaching the
// immediate-run cap must not drop the job back to Request-Interval, because
// that is what let a backlog grow until it fell off the source's seven-day
// retention. Once the backlog is drained the cap applies again.
func TestManagementBacklogDrainIsNotThrottledAtTheImmediateRunCap(t *testing.T) {
	rt := newMicrosoftTestRuntime(t)
	plugin := NewWithStateSync(&Config{}, func() error { return nil })
	key := managementStateKey("microsoft", "backlog")
	state := managementState{
		Version:     managementStateVersion,
		WindowStart: managementTestNow.Add(-time.Hour), WindowEnd: managementTestNow,
		TargetThrough:    managementTestNow,
		CompletedThrough: managementTestNow.Add(-72 * time.Hour),
	}
	for run := 1; run <= 3*managementMaximumImmediateRuns; run++ {
		pending, err := plugin.managementContinuation(rt, key, &state)
		if err != nil {
			t.Fatal(err)
		}
		if !pending {
			t.Fatalf("run=%d throttled the catch-up drain back to the poll interval", run)
		}
	}
	loaded, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ImmediateRuns > managementMaximumImmediateRuns {
		t.Fatalf("immediate runs=%d exceeded the stored bound", loaded.ImmediateRuns)
	}

	// Caught up: the bound applies again so the job returns to its interval.
	state.CompletedThrough = state.TargetThrough
	state.ImmediateRuns = 0
	for run := 1; run <= managementMaximumImmediateRuns; run++ {
		pending, err := plugin.managementContinuation(rt, key, &state)
		if err != nil {
			t.Fatal(err)
		}
		if pending != (run < managementMaximumImmediateRuns) {
			t.Fatalf("caught-up run=%d pending=%v", run, pending)
		}
	}

	// With no catch-up target at all the original bound is unchanged.
	state.TargetThrough, state.CompletedThrough, state.ImmediateRuns = time.Time{}, time.Time{}, 0
	for run := 1; run <= managementMaximumImmediateRuns; run++ {
		pending, err := plugin.managementContinuation(rt, key, &state)
		if err != nil {
			t.Fatal(err)
		}
		if pending != (run < managementMaximumImmediateRuns) {
			t.Fatalf("untargeted run=%d pending=%v", run, pending)
		}
	}
}

// runPollThroughJobAdapter drives one plugin poll through the real hosted job
// adapter instead of calling Handle directly. The adapter is what invalidated
// the previous signal: it calls ClearError() after every Handle that returns no
// error, including the very poll that reported the loss, so a status error set
// there is wiped microseconds later and never reaches the status API. Stop()
// ends the loop, and the adapter only looks at the continuation after it has
// already taken the clear path.
func runPollThroughJobAdapter(t *testing.T, plugin *Microsoft, rt hosted.Runtime, syncFn func() error) {
	t.Helper()
	polls := 0
	job := hosted.JobFunc(func(ctx context.Context, runtime hosted.Runtime) (*hosted.Continuation, error) {
		polls++
		continuation, err := plugin.Handle(ctx, runtime)
		if err != nil {
			t.Errorf("poll error=%v", err)
		}
		if continuation == nil {
			t.Error("plugin stopped rescheduling itself")
		}
		return hosted.Stop(), nil
	})
	if err := hosted.WrapJobWithSync(job, syncFn).Run(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if polls != 1 {
		t.Fatalf("polls=%d, want exactly 1", polls)
	}
}

// TestManagementRetentionLossIsOperatorVisible pins that an interval the source
// can no longer serve stays reported on the status surface, naming the skipped
// duration, after the job adapter has finished with the poll that reported it.
func TestManagementRetentionLossIsOperatorVisible(t *testing.T) {
	t.Run("checkpoint-outside-retention", func(t *testing.T) {
		server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
				writeEnabledSubscription(w, "Audit.Exchange")
			case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
				_, _ = io.WriteString(w, `[]`)
			default:
				http.NotFound(w, r)
			}
		})
		defer server.Close()
		rt := newMicrosoftTestRuntime(t)
		key := managementStateKey("microsoft", "microsoft365-audit-exchange")
		encoded, err := json.Marshal(managementState{
			Version: managementStateVersion, CompletedThrough: managementTestNow.Add(-10 * 24 * time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.Put(key, encoded); err != nil {
			t.Fatal(err)
		}
		syncer := &managementSyncController{}
		plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
		plugin.now = func() time.Time { return managementTestNow }
		runPollThroughJobAdapter(t, plugin, rt, syncer.Sync)
		assertRetentionLossReported(t, rt, "checkpoint is outside source retention", "72h5m0s",
			managementTestNow.Add(-10*24*time.Hour), managementTestNow.Add(-managementMaximumHistory).Add(managementRetentionDispatchMargin))
	})

	t.Run("pending-listing-crossed-retention", func(t *testing.T) {
		server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
				writeEnabledSubscription(w, "Audit.Exchange")
			case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
				_, _ = io.WriteString(w, `[]`)
			default:
				http.NotFound(w, r)
			}
		})
		defer server.Close()
		rt := newMicrosoftTestRuntime(t)
		key := managementStateKey("microsoft", "microsoft365-audit-exchange")
		plannedAt := managementTestNow.Add(-10 * time.Minute)
		staleStart := plannedAt.Add(-managementMaximumHistory).Add(managementRetentionDispatchMargin)
		encoded, err := json.Marshal(managementState{
			Version:          managementStateVersion,
			CompletedThrough: managementTestNow.Add(-8 * 24 * time.Hour),
			TargetThrough:    plannedAt,
			WindowStart:      staleStart,
			WindowEnd:        staleStart.Add(managementMaximumWindow),
			ListingStarted:   true,
			Pages:            1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.Put(key, encoded); err != nil {
			t.Fatal(err)
		}
		syncer := &managementSyncController{}
		plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
		plugin.now = func() time.Time { return managementTestNow }
		runPollThroughJobAdapter(t, plugin, rt, syncer.Sync)
		assertRetentionLossReported(t, rt, "pending listing crossed source retention", "10m0s",
			staleStart, managementTestNow.Add(-managementMaximumHistory).Add(managementRetentionDispatchMargin))
	})
}

func assertRetentionLossReported(t *testing.T, rt *microsoftTestRuntime, fragment, skipped string, unavailableFrom, resumedAt time.Time) {
	t.Helper()
	if got := rt.logsContaining("WARN", fragment); len(got) != 0 {
		t.Fatalf("permanent retention loss was still reported as a warning: %+v", got)
	}
	lines := rt.logsContaining("ERROR", fragment)
	if len(lines) != 1 {
		t.Fatalf("retention-loss errors=%d, want exactly 1 (%+v)", len(lines), lines)
	}
	line := lines[0]
	if got := line.Params["skipped"]; got != skipped {
		t.Fatalf("skipped=%q, want %q", got, skipped)
	}
	if got, want := line.Params["unavailable_from"], unavailableFrom.UTC().Format(time.RFC3339); got != want {
		t.Fatalf("unavailable_from=%q, want %q", got, want)
	}
	if got, want := line.Params["resumed_at"], resumedAt.UTC().Format(time.RFC3339); got != want {
		t.Fatalf("resumed_at=%q, want %q", got, want)
	}
	if got := line.Params["selector"]; got != "microsoft365-audit-exchange" {
		t.Fatalf("selector=%q", got)
	}
	// The adapter has already run ClearError() for this poll, so a signal left
	// on the error channel would be gone by now.
	warn, err := rt.Status()
	if err != nil {
		t.Fatalf("status error=%v, want the retention signal on a channel the poll loop does not clear", err)
	}
	if !strings.Contains(warn, "permanently skipped") {
		t.Fatalf("operator-visible status=%q, want the permanent skip reported", warn)
	}
	if !strings.Contains(warn, skipped) {
		t.Fatalf("operator-visible status %q does not state the skipped duration %q", warn, skipped)
	}
	if !strings.Contains(warn, "microsoft365-audit-exchange") {
		t.Fatalf("operator-visible status %q does not name the selector", warn)
	}
}
