package slack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)

func TestPaginationRestartDeduplicatesAndCheckpoints(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Query().Get("cursor") {
		case "":
			fmt.Fprint(w, `{"ok":true,"entries":[{"id":"one","date_create":1700000000},{"id":"one","date_create":1700000000}],"response_metadata":{"next_cursor":"next"}}`)
		case "next":
			fmt.Fprint(w, `{"ok":true,"entries":[{"id":"one","date_create":1700000000},{"id":"two","date_create":"1700000001"}],"response_metadata":{"next_cursor":""}}`)
		default:
			t.Fatalf("unexpected cursor %q", r.URL.Query().Get("cursor"))
		}
	}))
	defer server.Close()

	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	continuation, err := plugin.Handle(t.Context(), runtime)
	if err != nil {
		t.Fatal(err)
	}
	if continuation == nil || continuation.Delay != 0 || len(runtime.entries) != 1 {
		t.Fatalf("first continuation=%v entries=%d", continuation, len(runtime.entries))
	}
	state, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Active || state.Cursor != "next" || state.CompletedThrough != 0 {
		t.Fatalf("first state=%+v", state)
	}

	// A fresh plugin simulates a process restart and must rebuild the receipt
	// ring before it resumes the saved cursor.
	plugin = newTestPlugin(t, config, fixedNow, server.Client())
	continuation, err = plugin.Handle(t.Context(), runtime)
	if err != nil {
		t.Fatal(err)
	}
	if continuation == nil || continuation.Delay != time.Duration(config.Request_Interval)*time.Second {
		t.Fatalf("final continuation=%v", continuation)
	}
	if len(runtime.entries) != 2 || requests.Load() != 2 {
		t.Fatalf("entries=%d requests=%d", len(runtime.entries), requests.Load())
	}
	state, err = loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	wantThrough := fixedNow.Add(-pollBuffer).Unix()
	if state.Active || state.CompletedThrough != wantThrough {
		t.Fatalf("completed state=%+v want through=%d", state, wantThrough)
	}
	for _, ent := range runtime.entries {
		if len(ent.EnumeratedValues()) != 0 || ent.SRC != nil {
			t.Fatalf("unexpected entry metadata: evs=%v src=%v", ent.EnumeratedValues(), ent.SRC)
		}
		if bytes.ContainsAny(ent.Data, "\r\n") || !bytes.HasPrefix(ent.Data, []byte("{")) {
			t.Fatalf("entry is not compact one-object JSON: %q", ent.Data)
		}
	}
}

func TestLookbackReplayCollectsLateVisibleEventsWithoutDuplicatingPriorEntries(t *testing.T) {
	var requests atomic.Int32
	var oldest []string
	eventTime := fixedNow.Add(-2 * pollBuffer).Unix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oldest = append(oldest, r.URL.Query().Get("oldest"))
		if requests.Add(1) == 1 {
			fmt.Fprintf(w, `{"ok":true,"entries":[{"id":"one","date_create":%d}],"response_metadata":{"next_cursor":""}}`, eventTime)
			return
		}
		fmt.Fprintf(w, `{"ok":true,"entries":[{"id":"one","date_create":%d},{"id":"late","date_create":%d}],"response_metadata":{"next_cursor":""}}`, eventTime, eventTime+1)
	}))
	defer server.Close()

	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}

	secondNow := fixedNow.Add(5 * time.Minute)
	plugin = newTestPlugin(t, config, secondNow, server.Client())
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}

	if requests.Load() != 2 || len(runtime.entries) != 2 {
		t.Fatalf("requests=%d entries=%d", requests.Load(), len(runtime.entries))
	}
	wantOldest := strconv.FormatInt(fixedNow.Add(-pollBuffer-lateVisibilityOverlap).Unix(), 10)
	if len(oldest) != 2 || oldest[1] != wantOldest {
		t.Fatalf("oldest=%v want second=%q", oldest, wantOldest)
	}
}

