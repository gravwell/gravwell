package claudecompliance

// Coverage for Max-Pending eviction under a permanently failing child. A
// child that fails on every attempt stays Pending, so if eviction only ever
// considered non-pending candidates such a child could occupy every
// Max-Pending slot and wedge discovery of any further child forever.
//
// A child that has exhausted its retry backoff ceiling
// (Failures >= maxChildFailures) is therefore eligible as a fallback
// eviction victim, but only once no genuinely non-pending candidate exists,
// and only through the same tombstone=false pruneCheckpoint path used for
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

func seedScheduledWork(t *testing.T, p *Plugin, rt *runtime, items map[string]work) string {
	t.Helper()
	key := p.conf.key() + "/children"
	b, err := json.Marshal(worklist{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if e := rt.Put(key, b); e != nil {
		t.Fatal(e)
	}
	return key
}

// TestPendingWedge_ScheduledWorkCannotBeEvictedMidCycle covers nested
// discovery at capacity. A role can discover permissions while the outer
// loop is processing an already-sorted worklist. None of those scheduled
// keys may be evicted until that schedule has finished.
func TestPendingWedge_ScheduledWorkCannotBeEvictedMidCycle(t *testing.T) {
	for _, tc := range []struct {
		name            string
		currentFailures uint
		laterFailures   uint
	}{
		{name: "later scheduled key", laterFailures: maxChildFailures},
		{name: "current scheduled key", currentFailures: maxChildFailures},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, rt := setup(t, "organizations")
			p.conf.Follow_Children = "enabled"
			p.conf.Max_Pending = 3
			p.conf.Max_Children = 3
			base := p.now()
			workKey := seedScheduledWork(t, p, rt, map[string]work{
				"organization-roles/organization_id:orgA": {
					Dataset: "organization-roles", Parameter: []string{"organization_id:orgA"},
					Revision: "a", Pending: true, Failures: tc.currentFailures, LastAttempt: base.Add(-3 * time.Minute),
				},
				"organization-users/organization_id:orgB": {
					Dataset: "organization-users", Parameter: []string{"organization_id:orgB"},
					Revision: "b", Pending: true, Failures: tc.laterFailures, LastAttempt: base.Add(-2 * time.Minute),
				},
				"organization-users/organization_id:orgC": {
					Dataset: "organization-users", Parameter: []string{"organization_id:orgC"},
					Revision: "c", Pending: true, LastAttempt: base.Add(-time.Minute),
				},
			})
			var requested []string
			p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				requested = append(requested, r.URL.Path)
				switch {
				case r.URL.Path == "/v1/compliance/organizations":
					return reply(`{"data":[],"has_more":false}`, 200), nil
				case strings.HasSuffix(r.URL.Path, "/orgA/roles"):
					return reply(`{"data":[{"id":"role1"}],"has_more":false}`, 200), nil
				default:
					return reply(`{"data":[],"has_more":false}`, 200), nil
				}
			})
			if _, err := p.Handle(t.Context(), rt); err != nil {
				t.Fatalf("nested discovery aborted the scheduled cycle: %v; requests=%v", err, requested)
			}
			if !pathWithSuffixWasRequested(requested, "/orgC/users") {
				t.Fatalf("later scheduled child was not attempted: %v", requested)
			}
			var list worklist
			if err := json.Unmarshal(mustGet(t, rt, workKey), &list); err != nil {
				t.Fatal(err)
			}
			if len(list.Items) > p.conf.Max_Pending {
				t.Fatalf("Max-Pending exceeded: items=%d limit=%d", len(list.Items), p.conf.Max_Pending)
			}
			// The protected cycle may defer the newly discovered grandchild when
			// every slot is scheduled. Once successful siblings leave the pending
			// schedule, the next cycle must admit it without exceeding the bound.
			if _, err := p.Handle(t.Context(), rt); err != nil {
				t.Fatalf("nested discovery did not resume: %v", err)
			}
			list = worklist{}
			if err := json.Unmarshal(mustGet(t, rt, workKey), &list); err != nil {
				t.Fatal(err)
			}
			if _, ok := list.Items["role-permissions/organization_id:orgA/role_id:role1"]; !ok {
				t.Fatal("deferred role-permissions work was not admitted on the next cycle")
			}
			if len(list.Items) > p.conf.Max_Pending {
				t.Fatalf("Max-Pending exceeded after resumed discovery: items=%d limit=%d", len(list.Items), p.conf.Max_Pending)
			}
		})
	}
}

