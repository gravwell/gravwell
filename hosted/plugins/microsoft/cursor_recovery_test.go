package microsoft

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

const cursorRecoveryWatermark = "2026-10-07T06:00:00Z"

func newSelectorTestPlugin(t *testing.T, server *httptest.Server, selector string) *Microsoft {
	t.Helper()
	conf := validTestConfig(t, selector)
	conf.Graph_Host, conf.ARM_Host, conf.Defender_Host = server.URL, server.URL, server.URL
	conf.Defender_XDR_Host, conf.PowerBI_Host, conf.Auth_Host = server.URL, server.URL, server.URL
	plugin := New(conf)
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	plugin.client = client
	plugin.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
	return plugin
}

// seedStoredCursor persists a cursor document and a durable watermark exactly
// the way a prior poll would have left them.
func seedStoredCursor(t *testing.T, rt *microsoftTestRuntime, key string, continuation *FetchContinuation) time.Time {
	t.Helper()
	watermark, err := time.Parse(time.RFC3339, cursorRecoveryWatermark)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(cursorState{
		WindowStart: watermark, WindowEnd: watermark.Add(time.Hour),
		Continuation: continuation, Fetched: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(key+"/cursor", encoded); err != nil {
		t.Fatal(err)
	}
	if err := rt.PutTime(key+"/timestamp", watermark); err != nil {
		t.Fatal(err)
	}
	return watermark
}

func assertCursorWasReset(t *testing.T, rt *microsoftTestRuntime, key string, watermark time.Time) {
	t.Helper()
	if got := rt.logsContaining("WARN", "stored continuation was reset"); len(got) != 1 {
		t.Fatalf("reset warnings=%d, want exactly 1 (%+v)", len(got), got)
	}
	state, err := loadState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if state.Continuation != nil || state.Fetched != 0 || !state.WindowStart.IsZero() || !state.WindowEnd.IsZero() {
		t.Fatalf("cursor was not cleared: %+v", state)
	}
	if !state.Watermark.Equal(watermark) {
		t.Fatalf("durable watermark=%s, want %s", state.Watermark, watermark)
	}
}

// TestIncompatibleStoredContinuationResetsInsteadOfWedging pins the recovery
// path for a cursor whose Kind no longer matches its selector. Before the
// repair this returned an error on every poll forever, because nothing cleared
// the cursor document.
func TestIncompatibleStoredContinuationResetsInsteadOfWedging(t *testing.T) {
	var dataCalls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
			return
		}
		dataCalls++
		_, _ = w.Write([]byte(`{"value":[{"id":"user-1","displayName":"One"}]}`))
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newSelectorTestPlugin(t, server, "entra-users")
	key := stateKey("microsoft", "entra-users", "")
	watermark := seedStoredCursor(t, rt, key, &FetchContinuation{
		Version: fetchContinuationVersion, Kind: KindResourceGraph,
		SkipToken: "token-from-a-previous-catalog",
	})

	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("incompatible cursor was reported as an operational error: %v", err)
	}
	assertCursorWasReset(t, rt, key, watermark)
	if dataCalls != 0 || len(rt.Entries()) != 0 {
		t.Fatalf("reset pass issued %d requests and wrote %d entries", dataCalls, len(rt.Entries()))
	}

	// The next scheduled poll must collect normally from the watermark.
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if dataCalls != 1 || len(rt.Entries()) != 1 {
		t.Fatalf("recovered poll requests=%d entries=%d", dataCalls, len(rt.Entries()))
	}
}

// TestMalformedParentChildCursorResetsWithoutPanicking pins the bounds check on
// a persisted parent-child cursor. Before the repair, a ChildNextURL with an
// out-of-range ParentIndex indexed ParentIDs out of range, which the runner
// recovered as a panic and then restarted against the same unchanged state.
func TestMalformedParentChildCursorResetsWithoutPanicking(t *testing.T) {
	var parentCalls, childCalls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
		case strings.HasSuffix(r.URL.Path, "/logonusers"):
			childCalls++
			_, _ = w.Write([]byte(`{"value":[{"accountName":"alice"}]}`))
		case strings.HasSuffix(r.URL.Path, "/api/machines"):
			parentCalls++
			_, _ = w.Write([]byte(`{"value":[{"id":"machine-1"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newSelectorTestPlugin(t, server, "defender-users")
	key := stateKey("microsoft", "defender-users", "")
	watermark := seedStoredCursor(t, rt, key, &FetchContinuation{
		Version: fetchContinuationVersion, Kind: KindParentChild,
		ChildNextURL: server.URL + "/api/machines/machine-1/logonusers",
		ParentIndex:  3,
	})

	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("malformed cursor was reported as an operational error: %v", err)
	}
	assertCursorWasReset(t, rt, key, watermark)
	if parentCalls != 0 || childCalls != 0 {
		t.Fatalf("reset pass issued parent=%d child=%d requests", parentCalls, childCalls)
	}

	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if parentCalls != 1 || childCalls != 1 || len(rt.Entries()) != 1 {
		t.Fatalf("recovered poll parent=%d child=%d entries=%d", parentCalls, childCalls, len(rt.Entries()))
	}
}

// TestOutOfRangeHuntingCursorResetsInsteadOfStalling pins the hunting bounds.
// Before the repair, a persisted HuntingPartitions above the supported capacity
// left the delivery loop advancing toward a target it could never reach, which
// reported Complete=false forever and pinned the job in immediate-drain mode.
func TestOutOfRangeHuntingCursorResetsInsteadOfStalling(t *testing.T) {
	var huntingCalls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
			return
		}
		huntingCalls++
		_, _ = w.Write([]byte(`{"results":[{"DeviceId":"device-1","SoftwareVendor":"vendor","SoftwareName":"app","SoftwareVersion":"1","CveId":"CVE-DEMO-1"}]}`))
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newSelectorTestPlugin(t, server, "defender-xdr-vulnerability")
	key := stateKey("microsoft", "defender-xdr-vulnerability", "")
	watermark := seedStoredCursor(t, rt, key, &FetchContinuation{
		Version: fetchContinuationVersion, Kind: KindAdvancedHunting,
		HuntingPartitions: maximumHuntingPartitions + 1, HuntingPartition: 5,
	})

	pending, err := plugin.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("out-of-range hunting cursor was reported as an operational error: %v", err)
	}
	if pending == nil || pending.Delay == 0 {
		t.Fatalf("reset pass stayed in immediate-drain mode: %+v", pending)
	}
	assertCursorWasReset(t, rt, key, watermark)
	if huntingCalls != 0 {
		t.Fatalf("reset pass issued %d hunting requests", huntingCalls)
	}

	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.Entries()) != 1 {
		t.Fatalf("recovered poll entries=%d hunting requests=%d", len(rt.Entries()), huntingCalls)
	}
}

// TestUnreadableStoredDocumentsResetInsteadOfWedging pins the recovery path for
// a state document that cannot be decoded at all, rather than one that decodes
// into an impossible cursor. Before the repair only the latter reached the reset
// path: corrupt bytes, an overlap document written by a newer build that this
// one was rolled back from, or an out-of-bounds receipt set were returned as a
// plain load error, and nothing in the plugin ever rewrote those keys, so every
// later poll re-read the same bytes and failed identically. The durable
// watermark must survive, because it is all that is needed to resume.
func TestUnreadableStoredDocumentsResetInsteadOfWedging(t *testing.T) {
	for _, tc := range []struct {
		name     string
		document string
		seed     string
	}{
		{name: "corrupt-cursor", document: "cursor", seed: `{"window_start":`},
		{name: "corrupt-fabric", document: "fabric", seed: `{"anchor":"not-a-timestamp"}`},
		{name: "corrupt-overlap", document: "overlap", seed: `[]`},
		{name: "rolled-back-overlap-version", document: "overlap",
			seed: `{"version":2,"receipts":[{"timestamp":"2026-10-07T05:59:00Z","fingerprint":"from-a-newer-build"}]}`},
		{name: "invalid-overlap-receipt", document: "overlap",
			seed: `{"version":1,"receipts":[{"timestamp":"0001-01-01T00:00:00Z","fingerprint":""}]}`},
		// A fingerprint that is not the canonical lowercase sha256 hex form
		// suppresses nothing -- no record this plugin fetches can produce that
		// key -- so it can only occupy the budget and the map. The count bound
		// was all that ever applied to it.
		{name: "non-canonical-overlap-fingerprint", document: "overlap",
			seed: `{"version":1,"receipts":[{"timestamp":"2026-10-07T05:59:00Z","fingerprint":"not-a-digest"}]}`},
		{name: "uppercase-overlap-fingerprint", document: "overlap",
			seed: `{"version":1,"receipts":[{"timestamp":"2026-10-07T05:59:00Z","fingerprint":"` +
				strings.Repeat("A", 64) + `"}]}`},
		{name: "overlong-overlap-fingerprint", document: "overlap",
			seed: `{"version":1,"receipts":[{"timestamp":"2026-10-07T05:59:00Z","fingerprint":"` +
				strings.Repeat("a", 65) + `"}]}`},
		// The amplification the receipt bounds cannot see: an in-budget receipt
		// set inside a document the ignored-member padding has grown past the
		// pre-decode ceiling.
		{name: "oversized-overlap-document", document: "overlap",
			seed: `{"version":1,"receipts":[{"timestamp":"2026-10-07T05:59:00Z","fingerprint":"` +
				strings.Repeat("a", 64) + `"}],"unknown_member":"` +
				strings.Repeat("a", maximumOverlapDocumentBytes) + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dataCalls int
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
					_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
					return
				}
				dataCalls++
				_, _ = w.Write([]byte(`{"value":[{"id":"audit-1","activityDateTime":"2026-10-07T11:10:00Z"}]}`))
			}))
			defer server.Close()

			rt := newMicrosoftTestRuntime(t)
			plugin := newSelectorTestPlugin(t, server, "entra-directory-audits")
			key := stateKey("microsoft", "entra-directory-audits", "")
			watermark, err := time.Parse(time.RFC3339, cursorRecoveryWatermark)
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.PutTime(key+"/timestamp", watermark); err != nil {
				t.Fatal(err)
			}
			if err := rt.Put(key+"/"+tc.document, []byte(tc.seed)); err != nil {
				t.Fatal(err)
			}

			// The watermark that did load must come back with the failure, so
			// the reset can keep it instead of restarting from the lookback.
			partial, err := loadState(rt, key)
			if !errors.Is(err, errIncompatibleContinuation) {
				t.Fatalf("load error=%v, want an incompatible-continuation error", err)
			}
			if !partial.Watermark.Equal(watermark) {
				t.Fatalf("partial watermark=%s, want the durable %s", partial.Watermark, watermark)
			}

			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatalf("unreadable %s document was reported as an operational error: %v", tc.document, err)
			}
			assertCursorWasReset(t, rt, key, watermark)
			if dataCalls != 0 || len(rt.Entries()) != 0 {
				t.Fatalf("reset pass issued %d requests and wrote %d entries", dataCalls, len(rt.Entries()))
			}
			// The unreadable key itself has to have been overwritten, or the
			// next poll reads the same bytes again.
			if stored, err := rt.Get(key + "/" + tc.document); err != nil {
				t.Fatal(err)
			} else if string(stored) == tc.seed {
				t.Fatalf("%s document was left unreadable: %s", tc.document, stored)
			}

			// The next scheduled poll collects normally from the watermark.
			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			if dataCalls != 1 || len(rt.Entries()) != 1 {
				t.Fatalf("recovered poll requests=%d entries=%d", dataCalls, len(rt.Entries()))
			}
			if got := rt.logsContaining("WARN", "stored continuation was reset"); len(got) != 1 {
				t.Fatalf("reset warnings after recovery=%d, want exactly 1", len(got))
			}
		})
	}
}