func TestLookbackReplayBridgesTraversalLongerThanHorizon(t *testing.T) {
	var oldest string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oldest = r.URL.Query().Get("oldest")
		fmt.Fprint(w, `{"ok":true,"entries":[],"response_metadata":{"next_cursor":""}}`)
	}))
	defer server.Close()

	config := validConfig(server.URL)
	runtime := newTestRuntime()
	completedThrough := fixedNow.Add(-pollBuffer - 8*24*time.Hour).Unix()
	for key, value := range map[string]int64{
		stateVersionKey:          stateVersion,
		stateCompletedThroughKey: completedThrough,
		stateActiveKey:           0,
		stateGenerationKey:       1,
	} {
		if err := runtime.PutInt64(key, value); err != nil {
			t.Fatal(err)
		}
	}

	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if want := strconv.FormatInt(completedThrough-int64(lateVisibilityOverlap/time.Second), 10); oldest != want {
		t.Fatalf("oldest=%q want=%q", oldest, want)
	}
}

func TestCompletedCycleDoesNotReplayBeyondReceiptCapacity(t *testing.T) {
	const totalRecords = receiptCapacity + 1
	const pages = (totalRecords + int64(pageSize) - 1) / int64(pageSize)
	newest := fixedNow.Add(-2 * pollBuffer).Unix()
	var requests atomic.Int32
	var secondOldest string
	secondNow := fixedNow.Add(5 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := requests.Add(1)
		if int64(call) <= pages {
			pageIndex := int64(call - 1)
			firstRecord := pageIndex * int64(pageSize)
			records := int64(pageSize)
			if remaining := totalRecords - firstRecord; remaining < records {
				records = remaining
			}
			var response bytes.Buffer
			response.WriteString(`{"ok":true,"entries":[`)
			for offset := int64(0); offset < records; offset++ {
				if offset > 0 {
					response.WriteByte(',')
				}
				index := firstRecord + offset
				fmt.Fprintf(&response, `{"id":"event-%d","date_create":%d}`, index, newest-index)
			}
			next := ""
			if pageIndex+1 < pages {
				next = fmt.Sprintf("page-%d", pageIndex+1)
			}
			fmt.Fprintf(&response, `],"response_metadata":{"next_cursor":%q}}`, next)
			_, _ = w.Write(response.Bytes())
			return
		}
		secondOldest = r.URL.Query().Get("oldest")
		// event-0 is the newest record from the first, newest-first traversal.
		// A single FIFO receipt ring loses it when older history exceeds capacity.
		fmt.Fprintf(w, `{"ok":true,"entries":[{"id":"event-0","date_create":%d},{"id":"new","date_create":%d}],"response_metadata":{"next_cursor":""}}`, newest, secondNow.Add(-2*pollBuffer).Unix())
	}))
	defer server.Close()

	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	for range pages {
		if _, err := plugin.Handle(t.Context(), runtime); err != nil {
			t.Fatal(err)
		}
	}
	plugin = newTestPlugin(t, config, secondNow, server.Client())
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}

	wantOldest := strconv.FormatInt(fixedNow.Add(-pollBuffer-lateVisibilityOverlap).Unix(), 10)
	if secondOldest != wantOldest {
		t.Fatalf("second oldest=%q want=%q", secondOldest, wantOldest)
	}
	if got, want := len(runtime.entries), int(receiptCapacity)+2; got != want {
		t.Fatalf("entries=%d want=%d", got, want)
	}
}

func TestAcknowledgementFailureRetriesWithoutDuplicatingPriorRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"ok":true,"entries":[{"id":"one","date_create":1700000000},{"id":"two","date_create":1700000001}],"response_metadata":{"next_cursor":""}}`)
	}))
	defer server.Close()
	config := validConfig(server.URL)
	runtime := newTestRuntime()
	runtime.ackFailAt = 2
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	if continuation, err := plugin.Handle(t.Context(), runtime); err == nil || continuation != nil {
		t.Fatalf("first continuation=%v error=%v", continuation, err)
	}
	if len(runtime.entries) != 1 {
		t.Fatalf("entries after failure=%d", len(runtime.entries))
	}
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 2 || runtime.ackCalls != 3 {
		t.Fatalf("entries=%d acknowledgement calls=%d", len(runtime.entries), runtime.ackCalls)
	}
}

func TestConfiguredEntryLimitSkipsOversizedRecordAndCompletes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"ok":true,"entries":[{"id":"too-large","date_create":1700000000,"blob":%q},{"id":"valid","date_create":1700000001}],"response_metadata":{"next_cursor":""}}`, strings.Repeat("x", 256))
	}))
	defer server.Close()

	runtime := newTestRuntime()
	runtime.ackMaxSize = 128
	runtime.ackFailAt = 2
	plugin := newTestPlugin(t, validConfig(server.URL), fixedNow, server.Client())
	if _, err := plugin.Handle(t.Context(), runtime); err == nil {
		t.Fatal("expected the valid record's first acknowledgement to fail")
	}
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	state, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if state.Active || runtime.ackCalls != 3 || len(runtime.entries) != 1 {
		t.Fatalf("state=%+v acknowledgement calls=%d entries=%d", state, runtime.ackCalls, len(runtime.entries))
	}
	if !bytes.Contains(runtime.entries[0].Data, []byte(`"id":"valid"`)) {
		t.Fatalf("unexpected delivered entry: %s", runtime.entries[0].Data)
	}
	if len(runtime.warnings) != 1 || !strings.Contains(runtime.warnings[0], "configured ingest entry size") {
		t.Fatalf("warnings=%v", runtime.warnings)
	}
}

