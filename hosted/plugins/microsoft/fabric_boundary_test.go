package microsoft

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFabricSubMillisecondMidnightBoundary(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		start, e1 := time.Parse(time.RFC3339Nano, strings.Trim(r.URL.Query().Get("startDateTime"), "'"))
		end, e2 := time.Parse(time.RFC3339Nano, strings.Trim(r.URL.Query().Get("endDateTime"), "'"))
		if e1 != nil || e2 != nil || start.After(end) || start.Day() != end.Day() {
			t.Errorf("invalid Fabric day window start=%s end=%s", start, end)
		}
		fmt.Fprint(w, `{"activityEventEntities":[]}`)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.http = server.Client()
	start := time.Date(2026, 9, 20, 23, 59, 59, 999500000, time.UTC)
	if _, err := client.Fetch(context.Background(), datasets["fabric-activity"], start, start.Add(time.Second), ""); err != nil {
		t.Fatal(err)
	}
}

func TestFabricExpiredPendingRebasesToFinalizedRetentionWindow(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	var expiredCalls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		if r.URL.Path == "/expired" {
			expiredCalls++
			http.Error(w, "expired", http.StatusGone)
			return
		}
		fmt.Fprint(w, `{"activityEventEntities":[]}`)
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"fabric-activity"}
	p.now = func() time.Time { return now }
	oldStart := now.Add(-30 * 24 * time.Hour)
	state := streamState{
		WindowStart: oldStart, WindowEnd: oldStart.Add(time.Hour),
		Continuation: &FetchContinuation{Version: fetchContinuationVersion, Kind: KindFabricActivity, NextURL: server.URL + "/expired", FabricCursor: oldStart, FabricDayEnd: oldStart.Add(time.Hour)},
	}
	if err := saveState(rt, "microsoft/fabric-activity", state); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if expiredCalls != 0 {
		t.Fatalf("expired continuation requested %d times", expiredCalls)
	}
	want := now.Add(-sourceConsistencyDelay).Truncate(time.Millisecond)
	watermark, err := rt.GetTime("microsoft/fabric-activity/timestamp")
	if err != nil || !watermark.Equal(want) {
		t.Fatalf("watermark=%s error=%v, want %s", watermark, err, want)
	}
}

func TestFabricPendingWindowSurvivesBackwardClockWithoutIdleContinuation(t *testing.T) {
	forwardNow := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	windowStart := forwardNow.Add(-2 * time.Hour)
	windowEnd := forwardNow.Add(-sourceConsistencyDelay)
	var dataCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		dataCalls.Add(1)
		fmt.Fprint(w, `{"activityEventEntities":[]}`)
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"fabric-activity"}
	p.now = func() time.Time { return forwardNow.Add(-90 * time.Minute) }
	state := streamState{
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		Continuation: &FetchContinuation{
			Version:      fetchContinuationVersion,
			Kind:         KindFabricActivity,
			NextURL:      server.URL + "/continued",
			FabricCursor: windowStart.Add(time.Hour),
			FabricDayEnd: windowEnd,
		},
	}
	if err := saveState(rt, "microsoft/fabric-activity", state); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		continuation, err := p.Handle(t.Context(), rt)
		if err != nil {
			t.Fatal(err)
		}
		if continuation == nil || continuation.Delay == 0 {
			t.Fatalf("handle %d returned %v; frozen pending window must not spin immediately after a backward clock step", i+1, continuation)
		}
	}
	if got := dataCalls.Load(); got != 1 {
		t.Fatalf("Fabric data requests=%d, want 1 resumed request", got)
	}
	completed, err := loadState(rt, "microsoft/fabric-activity")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Continuation != nil || !completed.Watermark.Equal(windowEnd) {
		t.Fatalf("completed state=%+v, want watermark %s and no continuation", completed, windowEnd)
	}
}