func pathWithSuffixWasRequested(paths []string, suffix string) bool {
	for _, path := range paths {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

// TestPendingWedge_PermanentlyFailingChildEventuallyEvicted proves the
// invariant end to end: g1 fails on every attempt and occupies the only
// Max-Pending slot, and a genuinely new child g2 must still eventually be
// admitted once g1 reaches maxChildFailures.
func TestPendingWedge_PermanentlyFailingChildEventuallyEvicted(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1

	phase2 := false // flips once g1 has accumulated enough failures
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			if phase2 {
				return reply(`{"data":[{"id":"g2"}],"has_more":false}`, 200), nil
			}
			return reply(`{"data":[{"id":"g1"}],"has_more":false}`, 200), nil
		}
		if strings.Contains(r.URL.Path, "g1") {
			return reply(`{"error":{"message":"forbidden"}}`, 403), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	workKey := p.conf.key() + "/children"

	runCycle := func(i int) {
		var list worklist
		_ = json.Unmarshal(mustGet(t, rt, workKey), &list)
		if list.Items != nil {
			for k, it := range list.Items {
				it.RetryAt = time.Time{}
				list.Items[k] = it
			}
			if b, e := json.Marshal(list); e == nil {
				if e := rt.Put(workKey, b); e != nil {
					t.Fatal(e)
				}
			}
		}
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Logf("cycle %d: Handle returned %v (expected while g1 is failing)", i, e)
		}
		list = worklist{}
		_ = json.Unmarshal(mustGet(t, rt, workKey), &list)
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
	_ = json.Unmarshal(mustGet(t, rt, workKey), &afterPhase1)
	g1 := afterPhase1.Items["group-members/group_id:g1"]
	if g1.Failures < maxChildFailures {
		t.Fatalf("test setup issue: g1.Failures=%d after %d cycles, expected >= %d", g1.Failures, maxChildFailures, maxChildFailures)
	}
	if !g1.Pending {
		t.Fatal("test setup issue: g1 unexpectedly not Pending")
	}

	// Phase 2: a genuinely new child (g2) appears. g1 -- now stuck at
	// maxChildFailures -- becomes eligible as a fallback victim, so g2 must
	// be admitted rather than rejected by errPendingCapacity forever.
	phase2 = true
	admittedAtCycle := -1
	for i := 0; i < 5; i++ {
		runCycle(maxChildFailures + i)
		var list worklist
		_ = json.Unmarshal(mustGet(t, rt, workKey), &list)
		if _, ok := list.Items["group-members/group_id:g2"]; ok && admittedAtCycle == -1 {
			admittedAtCycle = i
		}
	}

	if admittedAtCycle == -1 {
		t.Fatal("g2 was never admitted -- permanently-failing g1 wedged discovery forever")
	}
	t.Logf("g2 was admitted %d phase-2 cycle(s) after g1 reached maxChildFailures=%d", admittedAtCycle, maxChildFailures)

	// The evicted child's checkpoint must be compacted but never tombstoned:
	// eviction for capacity (even of a stuck item) is unrelated to the
	// parent's own content and must never suppress a still-valid parent.
	var finalList worklist
	_ = json.Unmarshal(mustGet(t, rt, workKey), &finalList)
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
	g2Calls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g2"}],"has_more":false}`, 200), nil
		}
		if strings.Contains(r.URL.Path, "g1") {
			return reply(`{"error":{"message":"forbidden"}}`, 403), nil
		}
		g2Calls++
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	workKey := p.conf.key() + "/children"
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
	if e := rt.Put(workKey, b); e != nil {
		t.Fatal(e)
	}

	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if g2Calls == 0 {
		t.Fatal("g2 was never actually fetched -- eviction freed the slot but discovery did not resume")
	}
	var list worklist
	_ = json.Unmarshal(mustGet(t, rt, workKey), &list)
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
	active := "g2"
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(fmt.Sprintf(`{"data":[{"id":%q}],"has_more":false}`, active), 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	workKey := p.conf.key() + "/children"
	stuck := worklist{Items: map[string]work{
		"group-members/group_id:g1": {
			Dataset: "group-members", Parameter: []string{"group_id:g1"},
			Revision: "rev-g1", Pending: true, Failures: maxChildFailures,
			LastAttempt: time.Unix(0, 0),
		},
	}}
	b, _ := json.Marshal(stuck)
	if e := rt.Put(workKey, b); e != nil {
		t.Fatal(e)
	}
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
	pruned := readCheckpoint(t, rt, stateKey)
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
	_ = json.Unmarshal(mustGet(t, rt, workKey), &list)
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
	workKey := p.conf.key() + "/children"
	for _, failures := range []uint{0, 1, maxChildFailures - 1} {
		t.Run(fmt.Sprintf("failures=%d", failures), func(t *testing.T) {
			list := worklist{Items: map[string]work{
				"group-members/group_id:g1": {
					Dataset: "group-members", Parameter: []string{"group_id:g1"},
					Revision: "rev-g1", Pending: true, Failures: failures,
					LastAttempt: p.now(),
				},
			}}
			b, _ := json.Marshal(list)
			if e := rt.Put(workKey, b); e != nil {
				t.Fatal(e)
			}
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
			_ = json.Unmarshal(mustGet(t, rt, workKey), &after)
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
	workKey := p.conf.key() + "/children"
	list := worklist{Items: map[string]work{
		"group-members/group_id:g1": {
			Dataset: "group-members", Parameter: []string{"group_id:g1"},
			Revision: "rev-g1", Pending: true, Failures: 0,
			LastAttempt: p.now(),
		},
	}}
	b, _ := json.Marshal(list)
	if e := rt.Put(workKey, b); e != nil {
		t.Fatal(e)
	}
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
	_ = json.Unmarshal(mustGet(t, rt, workKey), &after)
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
				return reply(`{"error":{"message":"forbidden"}}`, 403), nil
			}
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	workKey := p.conf.key() + "/children"
	for cycle := 0; cycle < 30; cycle++ {
		if cycle == 5 && len(present) < len(ids) {
			present = append(present, ids[len(present)])
		}
		if cycle == 15 && len(present) < len(ids) {
			present = append(present, ids[len(present)])
		}
		var list worklist
		_ = json.Unmarshal(mustGet(t, rt, workKey), &list)
		if list.Items != nil {
			for k, it := range list.Items {
				it.RetryAt = time.Time{}
				list.Items[k] = it
			}
			if b, e := json.Marshal(list); e == nil {
				if e := rt.Put(workKey, b); e != nil {
					t.Fatal(e)
				}
			}
		}
		_, _ = p.Handle(t.Context(), rt)
		list = worklist{}
		_ = json.Unmarshal(mustGet(t, rt, workKey), &list)
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
	workKey := p.conf.key() + "/children"
	list := worklist{Items: map[string]work{"group-members/group_id:g1": stuckWork}}
	b, _ := json.Marshal(list)
	if e := rt.Put(workKey, b); e != nil {
		t.Fatal(e)
	}
	// Pre-seed a non-trivial checkpoint so we can also confirm it gets
	// compacted (Manifest/Traversal cleared), matching ordinary eviction.
	seedCheckpoint(t, rt, stateKey, checkpoint{Since: time.Unix(1, 0), Manifest: manifest{"x": {Digest: "d"}},
		Walk: &traversal{Cursor: "c", Manifest: manifest{}}})

	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g2"}],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	after := readCheckpoint(t, rt, stateKey)
	if after.Retired != "" {
		t.Fatalf("checkpoint was tombstoned by stuck-pending eviction: Retired=%q", after.Retired)
	}
	if len(after.Manifest) != 0 || after.Walk != nil {
		t.Fatal("checkpoint was not compacted (Manifest/Traversal) by eviction")
	}
}
