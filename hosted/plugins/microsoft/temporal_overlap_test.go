package microsoft

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTemporalOverlapRecoversLateRecordsWithoutReplayAcrossRestart(t *testing.T) {
	tests := []struct {
		name           string
		selector       string
		timeField      string
		idField        string
		subscriptionID string
		response       func([]string) string
	}{
		{name: "graph", selector: "entra-directory-audits", timeField: "activityDateTime", idField: "id", response: valueEnvelope},
		{name: "azure-activity", selector: "azure-activity", timeField: "eventTimestamp", idField: "eventDataId", subscriptionID: "33333333-3333-4333-8333-333333333333", response: valueEnvelope},
		{name: "advanced-hunting", selector: "defender-xdr-device-events", timeField: "Timestamp", idField: "ReportId", response: resultsEnvelope},
		{name: "fabric", selector: "fabric-activity", timeField: "CreationTime", idField: "Id", response: fabricEnvelope},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			visibleLate := false
			dataCalls := 0
			eventTime := time.Date(2026, 10, 7, 11, 50, 0, 0, time.UTC)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
					fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
					return
				}
				dataCalls++
				records := []string{
					temporalFixtureRecord(tc.timeField, tc.idField, "equal-a", eventTime),
					temporalFixtureRecord(tc.timeField, tc.idField, "equal-b", eventTime),
				}
				if visibleLate {
					records = append(records, temporalFixtureRecord(tc.timeField, tc.idField, "late", eventTime))
				}
				fmt.Fprint(w, tc.response(records))
			}))
			defer server.Close()

			rt := newMicrosoftTestRuntime(t)
			p := newMicrosoftTestPlugin(t, server, rt)
			p.conf.Api = []string{tc.selector}
			if tc.subscriptionID != "" {
				p.conf.Subscription_ID = []string{tc.subscriptionID}
			}
			key := stateKey("microsoft", tc.selector, tc.subscriptionID)
			if err := rt.PutTime(key+"/timestamp", time.Date(2026, 10, 7, 11, 45, 0, 0, time.UTC)); err != nil {
				t.Fatal(err)
			}
			p.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
			if _, err := p.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			if got := len(rt.Entries()); got != 2 {
				t.Fatalf("first-poll entries=%d, want two equal-timestamp identities", got)
			}
			firstState, err := loadState(rt, key)
			if err != nil {
				t.Fatal(err)
			}
			if firstState.Overlap == nil || len(firstState.Overlap.Receipts) != 2 {
				t.Fatalf("first-poll overlap state=%+v", firstState.Overlap)
			}

			// Recreate the plugin to prove the bounded receipt document, rather
			// than process memory, suppresses replay while admitting the late row.
			visibleLate = true
			p = newMicrosoftTestPlugin(t, server, rt)
			p.conf.Api = []string{tc.selector}
			if tc.subscriptionID != "" {
				p.conf.Subscription_ID = []string{tc.subscriptionID}
			}
			p.now = func() time.Time { return time.Date(2026, 10, 7, 12, 10, 0, 0, time.UTC) }
			if _, err := p.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			if got := len(rt.Entries()); got != 3 {
				t.Fatalf("two-poll entries=%d, want equal-a, equal-b, and one late record; calls=%d", got, dataCalls)
			}
			counts := map[string]int{}
			for _, ent := range rt.Entries() {
				var object map[string]any
				if err := json.Unmarshal(ent.Data, &object); err != nil {
					t.Fatal(err)
				}
				counts[fmt.Sprint(object[tc.idField])]++
			}
			for _, id := range []string{"equal-a", "equal-b", "late"} {
				if counts[id] != 1 {
					t.Fatalf("identity %q count=%d, all counts=%v", id, counts[id], counts)
				}
			}
			if dataCalls != 2 {
				t.Fatalf("data calls=%d, want exactly two polls", dataCalls)
			}
			secondState, err := loadState(rt, key)
			if err != nil {
				t.Fatal(err)
			}
			if secondState.Overlap == nil || len(secondState.Overlap.Receipts) != 0 {
				t.Fatalf("completed sliding overlap retained expired receipts: %+v", secondState.Overlap)
			}
		})
	}
}

