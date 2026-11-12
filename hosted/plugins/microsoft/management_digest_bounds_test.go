package microsoft

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// managementDigestEntryBound is the per-entry delivery bound this file pins
// independently of the production constant: the digest has to refuse exactly
// the events prepareManagementEvent refuses, so a silent change to either side
// of that lockstep has to fail here rather than reappear as the amplification
// the bound exists to prevent.
const managementDigestEntryBound = 1 << 20

// managementHTMLRichPayload returns a JSON-string-safe payload of about size
// bytes consisting only of the three characters encoding/json escapes into
// six-byte sequences by default. Canonicalizing it with default escaping
// amplifies it roughly six-fold, which is the whole of the exhaustion the
// per-entry bound and the non-escaping encoder have to keep out of the digest.
func managementHTMLRichPayload(size int) string {
	return strings.Repeat("<&>", size/3+1)
}

// managementHTMLRichEvent returns one well formed content event whose raw
// length is at least size bytes and which is dense in '<', '>' and '&'.
func managementHTMLRichEvent(t *testing.T, id string, size int) string {
	t.Helper()
	event := fmt.Sprintf(`{"CreationTime":"2026-10-08T11:40:00Z","Id":%q,"Operation":"MailItemsAccessed","payload":%q}`,
		id, managementHTMLRichPayload(size))
	if len(event) < size {
		t.Fatalf("event length=%d, want at least %d", len(event), size)
	}
	return event
}

