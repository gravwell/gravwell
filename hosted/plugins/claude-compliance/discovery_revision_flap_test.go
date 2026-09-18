package claudecompliance

// Regression coverage for Finding 1 of claude-compliance-6e0dbb23-review.md:
// a parent whose content revision changes on every poll (any live field --
// member_count, updated_at, a monotonic seq, etc.) used to reset its own
// permanently-failing child's Failures counter back to 0 every discovery
// pass, because the old carry-forward at discovery.go only preserved
// Failures (and RetryAt) when old.Revision == w.Revision. Since
// maxChildFailures is the sole eligibility gate for the stuck-pending
// eviction fallback added by commit 6e0dbb23 (see
// discovery_pending_wedge_test.go), a child could never reach that
// threshold as long as its parent's content kept changing at least once
// every ~10 attempts -- something the plugin does not control and cannot
// assume about vendor data. This exactly defeated the stated purpose of
// that commit.
//
// The fix carries a child's Failures forward across a revision change
// unconditionally -- only an actual successful request, or eviction itself,
// may reset it -- while RetryAt still resets on a revision change, so
// genuinely new content is still retried promptly rather than waiting out a
// stale backoff.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestRevisionFlap_StuckChildEvictedDespiteContentChurnThenRediscovered is
// the permanent end-to-end regression test for Finding 1. It proves, in a
// single scenario, every guarantee the fix depends on:
//
//   - the parent's content revision changes on every discovery pass;
//   - its child endpoint fails permanently;
//   - the child's Failures counter keeps increasing across those revision
//     changes instead of being reset;
//   - it reaches maxChildFailures;
//   - it becomes eligible for Max-Pending capacity eviction;
//   - a waiting sibling is admitted and actually executed (not merely
//     recorded in the worklist);
//   - the evicted child's checkpoint is compacted but never tombstoned;
//   - the evicted child is later rediscovered once its parent reappears.
//
// Against the pre-fix code, the assertion after phase 1 fails outright
// (Failures never climbs past 1, see the review's probe), so the whole
// scenario never gets far enough to reach phase 2 or 3.
func TestRevisionFlap_StuckChildEvictedDespiteContentChurnThenRediscovered(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}

	seq := 0
	flapping := true // g1's parent content changes on every /groups poll while true
	phase := "g1"    // "g1" | "g2" -- which id /groups currently reports
	g2Calls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			if phase == "g2" {
				return reply(`{"data":[{"id":"g2"}],"has_more":false}`, 200), nil
			}
			if flapping {
				seq++
			}
			return reply(fmt.Sprintf(`{"data":[{"id":"g1","seq":%d}],"has_more":false}`, seq), 200), nil
		}
		if strings.Contains(r.URL.Path, "g1") {
			return reply(`{"error":{"message":"boom"}}`, 500), nil
		}
		g2Calls++
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	workKey := p.conf.key() + "/child-work-v1"

	runCycle := func(i int) {
		var list worklist
		_ = json.Unmarshal(rt.states[workKey], &list)
		if list.Items != nil {
			for k, it := range list.Items {
				it.RetryAt = time.Time{}
				list.Items[k] = it
			}
			if b, e := json.Marshal(list); e == nil {
				rt.states[workKey] = b
			}
		}
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Logf("cycle %d: Handle returned %v (expected while g1 is failing)", i, e)
		}
	}

	// Phase 1: g1's parent content changes on every poll while its own
	// child endpoint fails on every attempt. Drive it to the failure
	// ceiling.
	for i := 0; i < maxChildFailures; i++ {
		runCycle(i)
	}
	if seq < maxChildFailures {
		t.Fatalf("test setup issue: parent revision did not actually change every cycle (seq=%d after %d cycles)", seq, maxChildFailures)
	}
	var afterPhase1 worklist
	_ = json.Unmarshal(rt.states[workKey], &afterPhase1)
	g1 := afterPhase1.Items["group-members/group_id:g1"]
	if g1.Failures < maxChildFailures {
		t.Fatalf("Failures did not survive revision flapping: g1.Failures=%d after %d cycles of a changing parent revision, expected >= %d (Finding 1 regressed)", g1.Failures, maxChildFailures, maxChildFailures)
	}
	if !g1.Pending {
		t.Fatal("test setup issue: g1 unexpectedly not Pending")
	}

	stateKey, e := (func() (string, error) {
		c, e := childConfig(p.conf, g1)
		if e != nil {
			return "", e
		}
		return c.key(), nil
	})()
	if e != nil {
		t.Fatal(e)
	}

	// Phase 2: a genuinely new sibling (g2) appears. Against the pre-fix
	// code this would be rejected by errPendingCapacity forever, because
	// g1's Failures kept getting reset by the flapping revision and so
	// never reached maxChildFailures. Against the fix, g1 is now eligible
	// and gets evicted, and g2 is admitted -- and actually runs.
	phase = "g2"
	admittedAtCycle := -1
	for i := 0; i < 5; i++ {
		runCycle(maxChildFailures + i)
		var list worklist
		_ = json.Unmarshal(rt.states[workKey], &list)
		if _, ok := list.Items["group-members/group_id:g2"]; ok && admittedAtCycle == -1 {
			admittedAtCycle = i
		}
	}
	if admittedAtCycle == -1 {
		t.Fatal("g2 was never admitted -- revision-flapping g1 wedged discovery forever (Finding 1 regressed)")
	}
	if g2Calls == 0 {
		t.Fatal("g2 was admitted into the worklist but its endpoint was never actually called -- discovery did not genuinely resume")
	}
	var afterPhase2 worklist
	_ = json.Unmarshal(rt.states[workKey], &afterPhase2)
	if _, stillPresent := afterPhase2.Items["group-members/group_id:g1"]; stillPresent {
		t.Fatal("expected g1 to have been evicted to make room for g2")
	}

	// The evicted child's checkpoint must be compacted, not tombstoned --
	// eviction for capacity is unrelated to the parent's own validity and
	// must never permanently suppress it.
	var pruned state
	if e := json.Unmarshal(rt.states[stateKey], &pruned); e != nil {
		t.Fatal(e)
	}
	if pruned.Retired != "" {
		t.Fatal("stuck-pending eviction must not tombstone the checkpoint -- it would permanently suppress a still-valid parent")
	}

	// Phase 3: g1's parent stops flapping and reappears once capacity
	// frees up. Eviction must not be permanent suppression.
	phase = "g1"
	flapping = false
	p.conf.Max_Pending = 2
	runCycle(maxChildFailures + 5)
	var afterPhase3 worklist
	_ = json.Unmarshal(rt.states[workKey], &afterPhase3)
	if _, ok := afterPhase3.Items["group-members/group_id:g1"]; !ok {
		t.Fatal("evicted child was never rediscovered once its parent reappeared and capacity freed")
	}
}

