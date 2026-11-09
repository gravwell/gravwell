package claudecompliance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestOversizedAnnouncedPageRetriesWithSmallerLimit(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Page_Size = 8
	var limits []string
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		limit := r.URL.Query().Get("limit")
		limits = append(limits, limit)
		if limit != "2" {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Header:        http.Header{},
				ContentLength: maxResponseBytes + 1,
				Body:          io.NopCloser(strings.NewReader("")),
			}, nil
		}
		return reply(`{"data":[{"id":"a1"},{"id":"a2"}],"has_more":false}`, http.StatusOK), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(limits, ","); got != "8,4,2" {
		t.Fatalf("adaptive page limits = %s, want 8,4,2", got)
	}
	if len(rt.entries) != 2 || !rt.committed(p) {
		t.Fatal("adapted page did not deliver and checkpoint both records")
	}
}

func TestParentContextIsSelfDelimitingJSON(t *testing.T) {
	value, err := parentContextValue([]string{"chat_id:abc,organization_id:evil", "scope_id:x:y"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(value), &got); err != nil {
		t.Fatalf("parent context is not JSON: %v", err)
	}
	if len(got) != 2 || got["chat_id"] != "abc,organization_id:evil" || got["scope_id"] != "x:y" {
		t.Fatalf("parent context changed boundaries: %#v", got)
	}
}

func TestRetiringChildAtFailureCapIsCapacityVictim(t *testing.T) {
	const key = "chat-messages/chat_id:c1"
	list := worklist{Items: map[string]work{
		key: {
			Dataset: "chat-messages", Parameter: []string{"chat_id:c1"},
			Pending: true, RetireAfterDrain: true, Failures: maxChildFailures,
		},
	}}
	if got := capacityVictim(list, nil); got != key {
		t.Fatalf("retiring failed child was not evictable: %q", got)
	}
}

func TestDeletedParentChild404RetiresOnlyTheDrainingChild(t *testing.T) {
	p, rt := setup(t, "chats")
	p.conf.Follow_Children = "enabled"
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/chats") {
			return reply(`{"data":[{"id":"c1","deleted_at":"2026-09-08T00:00:00Z"}],"has_more":false}`, http.StatusOK), nil
		}
		return reply(`{"error":{"type":"not_found_error"}}`, http.StatusNotFound), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	var list worklist
	if err := json.Unmarshal(mustGet(t, rt, p.conf.key()+keyChildren), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("deleted child remained scheduled after authoritative 404: %#v", list.Items)
	}
}

func TestResponsePageCannotExceedRequestedLimit(t *testing.T) {
	p, rt := setup(t, "activities")
	d := Datasets["activities"]
	var rows strings.Builder
	rows.WriteString(`{"data":[`)
	for i := 0; i <= d.Limit; i++ {
		if i > 0 {
			rows.WriteByte(',')
		}
		fmt.Fprintf(&rows, `{"id":"a%d"}`, i)
	}
	rows.WriteString(`],"has_more":false}`)
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(rows.String(), http.StatusOK), nil
	})
	if _, err := p.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "documented page limit") {
		t.Fatalf("oversized page was not rejected: %v", err)
	}
	if len(rt.entries) != 0 || rt.committed(p) {
		t.Fatal("oversized page wrote entries or advanced its checkpoint")
	}
}

func TestEnvelopeFieldCountIsBoundedBeforeUnknownKeysAccumulate(t *testing.T) {
	var body strings.Builder
	body.WriteByte('{')
	for i := 0; i <= maxEnvelopeFields; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `"unknown_%d":%d`, i, i)
	}
	body.WriteString(`,"data":[],"has_more":false}`)
	if _, _, err := decodePage([]byte(body.String()), Datasets["activities"]); err == nil || !strings.Contains(err.Error(), "too many envelope fields") {
		t.Fatalf("top-level key flood was accepted: %v", err)
	}
}