func TestReceiptHeadFailureRecoversOrphanWithoutDuplicate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"ok":true,"entries":[{"id":"one","date_create":1700000000}],"response_metadata":{"next_cursor":""}}`)
	}))
	defer server.Close()
	config := validConfig(server.URL)
	runtime := newTestRuntime()
	runtime.failPutKey = receiptHeadKey
	runtime.failPutAt = 2 // allow the preflight head write, fail after acknowledgement
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	if _, err := plugin.Handle(t.Context(), runtime); err == nil || !strings.Contains(err.Error(), "receipt head") {
		t.Fatalf("first error=%v", err)
	}
	if len(runtime.entries) != 1 {
		t.Fatalf("entries after failed head=%d", len(runtime.entries))
	}
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 1 {
		t.Fatalf("orphan recovery duplicated entry: %d", len(runtime.entries))
	}
}

func TestPersistentStateFailureDoesNotRepeatAcknowledgedDelivery(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ok":true,"entries":[{"id":"one","date_create":1700000000}],"response_metadata":{"next_cursor":""}}`)
	}))
	defer server.Close()
	config := validConfig(server.URL)
	runtime := newTestRuntime()
	runtime.failStateAfterAck = true
	for cycle := range 3 {
		plugin := newTestPlugin(t, config, fixedNow, server.Client())
		if _, err := plugin.Handle(t.Context(), runtime); err == nil {
			t.Fatalf("cycle %d: expected state persistence failure", cycle+1)
		}
	}
	if requests.Load() != 1 || runtime.ackCalls != 1 || len(runtime.entries) != 1 {
		t.Fatalf("requests=%d acknowledgements=%d entries=%d", requests.Load(), runtime.ackCalls, len(runtime.entries))
	}
}

func TestOversizedAndMalformedRecordsDoNotStarveValidSamePageRecords(t *testing.T) {
	large := strings.Repeat("x", maximumAuditRecordSize)
	response := fmt.Sprintf(`{"ok":true,"entries":[{"id":"large","date_create":1700000000,"blob":"%s"},"not-an-object",{"id":"valid","date_create":1700000001}],"response_metadata":{"next_cursor":"next"}}`, large)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("cursor") == "" {
			fmt.Fprint(w, response)
			return
		}
		fmt.Fprint(w, strings.Replace(response, `"next"`, `""`, 1))
	}))
	defer server.Close()
	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 1 || len(runtime.warnings) != 2 {
		t.Fatalf("entries=%d warnings=%v", len(runtime.entries), runtime.warnings)
	}
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 1 || len(runtime.warnings) != 2 || requests.Load() != 2 {
		t.Fatalf("repeat entries=%d warnings=%d requests=%d", len(runtime.entries), len(runtime.warnings), requests.Load())
	}
}