// Crossing the overlap receipt capacity used to be terminal: the error was
// never persisted and no later poll could produce a different answer, so a
// selector dense enough to fill the horizon stopped collecting for good. The
// bound is now bounded at-least-once recovery -- the newest receipts are kept,
// the replay the dropped ones no longer suppress is reported, and the window
// still completes.
func TestTemporalOverlapCapacityRecoversWithBoundedReplay(t *testing.T) {
	eventTime := time.Date(2026, 10, 7, 11, 54, 0, 0, time.UTC)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		fmt.Fprint(w, valueEnvelope([]string{
			temporalFixtureRecord("activityDateTime", "id", "one", eventTime),
			temporalFixtureRecord("activityDateTime", "id", "two", eventTime.Add(time.Second)),
			temporalFixtureRecord("activityDateTime", "id", "three", eventTime.Add(2*time.Second)),
		}))
	}))
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"entra-directory-audits"}
	p.overlapReceiptLimit = 2
	key := "microsoft/entra-directory-audits"
	priorWatermark := time.Date(2026, 10, 7, 11, 45, 0, 0, time.UTC)
	if err := rt.PutTime(key+"/timestamp", priorWatermark); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatalf("capacity error=%v, want bounded recovery", err)
	}
	// Every record the source published is still delivered: the bound governs
	// how much replay suppression survives, never whether a record is written.
	if got := len(rt.Entries()); got != 3 {
		t.Fatalf("capacity recovery wrote %d entries, want 3", got)
	}
	watermark, err := rt.GetTime(key + "/timestamp")
	if err != nil || !watermark.After(priorWatermark) {
		t.Fatalf("watermark=%s error=%v, want the window to complete past %s", watermark, err, priorWatermark)
	}
	encoded, err := rt.Get(key + "/overlap")
	if err != nil {
		t.Fatalf("capacity recovery did not persist overlap state: %v", err)
	}
	overlap, err := decodeTemporalOverlap(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlap.Receipts) > 2 {
		t.Fatalf("retained %d receipts, want at most the capacity of 2", len(overlap.Receipts))
	}
	// The newest receipts are the useful ones: they are what the next
	// overlapping window can still suppress.
	for _, receipt := range overlap.Receipts {
		if receipt.Timestamp.Before(eventTime.Add(time.Second)) {
			t.Fatalf("retained receipt at %s, want only the newest receipts", receipt.Timestamp)
		}
	}
	if got := rt.logsContaining("WARN", "overlap receipt capacity"); len(got) != 1 {
		t.Fatalf("capacity warnings=%d, want exactly 1 (%+v)", len(got), got)
	}
	if got := rt.warnStateContaining("replay"); len(got) == 0 {
		t.Fatal("no operator-visible warning state was published for bounded replay")
	}
}

// prepareRecords owns the bound itself: duplicate suppression inside the active
// overlap horizon and deterministic ordering both survive the trim.
func TestPrepareRecordsTrimsOverlapReceiptsDeterministically(t *testing.T) {
	window := time.Date(2026, 10, 7, 11, 50, 0, 0, time.UTC)
	dataset := datasets["entra-directory-audits"]
	p := New(validTestConfig(t, "entra-directory-audits"))
	p.overlapReceiptLimit = 2
	state := streamState{WindowStart: window.Add(-time.Minute), WindowEnd: window.Add(time.Minute)}
	records := []FetchedRecord{
		{ID: "oldest", Raw: []byte(`{"id":"oldest"}`), Timestamp: window},
		{ID: "middle", Raw: []byte(`{"id":"middle"}`), Timestamp: window.Add(10 * time.Second)},
		{ID: "newest", Raw: []byte(`{"id":"newest"}`), Timestamp: window.Add(20 * time.Second)},
		{ID: "newest", Raw: []byte(`{"id":"newest"}`), Timestamp: window.Add(20 * time.Second)},
	}
	prepared, overlap, _, _, replayed, dropped, err := p.prepareRecords(dataset, state, records, window)
	if err != nil {
		t.Fatalf("capacity error=%v, want bounded recovery", err)
	}
	// The repeat inside this batch is still suppressed, whatever the receipt
	// set had to drop.
	if len(prepared) != 3 || replayed != 1 {
		t.Fatalf("prepared=%d replayed=%d, want 3 and 1", len(prepared), replayed)
	}
	if dropped != 1 {
		t.Fatalf("dropped=%d, want the single oldest receipt", dropped)
	}
	if len(overlap.Receipts) != 2 {
		t.Fatalf("receipts=%d, want the capacity of 2", len(overlap.Receipts))
	}
	for i, receipt := range overlap.Receipts {
		if receipt.Fingerprint != temporalReceiptFingerprint(records[i+1].Timestamp, records[i+1].ID) {
			t.Fatalf("receipt %d=%+v, want the newest receipts in timestamp order", i, receipt)
		}
	}
	// The retained receipts still suppress their own records on the next
	// overlapping pass.
	state.Overlap = overlap
	_, _, _, _, replayed, _, err = p.prepareRecords(dataset, state, records[1:], window)
	if err != nil || replayed != 3 {
		t.Fatalf("overlap replay suppression=%d error=%v, want 3", replayed, err)
	}
}