func TestFabricExpiredCursorInsideRetainedWindowRebases(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	floor := now.Add(-fabricRetention)
	windowEnd := now.Add(-sourceConsistencyDelay)
	var expiredCalls, freshCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		if r.URL.Path == "/expired" {
			expiredCalls.Add(1)
			http.Error(w, "expired", http.StatusGone)
			return
		}
		freshCalls.Add(1)
		fmt.Fprint(w, `{"activityEventEntities":[]}`)
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"fabric-activity"}
	p.now = func() time.Time { return now }
	state := streamState{
		WindowStart: floor.Add(-24 * time.Hour),
		WindowEnd:   windowEnd,
		Continuation: &FetchContinuation{
			Version:      fetchContinuationVersion,
			Kind:         KindFabricActivity,
			NextURL:      server.URL + "/expired",
			FabricCursor: floor.Add(-24 * time.Hour),
			FabricDayEnd: floor,
		},
	}
	if err := saveState(rt, "microsoft/fabric-activity", state); err != nil {
		t.Fatal(err)
	}

	continuation, err := p.Handle(t.Context(), rt)
	if err != nil {
		t.Fatal(err)
	}
	if continuation == nil || continuation.Delay == 0 {
		t.Fatalf("handle returned %v; rebased retained work must complete without an immediate loop", continuation)
	}
	if got := expiredCalls.Load(); got != 0 {
		t.Fatalf("expired continuation requested %d times", got)
	}
	if got := freshCalls.Load(); got == 0 {
		t.Fatal("retained Fabric range was not fetched from a fresh endpoint")
	}
	completed, err := loadState(rt, "microsoft/fabric-activity")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Continuation != nil || !completed.Watermark.Equal(windowEnd) {
		t.Fatalf("completed state=%+v, want watermark %s and no continuation", completed, windowEnd)
	}
}

// Fabric serves only the last 28 days. A selector resuming from a watermark
// older than that floor has its replacement window clamped forward, and the
// interval the clamp drops can never be refetched by any later
// reconfiguration. That permanent loss has to reach the status API an operator
// watches: collect continues and returns a nil error on this path, and
// hosted/job.go calls ClearError() after every poll that returns none, so the
// error channel cannot carry it.
func TestFabricRetentionClampReportsPermanentLossToStatusAndLog(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	floor := now.Add(-fabricRetention)
	// A selector offline for longer than Fabric's retention.
	staleWatermark := now.Add(-40 * 24 * time.Hour)
	wantAnchor := staleWatermark.Add(-sourceReplayOverlap)
	wantSkipped := floor.Sub(wantAnchor)

	var mu sync.Mutex
	var earliestRequested time.Time
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		raw := r.URL.Query().Get("startDateTime")
		start, err := time.Parse(time.RFC3339Nano, strings.Trim(raw, "'"))
		if err != nil {
			t.Errorf("unparsable Fabric startDateTime %q: %v", raw, err)
		} else {
			mu.Lock()
			if earliestRequested.IsZero() || start.Before(earliestRequested) {
				earliestRequested = start
			}
			mu.Unlock()
		}
		fmt.Fprint(w, `{"activityEventEntities":[]}`)
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"fabric-activity"}
	p.now = func() time.Time { return now }
	if err := saveState(rt, "microsoft/fabric-activity", streamState{Watermark: staleWatermark}); err != nil {
		t.Fatal(err)
	}

	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}

	// The status warning survives the poll loop, so it must name the selector
	// and the whole skipped interval on its own.
	warnings := rt.warnStateContaining("permanently skipped")
	if len(warnings) != 1 {
		t.Fatalf("permanent-loss status warnings=%v, want exactly one; published=%v", warnings, rt.warnStateContaining(""))
	}
	for _, want := range []string{
		"fabric-activity",
		wantSkipped.String(),
		wantAnchor.UTC().Format(time.RFC3339),
		floor.UTC().Format(time.RFC3339),
	} {
		if !strings.Contains(warnings[0], want) {
			t.Fatalf("status warning %q does not name %q", warnings[0], want)
		}
	}

	// Log evidence at the severity the sibling permanent-loss reports use.
	lines := rt.logsContaining("ERROR", "outside the available retention window")
	if len(lines) != 1 {
		t.Fatalf("ERROR lines for the retention clamp=%d, want 1", len(lines))
	}
	for name, want := range map[string]string{
		"selector":        "fabric-activity",
		"anchor":          wantAnchor.UTC().Format(time.RFC3339Nano),
		"available_since": floor.UTC().Format(time.RFC3339Nano),
		"skipped":         wantSkipped.String(),
	} {
		if got := lines[0].Params[name]; got != want {
			t.Fatalf("logged %s=%q, want %q", name, got, want)
		}
	}
	if got := rt.logsContaining("WARN", "outside the available retention window"); len(got) != 0 {
		t.Fatalf("permanently skipped Fabric history is still reported at WARN only: %v", got)
	}

	// Window and checkpoint behavior is unchanged by the report: nothing before
	// the retention floor is requested, and the window still completes against
	// the finalized horizon.
	mu.Lock()
	requested := earliestRequested
	mu.Unlock()
	if requested.IsZero() || requested.Before(floor) {
		t.Fatalf("earliest requested Fabric start=%s, want no request before the retention floor %s", requested, floor)
	}
	wantWatermark := now.Add(-sourceConsistencyDelay).Truncate(time.Millisecond)
	completed, err := loadState(rt, "microsoft/fabric-activity")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Continuation != nil || !completed.Watermark.Equal(wantWatermark) {
		t.Fatalf("completed state=%+v, want watermark %s and no continuation", completed, wantWatermark)
	}
	if completed.Fabric == nil || !completed.Fabric.AvailableSince.Equal(floor) {
		t.Fatalf("recorded Fabric scan=%+v, want available_since %s", completed.Fabric, floor)
	}
}