// TestUnreadableStoredDocumentIsCollectedDuringADrainPass pins that a selector
// whose documents only collect can repair is not skipped by a drain pass, which
// visits only targets that report pending work.
func TestUnreadableStoredDocumentIsCollectedDuringADrainPass(t *testing.T) {
	rt := newMicrosoftTestRuntime(t)
	plugin := New(validTestConfig(t, "entra-users"))
	key := stateKey("microsoft", "entra-users", "")
	if err := rt.Put(key+"/cursor", []byte(`{"window_start":`)); err != nil {
		t.Fatal(err)
	}
	pending, err := plugin.targetHasPending(rt, sourceTarget{dataset: datasets["entra-users"], key: key})
	if err != nil {
		t.Fatalf("drain-pass inspection error=%v, want the selector reported as work to do", err)
	}
	if !pending {
		t.Fatal("a drain pass would skip the selector forever, so nothing would ever rewrite the document")
	}
}

// TestStoredCursorStructuralValidation documents exactly which persisted cursor
// shapes are refused. The ordinary non-snapshot cursor, where both hunting
// fields are zero, must keep being accepted.
func TestStoredCursorStructuralValidation(t *testing.T) {
	dataset := datasets["defender-users"]
	for _, tc := range []struct {
		name   string
		cursor FetchContinuation
		reject bool
	}{
		{name: "ordinary-empty-cursor"},
		{name: "ordinary-next-url", cursor: FetchContinuation{NextURL: "https://example.invalid/next"}},
		{name: "parent-index-at-end", cursor: FetchContinuation{ParentIDs: []string{"a", "b"}, ParentIndex: 2}},
		{name: "parent-index-past-end", cursor: FetchContinuation{ParentIDs: []string{"a"}, ParentIndex: 2}, reject: true},
		{name: "negative-parent-index", cursor: FetchContinuation{ParentIndex: -1}, reject: true},
		{name: "child-url-without-parent", cursor: FetchContinuation{ChildNextURL: "https://example.invalid/child"}, reject: true},
		{name: "child-url-with-parent", cursor: FetchContinuation{ChildNextURL: "https://example.invalid/child", ParentIDs: []string{"a"}}},
		{name: "planned-hunting-partition", cursor: FetchContinuation{HuntingPartitions: 16, HuntingPartition: 15}},
		{name: "hunting-partition-at-count", cursor: FetchContinuation{HuntingPartitions: 16, HuntingPartition: 16}, reject: true},
		{name: "negative-hunting-partition", cursor: FetchContinuation{HuntingPartition: -1}, reject: true},
		{name: "unplanned-hunting-partition", cursor: FetchContinuation{HuntingPartition: 3}, reject: true},
		{name: "hunting-partitions-over-capacity", cursor: FetchContinuation{HuntingPartitions: maximumHuntingPartitions + 1}, reject: true},
		{name: "negative-hunting-partitions", cursor: FetchContinuation{HuntingPartitions: -1}, reject: true},
		{name: "too-many-hunting-windows", cursor: FetchContinuation{HuntingWindows: make([]FetchWindow, maximumHuntingPartitions+1)}, reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prior := tc.cursor
			prior.Version, prior.Kind = fetchContinuationVersion, dataset.Kind
			resumed, err := continuationFor(dataset, &prior)
			if tc.reject {
				if !errors.Is(err, errIncompatibleContinuation) {
					t.Fatalf("error=%v, want an incompatible-continuation error", err)
				}
				return
			}
			if err != nil || resumed == nil {
				t.Fatalf("cursor=%v error=%v", resumed, err)
			}
		})
	}
	mismatched := FetchContinuation{Version: fetchContinuationVersion, Kind: KindResourceGraph}
	if _, err := continuationFor(dataset, &mismatched); !errors.Is(err, errIncompatibleContinuation) {
		t.Fatalf("kind mismatch error=%v", err)
	}
	outdated := FetchContinuation{Version: fetchContinuationVersion + 1, Kind: dataset.Kind}
	if _, err := continuationFor(dataset, &outdated); !errors.Is(err, errIncompatibleContinuation) {
		t.Fatalf("version mismatch error=%v", err)
	}
}

// repeatedCursorPlugin builds a plugin for one selector, including the Azure
// selectors whose configuration requires a subscription.
func repeatedCursorPlugin(t *testing.T, server *httptest.Server, selector string, subscriptions []string) *Microsoft {
	t.Helper()
	if len(subscriptions) == 0 {
		return newSelectorTestPlugin(t, server, selector)
	}
	conf := &Config{
		Tenant_ID:       "11111111-1111-1111-1111-111111111111",
		Client_ID:       "22222222-2222-2222-2222-222222222222",
		Client_Secret:   "synthetic-client-secret",
		Subscription_ID: subscriptions,
		Api:             []string{selector},
	}
	conf.Graph_Host, conf.ARM_Host, conf.Defender_Host = server.URL, server.URL, server.URL
	conf.Defender_XDR_Host, conf.PowerBI_Host, conf.Auth_Host = server.URL, server.URL, server.URL
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	plugin := New(conf)
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	plugin.client = client
	plugin.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
	return plugin
}

// TestPersistedRepeatedCursorResetsInsteadOfWedging covers every ordinary
// non-Management site that refuses a cursor the source has already served:
// the envelope page loop, Resource Graph skip tokens, Fabric continuation URIs,
// and both the parent and the child side of a parent-child fan-out.
//
// Each returned a plain error. collect only clears a cursor for an error that
// wraps errIncompatibleContinuation, and nothing else in the plugin rewrites
// that key, so a persisted repeat failed identically on every later poll
// forever -- for four of the five sites before a single request was even
// dispatched -- and a cycle encountered mid-pass repeated from the last durable
// cursor just as often. They are routed through the reset path now, each
// keeping its own operator-facing sentence.
func TestPersistedRepeatedCursorResetsInsteadOfWedging(t *testing.T) {
	const fabricRepeatPath = "/fabric-repeat"
	windowStart, err := time.Parse(time.RFC3339, cursorRecoveryWatermark)
	if err != nil {
		t.Fatal(err)
	}

	resourceGraphCalls := 0
	for _, tc := range []struct {
		name          string
		selector      string
		subscriptions []string
		// resetRequests is what the source may receive on the pass that reads
		// the persisted repeat. The envelope, Fabric and both parent-child
		// sites compare the cursor they are about to use, so they refuse before
		// dispatch and must cost nothing. Resource Graph publishes its skip
		// token in the response body, so detecting a repeat there costs exactly
		// one request and structurally cannot cost zero.
		resetRequests int
		reason        string
		cursor        func(base string) *FetchContinuation
		respond       func(w http.ResponseWriter, r *http.Request)
		wantEntries   int
	}{
		{
			name: "envelope", selector: "entra-users", resetRequests: 0,
			reason: "entra-users returned a repeated continuation URL",
			cursor: func(base string) *FetchContinuation {
				repeat := base + "/v1.0/users?page=2"
				return &FetchContinuation{
					Version: fetchContinuationVersion, Kind: KindODataGET,
					NextURL: repeat, Recent: []string{continuationFingerprint(repeat)},
				}
			},
			respond: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"value":[{"id":"user-1"}]}`)
			},
			wantEntries: 1,
		},
		{
			name: "resource-graph", selector: "azure-resource-inventory",
			subscriptions: []string{"33333333-3333-3333-3333-333333333333"},
			resetRequests: 1,
			reason:        "Resource Graph returned a repeated skip token",
			cursor: func(string) *FetchContinuation {
				return &FetchContinuation{
					Version: fetchContinuationVersion, Kind: KindResourceGraph,
					SkipToken: "resume", Recent: []string{continuationFingerprint("repeat")},
				}
			},
			respond: func(w http.ResponseWriter, _ *http.Request) {
				resourceGraphCalls++
				if resourceGraphCalls == 1 {
					fmt.Fprint(w, `{"data":[{"id":"resource-1"}],"skipToken":"repeat","resultTruncated":true}`)
					return
				}
				fmt.Fprint(w, `{"data":[{"id":"resource-1"}],"resultTruncated":false}`)
			},
			wantEntries: 1,
		},
		{
			name: "fabric", selector: "fabric-activity", resetRequests: 0,
			reason: "Fabric returned a repeated continuation URI",
			cursor: func(base string) *FetchContinuation {
				repeat := base + fabricRepeatPath
				return &FetchContinuation{
					Version: fetchContinuationVersion, Kind: KindFabricActivity,
					NextURL: repeat, Recent: []string{continuationFingerprint(repeat)},
					FabricCursor: windowStart, FabricDayEnd: windowStart.Add(time.Hour),
				}
			},
			respond: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"activityEventEntities":[{"Id":"event-1","CreationTime":"2026-10-07T10:00:00Z"}]}`)
			},
			wantEntries: 1,
		},
		{
			name: "parent-child-parent", selector: "defender-users", resetRequests: 0,
			reason: "parent-child source returned a repeated parent continuation URI",
			cursor: func(base string) *FetchContinuation {
				repeat := base + "/api/machines?page=2"
				return &FetchContinuation{
					Version: fetchContinuationVersion, Kind: KindParentChild,
					ParentNextURL: repeat, ParentRecent: []string{continuationFingerprint(repeat)},
				}
			},
			respond:     parentChildRecoveryResponse,
			wantEntries: 1,
		},
		{
			name: "parent-child-child", selector: "defender-users", resetRequests: 0,
			reason: "parent-child source returned a repeated child continuation URI",
			cursor: func(base string) *FetchContinuation {
				repeat := base + "/api/machines/machine-1/logonusers?page=2"
				return &FetchContinuation{
					Version: fetchContinuationVersion, Kind: KindParentChild,
					ParentIDs: []string{"machine-1"}, ChildParentID: "machine-1",
					ChildNextURL: repeat, ChildRecent: []string{continuationFingerprint(repeat)},
				}
			},
			respond:     parentChildRecoveryResponse,
			wantEntries: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
					_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
					return
				}
				calls.Add(1)
				tc.respond(w, r)
			}))
			defer server.Close()

			rt := newMicrosoftTestRuntime(t)
			plugin := repeatedCursorPlugin(t, server, tc.selector, tc.subscriptions)
			key := stateKey("microsoft", tc.selector, "")
			watermark := seedStoredCursor(t, rt, key, tc.cursor(server.URL))
			seeded, err := rt.Get(key + "/cursor")
			if err != nil {
				t.Fatal(err)
			}

			// The pass that reads the persisted repeat must recover rather than
			// report an operational error the runner would retry forever.
			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatalf("persisted repeated cursor was reported as an operational error: %v", err)
			}
			if got := calls.Load(); got != int64(tc.resetRequests) {
				t.Fatalf("reset pass issued %d source requests, want %d", got, tc.resetRequests)
			}
			if got := len(rt.Entries()); got != 0 {
				t.Fatalf("reset pass wrote %d entries", got)
			}
			assertCursorWasReset(t, rt, key, watermark)
			// The bad cursor document itself has to have been overwritten, or
			// the next poll reads the same repeated cursor again.
			after, err := rt.Get(key + "/cursor")
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(after, seeded) {
				t.Fatalf("repeated cursor was left on disk: %s", after)
			}
			// The reset has to say which cursor repeated; wrapping must not
			// flatten every site into one message.
			reset := rt.logsContaining("WARN", "stored continuation was reset")
			if !strings.Contains(reset[0].Params["reason"], tc.reason) {
				t.Fatalf("reset reason=%q, want it to carry %q", reset[0].Params["reason"], tc.reason)
			}

			// The next scheduled poll collects normally from the retained
			// watermark, which is the whole point of resetting rather than
			// failing.
			calls.Store(0)
			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			if got := calls.Load(); got == 0 {
				t.Fatal("recovered poll issued no source request")
			}
			if got := len(rt.Entries()); got != tc.wantEntries {
				t.Fatalf("recovered poll entries=%d, want %d", got, tc.wantEntries)
			}
			if got := rt.logsContaining("WARN", "stored continuation was reset"); len(got) != 1 {
				t.Fatalf("reset warnings after recovery=%d, want exactly 1", len(got))
			}
			if got := rt.logsContaining("ERROR", "selector poll failed"); len(got) != 0 {
				t.Fatalf("a recovered repeat reported %d poll failures: %+v", len(got), got)
			}
		})
	}
}