func TestTemporalOverlapRecoversLateRecordAfterEmptyPoll(t *testing.T) {
	tests := []struct {
		name, selector, timeField, idField, subscriptionID string
		response                                           func([]string) string
	}{
		{name: "graph", selector: "entra-directory-audits", timeField: "activityDateTime", idField: "id", response: valueEnvelope},
		{name: "azure-activity", selector: "azure-activity", timeField: "eventTimestamp", idField: "eventDataId", subscriptionID: "33333333-3333-4333-8333-333333333333", response: valueEnvelope},
		{name: "advanced-hunting", selector: "defender-xdr-device-events", timeField: "Timestamp", idField: "ReportId", response: resultsEnvelope},
		{name: "fabric", selector: "fabric-activity", timeField: "CreationTime", idField: "Id", response: fabricEnvelope},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			visible := false
			dataCalls := 0
			eventTime := time.Date(2026, 10, 7, 11, 50, 0, 0, time.UTC)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
					fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
					return
				}
				dataCalls++
				records := []string{}
				if visible {
					records = append(records, temporalFixtureRecord(tc.timeField, tc.idField, "late-after-empty", eventTime))
				}
				fmt.Fprint(w, tc.response(records))
			}))
			defer server.Close()

			rt := newMicrosoftTestRuntime(t)
			p := newMicrosoftTestPlugin(t, server, rt)
			p.conf.Api = []string{tc.selector}
			if tc.subscriptionID != "" {
				p.conf.Subscription_ID = []string{tc.subscriptionID}
			}
			key := stateKey("microsoft", tc.selector, tc.subscriptionID)
			if err := rt.PutTime(key+"/timestamp", time.Date(2026, 10, 7, 11, 45, 0, 0, time.UTC)); err != nil {
				t.Fatal(err)
			}
			p.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
			if _, err := p.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			if len(rt.Entries()) != 0 {
				t.Fatal("empty first poll emitted an entry")
			}

			visible = true
			p = newMicrosoftTestPlugin(t, server, rt)
			p.conf.Api = []string{tc.selector}
			if tc.subscriptionID != "" {
				p.conf.Subscription_ID = []string{tc.subscriptionID}
			}
			p.now = func() time.Time { return time.Date(2026, 10, 7, 12, 10, 0, 0, time.UTC) }
			if _, err := p.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			if len(rt.Entries()) != 1 || dataCalls != 2 {
				t.Fatalf("entries=%d dataCalls=%d, want one recovered event in two polls", len(rt.Entries()), dataCalls)
			}
		})
	}
}

func temporalFixtureRecord(timeField, idField, id string, timestamp time.Time) string {
	return fmt.Sprintf(`{"%s":%q,"%s":%q,"DeviceId":"device-%s"}`, idField, id, timeField, timestamp.Format(time.RFC3339Nano), id)
}

func valueEnvelope(records []string) string {
	return `{"value":[` + strings.Join(records, ",") + `]}`
}

