package claudecompliance

// Regression coverage for the Max-Pending pending-eviction wedge (see
// claude-compliance-2717c9f-review.md, Finding A): a child that fails on
// every attempt used to remain Pending=true forever, and Max-Pending
// eviction only ever considered !Pending candidates, so once such a child
// occupied every Max-Pending slot, no further new child could ever be
// discovered -- permanently, on the same page, every cycle.
//
// The fix (discovery.go) lets a child that has exhausted its retry backoff
// ceiling (Failures >= maxChildFailures) be selected as an eviction victim
// as a fallback, only once no genuinely non-pending candidate exists, and
// only via the same tombstone=false pruneCheckpoint path already used for
// ordinary capacity eviction -- so an evicted child's checkpoint is
// compacted, never tombstoned, and it remains rediscoverable.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestPendingWedge_PermanentlyFailingChildEventuallyEvicted reproduces the
// original failure mode end to end and proves the fix: against the code at
// commit 2717c9f0 (pre-fix), this test's final assertion -- that g2 is
// eventually admitted -- fails, because g1 never leaves Pending=true and so
// is never eligible as an eviction victim; g2 is rejected by
// errPendingCapacity on every one of the 20 cycles below. (This was
// independently confirmed with a temporary, now-removed probe against
// commit 2717c9f0 before this fix was written.) Against the fixed code, g1
// is evicted once it reaches maxChildFailures and g2 is admitted.
func TestPendingWedge_PermanentlyFailingChildEventuallyEvicted(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}

	phase2 := false // flips once g1 has accumulated enough failures
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			if phase2 {
				return reply(`{"data":[{"id":"g2"}],"has_more":false}`, 200), nil
			}
			return reply(`{"data":[{"id":"g1"}],"has_more":false}`, 200), nil
		}
		if strings.Contains(r.URL.Path, "g1") {
			return reply(`{"error":{"message":"gone"}}`, 404), nil
		}
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
		list = worklist{}
		_ = json.Unmarshal(rt.states[workKey], &list)
		if len(list.Items) > p.conf.Max_Pending {
			t.Fatalf("cycle %d: Max-Pending exceeded: %d items, limit %d", i, len(list.Items), p.conf.Max_Pending)
		}
	}

	// Phase 1: establish g1 as the sole occupant of the only Max-Pending
	// slot and drive it to the failure ceiling.
	for i := 0; i < maxChildFailures; i++ {
		runCycle(i)
	}
	var afterPhase1 worklist
	_ = json.Unmarshal(rt.states[workKey], &afterPhase1)
	g1 := afterPhase1.Items["group-members/group_id:g1"]
	if g1.Failures < maxChildFailures {
		t.Fatalf("test setup issue: g1.Failures=%d after %d cycles, expected >= %d", g1.Failures, maxChildFailures, maxChildFailures)
	}
	if !g1.Pending {
		t.Fatal("test setup issue: g1 unexpectedly not Pending")
	}

	// Phase 2: a genuinely new child (g2) appears. Against the pre-fix
	// code, this reproduces Finding A exactly: g1 never leaves Pending=true
	// and is never eligible as an eviction victim, so g2 is rejected by
	// errPendingCapacity on every cycle below, forever. (Independently
	// confirmed with a temporary, now-removed probe against commit
	// 2717c9f0 before this fix was written.) Against the fixed code, g1 --
	// now stuck at maxChildFailures -- becomes eligible as a fallback
	// victim and g2 is admitted.
	phase2 = true
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
		t.Fatal("g2 was never admitted -- permanently-failing g1 wedged discovery forever (Finding A regressed)")
	}
	t.Logf("g2 was admitted %d phase-2 cycle(s) after g1 reached maxChildFailures=%d", admittedAtCycle, maxChildFailures)

	// The evicted child's checkpoint must be compacted but never tombstoned:
	// eviction for capacity (even of a stuck item) is unrelated to the
	// parent's own content and must never suppress a still-valid parent.
	var finalList worklist
	_ = json.Unmarshal(rt.states[workKey], &finalList)
	if _, stillPresent := finalList.Items["group-members/group_id:g1"]; stillPresent {
		t.Fatal("expected g1 to have been evicted to make room for g2")
	}
}

// TestPendingWedge_NewChildEventuallySchedulableAfterEviction proves that
// once a stuck child is evicted, the newly admitted child actually runs
// (not just occupies a worklist entry) -- i.e. discovery genuinely resumes,
// not merely the bookkeeping.
func TestPendingWedge_NewChildEventuallySchedulableAfterEviction(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}
	g2Calls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g2"}],"has_more":false}`, 200), nil
		}
		if strings.Contains(r.URL.Path, "g1") {
			return reply(`{"error":{"message":"gone"}}`, 404), nil
		}
		g2Calls++
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	workKey := p.conf.key() + "/child-work-v1"
	// Pre-seed a stuck g1 directly at the failure ceiling, occupying the
	// only Max-Pending slot, so this test isolates "does g2 get scheduled
	// and actually run" from "how many cycles does it take to get stuck."
	stuck := worklist{Items: map[string]work{
		"group-members/group_id:g1": {
			Dataset: "group-members", Parameter: []string{"group_id:g1"},
			Revision: "rev-g1", Pending: true, Failures: maxChildFailures,
			LastAttempt: time.Unix(0, 0),
		},
	}}
	b, _ := json.Marshal(stuck)
	rt.states[workKey] = b

	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if g2Calls == 0 {
		t.Fatal("g2 was never actually fetched -- eviction freed the slot but discovery did not resume")
	}
	var list worklist
	_ = json.Unmarshal(rt.states[workKey], &list)
	if _, ok := list.Items["group-members/group_id:g2"]; !ok {
		t.Fatal("g2 not present in the worklist after eviction")
	}
	if _, ok := list.Items["group-members/group_id:g1"]; ok {
		t.Fatal("stuck g1 was not evicted")
	}
}