// managementDigestOracle recomputes the content digest the way the repaired
// production path has to compute it, written out independently here: one
// SHA-256 fold over per-event frames, each carrying a one-byte kind and an
// eight-byte big-endian length. An event past the per-entry bound contributes a
// fixed-size marker derived from the raw event -- its length and its SHA-256 --
// and never a copy or a canonical form of the oversized payload.
//
// The literal frame tags and the literal bound keep this an oracle rather than
// a restatement of the production constants, and the per-event framing means a
// digest built from one whole canonical array cannot match it.
func managementDigestOracle(t *testing.T, escapeHTML bool, events ...json.RawMessage) string {
	t.Helper()
	hash := sha256.New()
	var frame [9]byte
	for _, raw := range events {
		if len(raw) > managementDigestEntryBound {
			frame[0] = 'O'
			binary.BigEndian.PutUint64(frame[1:], uint64(len(raw)))
			marker := sha256.Sum256(raw)
			hash.Write(frame[:])
			hash.Write(marker[:])
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatalf("oracle decode of %q: %v", raw, err)
		}
		var canonical bytes.Buffer
		encoder := json.NewEncoder(&canonical)
		encoder.SetEscapeHTML(escapeHTML)
		if err := encoder.Encode(value); err != nil {
			t.Fatalf("oracle encode of %q: %v", raw, err)
		}
		encoded := bytes.TrimSuffix(canonical.Bytes(), []byte("\n"))
		frame[0] = 'C'
		binary.BigEndian.PutUint64(frame[1:], uint64(len(encoded)))
		hash.Write(frame[:])
		hash.Write(encoded)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// managementDigestAllocations reports the bytes the digest allocated, which is
// what bounds whether a blob inside the response bound can be processed at all.
func managementDigestAllocations(t *testing.T, events ...json.RawMessage) (string, uint64) {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	digest, err := managementContentDigest(events)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("digest error=%v", err)
	}
	return digest, after.TotalAlloc - before.TotalAlloc
}

// TestManagementContentDigestMarksOversizedEventsWithoutMaterializingThem pins
// the repair of the unbounded semantic digest. fetchManagementContent decoded a
// whole content array and digested it before prepareManagementEvent applied the
// 1 MiB per-entry bound, and the digest retained every event as a []any and
// marshalled one complete canonical array with encoding/json's default HTML
// escaping. A single well formed event inside the 16 MiB response bound, dense
// in '<', '>' and '&', therefore coexisted as the response body, the copied
// RawMessage, the decoded graph and a roughly six-times-larger canonical array,
// exhausting memory before any durable progress was recorded -- and because
// nothing advanced, every later poll repeated it.
//
// The bound now applies before any event is materialized, and an event past it
// folds in a fixed-size raw-derived marker instead.
func TestManagementContentDigestMarksOversizedEventsWithoutMaterializingThem(t *testing.T) {
	if managementMaximumEntrySize != managementDigestEntryBound {
		t.Fatalf("per-entry bound=%d, want the %d bytes this file pins", managementMaximumEntrySize, managementDigestEntryBound)
	}
	const oversize = 4 << 20
	event := json.RawMessage(managementHTMLRichEvent(t, "oversized", oversize))
	if !managementEventOversized(event) {
		t.Fatalf("event length=%d, want it past the per-entry bound", len(event))
	}
	if _, _, err := prepareManagementEvent(datasets["microsoft365-audit-exchange"], event, managementTestNow); err == nil {
		t.Fatal("delivery accepted an event past the per-entry bound")
	}

	digest, allocated := managementDigestAllocations(t, event)
	if want := managementDigestOracle(t, false, event); digest != want {
		t.Fatalf("digest=%s, want the fixed-size raw marker %s", digest, want)
	}
	// The marker is 41 bytes of frame and hash per event, so the digest cannot
	// need a copy of the payload, let alone a canonical form of it. Bounding the
	// allocation by one further copy of the raw event leaves the repaired path
	// three orders of magnitude of room and still refuses the decoded graph plus
	// canonical array the vulnerable digest built.
	if limit := uint64(len(event)); allocated > limit {
		t.Fatalf("digest allocated %d bytes for a %d-byte event, want at most %d", allocated, len(event), limit)
	}

	// Deterministic change detection still has to come out of the raw event: the
	// same bytes digest identically on every call, and any change to them -- in
	// length or in content -- moves the digest.
	repeat, err := managementContentDigest([]json.RawMessage{event})
	if err != nil || repeat != digest {
		t.Fatalf("repeat digest=%s error=%v, want %s", repeat, err, digest)
	}
	altered := json.RawMessage(string(event[:len(event)-3]) + `x"}`)
	if len(altered) != len(event) || bytes.Equal(altered, event) {
		t.Fatalf("mutated length=%d want %d, identical=%v", len(altered), len(event), bytes.Equal(altered, event))
	}
	mutated, err := managementContentDigest([]json.RawMessage{altered})
	if err != nil {
		t.Fatal(err)
	}
	if mutated == digest {
		t.Fatal("digest did not change when the oversized raw event changed")
	}
	reserialized := json.RawMessage(" " + string(event))
	spaced, err := managementContentDigest([]json.RawMessage{reserialized})
	if err != nil {
		t.Fatal(err)
	}
	if spaced == digest {
		t.Fatal("oversized marker was derived from a canonical form rather than the raw event")
	}
}

// TestManagementContentDigestStaysSemanticForAcceptedEvents keeps the property
// the digest exists for: an accepted event is identified by its JSON value, so
// a different backend node re-serializing the same blob cannot reset an
// in-progress ordinal. It also pins the two things the streaming fold has to
// get right -- no default HTML escaping, and a length-delimited frame per event
// so no two event sequences can fold to the same bytes.
func TestManagementContentDigestStaysSemanticForAcceptedEvents(t *testing.T) {
	equivalent := []struct {
		name  string
		left  string
		right string
	}{
		{"member-order", `{"a":1,"b":"<&>","c":{"x":true,"y":null}}`, `{"c":{"y":null,"x":true},"b":"<&>","a":1}`},
		{"insignificant-whitespace", `{"a": 1 , "b" : [ 1, 2 ]}`, "{\n\t\"a\":1,\"b\":[1,\n2]}"},
		{"escape-representation", `{"a":"<&>/"}`, `{"a":"<&>\/"}`},
		{"duplicate-member-last-wins", `{"a":1,"a":2}`, `{"a":2}`},
	}
	for _, tc := range equivalent {
		t.Run(tc.name, func(t *testing.T) {
			left, err := managementContentDigest([]json.RawMessage{json.RawMessage(tc.left)})
			if err != nil {
				t.Fatal(err)
			}
			right, err := managementContentDigest([]json.RawMessage{json.RawMessage(tc.right)})
			if err != nil {
				t.Fatal(err)
			}
			if left != right {
				t.Fatalf("digest %s != %s for semantically identical events", left, right)
			}
			if want := managementDigestOracle(t, false, json.RawMessage(tc.left)); left != want {
				t.Fatalf("digest=%s, want the per-event canonical frame %s", left, want)
			}
		})
	}

	// A change in value still has to move the digest, or change detection is
	// vacuous.
	changed, err := managementContentDigest([]json.RawMessage{json.RawMessage(`{"a":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := managementContentDigest([]json.RawMessage{json.RawMessage(`{"a":2}`)})
	if err != nil {
		t.Fatal(err)
	}
	if changed == unchanged {
		t.Fatal("digest ignored a changed member value")
	}

	// Default escaping would make the canonical form of an HTML-rich event six
	// times its own size. The accepted event is canonicalized without it, so the
	// escaped fold has to be a different digest.
	rich := json.RawMessage(managementHTMLRichEvent(t, "accepted", 4<<10))
	if managementEventOversized(rich) {
		t.Fatalf("event length=%d, want it inside the per-entry bound", len(rich))
	}
	digest, err := managementContentDigest([]json.RawMessage{rich})
	if err != nil {
		t.Fatal(err)
	}
	if want := managementDigestOracle(t, false, rich); digest != want {
		t.Fatalf("digest=%s, want the unescaped canonical frame %s", digest, want)
	}
	if escaped := managementDigestOracle(t, true, rich); digest == escaped {
		t.Fatal("accepted event was canonicalized with default HTML escaping")
	}

	// Length-delimited frames are what keep two different event sequences from
	// folding to the same canonical bytes; without them both of these fold the
	// digits 1, 2 and 3 in the same order.
	split, err := managementContentDigest([]json.RawMessage{json.RawMessage(`1`), json.RawMessage(`23`)})
	if err != nil {
		t.Fatal(err)
	}
	regrouped, err := managementContentDigest([]json.RawMessage{json.RawMessage(`12`), json.RawMessage(`3`)})
	if err != nil {
		t.Fatal(err)
	}
	if split == regrouped {
		t.Fatal("digest frames are not length-delimited across event boundaries")
	}

	// Event order and the mix of accepted and skipped events both have to be
	// carried, and a multi-event content must not retain a second whole-content
	// graph or one complete canonical array while doing it.
	oversized := json.RawMessage(managementHTMLRichEvent(t, "mixed-oversized", 2<<20))
	mixed := []json.RawMessage{rich, oversized, json.RawMessage(`{"Id":"tail"}`)}
	mixedDigest, allocated := managementDigestAllocations(t, mixed...)
	if want := managementDigestOracle(t, false, mixed...); mixedDigest != want {
		t.Fatalf("mixed digest=%s, want %s", mixedDigest, want)
	}
	reordered, err := managementContentDigest([]json.RawMessage{mixed[1], mixed[0], mixed[2]})
	if err != nil {
		t.Fatal(err)
	}
	if reordered == mixedDigest {
		t.Fatal("digest ignored event order")
	}
	// Only the accepted events can be materialized at all, one at a time, so the
	// multi-megabyte event in the middle may not contribute allocation anywhere
	// near its own size -- let alone the six-fold canonical form the vulnerable
	// digest built for it before delivery refused it.
	if limit := uint64(len(oversized) / 4); allocated > limit {
		t.Fatalf("digest allocated %d bytes around a %d-byte oversized event, want at most %d", allocated, len(oversized), limit)
	}
}

// TestManagementStreamingDigestBoundsManyAcceptedEvents pins the fold itself:
// the digest of a content whose events are all inside the per-entry bound may
// not cost a retained decode of the whole array plus one complete canonical
// array of it.
func TestManagementStreamingDigestBoundsManyAcceptedEvents(t *testing.T) {
	const events = 64
	content := make([]json.RawMessage, 0, events)
	total := 0
	for i := 0; i < events; i++ {
		event := json.RawMessage(managementHTMLRichEvent(t, fmt.Sprintf("accepted-%02d", i), 16<<10))
		if managementEventOversized(event) {
			t.Fatalf("event %d length=%d, want it inside the per-entry bound", i, len(event))
		}
		content = append(content, event)
		total += len(event)
	}
	digest, allocated := managementDigestAllocations(t, content...)
	if want := managementDigestOracle(t, false, content...); digest != want {
		t.Fatalf("digest=%s, want %s", digest, want)
	}
	// Folding one event at a time costs that event's decode and one reused
	// canonical buffer -- about five times the content in total, almost all of it
	// the per-event decoder buffer. Retaining the whole decoded array and then
	// marshalling it as one HTML-escaped canonical array costs roughly another
	// twelve times the content on top of that, which this bound refuses.
	if limit := uint64(9 * total); allocated > limit {
		t.Fatalf("digest allocated %d bytes for %d bytes of content, want at most %d", allocated, total, limit)
	}
}

// managementOversizedEventServer lists one descriptor inside the window it is
// asked for and serves body for it, counting every content fetch.
func managementOversizedEventServer(t *testing.T, content managementCapacityContent, body string, fetches *atomic.Int64) *httptest.Server {
	t.Helper()
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
			if content.created.Before(start) || !content.created.Before(end) {
				_, _ = io.WriteString(w, `[]`)
				return
			}
			_, _ = io.WriteString(w, "["+managementDescriptorAt(server.URL, "Audit.Exchange", content)+"]")
		case strings.Contains(r.URL.Path, managementCapacityContentPath):
			if id := strings.TrimPrefix(r.URL.Path, managementCapacityContentPath); id != content.id {
				http.NotFound(w, r)
				return
			}
			fetches.Add(1)
			_, _ = io.WriteString(w, body)
		default:
			http.NotFound(w, r)
		}
	})
	return server
}

// TestManagementOversizedHTMLRichEventMakesBoundedDurableProgress is the
// end-to-end regression for the amplification. One well formed event of several
// megabytes, dense in the characters default canonicalization expands six-fold,
// used to be digested in full before the per-entry bound refused it: the poll
// could exhaust memory, and because nothing durable advanced, the next poll
// fetched the same blob and did it again. The blob now has to be processed
// within a bound close to its own size, the event has to be skipped with a
// durable receipt and a completed window, and the next pass must not fetch it
// again.
func TestManagementOversizedHTMLRichEventMakesBoundedDurableProgress(t *testing.T) {
	content := managementCapacityContent{id: "content-oversized", created: managementTestNow.Add(-30 * time.Minute)}
	event := managementHTMLRichEvent(t, "oversized-delivery", 8<<20)
	body := "[" + event + "]"
	if len(body) >= managementMaximumResponseBody {
		t.Fatalf("body length=%d, want it inside the %d byte response bound", len(body), managementMaximumResponseBody)
	}
	var fetches atomic.Int64
	server := managementOversizedEventServer(t, content, body, &fetches)
	defer server.Close()

	// The digest is what the fetch does with this blob between decoding it and
	// delivery refusing the event, and it is the step that amplified: measured
	// against the real body, it may not cost even one further copy of the blob.
	// The vulnerable digest decoded it and built a canonical form roughly six
	// times its size here, an order of magnitude past this bound, before the
	// per-entry bound was consulted at all.
	decoded, err := decodeManagementEvents([]byte(body), managementMaximumEventsPerContent)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 1 || !managementEventOversized(decoded[0]) {
		t.Fatalf("decoded events=%d, want one event past the per-entry bound", len(decoded))
	}
	bodyDigest, allocated := managementDigestAllocations(t, decoded...)
	if limit := uint64(len(body)); allocated > limit {
		t.Fatalf("digesting the blob allocated %d bytes for %d bytes of content, want at most %d", allocated, len(body), limit)
	}
	if want := managementDigestOracle(t, false, decoded...); bodyDigest != want {
		t.Fatalf("blob digest=%s, want the fixed-size raw marker %s", bodyDigest, want)
	}

	rt := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	drainManagementSelector(t, plugin, rt)

	if got := fetches.Load(); got != 1 {
		t.Fatalf("content fetches=%d, want exactly one", got)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries=%d, want the oversized event skipped rather than written", got)
	}

	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	state, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if state.SkippedEvents != 1 {
		t.Fatalf("skipped=%d, want the one oversized event accounted for", state.SkippedEvents)
	}
	if state.CompletedThrough.IsZero() || state.hasPending() {
		t.Fatalf("state=%+v, want the window completed and nothing pending", state)
	}
	if len(state.Receipts) != 1 || state.Receipts[0].ContentID != content.id {
		t.Fatalf("receipts=%+v, want a durable receipt for the skipped blob", state.Receipts)
	}
	if got := rt.logsContaining("WARN", "skipped after a bounded decode failure"); len(got) != 1 {
		t.Fatalf("skip warnings=%d, want exactly one", len(got))
	}
	if got := rt.warnStateContaining("skipped 1 malformed or oversized events"); len(got) == 0 {
		t.Fatal("operator-visible skip state was not published")
	}

	// No repeat loop: the durable receipt and completed boundary mean the next
	// pass cannot fetch the same blob again.
	completed := state.CompletedThrough
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("follow-up poll error=%v", err)
	}
	if got := fetches.Load(); got != 1 {
		t.Fatalf("content fetches=%d after the follow-up poll, want the blob never fetched twice", got)
	}
	final, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if final.SkippedEvents != 1 || !final.CompletedThrough.Equal(completed) || len(rt.Entries()) != 0 {
		t.Fatalf("final state=%+v entries=%d, want no repeated work", final, len(rt.Entries()))
	}
}

// seedManagementOversizedDeliveryState writes the durable documents of a
// selector that has listed one blob, recorded digest for it and delivered none
// of it, so the next poll has to agree with that digest to avoid a replay.
func seedManagementOversizedDeliveryState(t *testing.T, rt *microsoftTestRuntime, key, serverURL string, content managementCapacityContent, digest string) {
	t.Helper()
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
		ActiveContentID: content.id, ActiveDigest: digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(managementProgressKey(key), progress); err != nil {
		t.Fatal(err)
	}
}

// TestManagementOversizedEventDigestSurvivesRestartWithoutReplay pins the
// marker inside the production path rather than only at the digest function. A
// restart resumes against the digest the previous pass recorded, so the marker
// a skipped event contributes has to be reproduced exactly: if the oversized
// payload were canonicalized here instead -- the amplification this repair
// removes -- the recorded digest would no longer match and every restart would
// burn a bounded content replay on a blob that delivers nothing.
func TestManagementOversizedEventDigestSurvivesRestartWithoutReplay(t *testing.T) {
	content := managementCapacityContent{id: "content-oversized-restart", created: managementTestNow.Add(-30 * time.Minute)}
	event := managementHTMLRichEvent(t, "oversized-restart", 2<<20)
	body := "[" + event + "]"
	var fetches atomic.Int64
	server := managementOversizedEventServer(t, content, body, &fetches)
	defer server.Close()

	rt := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	digest := managementDigestOracle(t, false, json.RawMessage(event))
	seedManagementOversizedDeliveryState(t, rt, key, server.URL, content, digest)

	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("resumed poll error=%v", err)
	}
	if got := rt.logsContaining("WARN", "content changed while partially delivered"); len(got) != 0 {
		t.Fatalf("replay warnings=%v, want the recorded marker reproduced exactly", got)
	}
	if got := fetches.Load(); got != 1 {
		t.Fatalf("content fetches=%d, want exactly one", got)
	}
	if got := len(rt.Entries()); got != 0 {
		t.Fatalf("entries=%d, want the oversized event skipped", got)
	}
	state, err := loadManagementState(rt, key)
	if err != nil {
		t.Fatal(err)
	}
	if state.SkippedEvents != 1 || state.ActiveDigestChanges != 0 {
		t.Fatalf("state=%+v, want one skipped event and no content replay", state)
	}
	if !state.CompletedThrough.Equal(managementTestNow) || state.hasPending() {
		t.Fatalf("state=%+v, want the seeded window completed at its own end", state)
	}
}