func resultsEnvelope(records []string) string {
	return `{"results":[` + strings.Join(records, ",") + `]}`
}

func fabricEnvelope(records []string) string {
	return `{"activityEventEntities":[` + strings.Join(records, ",") + `]}`
}

// The stored overlap document was unmarshaled before any size ceiling applied
// and accepted up to maximumOverlapReceipts receipts whose fingerprints were
// bounded by nothing at all. The count bound says how many receipts a document
// may carry and nothing about how large one is, and a member this build does not
// know is discarded by the decoder and charged against nothing, so the only
// bound on reading one was whatever the state store happened to hold.
//
// temporalTestFingerprint is the canonical form: the lowercase hex encoding of a
// sha256 digest, which is what temporalReceiptFingerprint produces for every
// receipt this plugin records.
func temporalTestFingerprint(seed int) string {
	return fmt.Sprintf("%064x", seed)
}

// temporalTestReceiptJSON encodes one stored receipt, optionally padded with
// interior whitespace. Padding is the amplification the canonical shape does not
// describe: it costs stored bytes to read and to keep, and re-encoding the
// parsed receipt would not show it at all.
func temporalTestReceiptJSON(fingerprint string, pad int) string {
	return `{"timestamp":"2026-10-07T11:56:00Z",` + strings.Repeat(" ", pad) + `"fingerprint":"` + fingerprint + `"}`
}

// temporalTestOverlapDocument assembles a stored overlap document, padding it to
// exactly total bytes with a JSON member this build does not know. That member
// is what makes the pre-decode ceiling load-bearing: it is discarded by the
// decoder, so nothing downstream of the decode can ever charge for it.
func temporalTestOverlapDocument(t *testing.T, receipts []string, total int) []byte {
	t.Helper()
	body := `{"version":1,"receipts":[` + strings.Join(receipts, ",") + `],"unknown_member":"`
	tail := `"}`
	if pad := total - len(body) - len(tail); pad >= 0 {
		return []byte(body + strings.Repeat("a", pad) + tail)
	}
	t.Fatalf("the receipts alone are %d bytes, over the %d-byte target", len(body)+len(tail), total)
	return nil
}

// TestStoredOverlapDocumentCeilingIsExactAndRejectsOneByteOver pins the
// pre-decode ceiling at its exact boundary in both directions, with the
// amplification shape it exists for: a handful of in-budget receipts carried
// inside a document the ignored-member padding has grown to the ceiling.
func TestStoredOverlapDocumentCeilingIsExactAndRejectsOneByteOver(t *testing.T) {
	receipts := []string{
		temporalTestReceiptJSON(temporalTestFingerprint(1), 0),
		temporalTestReceiptJSON(temporalTestFingerprint(2), 0),
	}

	atCeiling := temporalTestOverlapDocument(t, receipts, maximumOverlapDocumentBytes)
	if len(atCeiling) != maximumOverlapDocumentBytes {
		t.Fatalf("document length=%d, want exactly the ceiling %d", len(atCeiling), maximumOverlapDocumentBytes)
	}
	overlap, err := decodeTemporalOverlap(atCeiling)
	if err != nil {
		t.Fatalf("a document at exactly the ceiling was refused: %v", err)
	}
	if len(overlap.Receipts) != len(receipts) {
		t.Fatalf("receipts=%d, want the %d the document carries", len(overlap.Receipts), len(receipts))
	}
	// Non-vacuous: the padding is not charged against the receipt budget, so the
	// ceiling is the only thing that bounded this document.
	charged := 0
	for _, receipt := range receipts {
		charged += len(receipt) + 1
	}
	if charged > maximumOverlapReceiptBytes {
		t.Fatalf("the receipts alone cost %d bytes, so the ceiling claim is vacuous", charged)
	}

	overCeiling := temporalTestOverlapDocument(t, receipts, maximumOverlapDocumentBytes+1)
	if _, err := decodeTemporalOverlap(overCeiling); err == nil {
		t.Fatal("a document one byte over the ceiling was accepted")
	} else if !strings.Contains(err.Error(), "over the") || !strings.Contains(err.Error(), "ceiling") {
		t.Fatalf("error=%v, want the ceiling named for the operator", err)
	}
}