// parentChildRecoveryResponse answers one device with one logon user, which is
// what a defender-users poll reads once its cursor has been reset.
func parentChildRecoveryResponse(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/logonusers"):
		_, _ = w.Write([]byte(`{"value":[{"accountName":"alice"}]}`))
	case strings.HasSuffix(r.URL.Path, "/api/machines"):
		_, _ = w.Write([]byte(`{"value":[{"id":"machine-1"}]}`))
	default:
		http.NotFound(w, r)
	}
}

// fabricActivityPage answers one Fabric activity event with no continuation
// URI, which is what a terminal day page looks like. The event sits just after
// the recovery watermark, so it is inside both the pending window a resumed
// cursor carries and the fresh window a reset one collects.
func fabricActivityPage(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte(`{"activityEventEntities":[{"Id":"event-1","CreationTime":"2026-10-07T06:30:00Z"}]}`))
}

// TestCorruptFabricContinuationResetsInsteadOfWedging pins the whole structural
// invariant a pending Fabric cursor is written under: a page in flight names
// both the page and the exclusive end of the UTC day it belongs to, in that
// order, and a cursor between days names neither.
//
// Only pieces of that were checked. A next URL with a zero day end passed
// validation, and completing that page advanced the day cursor to the zero
// time: the window restarted at year 1, asked Fabric for a day it has never
// held, and failed with a plain vendor error -- not errIncompatibleContinuation,
// so the cursor was never cleared, and since nothing else in the plugin rewrites
// that key every later poll failed identically. The selector wedged on a
// corrupt document rather than resetting to its durable watermark.
func TestCorruptFabricContinuationResetsInsteadOfWedging(t *testing.T) {
	windowStart, err := time.Parse(time.RFC3339, cursorRecoveryWatermark)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		cursor func(base string) *FetchContinuation
	}{
		{
			// The reported shape: a page in flight whose day end was lost, which
			// is what advances the day cursor to the zero time once it completes.
			name: "pending-page-without-day-end",
			cursor: func(base string) *FetchContinuation {
				return &FetchContinuation{NextURL: base + "/fabric-pending", FabricCursor: windowStart}
			},
		},
		{
			name: "pending-page-without-day-cursor",
			cursor: func(base string) *FetchContinuation {
				return &FetchContinuation{NextURL: base + "/fabric-pending", FabricDayEnd: windowStart.Add(time.Hour)}
			},
		},
		{
			// A day end that does not follow its cursor would walk the window
			// backwards, or stand still, instead of advancing it.
			name: "day-end-before-day-cursor",
			cursor: func(base string) *FetchContinuation {
				return &FetchContinuation{NextURL: base + "/fabric-pending", FabricCursor: windowStart, FabricDayEnd: windowStart.Add(-time.Hour)}
			},
		},
		{
			name: "day-end-equal-to-day-cursor",
			cursor: func(base string) *FetchContinuation {
				return &FetchContinuation{NextURL: base + "/fabric-pending", FabricCursor: windowStart, FabricDayEnd: windowStart}
			},
		},
		{
			// No page is in flight, so there is no open day either: a day end
			// here is state no growth path could have written.
			name: "day-end-without-pending-page",
			cursor: func(string) *FetchContinuation {
				return &FetchContinuation{FabricCursor: windowStart, FabricDayEnd: windowStart.Add(time.Hour)}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dataCalls atomic.Int64
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
					_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
					return
				}
				dataCalls.Add(1)
				fabricActivityPage(w, r)
			}))
			defer server.Close()

			rt := newMicrosoftTestRuntime(t)
			plugin := newSelectorTestPlugin(t, server, "fabric-activity")
			key := stateKey("microsoft", "fabric-activity", "")
			corrupt := tc.cursor(server.URL)
			corrupt.Version, corrupt.Kind = fetchContinuationVersion, KindFabricActivity

			// The refusal has to carry the sentinel collect routes on, and it has
			// to happen in continuationFor, before any request is dispatched.
			if _, err := continuationFor(datasets["fabric-activity"], corrupt); !errors.Is(err, errIncompatibleContinuation) {
				t.Fatalf("corrupt Fabric cursor error=%v, want an incompatible-continuation error", err)
			}
			watermark := seedStoredCursor(t, rt, key, corrupt)
			seeded, err := rt.Get(key + "/cursor")
			if err != nil {
				t.Fatal(err)
			}

			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatalf("corrupt Fabric cursor was reported as an operational error: %v", err)
			}
			if got := dataCalls.Load(); got != 0 {
				t.Fatalf("reset pass issued %d source requests, want none", got)
			}
			if got := len(rt.Entries()); got != 0 {
				t.Fatalf("reset pass wrote %d entries", got)
			}
			// The durable watermark is all the next poll needs, so the reset must
			// keep it and clear everything else.
			assertCursorWasReset(t, rt, key, watermark)
			after, err := rt.Get(key + "/cursor")
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(after, seeded) {
				t.Fatalf("corrupt Fabric cursor was left on disk: %s", after)
			}

			// The next scheduled poll collects normally from that watermark,
			// which is the whole point of resetting rather than failing.
			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			if got := dataCalls.Load(); got == 0 {
				t.Fatal("recovered poll issued no source request")
			}
			if got := len(rt.Entries()); got != 1 {
				t.Fatalf("recovered poll entries=%d, want 1", got)
			}
			if got := rt.logsContaining("WARN", "stored continuation was reset"); len(got) != 1 {
				t.Fatalf("reset warnings after recovery=%d, want exactly 1 (%+v)", len(got), got)
			}
			if got := rt.logsContaining("ERROR", "selector poll failed"); len(got) != 0 {
				t.Fatalf("a recovered Fabric cursor reported %d poll failures: %+v", len(got), got)
			}
			recovered, err := rt.GetTime(key + "/timestamp")
			if err != nil || !recovered.After(watermark) {
				t.Fatalf("recovered watermark=%s error=%v, want it past the retained %s", recovered, err, watermark)
			}
		})
	}
}

// Valid Fabric state is not collateral damage of the invariant above: a pending
// page that names its day end resumes at that page, and a document written
// before fabric_day_end existed -- which therefore names no pending page at all
// -- still decodes, validates and resumes as the between-days cursor it is.
func TestValidAndLegacyFabricContinuationsStillResume(t *testing.T) {
	windowStart, err := time.Parse(time.RFC3339, cursorRecoveryWatermark)
	if err != nil {
		t.Fatal(err)
	}

	legacy := FetchContinuation{}
	document := fmt.Sprintf(`{"version":%d,"kind":%q,"fabric_cursor":%q}`,
		fetchContinuationVersion, string(KindFabricActivity), windowStart.Format(time.RFC3339Nano))
	if err := json.Unmarshal([]byte(document), &legacy); err != nil {
		t.Fatal(err)
	}
	if !legacy.FabricDayEnd.IsZero() || legacy.NextURL != "" {
		t.Fatalf("legacy document decoded as %+v, want no pending page", legacy)
	}
	if err := legacy.validate(); err != nil {
		t.Fatalf("legacy Fabric document rejected: %v", err)
	}
	resumed, err := continuationFor(datasets["fabric-activity"], &legacy)
	if err != nil || !resumed.FabricCursor.Equal(windowStart) {
		t.Fatalf("resumed legacy cursor=%+v error=%v", resumed, err)
	}

	for _, tc := range []struct {
		name   string
		cursor func(base string) *FetchContinuation
		// wantPending selects which request the resumed poll must issue: the
		// pending page itself, or a freshly built day window for a cursor that
		// sits between days.
		wantPending bool
	}{
		{
			name: "pending-page",
			cursor: func(base string) *FetchContinuation {
				return &FetchContinuation{
					NextURL: base + "/fabric-pending", FabricCursor: windowStart,
					FabricDayEnd: windowStart.Add(time.Hour),
				}
			},
			wantPending: true,
		},
		{
			name: "legacy-between-days",
			cursor: func(string) *FetchContinuation {
				return &FetchContinuation{FabricCursor: windowStart}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pendingCalls, freshCalls atomic.Int64
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
					_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
					return
				}
				if r.URL.Path == "/fabric-pending" {
					pendingCalls.Add(1)
				} else {
					freshCalls.Add(1)
				}
				fabricActivityPage(w, r)
			}))
			defer server.Close()

			rt := newMicrosoftTestRuntime(t)
			plugin := newSelectorTestPlugin(t, server, "fabric-activity")
			key := stateKey("microsoft", "fabric-activity", "")
			cursor := tc.cursor(server.URL)
			cursor.Version, cursor.Kind = fetchContinuationVersion, KindFabricActivity
			watermark := seedStoredCursor(t, rt, key, cursor)

			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			if got := rt.logsContaining("WARN", "stored continuation was reset"); len(got) != 0 {
				t.Fatalf("a resumable Fabric cursor was reset: %+v", got)
			}
			if tc.wantPending {
				if got := pendingCalls.Load(); got != 1 {
					t.Fatalf("pending Fabric page requested %d times, want exactly 1", got)
				}
			} else if got := freshCalls.Load(); got == 0 {
				t.Fatal("a between-days Fabric cursor issued no day request")
			}
			if got := len(rt.Entries()); got == 0 {
				t.Fatal("resumed Fabric poll delivered nothing")
			}
			state, err := loadState(rt, key)
			if err != nil {
				t.Fatal(err)
			}
			if state.Continuation != nil || !state.Watermark.After(watermark) {
				t.Fatalf("resumed state=%+v, want the window completed past %s", state, watermark)
			}
		})
	}
}