func TestSingleObjectFieldCountIsNotAnEnvelopeLimit(t *testing.T) {
	var body strings.Builder
	body.WriteString(`{"id":"record"`)
	for i := 0; i <= maxEnvelopeFields; i++ {
		fmt.Fprintf(&body, `,"field_%d":%d`, i, i)
	}
	body.WriteByte('}')
	_, rows, err := decodePage([]byte(body.String()), Datasets["group"])
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !bytes.Equal(rows[0], []byte(body.String())) {
		t.Fatal("single-object record was not retained intact")
	}
}

func TestDeclaredOversizedResponseIsRejectedBeforeBodyDecode(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		resp := reply(`{"data":[],"has_more":false}`, http.StatusOK)
		resp.ContentLength = maxResponseBytes + 1
		return resp, nil
	})
	if _, err := p.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized Content-Length was accepted: %v", err)
	}
	if len(rt.entries) != 0 || rt.committed(p) {
		t.Fatal("rejected response wrote entries or advanced state")
	}
}

func TestDeliveryRetryDoesNotQueueTheSamePageTwice(t *testing.T) {
	p, rt := setup(t, "activities")
	now := p.now()
	p.now = func() time.Time { return now }
	var queries []string
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		queries = append(queries, r.URL.RawQuery)
		return reply(`{"data":[{"id":"a"},{"id":"b"}],"has_more":false}`, http.StatusOK), nil
	})
	rt.failDelivery = true
	if _, err := p.Handle(t.Context(), rt); err == nil {
		t.Fatal("synthetic delivery failure was accepted")
	}
	if len(rt.entries) != 2 || rt.committed(p) {
		t.Fatalf("first attempt entries=%d committed=%v", len(rt.entries), rt.committed(p))
	}
	now = now.Add(10 * time.Minute)
	rt.failDelivery = false
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != 2 || !rt.committed(p) {
		t.Fatalf("retry queued duplicates: entries=%d committed=%v", len(rt.entries), rt.committed(p))
	}
	if len(queries) != 2 || queries[0] != queries[1] {
		t.Fatalf("retry window changed: %#v", queries)
	}
	p.delivery.mu.Lock()
	pages, windows := len(p.delivery.pages), len(p.delivery.windows)
	p.delivery.mu.Unlock()
	if pages != 0 || windows != 0 {
		t.Fatalf("committed dataset retained delivery state: pages=%d windows=%d", pages, windows)
	}
}

func TestPartialWriteRetryQueuesOnlyTheUnwrittenSuffix(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"a"},{"id":"b"},{"id":"c"}],"has_more":false}`, http.StatusOK), nil
	})
	writes := 0
	rt.writeFn = func(entry.Entry) error {
		writes++
		if writes == 2 {
			return errors.New("synthetic second-write failure")
		}
		return nil
	}
	if _, err := p.Handle(t.Context(), rt); err == nil {
		t.Fatal("synthetic partial write failure was accepted")
	}
	if len(rt.entries) != 1 || rt.committed(p) {
		t.Fatalf("first attempt entries=%d committed=%v", len(rt.entries), rt.committed(p))
	}
	rt.writeFn = nil
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != 3 || !rt.committed(p) {
		t.Fatalf("retry did not write exactly one copy of each record: entries=%d committed=%v", len(rt.entries), rt.committed(p))
	}
}

func TestManifestEvictionHeapIgnoresStaleUpdates(t *testing.T) {
	committed := manifest{
		"a": {Digest: "a", Seen: 1},
		"b": {Digest: "b", Seen: 2},
	}
	current := manifest{"c": {Digest: "c", Seen: 3}}
	evictor := newManifestEvictor(committed, current)
	current["c"] = manifestEntry{Digest: "new-c", Seen: 10}
	evictor.track("c", current["c"])
	if key, fromWalk, ok := evictor.evict(committed, current); !ok || key != "a" || fromWalk {
		t.Fatalf("heap had no committed victim: key=%q walk=%v ok=%v", key, fromWalk, ok)
	}
	if _, ok := committed["a"]; ok {
		t.Fatal("oldest committed identity was retained")
	}
	if key, fromWalk, ok := evictor.evict(committed, current); !ok || key != "b" || fromWalk {
		t.Fatalf("heap had no second committed victim: key=%q walk=%v ok=%v", key, fromWalk, ok)
	}
	if _, ok := committed["b"]; ok {
		t.Fatal("second-oldest valid identity was retained")
	}
	if got := current["c"]; got.Seen != 10 {
		t.Fatal("stale heap entry evicted the updated identity")
	}
}

func TestFutureVendorTimeCannotPinManifestEviction(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"future","created_at":"2099-01-01T00:00:00Z"}],"has_more":false}`, http.StatusOK), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	cp := rt.checkpoint(t, p)
	for _, value := range cp.Manifest {
		if value.Seen > p.now().Unix() {
			t.Fatalf("future vendor timestamp pinned manifest entry: %d > %d", value.Seen, p.now().Unix())
		}
	}
}