// TestStoredOverlapReceiptsAreBoundedInAggregateInsideTheDocumentCeiling pins
// the retained-byte budget. It is a separate bound from the ceiling and this is
// the region where it is the only one that answers: a document under the
// pre-decode ceiling whose receipts would still cost more to retain than the
// budget allows.
func TestStoredOverlapReceiptsAreBoundedInAggregateInsideTheDocumentCeiling(t *testing.T) {
	const padded = 1 << 20
	receipt := temporalTestReceiptJSON(temporalTestFingerprint(1), 0)
	pad := padded - len(receipt)
	if pad < 0 {
		t.Fatalf("a canonical receipt is already %d bytes", len(receipt))
	}
	// Enough padded receipts to cross the retained-byte budget while the whole
	// document stays under the pre-decode ceiling.
	count := maximumOverlapReceiptBytes/padded + 2
	receipts := make([]string, 0, count)
	for ordinal := range count {
		receipts = append(receipts, temporalTestReceiptJSON(temporalTestFingerprint(ordinal+1), pad))
	}
	document := []byte(`{"version":1,"receipts":[` + strings.Join(receipts, ",") + `]}`)

	// The two halves of the claim: the ceiling did not catch this, and the count
	// bound did not either.
	if len(document) > maximumOverlapDocumentBytes {
		t.Fatalf("document length=%d is over the ceiling %d, so the aggregate claim is vacuous", len(document), maximumOverlapDocumentBytes)
	}
	if len(receipts) > maximumOverlapReceipts {
		t.Fatalf("receipts=%d is over the count bound %d, so the aggregate claim is vacuous", len(receipts), maximumOverlapReceipts)
	}
	if _, err := decodeTemporalOverlap(document); err == nil {
		t.Fatal("a receipt set over the retained-byte budget was adopted")
	} else if !strings.Contains(err.Error(), "budget") {
		t.Fatalf("error=%v, want the retained-byte budget named", err)
	}

	// The same shape trimmed to what the budget admits is still accepted, so the
	// bound is a boundary rather than a refusal of the whole shape.
	within := receipts[:maximumOverlapReceiptBytes/(padded+1)]
	charged := 0
	for _, member := range within {
		charged += len(member) + 1
	}
	if charged > maximumOverlapReceiptBytes {
		t.Fatalf("the reduced set still costs %d bytes", charged)
	}
	accepted, err := decodeTemporalOverlap([]byte(`{"version":1,"receipts":[` + strings.Join(within, ",") + `]}`))
	if err != nil {
		t.Fatalf("a receipt set inside the budget was refused: %v", err)
	}
	if len(accepted.Receipts) != len(within) {
		t.Fatalf("receipts=%d, want %d", len(accepted.Receipts), len(within))
	}
}