// A stored cursor the source has not already served stays resumable: the reset
// path is reached by a repeat, not by every persisted continuation. The origin
// pinning that bounds where a continuation may point is unaffected by the
// routing change.
func TestValidStoredContinuationStillResumesAndStaysOriginPinned(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
			return
		}
		calls.Add(1)
		_, _ = w.Write([]byte(`{"value":[{"id":"user-resumed"}]}`))
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newSelectorTestPlugin(t, server, "entra-users")
	key := stateKey("microsoft", "entra-users", "")
	resumable := server.URL + "/v1.0/users?page=2"
	seedStoredCursor(t, rt, key, &FetchContinuation{
		Version: fetchContinuationVersion, Kind: KindODataGET, NextURL: resumable,
		// A history that does not contain the cursor about to be used.
		Recent: []string{continuationFingerprint(server.URL + "/v1.0/users?page=1")},
	})
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("resumed poll issued %d source requests, want 1", got)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("resumed poll entries=%d, want 1", got)
	}
	if got := rt.logsContaining("WARN", "stored continuation was reset"); len(got) != 0 {
		t.Fatalf("a resumable cursor was reset: %+v", got)
	}

	// Origin pinning still refuses a continuation that leaves the request
	// origin, and it is not routed into the reset path.
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.resolveContinuation(resumable, "https://evil.invalid/v1.0/users"); err == nil ||
		!strings.Contains(err.Error(), "retain the request origin") {
		t.Fatalf("cross-origin continuation error=%v", err)
	} else if errors.Is(err, errIncompatibleContinuation) {
		t.Fatal("a cross-origin continuation must not clear the stored cursor")
	}
}

// TestStoredContinuationResetPublishesAnOperatorVisibleWarning pins the
// reporting side of that recovery.
//
// Clearing a cursor this build cannot resume is the safe answer, but it is not
// a free one: the window it discards is collected again from the retained
// watermark, and a source that keeps handing back a cursor this build refuses
// repeats that reset on every poll, indefinitely, while appearing healthy. The
// poll itself returns no error -- that is the whole point of the recovery --
// and hosted/job.go calls ClearError() after every poll that returns none, so a
// log line was the only trace and no operator could see it. The reset therefore
// has to publish on the status channel the poll loop leaves alone, naming the
// selector, the watermark it retained, and why it reset.
//
// The reset itself must be unchanged: nothing delivered, nothing requested, the
// cursor cleared, and the durable watermark intact.
func TestStoredContinuationResetPublishesAnOperatorVisibleWarning(t *testing.T) {
	var dataCalls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
			return
		}
		dataCalls++
		_, _ = w.Write([]byte(`{"value":[{"id":"user-1","displayName":"One"}]}`))
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	plugin := newSelectorTestPlugin(t, server, "entra-users")
	key := stateKey("microsoft", "entra-users", "")
	watermark := seedStoredCursor(t, rt, key, &FetchContinuation{
		Version: fetchContinuationVersion, Kind: KindResourceGraph,
		SkipToken: "token-from-a-previous-catalog",
	})

	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("incompatible cursor was reported as an operational error: %v", err)
	}
	published := rt.warnStateContaining("reset a stored continuation")
	if len(published) != 1 {
		t.Fatalf("published status warnings=%d, want exactly 1 on the channel the poll loop leaves alone (%+v)",
			len(published), published)
	}
	for _, want := range []string{
		"entra-users",
		watermark.UTC().Format(time.RFC3339),
		errIncompatibleContinuation.Error(),
	} {
		if !strings.Contains(published[0], want) {
			t.Fatalf("status warning=%q, want it to name %q", published[0], want)
		}
	}

	// The safe reset behaviour is unchanged: no payload, no vendor request, the
	// cursor cleared and the durable watermark retained.
	assertCursorWasReset(t, rt, key, watermark)
	if dataCalls != 0 || len(rt.Entries()) != 0 {
		t.Fatalf("reset pass issued %d requests and wrote %d entries, want none of either", dataCalls, len(rt.Entries()))
	}

	// And the selector still recovers: the next poll collects from that
	// watermark exactly once, publishing no further reset.
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if dataCalls != 1 || len(rt.Entries()) != 1 {
		t.Fatalf("recovered poll requests=%d entries=%d, want 1 and 1", dataCalls, len(rt.Entries()))
	}
	if got := rt.warnStateContaining("reset a stored continuation"); len(got) != 1 {
		t.Fatalf("published status warnings after recovery=%d, want the single reset only (%+v)", len(got), got)
	}
}

// originProbe is one configured Microsoft service origin that counts, and
// separates, the token acquisitions and the data requests it receives. Both
// counters matter: a bearer token minted for one resource and sent to another
// is the disclosure, so a repair that merely failed the HTTP request after
// acquiring the token would still have handed out a credential.
type originProbe struct {
	server *httptest.Server
	tokens atomic.Int64
	data   atomic.Int64
}

func newOriginProbe(t *testing.T, respond http.HandlerFunc) *originProbe {
	t.Helper()
	probe := &originProbe{}
	probe.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			probe.tokens.Add(1)
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
			return
		}
		probe.data.Add(1)
		if respond == nil {
			http.NotFound(w, r)
			return
		}
		respond(w, r)
	}))
	t.Cleanup(probe.server.Close)
	return probe
}

func (p *originProbe) reset() {
	p.tokens.Store(0)
	p.data.Store(0)
}

// newCrossServiceTestPlugin points the configured origin of the selector's own
// service at primary and every other configured Microsoft service origin at
// secondary. That is the ordinary shape of a tenant with more than one
// Microsoft API enabled, and it is what makes the broad configured-origin
// allowlist testable: both origins pass validateURL, and only one of them is
// the resource the selector's bearer token was minted for.
//
// The token endpoint is on primary, so a token acquisition is observable
// separately from the data request it would have authorized.
func newCrossServiceTestPlugin(t *testing.T, selector string, primary, secondary *originProbe) *Microsoft {
	t.Helper()
	conf := validTestConfig(t, selector)
	service := datasets[selector].Service
	for configured, host := range map[Service]*string{
		ServiceGraph:       &conf.Graph_Host,
		ServiceARM:         &conf.ARM_Host,
		ServiceDefender:    &conf.Defender_Host,
		ServiceDefenderXDR: &conf.Defender_XDR_Host,
		ServicePowerBI:     &conf.PowerBI_Host,
	} {
		if configured == service {
			*host = primary.server.URL
			continue
		}
		*host = secondary.server.URL
	}
	conf.Auth_Host = primary.server.URL
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	if conf.Base(service) == conf.Base(ServiceManagement) {
		t.Fatal("the selector's own service shares an origin with another configured service, so the test proves nothing")
	}
	plugin := New(conf)
	client, err := NewClient(conf, primary.server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	plugin.client = client
	plugin.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
	return plugin
}

// graphUsersRecoveryResponse answers one Entra user, which is what an
// entra-users poll reads once its cursor has been reset or resumed.
func graphUsersRecoveryResponse(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte(`{"value":[{"id":"user-1"}]}`))
}

// TestRestoredContinuationCannotCrossToAnotherConfiguredService covers the
// three cursor fields a resumed poll dispatches directly.
//
// A continuation received during a pass was already pinned to the origin that
// served it, so same-origin pinning never saw these: NextURL, ParentNextURL and
// ChildNextURL come back from the persisted cursor document and were handed
// straight to the request path, which only checked the broad allowlist of every
// configured Microsoft origin. Stale state -- a cursor written before a host
// was reconfigured -- or a tampered state store therefore attached the
// selector's dataset token to a different configured Microsoft service (CWE-200).
//
// Each case proves the whole chain is refused: no token is acquired on either
// origin, no request is dispatched to either, and the cursor enters the
// existing reset path so the next poll resumes from the durable watermark.
func TestRestoredContinuationCannotCrossToAnotherConfiguredService(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selector string
		respond  http.HandlerFunc
		cursor   func(other string) *FetchContinuation
	}{
		{
			name: "next-url", selector: "entra-users", respond: graphUsersRecoveryResponse,
			cursor: func(other string) *FetchContinuation {
				return &FetchContinuation{
					Version: fetchContinuationVersion, Kind: KindODataGET,
					NextURL: other + "/v1.0/users?page=2",
				}
			},
		},
		{
			name: "parent-next-url", selector: "defender-users", respond: parentChildRecoveryResponse,
			cursor: func(other string) *FetchContinuation {
				return &FetchContinuation{
					Version: fetchContinuationVersion, Kind: KindParentChild,
					ParentNextURL: other + "/api/machines?page=2",
				}
			},
		},
		{
			name: "child-next-url", selector: "defender-users", respond: parentChildRecoveryResponse,
			cursor: func(other string) *FetchContinuation {
				return &FetchContinuation{
					Version: fetchContinuationVersion, Kind: KindParentChild,
					ParentIDs: []string{"machine-1"}, ChildParentID: "machine-1",
					ChildNextURL: other + "/api/machines/machine-1/logonusers?cursor=1",
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			primary := newOriginProbe(t, tc.respond)
			secondary := newOriginProbe(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("the dataset token was dispatched to another configured service origin: %s %s", r.Method, r.URL)
				http.NotFound(w, r)
			})
			rt := newMicrosoftTestRuntime(t)
			plugin := newCrossServiceTestPlugin(t, tc.selector, primary, secondary)
			key := stateKey("microsoft", tc.selector, "")
			watermark := seedStoredCursor(t, rt, key, tc.cursor(secondary.server.URL))

			// The cross-service cursor must recover rather than be reported as
			// an operational error the runner retries forever.
			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatalf("a cross-service stored cursor was reported as an operational error: %v", err)
			}
			if got := secondary.data.Load(); got != 0 {
				t.Fatalf("%d request(s) carrying the dataset token reached the other configured service", got)
			}
			if got := secondary.tokens.Load(); got != 0 {
				t.Fatalf("%d token acquisition(s) reached the other configured service", got)
			}
			if got := primary.tokens.Load(); got != 0 {
				t.Fatalf("the refused cursor still acquired %d token(s); the origin has to be bound before the token is minted", got)
			}
			if got := primary.data.Load(); got != 0 {
				t.Fatalf("the refused cursor still dispatched %d request(s) to its own service", got)
			}
			if got := len(rt.Entries()); got != 0 {
				t.Fatalf("reset pass wrote %d entries", got)
			}
			assertCursorWasReset(t, rt, key, watermark)
			reset := rt.logsContaining("WARN", "stored continuation was reset")
			if !strings.Contains(reset[0].Params["reason"], "not on the configured origin of its service") {
				t.Fatalf("reset reason=%q, want it to name the service-origin binding", reset[0].Params["reason"])
			}
			if strings.Contains(reset[0].Params["reason"], secondary.server.URL) {
				t.Fatalf("reset reason=%q carries the vendor-controlled URL", reset[0].Params["reason"])
			}

			// The selector still recovers from its durable watermark, against
			// its own service origin only.
			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			if got := primary.data.Load(); got == 0 {
				t.Fatal("the recovered poll issued no request to its own service")
			}
			if got := secondary.data.Load(); got != 0 {
				t.Fatalf("the recovered poll issued %d request(s) to the other configured service", got)
			}
			if got := len(rt.Entries()); got == 0 {
				t.Fatal("the recovered poll collected nothing")
			}
			if got := rt.logsContaining("WARN", "stored continuation was reset"); len(got) != 1 {
				t.Fatalf("reset warnings after recovery=%d, want exactly 1", len(got))
			}
		})
	}
}