// The clamp report is specific to permanent loss: a window entirely inside
// Fabric's retention skips nothing, so neither the status warning nor the error
// line may be published.
func TestFabricWindowInsideRetentionPublishesNoLoss(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		fmt.Fprint(w, `{"activityEventEntities":[]}`)
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"fabric-activity"}
	p.now = func() time.Time { return now }
	if err := saveState(rt, "microsoft/fabric-activity", streamState{Watermark: now.Add(-2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}

	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := rt.warnStateContaining(""); len(got) != 0 {
		t.Fatalf("status warnings=%v, want none for a window inside retention", got)
	}
	if got := rt.logsContaining("ERROR", "retention window"); len(got) != 0 {
		t.Fatalf("retention loss reported for a retained window: %v", got)
	}
}

func TestFabricPendingExpirationBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	floor := now.Add(-fabricRetention)
	validEnd := now.Add(-sourceConsistencyDelay)
	tests := []struct {
		name         string
		windowEnd    time.Time
		cursor       time.Time
		dayEnd       time.Time
		nextURL      string
		continuation bool
		wantExpired  bool
	}{
		{name: "no continuation", windowEnd: validEnd},
		{name: "fully expired window", windowEnd: floor.Add(-time.Nanosecond), cursor: floor, dayEnd: floor.Add(time.Hour), continuation: true, wantExpired: true},
		{name: "expired cursor without next URL", windowEnd: validEnd, cursor: floor.Add(-time.Nanosecond), dayEnd: floor, continuation: true, wantExpired: true},
		{name: "expired cursor ending at floor", windowEnd: validEnd, cursor: floor.Add(-time.Hour), dayEnd: floor, nextURL: "https://example.invalid/expired", continuation: true, wantExpired: true},
		{name: "expired cursor crossing floor", windowEnd: validEnd, cursor: floor.Add(-time.Hour), dayEnd: floor.Add(time.Hour), nextURL: "https://example.invalid/expired", continuation: true, wantExpired: true},
		{name: "cursor exactly at floor", windowEnd: validEnd, cursor: floor, dayEnd: floor.Add(time.Hour), nextURL: "https://example.invalid/current", continuation: true},
		{name: "cursor after floor without next URL", windowEnd: validEnd, cursor: floor.Add(time.Nanosecond), dayEnd: floor.Add(time.Hour), continuation: true},
		{name: "cursor after floor with next URL", windowEnd: validEnd, cursor: floor.Add(time.Hour), dayEnd: floor.Add(2 * time.Hour), nextURL: "https://example.invalid/current", continuation: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := streamState{WindowEnd: tt.windowEnd}
			if tt.continuation {
				state.Continuation = &FetchContinuation{
					Version: fetchContinuationVersion, Kind: KindFabricActivity,
					NextURL: tt.nextURL, FabricCursor: tt.cursor, FabricDayEnd: tt.dayEnd,
				}
			}
			if got := fabricPendingExpired(state, now); got != tt.wantExpired {
				t.Fatalf("fabricPendingExpired()=%t, want %t", got, tt.wantExpired)
			}
		})
	}
}

