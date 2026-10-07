// Coverage for the stored-state contract: a fresh installation writes one
// discrete layout, and restarting against that same layout resumes safely --
// no record skipped, none ingested twice.
package claudecompliance

import (
	"fmt"
	"net/http"
	"sort"
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
		keySince: true, keyManifest: true, keyHistory: true, keyRetired: true,
		keyWalkCursor: true, keyWalkSince: true, keyWalkUntil: true,
		keyWalkStarted: true, keyWalkHistory: true, keyWalkMan: true,
		keyChildren: true,
	}
	prefix := p.conf.key()
	var unexpected []string
	for _, k := range rt.storedKeys() {
		suffix, ok := strings.CutPrefix(k, prefix)
		if !ok || !known[suffix] {
			unexpected = append(unexpected, k)
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) != 0 {
		t.Fatalf("fresh install wrote keys outside the discrete layout: %v", unexpected)
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
func combinedEntries(t *testing.T, rt *runtime, prefix string) (int, bool) {
	t.Helper()
	cp := readCheckpoint(t, rt, prefix)
	primary, err := getManifest(rt, prefix+keyManifest)
	if err != nil {
		t.Fatal(err)
	}
	walk, err := getManifest(rt, prefix+keyWalkMan)
	if err != nil {
		t.Fatal(err)
	}
	n := len(primary) + len(walk)
	walking := cp.Walk != nil
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
		})
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