// TestRestoredSameServiceContinuationStillResumes is the other half of the
// binding. Pinning the dataset's own service origin must not have stopped a
// legitimate resumed cursor: all three fields, restored against the origin the
// selector's service is configured at, still resume and deliver.
func TestRestoredSameServiceContinuationStillResumes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selector string
		respond  http.HandlerFunc
		cursor   func(own string) *FetchContinuation
	}{
		{
			name: "next-url", selector: "entra-users", respond: graphUsersRecoveryResponse,
			cursor: func(own string) *FetchContinuation {
				return &FetchContinuation{
					Version: fetchContinuationVersion, Kind: KindODataGET,
					NextURL: own + "/v1.0/users?page=2",
				}
			},
		},
		{
			name: "parent-next-url", selector: "defender-users", respond: parentChildRecoveryResponse,
			cursor: func(own string) *FetchContinuation {
				return &FetchContinuation{
					Version: fetchContinuationVersion, Kind: KindParentChild,
					ParentNextURL: own + "/api/machines?page=2",
				}
			},
		},
		{
			name: "child-next-url", selector: "defender-users", respond: parentChildRecoveryResponse,
			cursor: func(own string) *FetchContinuation {
				return &FetchContinuation{
					Version: fetchContinuationVersion, Kind: KindParentChild,
					ParentIDs: []string{"machine-1"}, ChildParentID: "machine-1",
					ChildNextURL: own + "/api/machines/machine-1/logonusers?cursor=1",
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			primary := newOriginProbe(t, tc.respond)
			secondary := newOriginProbe(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("a same-service cursor reached another configured service: %s %s", r.Method, r.URL)
				http.NotFound(w, r)
			})
			rt := newMicrosoftTestRuntime(t)
			plugin := newCrossServiceTestPlugin(t, tc.selector, primary, secondary)
			key := stateKey("microsoft", tc.selector, "")
			seedStoredCursor(t, rt, key, tc.cursor(primary.server.URL))

			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatalf("a legitimate same-service stored cursor failed: %v", err)
			}
			if got := rt.logsContaining("WARN", "stored continuation was reset"); len(got) != 0 {
				t.Fatalf("a legitimate resumed cursor was reset: %+v", got)
			}
			if got := primary.data.Load(); got == 0 {
				t.Fatal("the resumed cursor issued no request")
			}
			if got := len(rt.Entries()); got == 0 {
				t.Fatal("the resumed cursor delivered nothing")
			}
			if got := secondary.data.Load(); got != 0 {
				t.Fatalf("the resumed cursor issued %d request(s) to another configured service", got)
			}
		})
	}
}

// storedCursorArrayDocument builds a stored cursor document whose continuation
// carries members copies of one JSON member in the named array. The frame is
// what saveState writes around it, so the only thing that varies between cases
// is the array under test.
func storedCursorArrayDocument(array, member string, members int) string {
	var document strings.Builder
	document.Grow(len(member)*members + members + 256)
	document.WriteString(`{"window_start":"` + cursorRecoveryWatermark + `","window_end":"2026-10-07T07:00:00Z","fetched":7,`)
	document.WriteString(`"continuation":{"version":1,"kind":"odata-get","`)
	document.WriteString(array)
	document.WriteString(`":[`)
	for ordinal := range members {
		if ordinal != 0 {
			document.WriteString(",")
		}
		document.WriteString(member)
	}
	document.WriteString(`]}}`)
	return document.String()
}

// storedCursorArrayMembers is how many copies of member, plus the separator each
// one needs, fit inside a cursor document of ceiling bytes.
func storedCursorArrayMembers(array, member string, ceiling int) int {
	frame := len(storedCursorArrayDocument(array, member, 1)) - len(member)
	return (ceiling - frame + 1) / (len(member) + 1)
}

// seedStoredCursorDocument writes a cursor document verbatim beside the durable
// watermark a prior poll would have left, which is what makes a cursor denser in
// members than any marshalled FetchContinuation could be testable.
func seedStoredCursorDocument(t *testing.T, rt *microsoftTestRuntime, key, document string) time.Time {
	t.Helper()
	watermark, err := time.Parse(time.RFC3339, cursorRecoveryWatermark)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(key+"/cursor", []byte(document)); err != nil {
		t.Fatal(err)
	}
	if err := rt.PutTime(key+"/timestamp", watermark); err != nil {
		t.Fatal(err)
	}
	return watermark
}

// assertStoredCursorDenseRejectionIsBounded fails unless a cursor far denser in
// members than its bound admits was both refused and refused cheaply: the cost
// of the refusal has to scale with the bound rather than with the member count,
// which is exactly what decoding the array whole before consulting the bound
// did not do.
//
// The allowance is per element the bound admits rather than per byte one such
// element occupies, because what dominates the measurement is the per-element
// bookkeeping of the decode -- and, under the race detector, of the race
// detector -- rather than the element itself. It is applied to both sides of the
// comparison, so what the assertion states is the ratio the document really
// carries: every member past the bound is one the refusal must not have paid
// for.
func assertStoredCursorDenseRejectionIsBounded(t *testing.T, field string, members, bound int, allocated uint64) {
	t.Helper()
	const perElement = 4 << 10
	if members < 10*bound {
		t.Fatalf("the dense %s document carries %d members against a bound of %d, which is not dense enough for the claim",
			field, members, bound)
	}
	ceiling, unbounded := uint64(perElement)*uint64(bound), uint64(perElement)*uint64(members)
	if ceiling >= unbounded/4 {
		t.Fatalf("the %s ceiling %d is not meaningfully below the %d bytes materializing every member costs",
			field, ceiling, unbounded)
	}
	if allocated > ceiling {
		t.Fatalf("refusing a cursor carrying %d dense %s members allocated %d bytes against a %d-byte ceiling; paying the same per element for all of them costs about %d: the array was decoded whole before the bound applied",
			members, field, allocated, ceiling, unbounded)
	}
}

// TestStoredCursorArraysApplyTheirBoundsBeforeRetention pins every untrusted
// array inside a stored cursor at its bounds.
//
// loadState decoded the cursor document straight into cursorState, so
// json.Unmarshal materialized Recent, ParentIDs, ParentRecent, ChildRecent and
// HuntingWindows whole -- one retained element per member, plus every byte
// behind it -- before FetchContinuation.validate was reached, and
// stateHasPending reached the same decode without validating at all.
// maximumCursorDocumentBytes bounds the bytes that are read and says nothing
// about what retaining them costs: a member of a dense array is two or three
// bytes while the element it decodes to is a 16-byte string header or a 48-byte
// window, so a document inside the 12 MiB ceiling carried millions of members
// and the decode allocated hundreds of megabytes before the bound that then
// refused the whole document was consulted (CWE-770).
//
// Each array is pinned at its exact count boundary in both directions and in the
// dense case the boundary alone cannot describe: a document far past the bound
// has to be refused without the allocation.
func TestStoredCursorArraysApplyTheirBoundsBeforeRetention(t *testing.T) {
	key := stateKey("microsoft", "entra-users", "")
	fingerprint := `"` + continuationFingerprint("https://graph.microsoft.com/v1.0/users?page=2") + `"`
	for _, tc := range []struct {
		name string
		// member is one member of the shape a build of this plugin persists, so
		// a document at exactly the count bound is adopted rather than refused
		// for some other reason.
		member string
		// dense is the shortest member the element type accepts, which is what
		// makes the member count an order of magnitude more than the bound.
		dense   string
		bound   int
		reason  string
		adopted func(cursor *FetchContinuation) int
	}{
		{
			name: "recent", member: fingerprint, dense: `"a"`, bound: maximumContinuationPages,
			reason:  fmt.Sprintf("stored continuation history exceeds the %d-element bound", maximumContinuationPages),
			adopted: func(cursor *FetchContinuation) int { return len(cursor.Recent) },
		},
		{
			name: "parent_recent", member: fingerprint, dense: `"a"`, bound: maximumContinuationPages,
			reason:  fmt.Sprintf("stored continuation history exceeds the %d-element bound", maximumContinuationPages),
			adopted: func(cursor *FetchContinuation) int { return len(cursor.ParentRecent) },
		},
		{
			name: "child_recent", member: fingerprint, dense: `"a"`, bound: maximumContinuationPages,
			reason:  fmt.Sprintf("stored continuation history exceeds the %d-element bound", maximumContinuationPages),
			adopted: func(cursor *FetchContinuation) int { return len(cursor.ChildRecent) },
		},
		{
			name: "parent_ids", member: `"machine-00000000000000000000000000000001"`, dense: `"a"`,
			bound:   maximumContinuationParents,
			reason:  fmt.Sprintf("stored parent list exceeds the %d-element bound", maximumContinuationParents),
			adopted: func(cursor *FetchContinuation) int { return len(cursor.ParentIDs) },
		},
		{
			name:   "hunting_windows",
			member: `{"start":"2026-10-07T06:00:00Z","end":"2026-10-07T07:00:00Z"}`, dense: `{}`,
			bound:   maximumHuntingPartitions,
			reason:  fmt.Sprintf("stored hunting window plan exceeds the %d-element bound", maximumHuntingPartitions),
			adopted: func(cursor *FetchContinuation) int { return len(cursor.HuntingWindows) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("exact-maximum", func(t *testing.T) {
				rt := newMicrosoftTestRuntime(t)
				document := storedCursorArrayDocument(tc.name, tc.member, tc.bound)
				if len(document) > maximumCursorDocumentBytes {
					t.Fatalf("a document at exactly the count bound is %d bytes, over the %d-byte ceiling",
						len(document), maximumCursorDocumentBytes)
				}
				seedStoredCursorDocument(t, rt, key, document)
				state, err := loadState(rt, key)
				if err != nil {
					t.Fatalf("a cursor at exactly the count bound was refused: %v", err)
				}
				if state.Continuation == nil {
					t.Fatal("the cursor was not adopted at all")
				}
				if got := tc.adopted(state.Continuation); got != tc.bound {
					t.Fatalf("adopted %s=%d, want the %d the document carries", tc.name, got, tc.bound)
				}
				if state.Fetched != 7 || !state.WindowEnd.After(state.WindowStart) {
					t.Fatalf("the rest of the cursor did not decode: %+v", state)
				}
			})

			t.Run("maximum-plus-one", func(t *testing.T) {
				rt := newMicrosoftTestRuntime(t)
				document := storedCursorArrayDocument(tc.name, tc.member, tc.bound+1)
				if len(document) > maximumCursorDocumentBytes {
					t.Fatalf("a document one member over the count bound is %d bytes, over the %d-byte ceiling, so the ceiling would catch it instead",
						len(document), maximumCursorDocumentBytes)
				}
				watermark := seedStoredCursorDocument(t, rt, key, document)
				state, err := loadState(rt, key)
				if err == nil {
					t.Fatal("a cursor one member over the count bound was adopted")
				}
				if !errors.Is(err, errIncompatibleContinuation) {
					t.Fatalf("error=%v, want an incompatible-continuation error so the cursor is replaced", err)
				}
				if !strings.Contains(err.Error(), tc.reason) {
					t.Fatalf("error=%v, want the field and the bound named for the operator", err)
				}
				// The durable watermark is what the restart needs, and it is
				// read before the cursor, so it survives the refusal.
				if !state.Watermark.Equal(watermark) || state.Continuation != nil {
					t.Fatalf("state=%+v, want the watermark kept and the cursor discarded", state)
				}
			})

			t.Run("dense-array", func(t *testing.T) {
				rt := newMicrosoftTestRuntime(t)
				members := storedCursorArrayMembers(tc.name, tc.dense, maximumCursorDocumentBytes)
				// Non-vacuous: the document really does carry far more members
				// than the bound admits, so a decode that materialized the array
				// first had to retain an element for every one of them.
				if members < 10*tc.bound {
					t.Fatalf("the dense document carries %d members against a bound of %d, which is not dense enough for the claim",
						members, tc.bound)
				}
				document := storedCursorArrayDocument(tc.name, tc.dense, members)
				if len(document) > maximumCursorDocumentBytes {
					t.Fatalf("the dense document is %d bytes, over the %d-byte ceiling, so the ceiling would catch it instead",
						len(document), maximumCursorDocumentBytes)
				}
				seedStoredCursorDocument(t, rt, key, document)
				var denseErr error
				allocated := managementDecodeAllocation(t, func() {
					_, denseErr = loadState(rt, key)
				})
				if denseErr == nil {
					t.Fatal("a cursor dense past the count bound was adopted")
				}
				// The cost of the refusal is asserted before its message, so a
				// decode that refuses the document only after materializing it
				// fails here rather than on the wording.
				assertStoredCursorDenseRejectionIsBounded(t, tc.name, members, tc.bound, allocated)
				if !strings.Contains(denseErr.Error(), tc.reason) {
					t.Fatalf("error=%v, want the count bound to be what refused it", denseErr)
				}
			})
		})
	}
}