func TestCredentialChangeSelectsAnIsolatedStateNamespace(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"same"}],"has_more":false}`, http.StatusOK), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("first source wrote %d entries", len(rt.entries))
	}
	oldPrefix := p.conf.key()
	if err := os.WriteFile(p.conf.Credential_File, []byte("rotated-synthetic-test-key"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != 2 {
		t.Fatalf("new source inherited old dedup state: entries=%d", len(rt.entries))
	}
	newPrefix := p.conf.key()
	if newPrefix == oldPrefix {
		t.Fatal("credential change reused the old source namespace")
	}
	if !strings.HasPrefix(oldPrefix, "source/v4/") || !strings.HasPrefix(newPrefix, "source/v4/") {
		t.Fatalf("source namespace is malformed: old=%q new=%q", oldPrefix, newPrefix)
	}
	if strings.Contains(oldPrefix+newPrefix, "rotated-synthetic-test-key") || strings.Contains(oldPrefix+newPrefix, "synthetic-test-key") {
		t.Fatal("source namespace exposed credential material")
	}
	if after := readCheckpoint(t, rt, oldPrefix); !after.Since.IsZero() || len(after.Manifest) != 0 || after.Walk != nil {
		t.Fatal("successful credential handover did not reclaim the superseded slot")
	}
	if current := readCheckpoint(t, rt, newPrefix); current.Since.IsZero() || len(current.Manifest) != 1 {
		t.Fatal("new source did not commit into its isolated namespace")
	}
}

func TestCredentialChangeIsolatesStateDespiteSiblingFailure(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Dataset = []string{"activities", "groups"}
	p.conf.path = ""
	if err := p.conf.Verify(); err != nil {
		t.Fatal(err)
	}
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"type":"error","error":{"type":"permission_error"}}`, http.StatusForbidden), nil
		}
		return reply(`{"data":[{"id":"same"}],"has_more":false}`, http.StatusOK), nil
	})
	if _, err := p.Handle(t.Context(), rt); err == nil {
		t.Fatal("persistent sibling failure was accepted")
	}
	if len(rt.entries) != 1 {
		t.Fatalf("healthy dataset wrote %d entries before rotation", len(rt.entries))
	}
	oldActivities, err := p.conf.bind("activities")
	if err != nil {
		t.Fatal(err)
	}
	oldPrefix := oldActivities.key()
	if readCheckpoint(t, rt, oldPrefix).Since.IsZero() {
		t.Fatal("healthy sibling did not commit its old-source checkpoint")
	}
	if err := os.WriteFile(p.conf.Credential_File, []byte("rotated-synthetic-test-key"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(t.Context(), rt); err == nil {
		t.Fatal("persistent sibling failure was accepted after rotation")
	}
	newActivities, err := p.conf.bind("activities")
	if err != nil {
		t.Fatal(err)
	}
	if newActivities.key() == oldPrefix {
		t.Fatal("partial-cycle source rotation reused the old namespace")
	}
	if len(rt.entries) != 2 {
		t.Fatalf("new source inherited the healthy sibling checkpoint: entries=%d", len(rt.entries))
	}
	if preserved := readCheckpoint(t, rt, oldPrefix); preserved.Since.IsZero() || len(preserved.Manifest) != 1 {
		t.Fatal("failed replacement credential destroyed the prior resumable checkpoint")
	}
}

func TestCredentialRotationFailsClosedWhenEverySourceSlotIsOccupied(t *testing.T) {
	p, rt := setup(t, "activities")
	originalFingerprint := p.conf.sourceID
	originalPrefix := p.conf.key()
	if err := rt.PutTime(originalPrefix+keySince, time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	for i := 1; i < maxSourceSlots; i++ {
		if _, err := p.conf.selectNamespace(rt, sourceFingerprint(p.conf.Host, fmt.Sprintf("failed-rotation-%d", i))); err != nil {
			t.Fatalf("occupy slot %d: %v", i, err)
		}
	}
	recordBefore, err := loadSourceRecord(rt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.conf.selectNamespace(rt, sourceFingerprint(p.conf.Host, "one-rotation-too-many")); err == nil || !strings.Contains(err.Error(), "no free state namespace") {
		t.Fatalf("full source-slot set did not fail closed: %v", err)
	}
	recordAfter, err := loadSourceRecord(rt)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recordAfter, recordBefore) {
		t.Fatalf("failed slot selection mutated the durable source record: before=%+v after=%+v", recordBefore, recordAfter)
	}
	if got, err := rt.GetTime(originalPrefix + keySince); err != nil || got.IsZero() {
		t.Fatalf("failed slot selection destroyed the original checkpoint: time=%v err=%v", got, err)
	}
	if _, err = p.conf.selectNamespace(rt, originalFingerprint); err != nil {
		t.Fatalf("original credential could not resume its retained namespace: %v", err)
	}
	if p.conf.key() != originalPrefix {
		t.Fatalf("original credential resumed the wrong namespace: got=%q want=%q", p.conf.key(), originalPrefix)
	}
}

func TestCredentialSnapshotBindsNamespaceAndRequestsForWholeCycle(t *testing.T) {
	p, rt := setup(t, "activities")
	frozen, changed, err := p.conf.cycleConfig(rt)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("unchanged initial credential reported a source change")
	}
	oldPrefix := frozen.key()
	if err := os.WriteFile(p.conf.Credential_File, []byte("rotated-synthetic-test-key"), 0600); err != nil {
		t.Fatal(err)
	}

	var firstToken string
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		firstToken = r.Header.Get("x-api-key")
		return reply(`{"data":[{"id":"same"}],"has_more":false}`, http.StatusOK), nil
	})
	cycle := *p
	cycle.conf = frozen
	if _, err := cycle.handleConfigured(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if firstToken != "synthetic-test-key" {
		t.Fatalf("frozen cycle used token %q", firstToken)
	}
	if cp := readCheckpoint(t, rt, oldPrefix); cp.Since.IsZero() {
		t.Fatal("frozen cycle did not commit under its matching namespace")
	}

	var secondToken string
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		secondToken = r.Header.Get("x-api-key")
		return reply(`{"data":[{"id":"same"}],"has_more":false}`, http.StatusOK), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if secondToken != "rotated-synthetic-test-key" {
		t.Fatalf("next cycle used token %q", secondToken)
	}
	if newPrefix := p.conf.key(); newPrefix == oldPrefix || readCheckpoint(t, rt, newPrefix).Since.IsZero() {
		t.Fatal("next cycle did not move the rotated credential to its own namespace")
	}
}