func TestRetryAfterPersistsAcrossRestart(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "300")
			http.Error(w, "retry", http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"ok":true,"entries":[],"response_metadata":{"next_cursor":""}}`)
	}))
	defer server.Close()
	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	continuation, err := plugin.Handle(t.Context(), runtime)
	if err != nil || continuation.Delay != 5*time.Minute {
		t.Fatalf("first continuation=%v error=%v", continuation, err)
	}

	plugin = newTestPlugin(t, config, fixedNow.Add(time.Minute), server.Client())
	continuation, err = plugin.Handle(t.Context(), runtime)
	if err != nil || continuation.Delay != 4*time.Minute || requests.Load() != 1 {
		t.Fatalf("early continuation=%v error=%v requests=%d", continuation, err, requests.Load())
	}

	plugin = newTestPlugin(t, config, fixedNow.Add(5*time.Minute), server.Client())
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("eligible requests=%d", requests.Load())
	}
}

func TestRetryAfterLongDelayDoesNotRequestEarly(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "600")
			http.Error(w, "retry", http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"ok":true,"entries":[],"response_metadata":{"next_cursor":""}}`)
	}))
	defer server.Close()
	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	continuation, err := plugin.Handle(t.Context(), runtime)
	if err != nil || continuation.Delay != 10*time.Minute {
		t.Fatalf("first continuation=%v error=%v", continuation, err)
	}

	plugin = newTestPlugin(t, config, fixedNow.Add(5*time.Minute), server.Client())
	continuation, err = plugin.Handle(t.Context(), runtime)
	if err != nil || continuation.Delay != 5*time.Minute || requests.Load() != 1 {
		t.Fatalf("early continuation=%v error=%v requests=%d", continuation, err, requests.Load())
	}

	plugin = newTestPlugin(t, config, fixedNow.Add(10*time.Minute), server.Client())
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("eligible requests=%d", requests.Load())
	}
}

func TestRetryAfterDeadlineUsesResponseTime(t *testing.T) {
	var clock atomic.Int64
	clock.Store(fixedNow.UnixNano())
	responseTime := fixedNow.Add(2 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		clock.Store(responseTime.UnixNano())
		w.Header().Set("Retry-After", "120")
		http.Error(w, "retry", http.StatusTooManyRequests)
	}))
	defer server.Close()

	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	currentTime := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	plugin.now = currentTime
	plugin.api.now = currentTime
	continuation, err := plugin.Handle(t.Context(), runtime)
	if err != nil || continuation == nil || continuation.Delay != 2*time.Minute {
		t.Fatalf("continuation=%v error=%v", continuation, err)
	}
	state, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	want := responseTime.Add(2 * time.Minute).UnixNano()
	if state.RetryAt != want {
		t.Fatalf("retry at=%d want=%d", state.RetryAt, want)
	}
}

func TestTokenFileReloadsBetweenPages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slack.token")
	if err := os.WriteFile(path, []byte("token-one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := requests.Add(1)
		want := "Bearer token-one"
		cursor := "next"
		if call == 2 {
			want = "Bearer token-two"
			cursor = ""
		}
		if got := r.Header.Get("Authorization"); got != want {
			t.Fatalf("call %d authorization=%q want=%q", call, got, want)
		}
		fmt.Fprintf(w, `{"ok":true,"entries":[],"response_metadata":{"next_cursor":%q}}`, cursor)
	}))
	defer server.Close()
	config := validConfig(server.URL)
	config.API_Token = ""
	config.API_Token_File = path
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	runtime := newTestRuntime()
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("token-two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d", requests.Load())
	}
}

func TestCursorLoopResetsWindowWithoutDuplicateDelivery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("cursor") {
		case "":
			fmt.Fprint(w, `{"ok":true,"entries":[{"id":"one","date_create":1}],"response_metadata":{"next_cursor":"A"}}`)
		case "A":
			fmt.Fprint(w, `{"ok":true,"entries":[{"id":"two","date_create":2}],"response_metadata":{"next_cursor":"B"}}`)
		case "B":
			fmt.Fprint(w, `{"ok":true,"entries":[{"id":"three","date_create":3}],"response_metadata":{"next_cursor":"A"}}`)
		}
	}))
	defer server.Close()
	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	for range 3 {
		if _, err := plugin.Handle(t.Context(), runtime); err != nil {
			t.Fatal(err)
		}
	}
	state, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Active || state.Cursor != "" || state.CursorCycle != 2 || len(runtime.entries) != 3 {
		t.Fatalf("state=%+v entries=%d", state, len(runtime.entries))
	}
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 3 {
		t.Fatalf("reset duplicated delivery: %d", len(runtime.entries))
	}
}