// The gate itself: loss is unread history only when nothing names a later
// resume position. A preserved cursor at or past the clamped start proves the
// clamped interval was collected, because the Fabric day walk advances its
// cursor only by completing a day.
func TestFabricRetentionLossUnreadTracksTheResumePosition(t *testing.T) {
	availableSince := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		cursor *FetchContinuation
		want   bool
	}{
		{name: "no continuation", want: true},
		{name: "zero cursor names no resume position", cursor: &FetchContinuation{}, want: true},
		{name: "cursor below the clamped start", cursor: &FetchContinuation{FabricCursor: availableSince.Add(-time.Nanosecond)}, want: true},
		{name: "cursor exactly at the clamped start", cursor: &FetchContinuation{FabricCursor: availableSince}},
		{name: "cursor past the clamped start", cursor: &FetchContinuation{FabricCursor: availableSince.Add(6 * time.Hour)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fabricRetentionLossUnread(tt.cursor, availableSince); got != tt.want {
				t.Fatalf("fabricRetentionLossUnread()=%t, want %t", got, tt.want)
			}
		})
	}
}

// A pending Fabric window is frozen at the start the pass that opened it asked
// for, and the retention floor moves forward with the clock. A window that
// takes several passes therefore has that frozen start clamped on every later
// pass -- even though the cursor it preserves has already walked past the new
// floor, which is the position collection actually resumes from, and the
// interval the clamp drops is history this window already read. Anchoring the
// permanent-loss report on the frozen start alone restated that loss on every
// pass: an operator-visible error and a sticky status warning naming history
// that was never skipped, for as long as the window kept running.
func TestFabricPendingWindowAgingBelowRetentionReportsNoLossWhileItsCursorIsValid(t *testing.T) {
	const passes = 4
	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	floor := base.Add(-fabricRetention)
	// The frozen start is already below the floor the first pass sees, and the
	// floor moves another hour under it on every later pass.
	frozenStart := floor.Add(-2 * time.Hour)
	windowEnd := base.Add(-sourceConsistencyDelay).Truncate(time.Millisecond)
	// The preserved cursor is inside retention and past every floor these
	// passes clamp to, so the clamped interval is history it already collected.
	resumeFrom := floor.Add(6 * time.Hour)

	var pendingCalls, rebasedCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
		case strings.HasPrefix(r.URL.Path, "/pending"):
			serial := pendingCalls.Add(1)
			fmt.Fprintf(w, `{"activityEventEntities":[{"Id":%q,"CreationTime":%q}],"continuationUri":%q}`,
				fmt.Sprintf("event-%d", serial), resumeFrom.Format(time.RFC3339Nano),
				fmt.Sprintf("%s/pending?page=%d", serverURL(r), serial+1))
		default:
			// A freshly built day URL means the pending window was dropped and
			// the day walk restarted, which this window must not do.
			rebasedCalls.Add(1)
			fmt.Fprint(w, `{"activityEventEntities":[]}`)
		}
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	p := newSelectorTestPlugin(t, server, "fabric-activity")
	clock := base
	p.now = func() time.Time { return clock }
	// One record per chunk, so every pass hands its pending cursor back and the
	// window really does stay open across the moving floor.
	p.client.chunkRetentionRecords = 1

	key := stateKey("microsoft", "fabric-activity", "")
	if err := saveState(rt, key, streamState{
		WindowStart: frozenStart, WindowEnd: windowEnd,
		Continuation: &FetchContinuation{
			Version: fetchContinuationVersion, Kind: KindFabricActivity,
			NextURL: server.URL + "/pending?page=1", FabricCursor: resumeFrom, FabricDayEnd: windowEnd,
		},
	}); err != nil {
		t.Fatal(err)
	}

	for pass := 1; pass <= passes; pass++ {
		if _, err := p.Handle(t.Context(), rt); err != nil {
			t.Fatalf("pass %d error=%v", pass, err)
		}
		stored, err := loadState(rt, key)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Continuation == nil {
			t.Fatalf("pass %d completed the window; this regression needs one that spans the moving floor", pass)
		}
		if !stored.Continuation.FabricCursor.Equal(resumeFrom) {
			t.Fatalf("pass %d resumes from %s, want the preserved cursor %s", pass, stored.Continuation.FabricCursor, resumeFrom)
		}
		if !stored.WindowStart.After(frozenStart) {
			t.Fatalf("pass %d window start=%s, want it clamped forward of the frozen %s", pass, stored.WindowStart, frozenStart)
		}
		if got := pendingCalls.Load(); got != int64(pass) {
			t.Fatalf("pending pages read=%d after %d passes, want one per pass", got, pass)
		}
		clock = clock.Add(time.Hour)
	}

	if got := rebasedCalls.Load(); got != 0 {
		t.Fatalf("the pending window was rebased %d times while its cursor was inside retention", got)
	}
	if got := rt.logsContaining("ERROR", "outside the available retention window"); len(got) != 0 {
		t.Fatalf("permanent loss reported %d times for history the cursor had already passed: %+v", len(got), got)
	}
	if got := rt.warnStateContaining("permanently skipped"); len(got) != 0 {
		t.Fatalf("permanent-loss status warnings=%v, want none while the resume position is inside retention", got)
	}
	if got := len(rt.Entries()); got != passes {
		t.Fatalf("entries=%d, want one per pass: the window has to keep making progress", got)
	}
}

