/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package n8n

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted/storage"
)

func TestREADMEAndExampleDocumentOperationalRetryAndCredentialSecurity(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	for _, required := range []string{
		"poll fetches each selected dataset's complete snapshot again",
		"`Requests-Per-Minute` is a combined budget",
		"non-canonical path escaping",
		"Allow-Plaintext=true",
		"API key in cleartext",
		"loopback or test",
		"90-second request context",
		"HTTP status responses",
		"including 429",
		"fixed 30 seconds",
		"each retry fetches every selected dataset's",
		"Responses are decoded as streams",
		"whole-snapshot disk spool",
		"arbitrary byte threshold.",
		"Only the `credentials` dataset",
		"`variables`, `workflows`, and `audit`",
		"`os.TempDir()`",
		"`TMPDIR`",
		"disk-backed",
		"1,073,741,823",
		"reused for one dataset collection",
		"snapshot-v6",
		"approximately 0.8 KiB of live process memory per tracked record",
		"roughly 200 MiB at the 250,000-record limit",
	} {
		if !strings.Contains(string(readme), required) {
			t.Errorf("README is missing operational or security statement %q", required)
		}
	}
	example, err := os.ReadFile("example.conf")
	if err != nil {
		t.Fatalf("read example.conf: %v", err)
	}
	if !strings.Contains(string(example), "Allow-Plaintext=false") || !strings.Contains(string(example), "API key in cleartext") {
		t.Error("example.conf must show the secure default and warn about cleartext credentials")
	}
}

// listServer serves a mutable single-page list response for a dataset.
type listServer struct {
	mu      sync.Mutex
	records []string
	hits    int
}