func TestPersistentCursorLoopStopsSpinningAndReportsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("cursor") {
		case "":
			fmt.Fprint(w, `{"ok":true,"entries":[{"id":"one","date_create":1}],"response_metadata":{"next_cursor":"A"}}`)
		case "A":
			fmt.Fprint(w, `{"ok":true,"entries":[{"id":"two","date_create":2}],"response_metadata":{"next_cursor":"B"}}`)
		case "B":
			fmt.Fprint(w, `{"ok":true,"entries":[{"id":"three","date_create":3}],"response_metadata":{"next_cursor":"A"}}`)
		default:
			t.Fatalf("unexpected cursor %q", r.URL.Query().Get("cursor"))
		}
	}))
	defer server.Close()

	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	var loopErr error
	for range maximumCursorCycles * 3 {
		if _, err := plugin.Handle(t.Context(), runtime); err != nil {
			loopErr = err
			break
		}
	}
	if loopErr == nil || !strings.Contains(loopErr.Error(), "cursor loop persisted") {
		t.Fatalf("loop error=%v", loopErr)
	}
	state, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if state.CursorCycle != maximumCursorCycles {
		t.Fatalf("cursor cycle=%d want=%d", state.CursorCycle, maximumCursorCycles)
	}
	if len(runtime.entries) != 3 {
		t.Fatalf("persistent loop duplicated entries: %d", len(runtime.entries))
	}
}

func TestCrashBetweenCursorMarkerAndCursorDoesNotFalselyDetectALoop(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")
		requested = append(requested, cursor)
		switch cursor {
		case "":
			fmt.Fprint(w, `{"ok":true,"entries":[{"id":"one","date_create":1}],"response_metadata":{"next_cursor":"A"}}`)
		case "A":
			fmt.Fprint(w, `{"ok":true,"entries":[{"id":"two","date_create":2}],"response_metadata":{"next_cursor":""}}`)
		default:
			t.Fatalf("unexpected cursor %q", cursor)
		}
	}))
	defer server.Close()

	config := validConfig(server.URL)
	runtime := newTestRuntime()
	runtime.failPutKey = stateCursorKey
	runtime.failPutAt = 2 // begin succeeds; the first pagination cursor write fails
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	if _, err := plugin.Handle(t.Context(), runtime); err == nil || !strings.Contains(err.Error(), "persist Slack cursor") {
		t.Fatalf("cursor persistence error=%v", err)
	}

	runtime.failPutKey = ""
	runtime.failPutAt = 0
	plugin = newTestPlugin(t, config, fixedNow, server.Client())
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	state, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if state.CursorCycle != 1 || state.Cursor != "A" || len(runtime.warnings) != 0 {
		t.Fatalf("state=%+v warnings=%v", state, runtime.warnings)
	}

	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(requested, ","), ",,A"; got != want {
		t.Fatalf("requested cursors=%q want=%q", got, want)
	}
	if got := len(runtime.entries); got != 2 {
		t.Fatalf("entries=%d want=2", got)
	}
}

func TestCursorMarkerCapacityStopsBeforeFurtherVendorRequests(t *testing.T) {
	runtime := newTestRuntime()
	state := newPollState()
	if err := beginPollState(runtime, &state, 10, 20); err != nil {
		t.Fatal(err)
	}
	ledger, err := loadCursorLedger(runtime, state)
	if err != nil {
		t.Fatal(err)
	}
	for page := int64(0); page < cursorMarkerCapacity; page++ {
		if err := ledger.remember(runtime, &state, fmt.Sprintf("cursor-%d", page)); err != nil {
			t.Fatalf("remember page %d: %v", page, err)
		}
	}
	if err := persistCursorState(runtime, &state, "current"); err != nil {
		t.Fatal(err)
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := requests.Add(1)
		next := "A"
		if call%2 == 0 {
			next = "B"
		}
		fmt.Fprintf(w, `{"ok":true,"entries":[],"response_metadata":{"next_cursor":%q}}`, next)
	}))
	defer server.Close()

	plugin := newTestPlugin(t, validConfig(server.URL), fixedNow, server.Client())
	for attempt := range 2 {
		if _, err := plugin.Handle(t.Context(), runtime); err == nil || !strings.Contains(err.Error(), "cursor marker capacity") {
			t.Fatalf("attempt %d capacity error=%v", attempt+1, err)
		}
	}
	state, err = loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Active || state.Cursor != "current" || state.CursorPageCount != cursorMarkerCapacity || state.CursorCycle != 1 {
		t.Fatalf("state after capacity error=%+v", state)
	}
	if requests.Load() != 0 || len(runtime.entries) != 0 {
		t.Fatalf("vendor requests=%d entries=%d", requests.Load(), len(runtime.entries))
	}
}