// TestStoredCursorArrayBoundsPreserveTheirLoadContract pins what the bounded
// decoders must not change: the shapes encoding/json already accepted and
// refused, the duplicate-member semantics, the pending check a drain pass runs,
// and the round trip between what saveState writes and what loadState reads.
func TestStoredCursorArrayBoundsPreserveTheirLoadContract(t *testing.T) {
	key := stateKey("microsoft", "entra-users", "")
	const frame = `{"window_start":"` + cursorRecoveryWatermark + `","window_end":"2026-10-07T07:00:00Z","continuation":`

	t.Run("shape-and-trailing-data", func(t *testing.T) {
		for _, tc := range []struct{ name, document, want string }{
			{"trailing-data", frame + `{"version":1,"kind":"odata-get","recent":[]}} {}`, "decode Microsoft cursor"},
			{"array-is-an-object", frame + `{"version":1,"recent":{"a":1}}}`, "stored continuation history must be a JSON array"},
			{"parents-are-a-string", frame + `{"version":1,"parent_ids":"machine-1"}}`, "stored parent list must be a JSON array"},
			{"windows-are-a-number", frame + `{"version":1,"hunting_windows":7}}`, "stored hunting window plan must be a JSON array"},
			{"history-member-of-the-wrong-type", frame + `{"version":1,"recent":[7]}}`, "decode Microsoft cursor"},
			{"window-member-of-the-wrong-type", frame + `{"version":1,"hunting_windows":["a"]}}`, "decode Microsoft cursor"},
			{"oversized-parent-identifier", frame + `{"version":1,"parent_ids":["` +
				strings.Repeat("p", maximumContinuationParentIDLength+1) + `"]}}`, "stored parent identifier exceeds"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				rt := newMicrosoftTestRuntime(t)
				watermark := seedStoredCursorDocument(t, rt, key, tc.document)
				state, err := loadState(rt, key)
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error=%v, want one naming %q", err, tc.want)
				}
				if !errors.Is(err, errIncompatibleContinuation) {
					t.Fatalf("error=%v, want an incompatible-continuation error", err)
				}
				if !state.Watermark.Equal(watermark) {
					t.Fatalf("watermark=%s, want the durable %s kept", state.Watermark, watermark)
				}
				if strings.Contains(err.Error(), "pppp") {
					t.Fatalf("error=%q carries the stored bytes", err)
				}
			})
		}
	})

	// An absent array, an explicit null and an empty one all stay the empty
	// answer decoding into a slice already left.
	t.Run("absent-null-and-empty", func(t *testing.T) {
		for _, document := range []string{
			frame + `{"version":1,"kind":"odata-get"}}`,
			frame + `{"version":1,"kind":"odata-get","recent":null,"parent_ids":null,"parent_recent":null,"child_recent":null,"hunting_windows":null}}`,
			frame + `{"version":1,"kind":"odata-get","recent":[],"parent_ids":[],"parent_recent":[],"child_recent":[],"hunting_windows":[]}}`,
			frame + `null}`,
		} {
			rt := newMicrosoftTestRuntime(t)
			seedStoredCursorDocument(t, rt, key, document)
			state, err := loadState(rt, key)
			if err != nil {
				t.Fatalf("document %s was refused: %v", document, err)
			}
			if document == frame+`null}` {
				if state.Continuation != nil {
					t.Fatalf("a null continuation decoded to %+v", state.Continuation)
				}
				continue
			}
			if state.Continuation == nil {
				t.Fatalf("document %s left no cursor", document)
			}
			if got := state.Continuation; len(got.Recent) != 0 || len(got.ParentIDs) != 0 ||
				len(got.ParentRecent) != 0 || len(got.ChildRecent) != 0 || len(got.HuntingWindows) != 0 {
				t.Fatalf("document %s decoded to %+v, want every array empty", document, got)
			}
		}
	})

	// A repeated bounded member is corrupt state. Accepting both occurrences
	// gives each one a fresh count and byte budget, so the document can retain
	// more than the durable cursor contract permits.
	t.Run("duplicate-member", func(t *testing.T) {
		for _, tc := range []struct {
			member string
			first  string
			second string
		}{
			{"recent", `["one"]`, `["two"]`},
			{"parent_ids", `["one"]`, `["two"]`},
			{"parent_recent", `["one"]`, `["two"]`},
			{"child_recent", `["one"]`, `["two"]`},
			{"hunting_windows", `[{"start":"2026-10-07T00:00:00Z","end":"2026-10-07T01:00:00Z"}]`, `[]`},
		} {
			t.Run(tc.member, func(t *testing.T) {
				rt := newMicrosoftTestRuntime(t)
				document := frame + fmt.Sprintf(`{"version":1,"kind":"odata-get",%q:%s,%q:%s}}`, tc.member, tc.first, tc.member, tc.second)
				seedStoredCursorDocument(t, rt, key, document)
				if _, err := loadState(rt, key); err == nil || !strings.Contains(err.Error(), "repeats bounded member "+tc.member) {
					t.Fatalf("error=%v, want duplicate %s refused", err, tc.member)
				}
			})
		}

		rt := newMicrosoftTestRuntime(t)
		var over strings.Builder
		over.WriteString(frame + `{"version":1,"kind":"odata-get","recent":[`)
		for ordinal := range maximumContinuationPages + 1 {
			if ordinal != 0 {
				over.WriteString(",")
			}
			over.WriteString(`"a"`)
		}
		over.WriteString(`],"recent":[]}}`)
		seedStoredCursorDocument(t, rt, key, over.String())
		if _, err := loadState(rt, key); err == nil ||
			!strings.Contains(err.Error(), fmt.Sprintf("stored continuation history exceeds the %d-element bound", maximumContinuationPages)) {
			t.Fatalf("error=%v, want the over-bound first occurrence refused on its own", err)
		}
	})

	// The pending check a drain pass runs for every target reads the same
	// document, so it is bounded by the same decoders -- and a document it
	// cannot read still reports pending work, so collect visits the selector and
	// replaces it.
	t.Run("state-has-pending", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		resumable := frame + `{"version":1,"kind":"odata-get","next_url":"https://graph.microsoft.com/v1.0/users?page=2","recent":["a"]}}`
		seedStoredCursorDocument(t, rt, key, resumable)
		if pending, err := stateHasPending(rt, key); err != nil || !pending {
			t.Fatalf("pending=%t error=%v, want a resumable cursor reported as pending", pending, err)
		}

		members := storedCursorArrayMembers("recent", `"a"`, maximumCursorDocumentBytes)
		seedStoredCursorDocument(t, rt, key, storedCursorArrayDocument("recent", `"a"`, members))
		var pending bool
		var err error
		allocated := managementDecodeAllocation(t, func() {
			pending, err = stateHasPending(rt, key)
		})
		if !errors.Is(err, errIncompatibleContinuation) {
			t.Fatalf("pending=%t error=%v, want the bound reported so the selector is still visited", pending, err)
		}
		const pendingCeiling = 8 << 20
		if allocated > pendingCeiling {
			t.Fatalf("the pending check on a cursor carrying %d dense members allocated %d bytes against a %d-byte ceiling",
				members, allocated, pendingCeiling)
		}
		plugin := New(validTestConfig(t, "entra-users"))
		if got, err := plugin.targetHasPending(rt, sourceTarget{dataset: datasets["entra-users"], key: key}); err != nil || !got {
			t.Fatalf("drain-pass pending=%t error=%v, want the unreadable cursor visited", got, err)
		}
	})

	// The aggregate budgets, exactly, in both directions, and then at the
	// production values through the real load. The charge has to be what the
	// accept path charges, or a cursor this build wrote would be refused by the
	// load that follows it.
	t.Run("aggregate-byte-budget", func(t *testing.T) {
		// A history and a window plan are charged the bytes they occupy in the
		// document: the members plus the separators between them.
		history := []byte(`["aa","bb","cc"]`)
		const historyBytes = len(`"aa","bb","cc"`)
		if decoded, err := decodeBoundedContinuationArray[string](history, "continuation history",
			8, historyBytes, storedContinuationElementBytes[string], nil); err != nil || len(decoded) != 3 {
			t.Fatalf("a history charging exactly its budget was refused: decoded=%d error=%v", len(decoded), err)
		}
		if _, err := decodeBoundedContinuationArray[string](history, "continuation history",
			8, historyBytes-1, storedContinuationElementBytes[string], nil); err == nil ||
			!strings.Contains(err.Error(), fmt.Sprintf("%d-byte budget", historyBytes-1)) {
			t.Fatalf("error=%v, want a history one byte over its budget refused and the budget named", err)
		}

		// The parent list is charged the re-encoded form instead, which is what
		// admitParentID charges while it grows the list.
		parents := []byte(`["machine-1","machine-2"]`)
		charged := continuationParentIDBytes("machine-1") + continuationParentIDBytes("machine-2")
		if decoded, err := decodeBoundedContinuationArray[string](parents, "parent list", 8, charged,
			func(id string, _ int) int { return continuationParentIDBytes(id) }, nil); err != nil || len(decoded) != 2 {
			t.Fatalf("a parent list charging exactly its budget was refused: decoded=%d error=%v", len(decoded), err)
		}
		if _, err := decodeBoundedContinuationArray[string](parents, "parent list", 8, charged-1,
			func(id string, _ int) int { return continuationParentIDBytes(id) }, nil); err == nil {
			t.Fatal("a parent list one byte over its budget was adopted")
		}

		// And at the production budgets, where the byte bound is what binds
		// rather than the count: each of these documents is inside the cursor
		// ceiling and inside its array's count bound.
		for _, tc := range []struct {
			name, array, member string
			budget              int
			// inside is the largest member count the budget admits for this
			// member, so the boundary is driven by the real budget rather than
			// by a count chosen to be comfortably past it.
			inside func(member string) int
			// bound is the array's own count bound, which must not be what
			// refuses the document here.
			bound int
		}{
			{
				name: "continuation-history", array: "recent",
				member: `"` + strings.Repeat("u", maximumContinuationURLLength) + `"`,
				budget: maximumContinuationHistoryBytes, bound: maximumContinuationPages,
				inside: func(member string) int {
					return (maximumContinuationHistoryBytes + 1) / (len(member) + 1)
				},
			},
			{
				name: "parent-list", array: "parent_ids",
				member: `"` + strings.Repeat("p", 420) + `"`,
				budget: maximumContinuationParentBytes, bound: maximumContinuationParents,
				// Charged the re-encoded identifier plus its separator, which for
				// an identifier with nothing to escape is the member as stored.
				inside: func(member string) int {
					return maximumContinuationParentBytes / (len(member) + 1)
				},
			},
			{
				name: "hunting-window-plan", array: "hunting_windows",
				member: `{"start":"2026-10-07T06:00:00Z","end":"2026-10-07T07:00:00Z","pad":"` +
					strings.Repeat("w", 240) + `"}`,
				budget: maximumContinuationHuntingWindowBytes, bound: maximumHuntingPartitions,
				inside: func(member string) int {
					return (maximumContinuationHuntingWindowBytes + 1) / (len(member) + 1)
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				rt := newMicrosoftTestRuntime(t)
				members := tc.inside(tc.member) + 1
				if members > tc.bound {
					t.Fatalf("crossing the %d-byte budget takes %d members, over the %d-element count bound, so the count would refuse it instead",
						tc.budget, members, tc.bound)
				}
				document := storedCursorArrayDocument(tc.array, tc.member, members)
				if len(document) > maximumCursorDocumentBytes {
					t.Fatalf("the over-budget document is %d bytes, over the %d-byte ceiling, so the ceiling would catch it instead",
						len(document), maximumCursorDocumentBytes)
				}
				watermark := seedStoredCursorDocument(t, rt, key, document)
				state, err := loadState(rt, key)
				if err == nil {
					t.Fatal("an array over its aggregate budget was adopted")
				}
				if !strings.Contains(err.Error(), fmt.Sprintf("%d-byte budget", tc.budget)) {
					t.Fatalf("error=%v, want the aggregate budget named for the operator", err)
				}
				if !errors.Is(err, errIncompatibleContinuation) || !state.Watermark.Equal(watermark) {
					t.Fatalf("error=%v state=%+v, want the cursor replaced and the watermark kept", err, state)
				}

				// One member fewer is inside the budget and still resumable, so
				// the budget is a boundary rather than a blanket refusal.
				seedStoredCursorDocument(t, rt, key, storedCursorArrayDocument(tc.array, tc.member, members-1))
				if _, err := loadState(rt, key); err != nil {
					t.Fatalf("a cursor inside the %d-byte budget was refused: %v", tc.budget, err)
				}
			})
		}
	})

	// Where the production history count is applied: the decode bound is the
	// widest count any build could have persisted, and the fetch path trims what
	// it reads back to the newest maximumContinuationHistory entries before using
	// it, which is what every later persist writes back.
	t.Run("production-history-count-applies-on-resume", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		seedStoredCursorDocument(t, rt, key,
			storedCursorArrayDocument("recent", `"`+continuationFingerprint("https://graph.microsoft.com/v1.0/users")+`"`,
				maximumContinuationPages))
		state, err := loadState(rt, key)
		if err != nil || state.Continuation == nil {
			t.Fatalf("a history at the stored bound was refused: %v", err)
		}
		resumed, err := continuationFor(datasets["entra-users"], state.Continuation)
		if err != nil {
			t.Fatalf("a cursor at the stored bound is not resumable: %v", err)
		}
		if len(resumed.Recent) != maximumContinuationHistory {
			t.Fatalf("resumed history=%d, want it trimmed to the newest %d", len(resumed.Recent), maximumContinuationHistory)
		}
	})

	// Round trip: what saveState writes is what loadState reads back, arrays
	// included, so the bounds cannot have narrowed the canonical accepted state.
	t.Run("round-trip", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		watermark, err := time.Parse(time.RFC3339, cursorRecoveryWatermark)
		if err != nil {
			t.Fatal(err)
		}
		cursor := &FetchContinuation{
			Version: fetchContinuationVersion, Kind: KindParentChild,
			ParentNextURL: "https://api.securitycenter.microsoft.com/api/machines?page=2",
			ParentIDs:     []string{"machine-1", "machine-2", "machine-3"},
			ParentIndex:   1,
			ChildNextURL:  "https://api.securitycenter.microsoft.com/api/machines/machine-2/logonusers?page=2",
			ChildParentID: "machine-2",
			Recent:        []string{continuationFingerprint("https://api.securitycenter.microsoft.com/api/machines")},
			ParentRecent:  []string{continuationFingerprint("https://api.securitycenter.microsoft.com/api/machines?page=1")},
			ChildRecent:   []string{continuationFingerprint("https://api.securitycenter.microsoft.com/api/machines/machine-2/logonusers")},
			HuntingWindows: []FetchWindow{
				{Start: watermark, End: watermark.Add(time.Hour)},
				{Start: watermark.Add(time.Hour), End: watermark.Add(2 * time.Hour)},
			},
			HuntingPartitions: 2, HuntingPartition: 1, Pages: 3, ChildPages: 2,
		}
		saved := streamState{
			Watermark: watermark, WindowStart: watermark, WindowEnd: watermark.Add(time.Hour),
			Continuation: cursor, Fetched: 9,
		}
		if err := saveState(rt, key, saved); err != nil {
			t.Fatal(err)
		}
		loaded, err := loadState(rt, key)
		if err != nil {
			t.Fatalf("a cursor this build just wrote was refused: %v", err)
		}
		if loaded.Continuation == nil {
			t.Fatal("the cursor did not survive the round trip")
		}
		if !reflect.DeepEqual(*loaded.Continuation, *cursor) {
			t.Fatalf("round trip changed the cursor:\n got %+v\nwant %+v", *loaded.Continuation, *cursor)
		}
		if loaded.Fetched != saved.Fetched || !loaded.WindowEnd.Equal(saved.WindowEnd) {
			t.Fatalf("round trip changed the window: %+v", loaded)
		}
		// And the cursor it read back is one the fetch path still resumes.
		if _, err := continuationFor(datasets["defender-users"], loaded.Continuation); err != nil {
			t.Fatalf("the restored cursor is not resumable: %v", err)
		}
	})
}

