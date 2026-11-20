package microsoft

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestListEnvelopeMustContainArray(t *testing.T) {
	for _, family := range []struct {
		kind  Kind
		field string
	}{{KindODataGET, "value"}, {KindResourceGraph, "data"}, {KindFabricActivity, "activityEventEntities"}} {
		var dataset Dataset
		for _, d := range datasets {
			if d.Kind == family.kind {
				dataset = d
				break
			}
		}
		for _, tc := range []struct {
			name, body string
			valid      bool
		}{{"missing", `{}`, false}, {"null-body", `null`, false}, {"null-array", fmt.Sprintf(`{"%s":null}`, family.field), false}, {"wrong-type", fmt.Sprintf(`{"%s":{}}`, family.field), false}, {"empty", fmt.Sprintf(`{"%s":[]}`, family.field), true}} {
			t.Run(string(family.kind)+"/"+tc.name, func(t *testing.T) {
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/token") {
						fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
						return
					}
					fmt.Fprint(w, tc.body)
				}))
				defer server.Close()
				conf := testClientConfig(t, server.URL)
				conf.Subscription_ID = []string{"33333333-3333-3333-3333-333333333333"}
				client, err := NewClient(conf, nil)
				if err != nil {
					t.Fatal(err)
				}
				client.http = server.Client()
				now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
				_, err = client.Fetch(context.Background(), dataset, now.Add(-time.Hour), now, conf.Subscription_ID[0])
				if (err == nil) != tc.valid {
					t.Fatalf("valid=%v error=%v", tc.valid, err)
				}
			})
		}
	}
}

func TestMalformedEnvelopeDoesNotAdvanceCheckpoint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	plugin := newMicrosoftTestPlugin(t, server, rt)
	dataset := datasets["entra-directory-audits"]
	key := stateKey(plugin.conf.StateNamespace(), dataset.Name, "")
	before := streamState{Watermark: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)}
	if err := saveState(rt, key, before); err != nil {
		t.Fatal(err)
	}
	priorTimestamp, err := rt.Get(key + "/timestamp")
	if err != nil {
		t.Fatal(err)
	}
	priorCursor, err := rt.Get(key + "/cursor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.collect(context.Background(), rt, dataset, ""); err == nil {
		t.Error("malformed envelope accepted")
	}
	gotTimestamp, _ := rt.Get(key + "/timestamp")
	gotCursor, _ := rt.Get(key + "/cursor")
	if string(gotTimestamp) != string(priorTimestamp) || string(gotCursor) != string(priorCursor) {
		t.Fatal("malformed envelope advanced persisted checkpoint")
	}
}