func TestLongCursorLoopCannotEvictItsOwnMarkers(t *testing.T) {
	const cursorPages = 150
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(requests.Add(1)) - 1
		cursor := r.URL.Query().Get("cursor")
		if call == 0 {
			if cursor != "" {
				t.Fatalf("initial cursor=%q", cursor)
			}
		} else if want := fmt.Sprintf("cursor-%d", call-1); cursor != want {
			t.Fatalf("call %d cursor=%q want=%q", call, cursor, want)
		}
		var response bytes.Buffer
		response.WriteString(`{"ok":true,"entries":[`)
		for offset := range pageSize {
			if offset > 0 {
				response.WriteByte(',')
			}
			fmt.Fprintf(&response, `{"id":"page-%d-event-%d","date_create":%d}`, call, offset, fixedNow.Add(-pollBuffer).Unix()-int64(call))
		}
		next := "cursor-0"
		if call < cursorPages {
			next = fmt.Sprintf("cursor-%d", call)
		}
		fmt.Fprintf(&response, `],"response_metadata":{"next_cursor":%q}}`, next)
		_, _ = w.Write(response.Bytes())
	}))
	defer server.Close()

	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	for range cursorPages + 1 {
		if _, err := plugin.Handle(t.Context(), runtime); err != nil {
			t.Fatal(err)
		}
	}
	state, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != cursorPages+1 || state.CursorCycle != 2 || state.Cursor != "" || state.CursorPageCount != 0 {
		t.Fatalf("requests=%d state=%+v", requests.Load(), state)
	}
	if got, want := len(runtime.entries), (cursorPages+1)*pageSize; got != want {
		t.Fatalf("entries=%d want=%d", got, want)
	}
}

func TestHistoricalReceiptOverflowAllowsDocumentedResetReplay(t *testing.T) {
	const pages = 101
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(requests.Add(1)) - 1
		if call == pages {
			if cursor := r.URL.Query().Get("cursor"); cursor != "" {
				t.Fatalf("reset cursor=%q want empty", cursor)
			}
			var response bytes.Buffer
			response.WriteString(`{"ok":true,"entries":[`)
			for offset := range pageSize {
				if offset > 0 {
					response.WriteByte(',')
				}
				fmt.Fprintf(&response, `{"id":"historical-%d","date_create":%d}`, offset, fixedNow.Add(-time.Hour).Unix()-int64(offset))
			}
			response.WriteString(`],"response_metadata":{"next_cursor":""}}`)
			_, _ = w.Write(response.Bytes())
			return
		}

		cursor := r.URL.Query().Get("cursor")
		if call == 0 {
			if cursor != "" {
				t.Fatalf("initial cursor=%q", cursor)
			}
		} else if want := fmt.Sprintf("history-page-%d", call); cursor != want {
			t.Fatalf("call %d cursor=%q want=%q", call, cursor, want)
		}
		var response bytes.Buffer
		response.WriteString(`{"ok":true,"entries":[`)
		firstRecord := call * pageSize
		for offset := range pageSize {
			if offset > 0 {
				response.WriteByte(',')
			}
			index := firstRecord + offset
			fmt.Fprintf(&response, `{"id":"historical-%d","date_create":%d}`, index, fixedNow.Add(-time.Hour).Unix()-int64(index))
		}
		next := "history-page-1"
		if call < pages-1 {
			next = fmt.Sprintf("history-page-%d", call+1)
		}
		fmt.Fprintf(&response, `],"response_metadata":{"next_cursor":%q}}`, next)
		_, _ = w.Write(response.Bytes())
	}))
	defer server.Close()

	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	for range pages + 1 {
		if _, err := plugin.Handle(t.Context(), runtime); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := len(runtime.entries), pages*pageSize+pageSize; got != want {
		t.Fatalf("documented at-least-once replay entries=%d want=%d", got, want)
	}
	state, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if state.Active || state.CursorCycle != 0 {
		t.Fatalf("state after documented reset replay=%+v", state)
	}
}