// TestStoredCursorArrayBoundResetsWithoutDispatch pins the operator-visible end
// of the repair: a cursor whose arrays cross a bound is replaced, the durable
// watermark is kept, and the pass that does it reaches the source not at all --
// no data request and no token, so no bearer credential is minted for a document
// this build refuses to read.
func TestStoredCursorArrayBoundResetsWithoutDispatch(t *testing.T) {
	for _, tc := range []struct{ name, array, member, reason string }{
		{"recent", "recent", `"a"`, "stored continuation history exceeds"},
		{"parent_ids", "parent_ids", `"a"`, "stored parent list exceeds"},
		{"hunting_windows", "hunting_windows", `{}`, "stored hunting window plan exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := newOriginProbe(t, graphUsersRecoveryResponse)
			rt := newMicrosoftTestRuntime(t)
			plugin := newSelectorTestPlugin(t, probe.server, "entra-users")
			key := stateKey("microsoft", "entra-users", "")

			members := storedCursorArrayMembers(tc.array, tc.member, maximumCursorDocumentBytes)
			watermark := seedStoredCursorDocument(t, rt, key, storedCursorArrayDocument(tc.array, tc.member, members))
			seeded, err := rt.Get(key + "/cursor")
			if err != nil {
				t.Fatal(err)
			}

			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatalf("an over-bound stored cursor was reported as an operational error: %v", err)
			}
			if got := probe.data.Load(); got != 0 {
				t.Fatalf("the reset pass dispatched %d request(s)", got)
			}
			if got := probe.tokens.Load(); got != 0 {
				t.Fatalf("the reset pass acquired %d token(s)", got)
			}
			if got := len(rt.Entries()); got != 0 {
				t.Fatalf("the reset pass wrote %d entries", got)
			}
			assertCursorWasReset(t, rt, key, watermark)
			after, err := rt.Get(key + "/cursor")
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(after, seeded) {
				t.Fatalf("the over-bound document was left on disk: %d bytes", len(after))
			}
			reset := rt.logsContaining("WARN", "stored continuation was reset")
			if !strings.Contains(reset[0].Params["reason"], tc.reason) {
				t.Fatalf("reset reason=%q, want it to carry %q", reset[0].Params["reason"], tc.reason)
			}

			// The selector recovers from its durable watermark on the next poll.
			probe.reset()
			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			if got := probe.data.Load(); got != 1 || len(rt.Entries()) != 1 {
				t.Fatalf("recovered poll requests=%d entries=%d, want 1 and 1", got, len(rt.Entries()))
			}
			if got := rt.logsContaining("WARN", "stored continuation was reset"); len(got) != 1 {
				t.Fatalf("reset warnings after recovery=%d, want exactly 1", len(got))
			}
		})
	}
}