func TestRecordTimestampRejectsOutOfRangeNumericAndStringValues(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name  string
		value string
	}{
		{name: "numeric milliseconds", value: "1766000000000"},
		{name: "numeric negative", value: "-1"},
		{name: "numeric far future", value: "9999999999"},
		{name: "string before epoch", value: `"0001-01-01T00:00:00Z"`},
		{name: "string far future", value: `"9999-12-31T23:59:59Z"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeTestRecord(t, fmt.Sprintf(`{"createdAt":%s}`, tc.value), false).timestamp
			if got.Before(now.Add(-time.Minute)) || got.After(now.Add(25*time.Hour)) {
				t.Fatalf("timestamp=%s is outside the sane fallback window", got)
			}
		})
	}
	valid := normalizeTestRecord(t, `{"createdAt":1766000000}`, false).timestamp
	if got, want := valid.Unix(), int64(1766000000); got != want {
		t.Fatalf("valid seconds timestamp=%d want %d", got, want)
	}
	validString := normalizeTestRecord(t, `{"createdAt":"2026-10-19T12:00:00Z"}`, false).timestamp
	if got, want := validString.Unix(), int64(1792411200); got != want {
		t.Fatalf("valid string timestamp=%d want %d", got, want)
	}
	for _, first := range []string{"1766000000000", `"9999-12-31T23:59:59Z"`} {
		validLaterField := normalizeTestRecord(t, fmt.Sprintf(`{"timestamp":%s,"startedAt":"2026-10-19T12:00:00Z"}`, first), false).timestamp
		if got, want := validLaterField.Unix(), int64(1792411200); got != want {
			t.Fatalf("out-of-range value %s did not fall through: got %d want %d", first, got, want)
		}
	}
}

func (l *listServer) set(records ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = records
}

func (l *listServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		body := `{"data":[` + strings.Join(l.records, ",") + `],"nextCursor":null}`
		l.hits++
		l.mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func record(id, name string) string {
	return fmt.Sprintf(`{"id":%q,"name":%q}`, id, name)
}

// pollOnce runs one Handle and reports how many entries that poll wrote.
func pollOnce(t *testing.T, plugin *Plugin, rt *mockRuntime) int {
	t.Helper()
	before := len(rt.Entries())
	if _, err := plugin.Handle(t.Context(), rt); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	return len(rt.Entries()) - before
}

// A dataset that only ever gains records must only ever emit the records it
// gained. Keying durable state on a single aggregate snapshot digest made any
// change re-emit every record in the snapshot, so an append-only dataset such as
// executions republished its entire retained history on every poll.
func TestGrowingDatasetEmitsOnlyNewRecords(t *testing.T) {
	// snapshot holds records 1..n, so each poll appends exactly one record and
	// leaves every earlier record byte-identical.
	snapshot := func(n int) []string {
		records := make([]string, 0, n)
		for i := 1; i <= n; i++ {
			records = append(records, record(fmt.Sprint(i), "name"))
		}
		return records
	}
	list := &listServer{}
	list.set(snapshot(1)...)
	server := list.start(t)

	conf := testConfig(t, server.URL+"/api/v1", "executions")
	plugin := New(conf)
	rt := newMockRuntime(t.Context())

	for poll := 1; poll <= 5; poll++ {
		if written := pollOnce(t, plugin, rt); written != 1 {
			t.Fatalf("poll %d over a snapshot of %d records wrote %d entries, want 1", poll, poll, written)
		}
		list.set(snapshot(poll + 1)...)
	}
	if got := len(rt.Entries()); got != 5 {
		t.Fatalf("total entries=%d want 5; the snapshot is being re-emitted", got)
	}
}

// Only the mutated record may be emitted, not the whole snapshot alongside it.
func TestMutationEmitsOnlyTheChangedRecord(t *testing.T) {
	list := &listServer{}
	list.set(record("1", "one"), record("2", "two"), record("3", "three"))
	server := list.start(t)

	conf := testConfig(t, server.URL+"/api/v1", "workflows")
	plugin := New(conf)
	rt := newMockRuntime(t.Context())

	if written := pollOnce(t, plugin, rt); written != 3 {
		t.Fatalf("first poll wrote %d entries, want 3", written)
	}
	list.set(record("1", "one"), record("2", "CHANGED"), record("3", "three"))
	if written := pollOnce(t, plugin, rt); written != 1 {
		t.Fatalf("mutating one of three records wrote %d entries, want 1", written)
	}
	last := rt.Entries()[len(rt.Entries())-1]
	if !strings.Contains(string(last.Data), "CHANGED") {
		t.Fatalf("the emitted entry was not the mutated record: %s", last.Data)
	}
}

// A deletion changes the snapshot, but the records that survived it are already
// durable and must not be republished.
func TestDeletionDoesNotReEmitSurvivors(t *testing.T) {
	list := &listServer{}
	list.set(record("1", "one"), record("2", "two"), record("3", "three"))
	server := list.start(t)

	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	rt := newMockRuntime(t.Context())
	stateKey := conf.stateKey(datasetCatalog["users"])

	if written := pollOnce(t, plugin, rt); written != 3 {
		t.Fatalf("first poll wrote %d entries, want 3", written)
	}
	list.set(record("1", "one"), record("3", "three"))
	if written := pollOnce(t, plugin, rt); written != 0 {
		t.Fatalf("deleting one of three records wrote %d entries, want 0", written)
	}
	state, err := rt.Get(stateKey)
	if err != nil {
		t.Fatalf("deletion did not advance state: %v", err)
	}
	if len(state) != 2*recordStateEntrySize {
		t.Fatalf("state holds %d bytes, want %d for two surviving records", len(state), 2*recordStateEntrySize)
	}

	// Reappearance is still a new record for state, so it is emitted again.
	list.set(record("1", "one"), record("2", "two"), record("3", "three"))
	if written := pollOnce(t, plugin, rt); written != 1 {
		t.Fatalf("reappearance wrote %d entries, want 1", written)
	}
}

// Durable state, not in-process memory, decides what has already been emitted.
func TestSnapshotStateSurvivesPluginRestart(t *testing.T) {
	list := &listServer{}
	list.set(record("1", "one"), record("2", "two"))
	server := list.start(t)

	conf := testConfig(t, server.URL+"/api/v1", "projects")
	rt := newMockRuntime(t.Context())

	if written := pollOnce(t, New(conf), rt); written != 2 {
		t.Fatalf("first poll wrote %d entries, want 2", written)
	}
	// A brand new Plugin over the same durable state, as happens after a restart
	// or a config-change driven runner restart.
	if written := pollOnce(t, New(conf), rt); written != 0 {
		t.Fatalf("restarted plugin re-emitted %d entries, want 0", written)
	}
	list.set(record("1", "one"), record("2", "two"), record("3", "three"))
	if written := pollOnce(t, New(conf), rt); written != 1 {
		t.Fatalf("restarted plugin wrote %d entries for one new record, want 1", written)
	}
}

// Per-record state is bounded. A snapshot too large to track must fail closed
// rather than silently collapsing back to whole-snapshot re-emission.
func TestRecordTrackingLimitFailsClosed(t *testing.T) {
	list := &listServer{}
	list.set(record("1", "one"), record("2", "two"), record("3", "three"))
	server := list.start(t)

	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	plugin.maxTracked = 2
	rt := newMockRuntime(t.Context())

	_, err := plugin.Handle(t.Context(), rt)
	if err == nil || !strings.Contains(err.Error(), "tracked records") {
		t.Fatalf("error=%v want a record tracking limit failure", err)
	}
	// The remedies the message names have to be ones that work for this dataset.
	// The limit is per dataset, so dropping other datasets cannot help.
	if strings.Contains(err.Error(), "narrow the collected Dataset set") {
		t.Fatalf("error suggests a remedy that cannot work: %v", err)
	}
	if !strings.Contains(err.Error(), "per-dataset") {
		t.Fatalf("error does not say the limit is per dataset: %v", err)
	}
	if got := len(rt.Entries()); got != 2 {
		t.Fatalf("entries=%d want the two records enqueued before the tracking limit is hit", got)
	}
	if _, err := rt.Get(conf.stateKey(datasetCatalog["users"])); !errors.Is(err, storage.ErrStorageNotFound) {
		t.Fatalf("oversized snapshot advanced state: %v", err)
	}
}

// The tracking limit bounds what gets stored, so pagination repeats that are
// deduplicated away must not count against it.
func TestTrackingLimitCountsDeduplicatedRecords(t *testing.T) {
	// Three raw records, but the third repeats the first exactly, as a cursor
	// boundary overlap does, so only two distinct records are ever tracked.
	list := &listServer{}
	list.set(record("1", "one"), record("2", "two"), record("1", "one"))
	server := list.start(t)

	conf := testConfig(t, server.URL+"/api/v1", "users")
	plugin := New(conf)
	plugin.maxTracked = 2
	rt := newMockRuntime(t.Context())

	if written := pollOnce(t, plugin, rt); written != 2 {
		t.Fatalf("wrote %d entries, want 2; the limit is counting raw records", written)
	}
	state, err := rt.Get(conf.stateKey(datasetCatalog["users"]))
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 2*recordStateEntrySize {
		t.Fatalf("state holds %d bytes, want %d", len(state), 2*recordStateEntrySize)
	}
}

// State that does not parse must be discarded and rebuilt rather than wedging
// the dataset or being read as a phantom record.
func TestUnreadableSnapshotStateSelfHeals(t *testing.T) {
	var a, b recordState
	a[0], b[0] = 1, 2
	// A retired v3 aggregate digest is deliberately absent here: it is 64 bytes,
	// so it parses cleanly as one phantom entry, which is exactly why the state
	// key had to be versioned instead. TestStateKeyRetiresTheAggregateDigestFormat
	// covers that.
	cases := map[string][]byte{
		// Not a whole number of per-record entries.
		"partial entry": make([]byte, recordStateEntrySize+7),
		// Right length, but not the sorted duplicate-free form we write. Membership
		// is a binary search, so reading these would at best cost spurious
		// re-emission; they are rejected so that cost is explicit and warned about
		// rather than silent.
		"descending": append(append([]byte{}, b[:]...), a[:]...),
		"duplicated": append(append([]byte{}, a[:]...), a[:]...),
	}
	for name, blob := range cases {
		t.Run(name, func(t *testing.T) {
			list := &listServer{}
			list.set(record("1", "one"))
			server := list.start(t)

			conf := testConfig(t, server.URL+"/api/v1", "tags")
			plugin := New(conf)
			rt := newMockRuntime(t.Context())
			stateKey := conf.stateKey(datasetCatalog["tags"])

			if err := rt.Put(stateKey, blob); err != nil {
				t.Fatal(err)
			}
			if written := pollOnce(t, plugin, rt); written != 1 {
				t.Fatalf("unparseable state wrote %d entries, want a full re-emit of 1", written)
			}
			state, err := rt.Get(stateKey)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := parseRecordState(state); !ok {
				t.Fatalf("state was not repaired: %x", state)
			}
			if written := pollOnce(t, plugin, rt); written != 0 {
				t.Fatalf("repaired state still re-emits %d entries", written)
			}
		})
	}
}

// A v3 aggregate digest is 64 bytes, which is also a whole number of per-record
// entries, so it must not be reachable under the current state key at all.
func TestStateKeyRetiresTheAggregateDigestFormat(t *testing.T) {
	conf := testConfig(t, "http://127.0.0.1:12345/api/v1", "users")
	key := conf.stateKey(datasetCatalog["users"])
	if !strings.Contains(key, "/snapshot-v6/") {
		t.Fatalf("state key %q is not versioned for per-record state", key)
	}
	if strings.Contains(key, "snapshot-v3") || strings.Contains(key, "snapshot-v4") || strings.Contains(key, "snapshot-v5") {
		t.Fatalf("state key still reads retired aggregate-digest state: %q", key)
	}
}

func TestTypedIdentityNamespaceReplaysOnceFromPreviousManifest(t *testing.T) {
	l := &listServer{}
	l.set(`{"id":"one"}`)
	server := l.start(t)
	conf := testConfig(t, server.URL+"/api/v1", "users")
	rt := newMockRuntime(t.Context())
	body := normalizeTestRecord(t, `{"id":"one"}`, false)
	var pair recordState
	copy(pair[:32], body.identity[:])
	copy(pair[32:], body.fingerprint[:])
	prior := strings.Replace(conf.stateKey(datasetCatalog["users"]), "/snapshot-v6/", "/snapshot-v5/", 1)
	old := encodeRecordState(map[recordState]struct{}{pair: {}})
	if err := rt.Put(prior, old); err != nil {
		t.Fatal(err)
	}
	p := New(conf)
	if got := pollOnce(t, p, rt); got != 1 {
		t.Fatalf("new identity namespace wrote %d, want one bounded replay", got)
	}
	if got := pollOnce(t, New(conf), rt); got != 0 {
		t.Fatalf("unchanged restart re-emitted %d records", got)
	}
	if saved, err := rt.Get(prior); err != nil || string(saved) != string(old) {
		t.Fatalf("previous namespace changed: %x, %v", saved, err)
	}
}

// Large pages are decoded incrementally and records are delivered one at a
// time, without a response/snapshot byte cap or an aggregate temporary spool.
func TestSnapshotStreamsResponsesBeyondFormerByteCaps(t *testing.T) {
	const fields = 65
	const chunk = 64 * 1024
	filler := strings.Repeat("x", chunk)
	var pages int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		page := pages
		_, _ = io.WriteString(w, "{")
		for field := 0; field < fields; field++ {
			if field > 0 {
				_, _ = io.WriteString(w, ",")
			}
			_, _ = fmt.Fprintf(w, `"pad%d":"`, field)
			for part := 0; part < 16; part++ {
				_, _ = io.WriteString(w, filler)
			}
			_, _ = io.WriteString(w, `"`)
		}
		cursor := `null`
		if page == 1 {
			cursor = `"next"`
		}
		_, _ = fmt.Fprintf(w, `,"data":[{"id":"p%d"}],"nextCursor":%s}`, page, cursor)
	}))
	defer server.Close()

	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 3, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var delivered [][]byte
	result, err := c.collectStreaming(t.Context(), datasetCatalog["users"], func(_ collectedRecord, body *normalizedRecord) error {
		payload, err := body.payload()
		if err != nil {
			return err
		}
		delivered = append(delivered, append([]byte(nil), payload...))
		return nil
	})
	if err != nil {
		t.Fatalf("streamed collection failed: %v", err)
	}
	if result.Count != 2 || len(result.Records) != 0 || pages != 2 {
		t.Fatalf("records=%d retained=%d pages=%d want 2 streamed records over 2 pages", result.Count, len(result.Records), pages)
	}
	if len(delivered) != 2 || string(delivered[0]) != `{"id":"p1"}` || string(delivered[1]) != `{"id":"p2"}` {
		t.Fatalf("delivered records=%q", delivered)
	}
}