// TestStoredOverlapReceiptShapeIsCanonical pins exactly which fingerprints a
// stored receipt may carry. Normal fingerprints are canonical lowercase sha256
// hex, so that exact form is what a restored one has to be: a key no record this
// plugin fetches can produce suppresses nothing and can only occupy budget, and
// the same digest in two cases would be two keys for one record.
func TestStoredOverlapReceiptShapeIsCanonical(t *testing.T) {
	canonical := temporalReceiptFingerprint(time.Date(2026, 10, 7, 11, 56, 0, 0, time.UTC), "record-1")
	if len(canonical) != temporalFingerprintLength {
		t.Fatalf("a fingerprint this plugin records is %d characters, want %d", len(canonical), temporalFingerprintLength)
	}
	if !canonicalTemporalFingerprint(canonical) {
		t.Fatalf("the fingerprint this plugin records, %q, is not accepted as canonical", canonical)
	}
	// The undated keying is the same shape, so it survives a restart too.
	if !canonicalTemporalFingerprint(undatedReceiptFingerprint("record-1")) {
		t.Fatal("the undated receipt fingerprint is not accepted as canonical")
	}

	for _, tc := range []struct {
		name        string
		fingerprint string
		want        bool
	}{
		{"canonical", canonical, true},
		{"all-digits", strings.Repeat("0", temporalFingerprintLength), true},
		{"all-letters", strings.Repeat("f", temporalFingerprintLength), true},
		{"uppercase", strings.Repeat("A", temporalFingerprintLength), false},
		{"mixed-case", strings.Repeat("a", temporalFingerprintLength-1) + "A", false},
		{"one-short", canonical[:temporalFingerprintLength-1], false},
		{"one-long", canonical + "0", false},
		{"empty", "", false},
		{"non-hexadecimal", strings.Repeat("g", temporalFingerprintLength), false},
		{"prefixed", "sha256:" + canonical[:temporalFingerprintLength-7], false},
		// The shape the count bound alone admitted: an arbitrarily long
		// nonempty key.
		{"arbitrarily-long", strings.Repeat("a", 64<<10), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalTemporalFingerprint(tc.fingerprint); got != tc.want {
				t.Fatalf("canonicalTemporalFingerprint(%d characters)=%t, want %t", len(tc.fingerprint), got, tc.want)
			}
			document := []byte(`{"version":1,"receipts":[` + temporalTestReceiptJSON(tc.fingerprint, 0) + `]}`)
			overlap, err := decodeTemporalOverlap(document)
			if tc.want {
				if err != nil {
					t.Fatalf("a canonical receipt was refused: %v", err)
				}
				if len(overlap.Receipts) != 1 || overlap.Receipts[0].Fingerprint != tc.fingerprint {
					t.Fatalf("decoded receipts=%+v, want the stored fingerprint verbatim", overlap.Receipts)
				}
				return
			}
			if err == nil {
				t.Fatal("a receipt whose fingerprint is not a canonical digest was adopted")
			}
			// The value itself is never named back to the operator.
			if tc.fingerprint != "" && strings.Contains(err.Error(), tc.fingerprint) {
				t.Fatalf("error=%v names the stored fingerprint", err)
			}
		})
	}

	// A receipt with no timestamp survives no prune -- pruneTemporalOverlap keys
	// on it and the zero time is before every horizon -- yet would have been
	// trusted to suppress delivery for the pass that read it.
	undated := []byte(`{"version":1,"receipts":[{"timestamp":"0001-01-01T00:00:00Z","fingerprint":"` + canonical + `"}]}`)
	if _, err := decodeTemporalOverlap(undated); err == nil || !strings.Contains(err.Error(), "no timestamp") {
		t.Fatalf("error=%v, want a receipt with no timestamp refused", err)
	}
}

// TestValidOverlapStateSurvivesSaveAndLoadRoundTrip is the preservation side.
// Every bound added here has to leave a document the plugin itself wrote
// completely intact, including the receipts that suppress replay across the
// sliding horizon.
func TestValidOverlapStateSurvivesSaveAndLoadRoundTrip(t *testing.T) {
	rt := newMicrosoftTestRuntime(t)
	key := stateKey("microsoft", "entra-directory-audits", "")
	window := time.Date(2026, 10, 7, 11, 55, 0, 0, time.UTC)
	state := streamState{
		Watermark:   window,
		WindowStart: window.Add(-time.Hour), WindowEnd: window,
		Overlap: &temporalOverlap{Version: temporalOverlapVersion, Receipts: []temporalReceipt{
			{Timestamp: window.Add(-time.Minute), Fingerprint: temporalReceiptFingerprint(window.Add(-time.Minute), "record-1")},
			{Timestamp: window.Add(-2 * time.Minute), Fingerprint: undatedReceiptFingerprint("record-2")},
		}},
	}
	if err := saveState(rt, key, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadState(rt, key)
	if err != nil {
		t.Fatalf("a document this plugin wrote was refused on load: %v", err)
	}
	if loaded.Overlap == nil || len(loaded.Overlap.Receipts) != len(state.Overlap.Receipts) {
		t.Fatalf("loaded overlap=%+v, want both receipts", loaded.Overlap)
	}
	for index, receipt := range loaded.Overlap.Receipts {
		want := state.Overlap.Receipts[index]
		if receipt.Fingerprint != want.Fingerprint || !receipt.Timestamp.Equal(want.Timestamp) {
			t.Fatalf("receipt %d=%+v, want %+v", index, receipt, want)
		}
	}

	// The persist path enforces the same ceiling before the first Put, so a
	// document the next load would refuse is never the durable one.
	state.Overlap.Receipts = append(state.Overlap.Receipts, temporalReceipt{
		Timestamp:   window.Add(-3 * time.Minute),
		Fingerprint: temporalTestFingerprint(3),
	})
	before, err := rt.Get(key + "/overlap")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveState(rt, key, state); err != nil {
		t.Fatalf("a document inside the ceiling was refused on save: %v", err)
	}
	after, err := rt.Get(key + "/overlap")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) > maximumOverlapDocumentBytes {
		t.Fatalf("persisted overlap document is %d bytes, over the %d-byte ceiling", len(after), maximumOverlapDocumentBytes)
	}
	if string(before) == string(after) {
		t.Fatal("the third receipt was not persisted, so the save-path claim is vacuous")
	}
	reloaded, err := loadState(rt, key)
	if err != nil {
		t.Fatalf("the persisted document was refused on reload: %v", err)
	}
	if reloaded.Overlap == nil || len(reloaded.Overlap.Receipts) != 3 {
		t.Fatalf("reloaded overlap=%+v, want all three receipts", reloaded.Overlap)
	}
}