// TestOversizedStoredCursorResetsWithoutDispatch is the load-side proof for the
// cursor size ceilings. A persisted cursor over any one of them is state no
// growth path could have written, so it has to enter the existing
// reset/recovery path -- cursor rewritten, durable watermark retained -- before
// a request is dispatched, rather than being re-marshalled and re-persisted on
// every poll for as long as the window stays open.
func TestOversizedStoredCursorResetsWithoutDispatch(t *testing.T) {
	cursorKey := func(field string, size int) *FetchContinuation {
		cursor := storedCursorAtSize(field, size)
		cursor.Kind = KindODataGET
		return &cursor
	}
	for _, tc := range []struct {
		name   string
		cursor *FetchContinuation
		// document, when set, is written to the cursor key verbatim instead of
		// a marshalled cursor state, which is how the whole-document ceiling is
		// driven: a document that large is refused before it is even decoded.
		document []byte
		reason   string
	}{
		{name: "next-url", cursor: cursorKey("next_url", maximumContinuationURLLength+1), reason: "next URL exceeds"},
		{name: "skip-token", cursor: cursorKey("skip_token", maximumContinuationSkipTokenLength+1), reason: "skip token exceeds"},
		{name: "history-entry", cursor: cursorKey("history_entry", maximumContinuationURLLength+1), reason: "continuation history entry exceeds"},
		{
			name:     "whole-document",
			document: append(append([]byte(`{"fetched":1,"pad":"`), bytes.Repeat([]byte("a"), maximumCursorDocumentBytes)...), []byte(`"}`)...),
			reason:   "over the 12582912-byte ceiling",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := newOriginProbe(t, graphUsersRecoveryResponse)
			rt := newMicrosoftTestRuntime(t)
			plugin := newSelectorTestPlugin(t, probe.server, "entra-users")
			key := stateKey("microsoft", "entra-users", "")

			watermark, err := time.Parse(time.RFC3339, cursorRecoveryWatermark)
			if err != nil {
				t.Fatal(err)
			}
			if tc.document != nil {
				if err := rt.Put(key+"/cursor", tc.document); err != nil {
					t.Fatal(err)
				}
				if err := rt.PutTime(key+"/timestamp", watermark); err != nil {
					t.Fatal(err)
				}
			} else {
				watermark = seedStoredCursor(t, rt, key, tc.cursor)
			}
			seeded, err := rt.Get(key + "/cursor")
			if err != nil {
				t.Fatal(err)
			}

			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatalf("an oversized stored cursor was reported as an operational error: %v", err)
			}
			if got := probe.data.Load(); got != 0 {
				t.Fatalf("the reset pass dispatched %d request(s)", got)
			}
			if got := probe.tokens.Load(); got != 0 {
				t.Fatalf("the reset pass acquired %d token(s)", got)
			}
			if got := len(rt.Entries()); got != 0 {
				t.Fatalf("the reset pass wrote %d entries", got)
			}
			assertCursorWasReset(t, rt, key, watermark)
			after, err := rt.Get(key + "/cursor")
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(after, seeded) {
				t.Fatalf("the oversized document was left on disk: %d bytes", len(after))
			}
			if len(after) > maximumCursorDocumentBytes {
				t.Fatalf("the replacement document is %d bytes, over the ceiling", len(after))
			}
			reset := rt.logsContaining("WARN", "stored continuation was reset")
			if !strings.Contains(reset[0].Params["reason"], tc.reason) {
				t.Fatalf("reset reason=%q, want it to carry %q", reset[0].Params["reason"], tc.reason)
			}
			if strings.Contains(reset[0].Params["reason"], "aaaa") {
				t.Fatalf("reset reason=%q carries the stored bytes", reset[0].Params["reason"])
			}

			// The selector recovers from its durable watermark on the next poll.
			probe.reset()
			if _, err := plugin.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			if got := probe.data.Load(); got != 1 || len(rt.Entries()) != 1 {
				t.Fatalf("recovered poll requests=%d entries=%d, want 1 and 1", got, len(rt.Entries()))
			}
			if got := rt.logsContaining("WARN", "stored continuation was reset"); len(got) != 1 {
				t.Fatalf("reset warnings after recovery=%d, want exactly 1", len(got))
			}
		})
	}
}

func fabricStateDocumentAtSize(t *testing.T, size int, duplicate bool) []byte {
	t.Helper()
	prefix := []byte(`{"unknown":"`)
	suffix := []byte(`"}`)
	if duplicate {
		suffix = []byte(`","unknown":0}`)
	}
	padding := size - len(prefix) - len(suffix)
	if padding < 0 {
		t.Fatalf("cannot build a %d-byte Fabric state document", size)
	}
	document := make([]byte, 0, size)
	document = append(document, prefix...)
	document = append(document, bytes.Repeat([]byte("a"), padding)...)
	document = append(document, suffix...)
	if len(document) != size {
		t.Fatalf("Fabric state document bytes=%d, want %d", len(document), size)
	}
	return document
}

// TestFabricStateDocumentCeiling pins both sides of the stored-document
// boundary. The largest state this build can legitimately marshal retains
// ample headroom; a document exactly at the ceiling still reaches structural
// validation, while one byte more is refused before either JSON decoder runs.
func TestFabricStateDocumentCeiling(t *testing.T) {
	latest := time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.FixedZone("maximum", 14*60*60))
	legitimate, err := marshalFabricState(&fabricScan{
		Anchor: latest, AvailableSince: latest, Through: latest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(legitimate) > maximumFabricStateBytes {
		t.Fatalf("largest current Fabric state is %d bytes, over the %d-byte ceiling", len(legitimate), maximumFabricStateBytes)
	}

	t.Run("exactly-ceiling-reaches-structural-decoder", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		key := stateKey("microsoft", "fabric-activity", "")
		document := fabricStateDocumentAtSize(t, maximumFabricStateBytes, true)
		if err := rt.Put(key+"/fabric", document); err != nil {
			t.Fatal(err)
		}
		_, err := loadState(rt, key)
		if !errors.Is(err, errIncompatibleContinuation) {
			t.Fatalf("load error=%v, want structural refusal", err)
		}
		if strings.Contains(err.Error(), "over the") || !strings.Contains(err.Error(), "repeats an object member") {
			t.Fatalf("load error=%v, want duplicate-member refusal after the size gate", err)
		}
	})

	t.Run("one-byte-over-is-refused-by-size", func(t *testing.T) {
		rt := newMicrosoftTestRuntime(t)
		key := stateKey("microsoft", "fabric-activity", "")
		document := fabricStateDocumentAtSize(t, maximumFabricStateBytes+1, false)
		if err := rt.Put(key+"/fabric", document); err != nil {
			t.Fatal(err)
		}
		state, err := loadState(rt, key)
		if !errors.Is(err, errIncompatibleContinuation) {
			t.Fatalf("load error=%v, want incompatible continuation", err)
		}
		if state.Fabric == nil || *state.Fabric != (fabricScan{}) {
			t.Fatalf("oversized Fabric state=%+v, want inert replacement", state.Fabric)
		}
		want := fmt.Sprintf("over the %d-byte ceiling", maximumFabricStateBytes)
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("load error=%v, want %q", err, want)
		}
	})
}

// TestOversizedStoredFabricStateResetsWithoutDispatch proves the load refusal
// uses the ordinary safe-recovery path: the durable watermark survives, the
// rejected scan is inert, the reset pass neither acquires a token nor reaches
// Fabric, and the following scheduled poll collects normally.
func TestOversizedStoredFabricStateResetsWithoutDispatch(t *testing.T) {
	probe := newOriginProbe(t, fabricActivityPage)
	rt := newMicrosoftTestRuntime(t)
	plugin := newSelectorTestPlugin(t, probe.server, "fabric-activity")
	key := stateKey("microsoft", "fabric-activity", "")
	wantDocument := fabricStateDocumentAtSize(t, maximumFabricStateBytes+1, false)
	watermark, err := time.Parse(time.RFC3339, cursorRecoveryWatermark)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.PutTime(key+"/timestamp", watermark); err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(key+"/fabric", wantDocument); err != nil {
		t.Fatal(err)
	}

	partial, err := loadState(rt, key)
	if !errors.Is(err, errIncompatibleContinuation) {
		t.Fatalf("load error=%v, want incompatible continuation", err)
	}
	if !partial.Watermark.Equal(watermark) {
		t.Fatalf("partial watermark=%s, want %s", partial.Watermark, watermark)
	}
	if partial.Fabric == nil || *partial.Fabric != (fabricScan{}) {
		t.Fatalf("partial Fabric state=%+v, want inert replacement", partial.Fabric)
	}

	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("oversized Fabric state was reported as an operational error: %v", err)
	}
	if got := probe.tokens.Load(); got != 0 {
		t.Fatalf("reset pass acquired %d token(s)", got)
	}
	if got := probe.data.Load(); got != 0 {
		t.Fatalf("reset pass dispatched %d request(s)", got)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("reset pass wrote %d entries", got)
	}
	assertCursorWasReset(t, rt, key, watermark)
	after, err := rt.Get(key + "/fabric")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(after, wantDocument) || len(after) > maximumFabricStateBytes {
		t.Fatalf("oversized Fabric state was not safely replaced: %d bytes", len(after))
	}

	probe.reset()
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := probe.tokens.Load(); got != 1 {
		t.Fatalf("recovered poll acquired %d token(s), want 1", got)
	}
	if got := probe.data.Load(); got != 1 {
		t.Fatalf("recovered poll requests=%d, want 1", got)
	}
	if got := len(rt.Entries()); got != 1 {
		t.Fatalf("recovered poll entries=%d, want 1", got)
	}
}

// TestFabricStateCeilingRefusesPersistenceBeforeAnyKeyIsWritten proves the
// save-side bound is one atomic preflight. The injected encoder models a later
// compatible fabricScan shape large enough to cross the ceiling; no checkpoint
// key may change when that encoding is refused.
func TestFabricStateCeilingRefusesPersistenceBeforeAnyKeyIsWritten(t *testing.T) {
	rt := newMicrosoftTestRuntime(t)
	key := stateKey("microsoft", "fabric-activity", "")
	watermark, err := time.Parse(time.RFC3339, cursorRecoveryWatermark)
	if err != nil {
		t.Fatal(err)
	}
	baseline := streamState{
		Watermark: watermark, WindowStart: watermark, WindowEnd: watermark.Add(time.Hour), Fetched: 3,
		Fabric: &fabricScan{Anchor: watermark, AvailableSince: watermark, Through: watermark.Add(time.Hour)},
		Overlap: &temporalOverlap{Version: temporalOverlapVersion, Receipts: []temporalReceipt{{
			Timestamp: watermark, Fingerprint: strings.Repeat("a", temporalFingerprintLength),
		}}},
	}
	if err := saveState(rt, key, baseline); err != nil {
		t.Fatal(err)
	}
	keys := []string{"overlap", "cursor", "fabric"}
	before := make(map[string][]byte, len(keys))
	for _, suffix := range keys {
		before[suffix], err = rt.Get(key + "/" + suffix)
		if err != nil {
			t.Fatal(err)
		}
	}
	beforeTime, err := rt.GetTime(key + "/timestamp")
	if err != nil {
		t.Fatal(err)
	}

	changed := streamState{
		Watermark: watermark.Add(24 * time.Hour), WindowStart: watermark.Add(time.Hour),
		WindowEnd: watermark.Add(2 * time.Hour), Fetched: 99,
		Fabric: &fabricScan{Anchor: watermark.Add(time.Hour), AvailableSince: watermark.Add(time.Hour), Through: watermark.Add(2 * time.Hour)},
		Overlap: &temporalOverlap{Version: temporalOverlapVersion, Receipts: []temporalReceipt{{
			Timestamp: watermark.Add(time.Hour), Fingerprint: strings.Repeat("b", temporalFingerprintLength),
		}}},
	}
	err = saveStateWithFabricMarshal(rt, key, changed, func(*fabricScan) ([]byte, error) {
		return fabricStateDocumentAtSize(t, maximumFabricStateBytes+1, false), nil
	})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("over the %d-byte ceiling", maximumFabricStateBytes)) {
		t.Fatalf("save error=%v, want Fabric state ceiling refusal", err)
	}
	for _, suffix := range keys {
		after, getErr := rt.Get(key + "/" + suffix)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if !bytes.Equal(after, before[suffix]) {
			t.Fatalf("%s changed after a refused Fabric state save", suffix)
		}
	}
	afterTime, err := rt.GetTime(key + "/timestamp")
	if err != nil {
		t.Fatal(err)
	}
	if !afterTime.Equal(beforeTime) {
		t.Fatalf("timestamp changed from %s to %s after refusal", beforeTime, afterTime)
	}
}