func TestInvalidCursorRestartsWindowWithoutDuplicateDelivery(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("cursor") {
		case "":
			next := "expired"
			if requests.Add(1) == 3 {
				next = ""
			}
			fmt.Fprintf(w, `{"ok":true,"entries":[{"id":"one","date_create":1}],"response_metadata":{"next_cursor":%q}}`, next)
		case "expired":
			requests.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"ok":false,"error":"invalid_cursor"}`)
		default:
			t.Fatalf("unexpected cursor %q", r.URL.Query().Get("cursor"))
		}
	}))
	defer server.Close()
	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	for range 3 {
		if _, err := plugin.Handle(t.Context(), runtime); err != nil {
			t.Fatal(err)
		}
	}
	state, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 || len(runtime.entries) != 1 || state.Active {
		t.Fatalf("requests=%d entries=%d state=%+v", requests.Load(), len(runtime.entries), state)
	}
}

func TestPersistentInvalidCursorStopsSpinningAndReportsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("cursor") {
		case "":
			fmt.Fprint(w, `{"ok":true,"entries":[{"id":"one","date_create":1}],"response_metadata":{"next_cursor":"expired"}}`)
		case "expired":
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"ok":false,"error":"invalid_cursor"}`)
		default:
			t.Fatalf("unexpected cursor %q", r.URL.Query().Get("cursor"))
		}
	}))
	defer server.Close()

	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	var cursorErr error
	for range maximumCursorCycles * 2 {
		if _, err := plugin.Handle(t.Context(), runtime); err != nil {
			cursorErr = err
			break
		}
	}
	if cursorErr == nil || !strings.Contains(cursorErr.Error(), "invalid pagination cursor persisted") {
		t.Fatalf("cursor error=%v", cursorErr)
	}
	state, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if state.CursorCycle != maximumCursorCycles || state.Cursor != "expired" {
		t.Fatalf("state=%+v", state)
	}
	if len(runtime.entries) != 1 {
		t.Fatalf("persistent invalid cursor duplicated entries: %d", len(runtime.entries))
	}
}

func TestRecordProgressUsesBoundedWritesNotGrowingCoreState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"ok":true,"entries":[{"id":"1","date_create":1},{"id":"2","date_create":2},{"id":"3","date_create":3},{"id":"4","date_create":4}],"response_metadata":{"next_cursor":""}}`)
	}))
	defer server.Close()
	config := validConfig(server.URL)
	runtime := newTestRuntime()
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if got := runtime.putCalls[stateActiveKey]; got != 3 {
		t.Fatalf("active-state writes=%d want=3 (window begin, writability barrier, and complete)", got)
	}
	if got := runtime.putCalls[stateCompletedThroughKey]; got != 1 {
		t.Fatalf("checkpoint writes=%d want=1", got)
	}
	for _, key := range []string{stateVersionKey, stateGenerationKey, stateOldestKey, stateLatestKey, stateCursorKey, stateCursorCycleKey, stateRetryAtKey} {
		if got := runtime.putCalls[key]; got != 1 {
			t.Fatalf("core state writes for %s=%d want=1", key, got)
		}
	}
	if got := runtime.putCalls[stateCursorPageCountKey]; got != 2 {
		t.Fatalf("cursor marker count writes=%d want=2 (window begin and writability barrier)", got)
	}
	if got := runtime.putCalls[receiptHeadKey]; got != 5 {
		t.Fatalf("bounded receipt head writes=%d want=5 (one barrier and four receipts)", got)
	}
	if got := runtime.putCalls[overlapReceiptHeadKey]; got != 1 {
		t.Fatalf("bounded overlap receipt head writes=%d want=1 (writability barrier)", got)
	}
	for key, sizes := range runtime.putSizes {
		if (strings.HasPrefix(key, receiptSlotPrefix+"/") && key != receiptHeadKey) ||
			(strings.HasPrefix(key, overlapReceiptSlotPrefix+"/") && key != overlapReceiptHeadKey) {
			for _, size := range sizes {
				if size > 160 {
					t.Fatalf("receipt %s grew to %d bytes", key, size)
				}
			}
		}
	}
}

func TestNormalizeRecordRejectsMissingOrInvalidTimestamp(t *testing.T) {
	for _, raw := range []string{`[]`, `{}`, `{"date_create":-1}`, `{"date_create":"not-time"}`} {
		if _, _, _, err := normalizeRecord([]byte(raw)); err == nil {
			t.Fatalf("record %s accepted", raw)
		}
	}
	compact, identity, timestamp, err := normalizeRecord([]byte(" { \"id\": \"event\", \"date_create\": 1700000000 } "))
	if err != nil {
		t.Fatal(err)
	}
	if string(compact) != `{"id":"event","date_create":1700000000}` || identity != "id:event" || timestamp.Unix() != 1700000000 {
		t.Fatalf("compact=%s identity=%q timestamp=%v", compact, identity, timestamp)
	}
}

func TestCancellationStopsBeforeRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	config := validConfig(server.URL)
	plugin := newTestPlugin(t, config, fixedNow, server.Client())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := plugin.Handle(ctx, newTestRuntime()); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("requests=%d", requests.Load())
	}
}