// temporalDenseOverlapDocument builds a stored overlap document whose receipt
// array holds members copies of one JSON member.
func temporalDenseOverlapDocument(member string, members int) string {
	var document strings.Builder
	document.Grow(len(member)*members + members + 32)
	document.WriteString(`{"version":1,"receipts":[`)
	for ordinal := range members {
		if ordinal != 0 {
			document.WriteString(",")
		}
		document.WriteString(member)
	}
	document.WriteString(`]}`)
	return document.String()
}

// The stored overlap receipt array was fully materialized before its
// element-count check ran. Holding the members as []json.RawMessage charged the
// aggregate budget against the right bytes, but json.Unmarshal still built the
// whole slice -- one 24-byte header plus a copy of its bytes per member --
// before either the count bound or the budget was consulted, so a document
// inside the pre-decode ceiling but dense in two-byte members allocated an
// order of magnitude more members than the count bound admits, and hundreds of
// megabytes with them (CWE-770).
//
// TestStoredOverlapReceiptsApplyTheirCountBoundBeforeRetention pins the count
// boundary in both directions and pins the dense case the boundary alone cannot
// describe.
func TestStoredOverlapReceiptsApplyTheirCountBoundBeforeRetention(t *testing.T) {
	canonical := temporalTestReceiptJSON(temporalTestFingerprint(1), 0)

	t.Run("maximum", func(t *testing.T) {
		document := temporalDenseOverlapDocument(canonical, maximumOverlapReceipts)
		// Non-vacuous: neither the pre-decode ceiling nor the aggregate budget
		// is what answers here, so the count bound is.
		if len(document) > maximumOverlapDocumentBytes {
			t.Fatalf("a document at the count bound is %d bytes, over the %d-byte ceiling",
				len(document), maximumOverlapDocumentBytes)
		}
		if charged := maximumOverlapReceipts * (len(canonical) + 1); charged > maximumOverlapReceiptBytes {
			t.Fatalf("a set at the count bound costs %d bytes, over the %d-byte budget",
				charged, maximumOverlapReceiptBytes)
		}
		overlap, err := decodeTemporalOverlap([]byte(document))
		if err != nil {
			t.Fatalf("a set at exactly the count bound was refused: %v", err)
		}
		if len(overlap.Receipts) != maximumOverlapReceipts {
			t.Fatalf("receipts=%d, want the %d the document carries", len(overlap.Receipts), maximumOverlapReceipts)
		}
	})

	t.Run("maximum-plus-one", func(t *testing.T) {
		document := temporalDenseOverlapDocument(canonical, maximumOverlapReceipts+1)
		if len(document) > maximumOverlapDocumentBytes {
			t.Fatalf("a document one member over the count bound is %d bytes, over the %d-byte ceiling",
				len(document), maximumOverlapDocumentBytes)
		}
		if _, err := decodeTemporalOverlap([]byte(document)); err == nil {
			t.Fatal("a set one member over the count bound was adopted")
		} else if !strings.Contains(err.Error(), fmt.Sprintf("exceeds receipt capacity %d", maximumOverlapReceipts)) {
			t.Fatalf("error=%v, want the count bound named for the operator", err)
		}
	})

	// A document dense in members no receipt shape can hold is the shape the
	// old decode materialized wholesale. It has to be refused on the member it
	// fails at, having retained nothing.
	t.Run("dense-past-every-bound", func(t *testing.T) {
		const member = "0"
		members := (maximumOverlapDocumentBytes - len(temporalDenseOverlapDocument(member, 1)) + len(member) + 1) / (len(member) + 1)
		document := temporalDenseOverlapDocument(member, members)
		if len(document) > maximumOverlapDocumentBytes {
			t.Fatalf("the dense document is %d bytes, over the %d-byte ceiling, so the ceiling would catch it instead",
				len(document), maximumOverlapDocumentBytes)
		}
		if members <= 10*maximumOverlapReceipts {
			t.Fatalf("the dense document carries %d members against a bound of %d, which is not dense enough for the claim",
				members, maximumOverlapReceipts)
		}

		// The document is materialized before the measurement starts, so what
		// is counted is the cost of reading it rather than of being handed it.
		encoded := []byte(document)
		var overlap *temporalOverlap
		var err error
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		overlap, err = decodeTemporalOverlap(encoded)
		runtime.ReadMemStats(&after)
		allocated := after.TotalAlloc - before.TotalAlloc

		if err == nil {
			t.Fatalf("a document of %d unusable members was adopted: %+v", members, overlap)
		}
		if overlap != nil {
			t.Fatalf("a refused overlap document was retained: %d receipts", len(overlap.Receipts))
		}
		// Materializing the array first allocated a 24-byte header and a copy
		// of its bytes for every member before any bound was consulted. Reading
		// it a member at a time stops at the first one that cannot be a
		// receipt, so the whole refusal costs a few kilobytes of decoder
		// buffer.
		const ceiling = 1 << 20
		if unbounded := uint64(members) * 24; ceiling >= unbounded/4 {
			t.Fatalf("the %d-byte ceiling is not meaningfully below the %d bytes the member headers alone cost", ceiling, unbounded)
		}
		if allocated > ceiling {
			t.Fatalf("refusing a document of %d dense members allocated %d bytes against a %d-byte ceiling: the array was materialized before the bound applied",
				members, allocated, ceiling)
		}
	})

	// Preservation: everything else this decode already refused or accepted is
	// unchanged.
	t.Run("shape-and-trailing-data", func(t *testing.T) {
		for _, tc := range []struct{ name, document, want string }{
			{"trailing-data", `{"version":1,"receipts":[]} {}`, "decode Microsoft overlap state"},
			{"receipts-is-an-object", `{"version":1,"receipts":{"a":1}}`, "must be a JSON array"},
			{"unsupported-version", `{"version":2,"receipts":[]}`, "unsupported version 2"},
			{"truncated", `{"version":1,"receipts":[`, "decode Microsoft overlap state"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := decodeTemporalOverlap([]byte(tc.document)); err == nil ||
					!strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error=%v, want one naming %q", err, tc.want)
				}
			})
		}
		// An absent array, an explicit null and an empty one all stay the empty
		// answer a document this plugin writes for a horizon it holds nothing
		// in, and an unknown member is still ignored.
		for _, document := range []string{
			`{"version":1}`, `{"version":1,"receipts":null}`, `{"version":1,"receipts":[]}`,
			`{"version":1,"unknown_member":"x","receipts":[]}`,
		} {
			overlap, err := decodeTemporalOverlap([]byte(document))
			if err != nil || overlap == nil || len(overlap.Receipts) != 0 || overlap.Version != temporalOverlapVersion {
				t.Fatalf("document %s: overlap=%+v error=%v", document, overlap, err)
			}
		}
	})
}