func TestOpaqueIdentifiersRejectDotSegments(t *testing.T) {
	for _, value := range []string{".", "..", "/", `\`, "?", "#", ",", `"`, "\x00", "\x1f", "\x7f"} {
		if !invalidOpaqueID(value) {
			t.Fatalf("unsafe opaque identifier %q was accepted", value)
		}
	}
	for _, value := range []string{"id", "id.with.dots", "id..inside"} {
		if invalidOpaqueID(value) {
			t.Fatalf("safe opaque identifier %q was rejected", value)
		}
	}
	p, _ := setup(t, "group-members")
	p.conf.Parameter = []string{"group_id:.."}
	if err := p.conf.Verify(); err == nil {
		t.Fatal("operator dot-segment parameter was accepted")
	}
	if _, err := childWork(Datasets["chats"], nil, []byte(`{"id":".."}`)); !errors.Is(err, errInvalidIdentity) {
		t.Fatalf("discovered dot-segment identity error = %v", err)
	}
	if _, _, err := deletedChildWork(Datasets["chats"], nil, []byte(`{"id":".","deleted_at":"2026-01-01T00:00:00Z"}`)); !errors.Is(err, errInvalidIdentity) {
		t.Fatalf("deleted dot-segment identity error = %v", err)
	}
}

func TestPaginationTokenLengthIsBounded(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  json.RawMessage
		ok   bool
	}{
		{"maximum string", json.RawMessage(`"` + strings.Repeat("t", maxPaginationTokenBytes) + `"`), true},
		{"oversized string", json.RawMessage(`"` + strings.Repeat("t", maxPaginationTokenBytes+1) + `"`), false},
		{"maximum number", json.RawMessage(strings.Repeat("1", maxPaginationTokenBytes)), true},
		{"oversized number", json.RawMessage(strings.Repeat("1", maxPaginationTokenBytes+1)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := paginationToken(tc.raw)
			if (err == nil) != tc.ok {
				t.Fatalf("paginationToken error = %v, want accepted=%v", err, tc.ok)
			}
		})
	}
}