// The single-object audit path rejects a JSON null so it cannot be framed as a
// report. Array elements need the same guard, or a null, a scalar, or a nested
// array is normalized into an entry and checkpointed as a valid snapshot.
func TestListRecordsMustBeJSONObjects(t *testing.T) {
	for _, body := range []string{
		`{"data":[null],"nextCursor":null}`,
		`{"data":[7],"nextCursor":null}`,
		`{"data":["loose"],"nextCursor":null}`,
		`{"data":[[1,2]],"nextCursor":null}`,
		`{"data":[true],"nextCursor":null}`,
		`{"data":[{"id":"ok"},null],"nextCursor":null}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			conf := testConfig(t, server.URL+"/api/v1", "users")
			rt := newMockRuntime(t.Context())
			_, err := New(conf).Handle(t.Context(), rt)
			if err == nil || !strings.Contains(err.Error(), "expected a JSON object") {
				t.Fatalf("error=%v want a JSON object rejection", err)
			}
			want := 0
			if strings.Contains(body, `{"id":"ok"}`) {
				want = 1
			}
			if got := len(rt.Entries()); got != want {
				t.Fatalf("entries=%d want %d", got, want)
			}
			if _, err := rt.Get(conf.stateKey(datasetCatalog["users"])); !errors.Is(err, storage.ErrStorageNotFound) {
				t.Fatalf("non-object record advanced state: %v", err)
			}
		})
	}
}

// An object-shaped record, including an empty one, is still accepted.
func TestListRecordsAcceptObjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{},{"id":"1"}],"nextCursor":null}`))
	}))
	defer server.Close()

	conf := testConfig(t, server.URL+"/api/v1", "users")
	rt := newMockRuntime(t.Context())
	if _, err := New(conf).Handle(t.Context(), rt); err != nil {
		t.Fatalf("object records were rejected: %v", err)
	}
	if got := len(rt.Entries()); got != 2 {
		t.Fatalf("entries=%d want 2", got)
	}
}