// TestRevisionFlap_RevisionChangeResetsRetryAtButPreservesFailures proves
// the two halves of the fix together. g1 starts Pending with Failures=5 and
// RetryAt an hour in the future (mid-backoff). The parent's content then
// changes. If RetryAt had not reset to allow prompt eligibility, g1 would
// not be attempted this cycle at all and Failures would stay at 5; if
// Failures had been reset by the revision change (the pre-fix bug), a
// failed attempt would land on 1, not 6. Observing Failures==6 after this
// single cycle is therefore only possible if RetryAt was reset (so the
// attempt actually ran) *and* Failures was preserved and then incremented
// by that attempt's failure (5 -> 6) rather than reset and incremented
// (0 -> 1).
func TestRevisionFlap_RevisionChangeResetsRetryAtButPreservesFailures(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}
	workKey := p.conf.key() + "/child-work-v1"

	staleRetryAt := p.now().Add(time.Hour)
	seeded := worklist{Items: map[string]work{
		"group-members/group_id:g1": {
			Dataset: "group-members", Parameter: []string{"group_id:g1"},
			Revision: "rev-old", Pending: true, Failures: 5, RetryAt: staleRetryAt,
			LastAttempt: p.now().Add(-time.Minute),
		},
	}}
	b, _ := json.Marshal(seeded)
	rt.states[workKey] = b

	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g1","seq":2}],"has_more":false}`, 200), nil
		}
		return reply(`{"error":{"message":"boom"}}`, 500), nil
	})

	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Logf("Handle returned %v (expected -- g1's attempt fails)", e)
	}
	var after worklist
	_ = json.Unmarshal(rt.states[workKey], &after)
	g1 := after.Items["group-members/group_id:g1"]
	if g1.Revision == "rev-old" {
		t.Fatal("test setup issue: changed parent content did not produce a new revision")
	}
	if g1.Failures != 6 {
		t.Fatalf("expected Failures=6 (5 carried forward across the revision change, then incremented by this cycle's failed attempt); got %d -- either RetryAt was not reset (no attempt ran) or Failures was reset by the revision change", g1.Failures)
	}
	if g1.RetryAt.Equal(staleRetryAt) {
		t.Fatal("expected RetryAt to have been recomputed from this cycle's attempt, not left at its stale pre-revision-change value")
	}
}