func TestSessionEnvelopeIsProcessedOncePerPage(t *testing.T) {
	p, rt := setup(t, "local-session-messages")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"session":"not-an-object","data":[{"id":"m1"},{"id":"m2"}],"next_page":null}`, http.StatusOK), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if rt.warnings != 1 {
		t.Fatalf("session envelope produced %d warnings, want one per page", rt.warnings)
	}
	if len(rt.entries) != 2 {
		t.Fatalf("messages were not retained: %d", len(rt.entries))
	}
}

func TestManifestIdentityKeysHaveFixedSize(t *testing.T) {
	m := manifest{}
	d := Datasets["role-permissions"]
	for i := 0; i < 1000; i++ {
		value := strings.Repeat("\x01", maxDiscoveredParameterLen-12) + fmt.Sprintf("%012d", i)
		raw, err := json.Marshal(map[string]string{
			"resource_type": value,
			"resource_id":   value,
			"action":        value,
		})
		if err != nil {
			t.Fatal(err)
		}
		key := identity(raw, d.Identity, "digest")
		if !strings.HasPrefix(key, "identity-sha256:") || len(key) != len("identity-sha256:")+64 {
			t.Fatalf("identity key is not fixed-size: prefix=%q bytes=%d", key[:min(len(key), 24)], len(key))
		}
		m[key] = manifestEntry{Digest: strings.Repeat("d", 64), Seen: time.Now().Unix()}
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 256<<10 {
		t.Fatalf("1000-entry manifest uses %d bytes, want at most 256 KiB", len(b))
	}
}

func TestRawConfigOmitsOpaqueParameters(t *testing.T) {
	c := Config{Parameter: []string{"organization_id:private-customer-id"}}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("private-customer-id")) || bytes.Contains(b, []byte("Parameter")) {
		t.Fatalf("raw config exposes opaque customer identifiers: %s", b)
	}
}