func TestRecordStateRoundTrip(t *testing.T) {
	if set, ok := parseRecordState(nil); !ok || set.len() != 0 {
		t.Fatalf("absent state parsed as ok=%v len=%d", ok, set.len())
	}
	if got := encodeRecordState(map[recordState]struct{}{}); len(got) != 0 {
		t.Fatalf("empty state encoded to %d bytes", len(got))
	}
	for _, size := range []int{1, recordStateEntrySize - 1, recordStateEntrySize + 1, 2*recordStateEntrySize - 3} {
		if _, ok := parseRecordState(make([]byte, size)); ok {
			t.Fatalf("a %d byte blob parsed, want rejection", size)
		}
	}

	var a, b, c recordState
	a[0], b[0], c[0] = 1, 2, 3
	set := map[recordState]struct{}{c: {}, b: {}, a: {}}
	encoded := encodeRecordState(set)
	if len(encoded) != 3*recordStateEntrySize {
		t.Fatalf("encoded %d bytes want %d", len(encoded), 3*recordStateEntrySize)
	}
	// Encoding is sorted, so an unchanged snapshot always produces identical bytes
	// regardless of map iteration order.
	for i := 0; i < 20; i++ {
		if got := encodeRecordState(set); string(got) != string(encoded) {
			t.Fatal("encoding is not deterministic across map iteration orders")
		}
	}

	// Membership is a binary search over the stored form, so it only answers
	// correctly while that form really is sorted and duplicate free.
	parsed, ok := parseRecordState(encoded)
	if !ok || parsed.len() != 3 {
		t.Fatalf("round trip produced ok=%v len=%d", ok, parsed.len())
	}
	for _, key := range []recordState{a, b, c} {
		if !parsed.contains(key) {
			t.Fatalf("round trip lost entry %x", key[0])
		}
	}
	var absent recordState
	absent[0] = 9
	if parsed.contains(absent) {
		t.Fatal("membership reported an entry that was never stored")
	}
	if (recordStateSet)(nil).contains(a) {
		t.Fatal("empty state reported membership")
	}

	// Descending and duplicate blobs are the right length but are not the form
	// encodeRecordState produces, so a binary search over them would be wrong.
	descending := append(append([]byte{}, c[:]...), a[:]...)
	if _, ok := parseRecordState(descending); ok {
		t.Fatal("unsorted state parsed, which would break membership search")
	}
	duplicated := append(append([]byte{}, a[:]...), a[:]...)
	if _, ok := parseRecordState(duplicated); ok {
		t.Fatal("duplicated state parsed, which is not a form we ever write")
	}
}