// TestRevisionFlap_StaticParentPreservesRetryAtAndFailures proves the
// unchanged-revision path still behaves exactly as before the fix. It
// drives the same discover() carry-forward code path that
// TestRevisionFlap_RevisionChangeResetsRetryAtButPreservesFailures
// exercises for a *changed* revision, but via the hourly refresh trigger
// (an unchanged, already-completed parent re-observed after an hour) so the
// nested "old.Revision == w.Revision" branch actually runs. Both RetryAt
// and Failures must come through the cycle completely unmodified.
func TestRevisionFlap_StaticParentPreservesRetryAtAndFailures(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}
	workKey := p.conf.key() + "/child-work-v1"
	oldNow := p.now()

	const parentBody = `{"data":[{"id":"g1"}],"has_more":false}`
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(parentBody, 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	// Discover g1's real, computed revision first via an ordinary
	// successful cycle, so the replay below is a genuine unchanged-revision
	// case rather than a coincidental string match.
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	var seeded worklist
	_ = json.Unmarshal(rt.states[workKey], &seeded)
	g1 := seeded.Items["group-members/group_id:g1"]
	if g1.Pending {
		t.Fatal("test setup issue: expected g1 to have completed successfully")
	}
	// Push it just outside the hourly refresh window and stamp sentinel
	// Failures/RetryAt values to prove they survive the refresh untouched.
	sentinelRetryAt := oldNow.Add(10 * time.Hour)
	g1.LastCompleted = oldNow.Add(-2 * time.Hour)
	g1.Failures = 6
	g1.RetryAt = sentinelRetryAt
	seeded.Items["group-members/group_id:g1"] = g1
	b, _ := json.Marshal(seeded)
	rt.states[workKey] = b

	// Same parent body (identical revision), but now more than an hour
	// after LastCompleted, so the hourly refresh path runs the carry-forward
	// logic under test even though nothing about the parent changed.
	p.now = func() time.Time { return oldNow.Add(2 * time.Hour) }
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	var after worklist
	_ = json.Unmarshal(rt.states[workKey], &after)
	g1After := after.Items["group-members/group_id:g1"]
	if !g1After.RetryAt.Equal(sentinelRetryAt) {
		t.Fatalf("expected RetryAt to be preserved when revision is unchanged, got %v want %v", g1After.RetryAt, sentinelRetryAt)
	}
	if g1After.Failures != 6 {
		t.Fatalf("expected Failures to be preserved when revision is unchanged, got %d want 6", g1After.Failures)
	}
}

// TestRevisionFlap_SuccessfulChildRequestResetsFailures proves the only
// other path that may clear Failures -- an actual successful request
// against the child -- still works: it is unconditional and independent of
// the revision-carry-forward logic under test above.
func TestRevisionFlap_SuccessfulChildRequestResetsFailures(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}
	workKey := p.conf.key() + "/child-work-v1"
	seeded := worklist{Items: map[string]work{
		"group-members/group_id:g1": {
			Dataset: "group-members", Parameter: []string{"group_id:g1"},
			Revision: "rev-g1", Pending: true, Failures: 7,
			LastAttempt: p.now().Add(-time.Minute),
		},
	}}
	b, _ := json.Marshal(seeded)
	rt.states[workKey] = b
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g1"}],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	var after worklist
	_ = json.Unmarshal(rt.states[workKey], &after)
	g1 := after.Items["group-members/group_id:g1"]
	if g1.Failures != 0 {
		t.Fatalf("expected a successful child request to reset Failures to 0, got %d", g1.Failures)
	}
}

// TestRevisionFlap_NewlyDiscoveredChildStartsWithZeroFailures proves a
// genuinely new, never-before-seen child identity is unaffected by the
// carry-forward change: it always starts at Failures=0. It is observed here
// via a single failed attempt (Failures==1 iff it started at 0) since the
// discovery and first-attempt phases run within the same Handle() cycle.
func TestRevisionFlap_NewlyDiscoveredChildStartsWithZeroFailures(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}
	workKey := p.conf.key() + "/child-work-v1"
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g1"}],"has_more":false}`, 200), nil
		}
		return reply(`{"error":{"message":"boom"}}`, 500), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Logf("Handle returned %v (expected -- g1's first attempt fails)", e)
	}
	var after worklist
	_ = json.Unmarshal(rt.states[workKey], &after)
	g1, ok := after.Items["group-members/group_id:g1"]
	if !ok {
		t.Fatal("expected g1 to be discovered")
	}
	if g1.Failures != 1 {
		t.Fatalf("expected a genuinely new child identity to start with Failures=0 and read 1 after its single failed attempt, got %d", g1.Failures)
	}
}