func TestMalformedRecordDoesNotStarveLaterValidRecord(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		fmt.Fprint(w, `{"value":[null,{"id":"valid"}]}`)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := client.FetchChunk(context.Background(), datasets["entra-users"], time.Now().Add(-time.Hour), time.Now(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Malformed != 1 || len(chunk.Records) != 1 || chunk.Records[0].ID != "valid" {
		t.Fatalf("chunk=%+v", chunk)
	}
}

// envelopeRepeatTrackingMembers is large enough that a map keyed by every
// member name costs far more than envelopeRepeatTrackingMargin, which is what
// makes the comparison below a measurement rather than noise. Each member is a
// fixed thirteen bytes, so the two bodies the test builds are byte-for-byte the
// same length and cost the decoder the same tokens.
const (
	envelopeRepeatTrackingMembers = 600000
	envelopeRepeatTrackingMargin  = 8 << 20
)

// envelopeIrrelevantMemberBody builds one top-level object carrying members
// irrelevant members ahead of the value array and the nextLink continuation.
// distinct names every irrelevant member separately; otherwise one name is
// repeated, which the envelope has always accepted because repeat detection
// only ever spoke for the array and the captured members.
func envelopeIrrelevantMemberBody(members int, distinct bool) []byte {
	var body strings.Builder
	body.Grow(members*13 + 128)
	body.WriteByte('{')
	for member := 0; member < members; member++ {
		name := 0
		if distinct {
			name = member
		}
		fmt.Fprintf(&body, `"a%07d":0,`, name)
	}
	body.WriteString(`"value":[1,2],"nextLink":"https://graph.microsoft.com/next"}`)
	return []byte(body.String())
}

// measureEnvelopeAllocation reports the bytes work allocated. TotalAlloc is
// cumulative, so unlike a live-heap reading it does not depend on when the
// collector happens to run.
func measureEnvelopeAllocation(work func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	work()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestStreamedEnvelopeRepeatTrackingStaysProportionalToTrackedMembers pins
// streamEnvelopeObject's repeat-detection state as constant-space in the
// members it does not track.
//
// The member loop recorded every distinct top-level name in seen, including the
// ones whose only handling is the default branch that decodes and discards
// them. The repeat check reads seen only when the name is the array or one of
// the captured metadata members, so those extra entries could never be
// consulted -- but they were still retained, and maximumAPIResponseBody admits
// a 16 MiB object carrying millions of short unique names. One response
// therefore amplified into a map proportional to its own size, built out of
// names nothing would ever ask about.
//
// Storing only the tracked names makes the irrelevant members constant-space
// and leaves duplicate rejection for the array and the captured members exactly
// where it was.
func TestStreamedEnvelopeRepeatTrackingStaysProportionalToTrackedMembers(t *testing.T) {
	distinct := envelopeIrrelevantMemberBody(envelopeRepeatTrackingMembers, true)
	repeated := envelopeIrrelevantMemberBody(envelopeRepeatTrackingMembers, false)
	if len(distinct) != len(repeated) {
		t.Fatalf("body lengths %d and %d differ, so the comparison would measure parsing work rather than retained names",
			len(distinct), len(repeated))
	}

	// Both bodies are legitimate envelopes, so whatever each call retains it
	// reaches the same answer: two elements and the one continuation link.
	stream := func(body []byte) (int, string) {
		var nextLink json.RawMessage
		elements := 0
		found, err := streamEnvelopeObject(body, "value",
			map[string]*json.RawMessage{"nextLink": &nextLink},
			func(json.RawMessage) error {
				elements++
				return nil
			})
		if err != nil || !found {
			t.Fatalf("found=%v error=%v, want the envelope accepted", found, err)
		}
		return elements, string(nextLink)
	}

	var distinctElements, repeatedElements int
	var distinctNext, repeatedNext string
	distinctAllocated := measureEnvelopeAllocation(func() {
		distinctElements, distinctNext = stream(distinct)
	})
	repeatedAllocated := measureEnvelopeAllocation(func() {
		repeatedElements, repeatedNext = stream(repeated)
	})
	const wantNext = `"https://graph.microsoft.com/next"`
	if distinctElements != 2 || repeatedElements != 2 || distinctNext != wantNext || repeatedNext != wantNext {
		t.Fatalf("distinct=(%d,%s) repeated=(%d,%s), want two elements and %s from both",
			distinctElements, distinctNext, repeatedElements, repeatedNext, wantNext)
	}

	// The only difference between the two bodies is whether the irrelevant names
	// repeat, so the only thing the first can cost more is state kept per
	// distinct name.
	if distinctAllocated > repeatedAllocated+envelopeRepeatTrackingMargin {
		t.Fatalf("%d distinct irrelevant members allocated %d bytes against %d for the same bytes under one repeated name; the %d-byte excess is repeat-tracking state charged to members the default branch only discards",
			envelopeRepeatTrackingMembers, distinctAllocated, repeatedAllocated, distinctAllocated-repeatedAllocated)
	}

	// Retaining those names is what the loop used to do, and it costs multiples
	// of the margin allowed above. Without this the comparison could pass purely
	// because the quantity it excludes is too small to see.
	names := make([]string, envelopeRepeatTrackingMembers)
	for name := range names {
		names[name] = fmt.Sprintf("a%07d", name)
	}
	var tracked map[string]struct{}
	trackedAllocated := measureEnvelopeAllocation(func() {
		tracked = make(map[string]struct{}, 2)
		for _, name := range names {
			tracked[name] = struct{}{}
		}
	})
	if len(tracked) != envelopeRepeatTrackingMembers {
		t.Fatalf("control map holds %d names, want %d distinct ones", len(tracked), envelopeRepeatTrackingMembers)
	}
	if trackedAllocated <= envelopeRepeatTrackingMargin {
		t.Fatalf("a map over %d names allocated only %d bytes, inside the %d-byte margin: the assertion above cannot see the state it is about",
			envelopeRepeatTrackingMembers, trackedAllocated, envelopeRepeatTrackingMargin)
	}
}

// TestStreamedEnvelopeRejectsRepeatedTrackedMembers pins the duplicate
// rejection that survives narrowing seen to the tracked names. The array and
// every captured metadata member may appear at most once however many
// irrelevant members surround or repeat between them; an irrelevant member may
// repeat freely, which it always could, because last-wins on a skipped member
// decides nothing.
func TestStreamedEnvelopeRejectsRepeatedTrackedMembers(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"repeated-array", `{"value":[1],"value":[2]}`, "repeats the value member"},
		// An explicit null array continues out of the switch before it decodes
		// anything, so it has to still be recorded as having been seen.
		{"repeated-array-after-null", `{"value":null,"value":[1]}`, "repeats the value member"},
		{"repeated-array-null-second", `{"value":[1],"value":null}`, "repeats the value member"},
		{"repeated-capture", `{"nextLink":"a","value":[1],"nextLink":"b"}`, "repeats the nextLink member"},
		{"repeated-capture-no-array", `{"nextLink":"a","nextLink":"b"}`, "repeats the nextLink member"},
		{"repeated-second-capture", `{"@odata.nextLink":"a","value":[1],"@odata.nextLink":"b"}`, "repeats the @odata.nextLink member"},
		// Irrelevant members repeating around and between the tracked ones must
		// not shadow the tracked duplicate behind them.
		{"repeated-array-among-irrelevant", `{"junk":1,"value":[1],"junk":2,"junk":3,"value":[2]}`, "repeats the value member"},
		{"repeated-capture-among-irrelevant", `{"junk":1,"nextLink":"a","junk":1,"value":[1],"junk":2,"nextLink":"b"}`, "repeats the nextLink member"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var nextLink, odataNextLink json.RawMessage
			_, err := streamEnvelopeObject([]byte(tc.body), "value",
				map[string]*json.RawMessage{"nextLink": &nextLink, "@odata.nextLink": &odataNextLink},
				func(json.RawMessage) error { return nil })
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want one naming %q", err, tc.want)
			}
		})
	}

	// A repeated irrelevant member is accepted, and the tracked members are
	// still read out of the same pass.
	for _, tc := range []struct {
		name, body string
		elements   int
		next       string
	}{
		{"repeated-irrelevant", `{"junk":1,"junk":2,"junk":3,"value":[1,2],"nextLink":"a"}`, 2, `"a"`},
		{"repeated-irrelevant-nested", `{"extra":{"value":[1,2,3]},"extra":[{"nextLink":"x"}],"value":[1],"nextLink":"b"}`, 1, `"b"`},
		{"repeated-irrelevant-only", `{"junk":null,"junk":null,"value":[]}`, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var nextLink, odataNextLink json.RawMessage
			elements := 0
			found, err := streamEnvelopeObject([]byte(tc.body), "value",
				map[string]*json.RawMessage{"nextLink": &nextLink, "@odata.nextLink": &odataNextLink},
				func(json.RawMessage) error {
					elements++
					return nil
				})
			if err != nil || !found {
				t.Fatalf("found=%v error=%v, want a repeated skipped member accepted", found, err)
			}
			if elements != tc.elements || string(nextLink) != tc.next {
				t.Fatalf("elements=%d nextLink=%s, want %d and %s", elements, nextLink, tc.elements, tc.next)
			}
		})
	}
}