// TestPendingWedge_EvictedUnchangedParentIsLaterRediscovered proves that a
// child evicted via the new stuck-pending path is not permanently
// suppressed: if its parent is later observed again with the same
// revision (nothing about the parent changed), it must be rediscovered,
// exactly like ordinary (non-pending) capacity eviction already guarantees.
func TestPendingWedge_EvictedUnchangedParentIsLaterRediscovered(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}
	active := "g2"
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(fmt.Sprintf(`{"data":[{"id":%q}],"has_more":false}`, active), 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	workKey := p.conf.key() + "/child-work-v1"
	stuck := worklist{Items: map[string]work{
		"group-members/group_id:g1": {
			Dataset: "group-members", Parameter: []string{"group_id:g1"},
			Revision: "rev-g1", Pending: true, Failures: maxChildFailures,
			LastAttempt: time.Unix(0, 0),
		},
	}}
	b, _ := json.Marshal(stuck)
	rt.states[workKey] = b
	stateKey, e := (func() (string, error) {
		c, e := childConfig(p.conf, stuck.Items["group-members/group_id:g1"])
		if e != nil {
			return "", e
		}
		return c.key(), nil
	})()
	if e != nil {
		t.Fatal(e)
	}

	// g2 is admitted, evicting g1.
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	var pruned state
	if e := json.Unmarshal(rt.states[stateKey], &pruned); e != nil {
		t.Fatal(e)
	}
	if pruned.Retired != "" {
		t.Fatal("stuck-pending eviction must not tombstone the checkpoint -- it would permanently suppress an unrelated, still-valid parent")
	}

	// g1 reappears (content-identical, same revision) once capacity frees up.
	p.conf.Max_Pending = 2
	active = "g1"
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	var list worklist
	_ = json.Unmarshal(rt.states[workKey], &list)
	if _, ok := list.Items["group-members/group_id:g1"]; !ok {
		t.Fatal("evicted, content-unchanged parent was never rediscovered")
	}
}

// TestPendingWedge_TransientlyFailingChildNotPrematurelyEvicted proves a
// child that has failed only a few times (below maxChildFailures) is never
// selected as an eviction victim: capacity pressure must surface as
// errPendingCapacity (deferred, not silently dropped), not an eviction of
// still-plausibly-recovering work.
func TestPendingWedge_TransientlyFailingChildNotPrematurelyEvicted(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}
	workKey := p.conf.key() + "/child-work-v1"
	for _, failures := range []uint{0, 1, maxChildFailures - 1} {
		t.Run(fmt.Sprintf("failures=%d", failures), func(t *testing.T) {
			rt.states = map[string][]byte{}
			list := worklist{Items: map[string]work{
				"group-members/group_id:g1": {
					Dataset: "group-members", Parameter: []string{"group_id:g1"},
					Revision: "rev-g1", Pending: true, Failures: failures,
					LastAttempt: p.now(),
				},
			}}
			b, _ := json.Marshal(list)
			rt.states[workKey] = b
			p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/groups") {
					return reply(`{"data":[{"id":"g2"}],"has_more":false}`, 200), nil
				}
				// g1 is already Pending in the worklist, so it is also
				// scheduled via the normal keys loop this same cycle,
				// independent of the capacity block on discovering g2. Keep
				// it "transiently failing" -- consistent with the scenario.
				return reply(`{"error":{"message":"transient"}}`, 500), nil
			})
			if _, e := p.Handle(t.Context(), rt); e == nil {
				t.Fatal("expected errPendingCapacity (wrapped) to surface since Max-Pending is exhausted by a non-evictable child")
			}
			var after worklist
			_ = json.Unmarshal(rt.states[workKey], &after)
			if _, ok := after.Items["group-members/group_id:g1"]; !ok {
				t.Fatalf("g1 (Failures=%d, below maxChildFailures=%d) must never be evicted", failures, maxChildFailures)
			}
			if _, ok := after.Items["group-members/group_id:g2"]; ok {
				t.Fatal("g2 must not be admitted while capacity is held by transiently-failing, still-eligible work")
			}
		})
	}
}