// The gate is on the resume position, not on the report. A pending window whose
// own cursor has expired is rebased by the path that already drops it, which
// leaves nothing to resume from, and the interval the clamp drops really is
// gone: that loss is still reported, exactly once, and the window still
// completes against its finalized end.
func TestFabricExpiredCursorStillReportsRetentionLossExactlyOnce(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	floor := now.Add(-fabricRetention)
	windowEnd := now.Add(-sourceConsistencyDelay).Truncate(time.Millisecond)
	frozenStart := floor.Add(-24 * time.Hour)

	var expiredCalls, freshCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
		case r.URL.Path == "/expired":
			expiredCalls.Add(1)
			http.Error(w, "expired", http.StatusGone)
		default:
			freshCalls.Add(1)
			fmt.Fprint(w, `{"activityEventEntities":[]}`)
		}
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	p := newSelectorTestPlugin(t, server, "fabric-activity")
	clock := now
	p.now = func() time.Time { return clock }
	key := stateKey("microsoft", "fabric-activity", "")
	if err := saveState(rt, key, streamState{
		WindowStart: frozenStart, WindowEnd: windowEnd,
		Continuation: &FetchContinuation{
			Version: fetchContinuationVersion, Kind: KindFabricActivity,
			NextURL: server.URL + "/expired", FabricCursor: frozenStart, FabricDayEnd: floor,
		},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := expiredCalls.Load(); got != 0 {
		t.Fatalf("expired continuation requested %d times", got)
	}
	if freshCalls.Load() == 0 {
		t.Fatal("the retained part of the window was never fetched")
	}
	reports := rt.logsContaining("ERROR", "outside the available retention window")
	if len(reports) != 1 {
		t.Fatalf("permanent-loss reports=%d, want exactly 1 (%+v)", len(reports), reports)
	}
	for name, want := range map[string]string{
		"selector":        "fabric-activity",
		"anchor":          frozenStart.UTC().Format(time.RFC3339Nano),
		"available_since": floor.UTC().Format(time.RFC3339Nano),
		"skipped":         floor.Sub(frozenStart).String(),
	} {
		if got := reports[0].Params[name]; got != want {
			t.Fatalf("logged %s=%q, want %q", name, got, want)
		}
	}
	if got := rt.warnStateContaining("permanently skipped"); len(got) != 1 {
		t.Fatalf("permanent-loss status warnings=%v, want exactly one", got)
	}
	completed, err := loadState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Continuation != nil || !completed.Watermark.Equal(windowEnd) {
		t.Fatalf("completed state=%+v, want watermark %s and no continuation", completed, windowEnd)
	}

	// The next window starts at that watermark, well inside retention, so the
	// loss is stated once rather than on every later poll.
	clock = clock.Add(time.Hour)
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := rt.logsContaining("ERROR", "outside the available retention window"); len(got) != 1 {
		t.Fatalf("permanent-loss reports after a later poll=%d, want still exactly 1", len(got))
	}
}
