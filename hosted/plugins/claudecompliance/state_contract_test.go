// Coverage for the stored-state contract: a fresh installation writes one
// discrete layout, and restarting against that same layout resumes safely --
// no record skipped, none ingested twice.
package claudecompliance

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// restart builds a new Plugin against the same configuration and store, the
// way the runner would after a process restart.
func restart(t *testing.T, p *Plugin, rt *runtime) *Plugin {
	t.Helper()
	fresh, err := New(p.conf, rt)
	if err != nil {
		t.Fatal(err)
	}
	fresh.now, fresh.limiter = p.now, p.limiter
	fresh.http.Transport = p.http.Transport
	fresh.maxRecords, fresh.maxManifestEntries = p.maxRecords, p.maxManifestEntries
	return fresh
}

// A fresh installation starts from nothing, and the only keys it leaves
// behind are the documented discrete ones.
func TestFreshInstallWritesOnlyTheDiscreteLayout(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"a1","created_at":"2026-09-07T23:00:00Z"}],"has_more":false}`, 200), nil
	})
	if cp := readCheckpoint(t, rt, p.conf.key()); !cp.Since.IsZero() || cp.Walk != nil || len(cp.Manifest) != 0 {
		t.Fatalf("a fresh install already had stored progress: %+v", cp)
	}
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	known := map[string]bool{
		keySince: true, keyManifest: true, keyHistory: true,
		keyWalkCursor: true, keyWalkSince: true, keyWalkUntil: true,
		keyWalkStarted: true, keyWalkHistory: true, keyWalkEmpty: true, keyWalkMan: true,
		keyWalkDeltas: true, keyChildren: true, keyCleanup: true, keyTombstones: true,
	}
	prefix := p.conf.key()
	var unexpected []string
	for _, k := range rt.storedKeys() {
		// The source-namespace record is owned by the stanza rather than by
		// one dataset: it is what makes a namespace superseded by a credential
		// rotation findable afterwards, so it cannot live inside one.
		if k == keySourceRecord {
			continue
		}
		suffix, ok := strings.CutPrefix(k, prefix)
		if !ok || (!known[suffix] && !strings.HasPrefix(suffix, keyWalkDelta)) {
			unexpected = append(unexpected, k)
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) != 0 {
		t.Fatalf("fresh install wrote keys outside the discrete layout: %v", unexpected)
	}
}

func TestTrackRootsRetainsPartiallyReclaimedNamespace(t *testing.T) {
	rt := newRuntime(t)
	current := make([]string, maxTrackedRoots)
	for i := range current {
		current[i] = fmt.Sprintf("root-%02d", i)
	}
	stale := "root-stale"
	record := sourceRecord{Slots: make([]sourceSlot, maxSourceSlots)}
	record.Slots[0].Roots = append(append([]string(nil), current...), stale)

	items := make(map[string]work, maxReclaimPerCycle+1)
	for i := 0; i <= maxReclaimPerCycle; i++ {
		key := fmt.Sprintf("child-%03d", i)
		items[key] = work{Dataset: "group-members"}
	}
	if err := putWorklist(rt, stale+keyChildren, worklist{Items: items}); err != nil {
		t.Fatal(err)
	}
	if err := rt.PutTime(stale+keySince, time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	if changed, err := record.trackRoots(rt, 0, current); !errors.Is(err, errRootNamespaceRecycling) || changed {
		t.Fatalf("partial reclaim was not deferred: changed=%v err=%v", changed, err)
	}
	if got := record.Slots[0].Roots; len(got) != maxTrackedRoots+1 || got[len(got)-1] != stale {
		t.Fatalf("partially reclaimed root was forgotten: %v", got)
	}
	remaining, err := loadWorklist(rt, stale+keyChildren)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.Items) != 1 {
		t.Fatalf("first bounded pass left %d children, want 1", len(remaining.Items))
	}
	if since, err := rt.GetTime(stale + keySince); err != nil || since.IsZero() {
		t.Fatalf("partial reclaim cleared the root checkpoint early: time=%v err=%v", since, err)
	}

	if changed, err := record.trackRoots(rt, 0, current); err != nil || !changed {
		t.Fatalf("completed reclaim did not update the source record: changed=%v err=%v", changed, err)
	}
	if got := record.Slots[0].Roots; len(got) != maxTrackedRoots {
		t.Fatalf("completed reclaim retained stale root: %v", got)
	}
	remaining, err = loadWorklist(rt, stale+keyChildren)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.Items) != 0 {
		t.Fatalf("completed reclaim left %d children", len(remaining.Items))
	}
	if since, err := rt.GetTime(stale + keySince); err != nil || !since.IsZero() {
		t.Fatalf("completed reclaim left the stale root checkpoint: time=%v err=%v", since, err)
	}
}

// Restarting mid-traversal and again after completion must neither skip a
// record nor write one twice.
func TestRestartAcrossTheCurrentLayoutNeitherSkipsNorDuplicates(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Max_Pages = 1
	recordTimes := map[string]time.Time{
		"":   mustTime(t, "2026-09-07T20:00:00Z"),
		"a1": mustTime(t, "2026-09-07T21:00:00Z"),
		"a2": mustTime(t, "2026-09-07T22:00:00Z"),
	}
	pages := map[string]string{
		"":   `{"data":[{"id":"a1","created_at":"2026-09-07T20:00:00Z"}],"has_more":true,"last_id":"a1"}`,
		"a1": `{"data":[{"id":"a2","created_at":"2026-09-07T21:00:00Z"}],"has_more":true,"last_id":"a2"}`,
		"a2": `{"data":[{"id":"a3","created_at":"2026-09-07T22:00:00Z"}],"has_more":false}`,
	}
	// The vendor honors the requested window, so a record older than the
	// lower bound is not returned again. The dedup manifest drops exactly
	// those identities once a walk completes, and the two must agree.
	serve := transport(func(r *http.Request) (*http.Response, error) {
		gte, e := time.Parse(time.RFC3339Nano, r.URL.Query().Get("created_at.gte"))
		if e != nil {
			t.Fatalf("missing window lower bound: %v", e)
		}
		cursor := r.URL.Query().Get("after_id")
		body, ok := pages[cursor]
		if !ok {
			t.Fatalf("unexpected cursor %q", cursor)
		}
		if at := recordTimes[cursor]; at.Before(gte) {
			return reply(`{"data":[],"has_more":false}`, 200), nil
		}
		return reply(body, 200), nil
	})
	p.http.Transport = serve

	// One page per cycle, with a full restart between each.
	current := p
	for i := 0; i < 3; i++ {
		cont, e := current.Handle(t.Context(), rt)
		if e != nil {
			t.Fatalf("cycle %d: %v", i, e)
		}
		if i < 2 && (cont == nil || cont.Delay != 0) {
			t.Fatalf("cycle %d: page limit did not request an immediate continuation", i)
		}
		current = restart(t, current, rt)
		current.conf.Max_Pages = 1
	}

	got := map[string]int{}
	for _, ent := range rt.entries {
		got[string(ent.Data)]++
	}
	for _, id := range []string{"a1", "a2", "a3"} {
		var found string
		for data := range got {
			if strings.Contains(data, `"`+id+`"`) {
				found = data
			}
		}
		if found == "" {
			t.Errorf("record %s was skipped across restarts", id)
			continue
		}
		if got[found] != 1 {
			t.Errorf("record %s ingested %d times across restarts", id, got[found])
		}
	}
	if len(rt.entries) != 3 {
		t.Fatalf("expected exactly 3 entries, got %d", len(rt.entries))
	}

	// A further restart after the traversal completed must re-request the
	// overlap window and still write nothing new.
	before := len(rt.entries)
	final := restart(t, current, rt)
	if _, e := final.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != before {
		t.Fatalf("restart after completion duplicated %d records", len(rt.entries)-before)
	}
}

// TestPageWalkDoesNotRewriteCompleteManifestsPerPage covers write
// amplification. The dedup manifest has to stay whole because an exact-key
// store cannot enumerate it, so rewriting it costs the whole retained state;
// doing that on every page of a long traversal made each page cost O(persisted
// state) and put every page of every dataset in contention for the same store.
// A page journals only its own bounded change, and the complete maps are
// rewritten once the journal is full.
func TestPageWalkDoesNotRewriteCompleteManifestsPerPage(t *testing.T) {
	p, rt := setup(t, "activities")
	const pages = 2*maxWalkDeltas + 1
	p.conf.Max_Pages = pages + 1
	page := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		page++
		if page >= pages {
			return reply(fmt.Sprintf(`{"data":[{"id":"a%d","created_at":"2026-09-07T12:00:00Z"}],"has_more":false}`, page), http.StatusOK), nil
		}
		return reply(fmt.Sprintf(`{"data":[{"id":"a%d","created_at":"2026-09-07T12:00:00Z"}],"has_more":true,"last_id":"c%d"}`, page, page), http.StatusOK), nil
	})
	rt.putCounts = map[string]int{}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	prefix := p.conf.key()
	whole := rt.putCounts[prefix+keyManifest] + rt.putCounts[prefix+keyWalkMan]
	journal := rt.putsUnder(prefix + keyWalkDelta)
	t.Logf("%d pages: %d complete-manifest writes, %d journal records", page, whole, journal)
	// One fold per full journal, plus the final dataset commit.
	if want := 2*(pages/maxWalkDeltas) + 2; whole > want {
		t.Fatalf("a %d-page walk rewrote the complete manifests %d times, want at most %d", page, whole, want)
	}
	// Every continuing page journals its own change except the ones that fold.
	if journal < pages/2 {
		t.Fatalf("page progress was not journaled: %d records for %d continuing pages", journal, pages-1)
	}
	if len(rt.entries) != pages {
		t.Fatalf("entries=%d, want one per page", len(rt.entries))
	}
	if !rt.committed(p) {
		t.Fatal("the completed traversal was not checkpointed")
	}
}

// TestJournalFoldBoundaryResumesWithoutSkippingOrDuplicating drives one page
// per cycle with a full restart between each, across the fold boundary, so the
// journal is written, read back, folded, and emptied by different processes.
func TestJournalFoldBoundaryResumesWithoutSkippingOrDuplicating(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Max_Pages = 1
	const pages = maxWalkDeltas + 4
	served := map[string]bool{}
	serve := transport(func(r *http.Request) (*http.Response, error) {
		cursor := r.URL.Query().Get("after_id")
		index := 0
		if cursor != "" {
			n, err := strconv.Atoi(strings.TrimPrefix(cursor, "c"))
			if err != nil {
				t.Fatalf("unexpected cursor %q", cursor)
			}
			index = n
		}
		served[cursor] = true
		// Inside the overlap window, so a completed traversal retains every
		// identity the journal carried instead of pruning it by time.
		body := fmt.Sprintf(`{"data":[{"id":"a%02d","created_at":"2026-09-07T23:58:00Z"}]`, index)
		if index+1 >= pages {
			return reply(body+`,"has_more":false}`, http.StatusOK), nil
		}
		return reply(fmt.Sprintf(`%s,"has_more":true,"last_id":"c%d"}`, body, index+1), http.StatusOK), nil
	})
	p.http.Transport = serve

	current := p
	for i := 0; i < pages; i++ {
		cont, err := current.Handle(t.Context(), rt)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		if i < pages-1 && (cont == nil || cont.Delay != 0) {
			t.Fatalf("page %d did not request an immediate continuation: %v", i, cont)
		}
		current = restart(t, current, rt)
		current.conf.Max_Pages = 1
	}
	seen := map[string]int{}
	for _, ent := range rt.entries {
		seen[string(ent.Data)]++
	}
	if len(rt.entries) != pages || len(seen) != pages {
		t.Fatalf("restarts across the journal fold skipped or duplicated records: entries=%d distinct=%d want=%d",
			len(rt.entries), len(seen), pages)
	}
	if cp := readCheckpoint(t, rt, p.conf.key()); cp.Walk != nil {
		t.Fatalf("the traversal did not complete: %+v", cp.Walk)
	}
	// A further poll of the same, unchanged data writes nothing new: the
	// identities the journal carried are all in the committed manifest.
	before := len(rt.entries)
	current.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"a00","created_at":"2026-09-07T23:58:00Z"}],"has_more":false}`, http.StatusOK), nil
	})
	if _, err := current.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != before {
		t.Fatal("a journaled identity was lost when the walk completed")
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// combinedEntries is the total retained dedup state for a dataset: the
// committed manifest plus whatever an in-flight page walk is holding.
// It is read back through the same loader collection uses, so the per-page
// journal is folded in and the count is the state that would actually be
// resumed rather than only the part written whole.
func combinedEntries(t *testing.T, rt *runtime, prefix string) (int, bool) {
	t.Helper()
	cp := readCheckpoint(t, rt, prefix)
	n := len(cp.Manifest)
	walking := cp.Walk != nil
	if walking {
		n += len(cp.Walk.Manifest)
	}
	return n, walking
}

// The combined retained manifest must never exceed its configured bound at
// any persisted boundary -- including mid-traversal, where a continuation
// cursor is stored alongside an in-flight manifest -- while the traversal
// still completes and every record is still emitted.
func TestCombinedManifestStaysWithinBoundAtEveryBoundary(t *testing.T) {
	const limit = 5
	for _, tc := range []struct {
		name   string
		newIDs int // brand new identities the vendor returns
	}{
		{name: "exact limit", newIDs: limit},
		{name: "limit plus one", newIDs: limit + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, rt := setup(t, "organizations")
			p.maxManifestEntries = limit
			p.conf.Max_Pages = 1 // force a continuation cursor between pages

			// Start from a primary manifest already at the bound, holding
			// identities the vendor will not return again.
			prior := manifest{}
			for i := 0; i < limit; i++ {
				prior[fmt.Sprintf("uuid:old%d", i)] = manifestEntry{Digest: "d", Seen: int64(i)}
			}
			if e := commitDataset(rt, p.conf.key(), p.now().Add(-time.Hour), prior, false); e != nil {
				t.Fatal(e)
			}

			ids := make([]string, tc.newIDs)
			for i := range ids {
				ids[i] = fmt.Sprintf("new%d", i)
			}
			half := len(ids) / 2
			p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("page") == "" {
					return reply(rowsFor(ids[:half])+`,"has_more":true,"next_page":"p2"}`, 200), nil
				}
				return reply(rowsFor(ids[half:])+`,"has_more":false}`, 200), nil
			})

			// Drive to completion across restarts, checking the bound after
			// every persisted boundary.
			current := p
			completed := false
			for cycle := 0; cycle < 6 && !completed; cycle++ {
				cont, e := current.Handle(t.Context(), rt)
				if e != nil {
					t.Fatalf("cycle %d: %v", cycle, e)
				}
				n, walking := combinedEntries(t, rt, p.conf.key())
				if n > limit {
					t.Fatalf("cycle %d: %d combined entries exceed the bound of %d (walking=%v)",
						cycle, n, limit, walking)
				}
				completed = cont != nil && cont.Delay != 0 && !walking
				current = restart(t, current, rt)
				current.maxManifestEntries = limit
				current.conf.Max_Pages = 1
			}
			if !completed {
				t.Fatal("traversal never completed")
			}
			// Every new identity was emitted at least once.
			for _, id := range ids {
				found := false
				for _, ent := range rt.entries {
					if strings.Contains(string(ent.Data), `"`+id+`"`) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("record %s was never emitted", id)
				}
			}
			if n, _ := combinedEntries(t, rt, p.conf.key()); n > limit {
				t.Fatalf("final state holds %d combined entries, bound is %d", n, limit)
			}
			if tc.newIDs <= limit {
				final := readCheckpoint(t, rt, p.conf.key())
				if got := len(final.Manifest); got != tc.newIDs {
					t.Fatalf("completed manifest retained %d of %d live identities", got, tc.newIDs)
				}

				// A second identical traversal must be fully deduplicated when
				// the complete live set fits in the manifest bound.
				before := len(rt.entries)
				completed = false
				for cycle := 0; cycle < 6 && !completed; cycle++ {
					cont, e := current.Handle(t.Context(), rt)
					if e != nil {
						t.Fatalf("repeat cycle %d: %v", cycle, e)
					}
					_, walking := combinedEntries(t, rt, p.conf.key())
					completed = cont != nil && cont.Delay != 0 && !walking
					current = restart(t, current, rt)
					current.maxManifestEntries = limit
					current.conf.Max_Pages = 1
				}
				if !completed {
					t.Fatal("repeat traversal never completed")
				}
				if got := len(rt.entries) - before; got != 0 {
					t.Fatalf("repeat traversal rewrote %d unchanged records", got)
				}
			}
		})
	}
}

func TestManifestEvictorHandlesEmptyIdentityKey(t *testing.T) {
	committed := manifest{"": {Digest: "old", Seen: 1}}
	current := manifest{}
	key, fromWalk, ok := newManifestEvictor(committed, current).evict(committed, current)
	if !ok || key != "" || fromWalk || len(committed) != 0 {
		t.Fatalf("empty committed identity key was not evicted: %#v", committed)
	}
	current[""] = manifestEntry{Digest: "new", Seen: 2}
	key, fromWalk, ok = newManifestEvictor(committed, current).evict(committed, current)
	if !ok || key != "" || !fromWalk || len(current) != 0 {
		t.Fatalf("empty current identity key was not evicted: %#v", current)
	}
}

func rowsFor(ids []string) string {
	var b strings.Builder
	b.WriteString(`{"data":[`)
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"uuid":%q}`, id)
	}
	b.WriteString(`]`)
	return b.String()
}