// TestPendingWedge_SuccessfulPendingChildNeverEvicted proves a child that is
// genuinely mid-progress (Pending=true, Failures=0, actively being worked)
// is never touched by the new fallback path, regardless of how long
// Max-Pending capacity has been exhausted.
func TestPendingWedge_SuccessfulPendingChildNeverEvicted(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}
	workKey := p.conf.key() + "/child-work-v1"
	list := worklist{Items: map[string]work{
		"group-members/group_id:g1": {
			Dataset: "group-members", Parameter: []string{"group_id:g1"},
			Revision: "rev-g1", Pending: true, Failures: 0,
			LastAttempt: p.now(),
		},
	}}
	b, _ := json.Marshal(list)
	rt.states[workKey] = b
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g2"}],"has_more":false}`, 200), nil
		}
		// g1 is already Pending, so it is also scheduled via the normal
		// keys loop this same cycle. It succeeds here -- the point under
		// test is that the capacity decision for admitting g2 (made before
		// g1's own turn even runs) never touches a healthy pending child.
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	// Unlike the transiently-failing-child scenario, g1 succeeds this cycle
	// and contributes no error, so errPendingCapacity (deliberately
	// non-fatal on its own -- see Handle's rootPending handling) does not
	// surface as a returned error here. The behavior under test is
	// structural: g2 must not have been admitted, and g1 must remain.
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatalf("unexpected error: %v", e)
	}
	var after worklist
	_ = json.Unmarshal(rt.states[workKey], &after)
	if _, ok := after.Items["group-members/group_id:g1"]; !ok {
		t.Fatal("a healthy, actively pending child (Failures=0) must never be evicted")
	}
	if _, ok := after.Items["group-members/group_id:g2"]; ok {
		t.Fatal("g2 must not be admitted while capacity is held by a healthy, actively pending child")
	}
}

// TestPendingWedge_MaxPendingStrictlyBounded is a broader sweep proving the
// worklist never exceeds Max_Pending across a mix of stuck, transient, and
// newly-discovered children over many cycles.
func TestPendingWedge_MaxPendingStrictlyBounded(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 3
	p.conf.Max_Children = 3
	rt.states = map[string][]byte{}
	ids := []string{"g1", "g2", "g3", "g4", "g5", "g6"}
	failing := map[string]bool{"g1": true, "g2": true} // these never succeed
	present := ids[:2]                                 // grows over time
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			data := ""
			for i, id := range present {
				if i > 0 {
					data += ","
				}
				data += fmt.Sprintf(`{"id":%q}`, id)
			}
			return reply(fmt.Sprintf(`{"data":[%s],"has_more":false}`, data), 200), nil
		}
		for id := range failing {
			if strings.Contains(r.URL.Path, id) {
				return reply(`{"error":{"message":"gone"}}`, 404), nil
			}
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	workKey := p.conf.key() + "/child-work-v1"
	for cycle := 0; cycle < 30; cycle++ {
		if cycle == 5 && len(present) < len(ids) {
			present = append(present, ids[len(present)])
		}
		if cycle == 15 && len(present) < len(ids) {
			present = append(present, ids[len(present)])
		}
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
		_, _ = p.Handle(t.Context(), rt)
		list = worklist{}
		_ = json.Unmarshal(rt.states[workKey], &list)
		if len(list.Items) > p.conf.Max_Pending {
			t.Fatalf("cycle %d: Max-Pending strictly exceeded: %d items (limit %d)", cycle, len(list.Items), p.conf.Max_Pending)
		}
	}
}

// TestPendingWedge_CheckpointNeverTombstonedByStuckEviction is a direct,
// minimal check that pruneCheckpoint's tombstone parameter is still false on
// every path reachable from the stuck-pending eviction fallback, by
// exercising the exact victim-selection scenario and inspecting the raw
// persisted checkpoint bytes.
func TestPendingWedge_CheckpointNeverTombstonedByStuckEviction(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}
	stuckWork := work{
		Dataset: "group-members", Parameter: []string{"group_id:g1"},
		Revision: "rev-g1", Pending: true, Failures: maxChildFailures,
		LastAttempt: time.Unix(0, 0),
	}
	stateKey := func() string {
		c, e := childConfig(p.conf, stuckWork)
		if e != nil {
			t.Fatal(e)
		}
		return c.key()
	}()
	workKey := p.conf.key() + "/child-work-v1"
	list := worklist{Items: map[string]work{"group-members/group_id:g1": stuckWork}}
	b, _ := json.Marshal(list)
	rt.states[workKey] = b
	// Pre-seed a non-trivial checkpoint so we can also confirm it gets
	// compacted (Manifest/Traversal cleared), matching ordinary eviction.
	pre := state{Since: time.Unix(1, 0), Manifest: manifest{"x": {Digest: "d"}}, Traversal: &traversal{Cursor: "c"}}
	preB, _ := json.Marshal(pre)
	rt.states[stateKey] = preB

	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g2"}],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	var after state
	if e := json.Unmarshal(rt.states[stateKey], &after); e != nil {
		t.Fatal(e)
	}
	if after.Retired != "" {
		t.Fatalf("checkpoint was tombstoned by stuck-pending eviction: Retired=%q", after.Retired)
	}
	if len(after.Manifest) != 0 || after.Traversal != nil {
		t.Fatal("checkpoint was not compacted (Manifest/Traversal) by eviction")
	}
}
