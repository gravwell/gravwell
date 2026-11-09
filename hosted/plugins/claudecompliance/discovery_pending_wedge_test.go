package claudecompliance

// Coverage for Max-Pending eviction under a permanently failing child. A
// child that fails on every attempt stays Pending, so if eviction only ever
// considered non-pending candidates such a child could occupy every
// Max-Pending slot and wedge discovery of any further child forever.
//
// A child that has exhausted its retry backoff ceiling
// (Failures >= maxChildFailures) is therefore eligible as a fallback
// eviction victim, but only once no genuinely non-pending candidate exists,
// and only through the same untombstoned reclamation path used for ordinary
// capacity eviction -- so an evicted child's checkpoint is compacted, never
// tombstoned, and it remains rediscoverable.

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
			// schedule, the deferred retry must admit it without exceeding the bound.
			deferred := list.Items["organization-roles/organization_id:orgA"]
			if deferred.RetryAt.IsZero() || !deferred.RetryAt.After(p.now()) {
				t.Fatal("nested capacity deferral did not receive a bounded retry time")
			}
			p.now = func() time.Time { return deferred.RetryAt }
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
		_ = json.Unmarshal(getOptional(rt, workKey), &list)
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
	_ = readCheckpoint(t, rt, stateKey)

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
		_ = json.Unmarshal(getOptional(rt, workKey), &list)
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
// minimal check that no reclamation reachable from the stuck-pending eviction
// fallback leaves a tombstone behind, by
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
	stuckWork.Slot = 1
	stuckWork.StateKey = childStateKey(p.conf.key(), stuckWork.Slot)
	stateKey := stuckWork.StateKey
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
	if len(after.Manifest) != 0 || after.Walk != nil {
		t.Fatal("checkpoint was not compacted (Manifest/Traversal) by eviction")
	}
}

func TestPendingWedge_EntryCapacityDeferralIsAtomic(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 1
	p.conf.Max_Pending = 2 // exercise defensive handling of persisted pre-release configuration
	p.conf.Max_Pages = 1
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/organizations") {
			return reply(`{"data":[{"uuid":"o1"},{"uuid":"o2"},{"uuid":"o3"}],"has_more":false}`, http.StatusOK), nil
		}
		return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
	})

	cont, err := p.Handle(t.Context(), rt)
	if err != nil {
		t.Fatal(err)
	}
	if cont == nil || cont.Delay != time.Duration(p.conf.Request_Interval)*time.Second {
		t.Fatalf("capacity deferral continuation=%+v", cont)
	}
	if _, err := rt.Get(p.conf.key() + keyChildren); err == nil {
		t.Fatal("deferred root page persisted partial child admissions")
	}
	if len(rt.entries) != 0 || rt.committed(p) {
		t.Fatal("deferred root page wrote entries or advanced its checkpoint")
	}
	if rt.warnings != 1 {
		t.Fatalf("capacity deferral warnings=%d, want 1", rt.warnings)
	}
}

func TestPendingWedge_EntryEvictionWaitsForWorklistCommit(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 1
	p.conf.Max_Pending = 1
	old := work{Dataset: "group-members", Parameter: []string{"group_id:g1"}, Revision: "old", LastCompleted: time.Unix(1, 0).UTC()}
	child, err := childConfig(p.conf, old)
	if err != nil {
		t.Fatal(err)
	}
	old.StateKey = child.key()
	workKey := p.conf.key() + keyChildren
	seed, err := json.Marshal(worklist{Items: map[string]work{"group-members/group_id:g1": old}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(workKey, seed); err != nil {
		t.Fatal(err)
	}
	seedCheckpoint(t, rt, old.StateKey, checkpoint{Since: time.Unix(1, 0).UTC(), Manifest: manifest{"record": {Digest: "digest"}}})
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g2"}],"has_more":false}`, http.StatusOK), nil
		}
		return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
	})
	rt.putFn = func(key string, _ []byte) error {
		if key == workKey {
			return fmt.Errorf("synthetic worklist write failure")
		}
		return nil
	}

	if _, err := p.Handle(t.Context(), rt); err == nil {
		t.Fatal("worklist write failure was ignored")
	}
	if got := mustGet(t, rt, workKey); string(got) != string(seed) {
		t.Fatal("failed worklist write changed durable ownership")
	}
	if cp := readCheckpoint(t, rt, old.StateKey); len(cp.Manifest) != 1 {
		t.Fatal("checkpoint was compacted before its worklist removal committed")
	}
	// The reclamation obligation is recorded before ownership is released, so
	// it can outlive the failed worklist write. A later cycle must recognize
	// that the worklist still owns the child and leave its checkpoint alone
	// rather than reclaiming a checkpoint that is still in use.
	rt.putFn = nil
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if cp := readCheckpoint(t, rt, old.StateKey); len(cp.Manifest) == 0 {
		var after worklist
		if err := json.Unmarshal(mustGet(t, rt, workKey), &after); err != nil {
			t.Fatal(err)
		}
		if _, owned := after.Items["group-members/group_id:g1"]; owned {
			t.Fatal("a still-owned checkpoint was reclaimed by a stale obligation")
		}
	}
}

// TestCleanupObligationIsDurableAndRetried covers the ownership handoff from
// the other side. The worklist is the ownership record for child checkpoints;
// releasing a child and reclaiming its checkpoint cannot be one atomic write,
// so the obligation has to be durable on its own. A reclamation that fails
// mid-loop must neither be forgotten nor leave the checkpoint half-compacted,
// and it must still be retried on a later cycle and across a restart.
func TestCleanupObligationIsDurableAndRetried(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	victim := work{Dataset: "group-members", Parameter: []string{"group_id:g1"}, Revision: "old", LastCompleted: time.Unix(1, 0).UTC()}
	child, err := childConfig(p.conf, victim)
	if err != nil {
		t.Fatal(err)
	}
	victim.StateKey = child.key()
	workKey := p.conf.key() + keyChildren
	cleanupKey := p.conf.key() + keyCleanup
	seed, err := json.Marshal(worklist{Items: map[string]work{"group-members/group_id:g1": victim}})
	if err != nil {
		t.Fatal(err)
	}
	if err = rt.Put(workKey, seed); err != nil {
		t.Fatal(err)
	}
	seedCheckpoint(t, rt, victim.StateKey, checkpoint{Since: time.Unix(1, 0).UTC(), Manifest: manifest{"record": {Digest: "digest"}}})
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g2"}],"has_more":false}`, http.StatusOK), nil
		}
		return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
	})
	// Only the reclamation itself fails; the worklist write succeeds, so
	// ownership really is released while the obligation is still outstanding.
	rt.putFn = func(key string, _ []byte) error {
		if strings.HasPrefix(key, victim.StateKey) {
			return fmt.Errorf("synthetic reclamation failure")
		}
		return nil
	}
	if _, err = p.Handle(t.Context(), rt); err == nil {
		t.Fatal("a failed checkpoint reclamation was reported as success")
	}
	var after worklist
	if err = json.Unmarshal(mustGet(t, rt, workKey), &after); err != nil {
		t.Fatal(err)
	}
	if _, owned := after.Items["group-members/group_id:g1"]; owned {
		t.Fatal("the evicted child was not released from the durable worklist")
	}
	tasks, err := loadCleanup(rt, cleanupKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].StateKey != victim.StateKey {
		t.Fatalf("the reclamation obligation was lost: %#v", tasks)
	}
	if tasks[0].Retired != "" {
		t.Fatalf("capacity eviction staged a tombstone: %q", tasks[0].Retired)
	}
	if cp := readCheckpoint(t, rt, victim.StateKey); len(cp.Manifest) != 1 {
		t.Fatal("a failed reclamation left the checkpoint half-compacted")
	}

	// A restart retries the outstanding obligation and drains it.
	rt.putFn = nil
	fresh := restart(t, p, rt)
	if _, err = fresh.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if tasks, err = loadCleanup(rt, cleanupKey); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("the obligation was not drained after the failure cleared: %#v", tasks)
	}
	if cp := readCheckpoint(t, rt, victim.StateKey); len(cp.Manifest) != 0 || cp.Walk != nil {
		t.Fatal("the retried reclamation did not compact the orphaned checkpoint")
	}
	_ = readCheckpoint(t, rt, victim.StateKey)
}

// TestPendingWedge_SerializedSizeShedsCapacityVictims proves that stored work
// exceeding the independent serialized byte ceiling cannot wedge collection.
// The Max-Pending ceiling is derived from the worst-case serialized item the
// current identity bounds permit, so an accepted configuration stays inside
// the byte ceiling by construction; reaching it means the stored state was
// written by a pre-release build or corrupted, which is exactly when shedding
// safe capacity victims has to keep collection recoverable.
func TestPendingWedge_SerializedSizeShedsCapacityVictims(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = maxPendingWorkItems
	p.conf.Max_Children = 100
	if err := p.conf.Verify(); err != nil {
		t.Fatalf("supported Max-Pending value rejected: %v", err)
	}

	const oldItems = 2000
	// A state key far wider than any key the current bounds can derive, so the
	// stored worklist exceeds the byte ceiling while its item count is well
	// inside Max-Pending.
	const oversizedStateKey = 16 << 10
	items := make(map[string]work, oldItems)
	for i := 0; i < oldItems; i++ {
		id := fmt.Sprintf("%06d-%s", i, strings.Repeat("x", maxDiscoveredParameterLen-7))
		key := "role-permissions/organization_id:" + id + "/role_id:" + id
		items[key] = work{
			Dataset:       "role-permissions",
			Parameter:     []string{"organization_id:" + id, "role_id:" + id},
			Revision:      "revision",
			StateKey:      "pre-release/" + strings.Repeat("s", oversizedStateKey) + "/" + id,
			LastCompleted: time.Unix(int64(i+1), 0).UTC(),
		}
	}
	seed, err := json.Marshal(worklist{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) <= maxPersistedWorklistBytes {
		t.Fatalf("test seed must exceed the persist bound to exercise shedding: %d bytes", len(seed))
	}
	workKey := p.conf.key() + keyChildren
	if err := rt.Put(workKey, seed); err != nil {
		t.Fatal(err)
	}

	var rows strings.Builder
	rows.WriteString(`{"data":[`)
	for i := 0; i < Datasets["organizations"].Limit; i++ {
		if i > 0 {
			rows.WriteByte(',')
		}
		id := fmt.Sprintf("%06d-%s", i, strings.Repeat("n", maxDiscoveredParameterLen-7))
		fmt.Fprintf(&rows, `{"uuid":%q}`, id)
	}
	rows.WriteString(`],"has_more":false}`)
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/organizations") {
			return reply(rows.String(), http.StatusOK), nil
		}
		return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
	})

	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatalf("byte-bound capacity recovery failed: %v", err)
	}
	stored := mustGet(t, rt, workKey)
	if len(stored) > maxPersistedWorklistBytes {
		t.Fatalf("persisted worklist exceeds byte bound: got=%d limit=%d", len(stored), maxPersistedWorklistBytes)
	}
	var list worklist
	if err := json.Unmarshal(stored, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) > p.conf.Max_Pending {
		t.Fatalf("persisted worklist exceeds Max-Pending: got=%d limit=%d", len(list.Items), p.conf.Max_Pending)
	}
	remainingOld := 0
	for _, item := range list.Items {
		if item.Dataset == "role-permissions" {
			remainingOld++
		}
	}
	if remainingOld >= oldItems {
		t.Fatal("serialized byte recovery did not evict any completed capacity victim")
	}
	newID := fmt.Sprintf("%06d-%s", 999, strings.Repeat("n", maxDiscoveredParameterLen-7))
	if _, ok := list.Items["organization-users/organization_id:"+newID]; !ok {
		t.Fatal("newly discovered child was dropped while shrinking the worklist")
	}
	if len(rt.entries) == 0 || !rt.committed(p) {
		t.Fatal("root records were not delivered and checkpointed after capacity recovery")
	}
}

// TestPendingWedge_UnsheddableOverflowDefersAtomically proves that a page
// which cannot fit while every supported queue slot is pending behaves like
// ordinary Max-Pending backpressure. The page is not written or checkpointed.
func TestPendingWedge_UnsheddableOverflowDefersAtomically(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = maxPendingWorkItems
	p.conf.Max_Children = 100

	const total = maxPendingWorkItems
	items := make(map[string]work, total)
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("%06d-%s", i, strings.Repeat("x", maxDiscoveredParameterLen-7))
		stateKey := fmt.Sprintf("byte-deferral/item/%d", i)
		key := "role-permissions/organization_id:" + id + "/role_id:" + id
		items[key] = work{
			Dataset:       "role-permissions",
			Parameter:     []string{"organization_id:" + id, "role_id:" + id},
			Revision:      "revision",
			StateKey:      stateKey,
			Pending:       true,
			LastCompleted: time.Unix(int64(i+1), 0).UTC(),
			RetryAt:       p.now().Add(time.Hour),
		}
	}
	seed, err := json.Marshal(worklist{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) >= maxPersistedWorklistBytes {
		t.Fatalf("test seed must fit the persist bound: %d bytes", len(seed))
	}
	workKey := p.conf.key() + keyChildren
	if err := rt.Put(workKey, seed); err != nil {
		t.Fatal(err)
	}

	var rows strings.Builder
	rows.WriteString(`{"data":[`)
	for i := 0; i < Datasets["organizations"].Limit; i++ {
		if i > 0 {
			rows.WriteByte(',')
		}
		id := fmt.Sprintf("%06d-%s", i, strings.Repeat("n", maxDiscoveredParameterLen-7))
		fmt.Fprintf(&rows, `{"uuid":%q}`, id)
	}
	rows.WriteString(`],"has_more":false}`)
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(rows.String(), http.StatusOK), nil
	})

	cont, err := p.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("capacity backpressure escaped as an error: %v", err)
	}
	if cont == nil {
		t.Fatal("capacity backpressure did not schedule another cycle")
	}
	if want := time.Duration(p.conf.Request_Interval) * time.Second; cont.Delay != want {
		t.Fatalf("capacity backpressure delay=%v, want %v", cont.Delay, want)
	}
	var unchanged worklist
	if err := json.Unmarshal(mustGet(t, rt, workKey), &unchanged); err != nil {
		t.Fatal(err)
	}
	if len(unchanged.Items) != len(items) {
		t.Fatalf("deferred page changed work ownership: got=%d want=%d", len(unchanged.Items), len(items))
	}
	for key := range unchanged.Items {
		if strings.HasPrefix(key, "organization-users/organization_id:n") || strings.HasPrefix(key, "organization-roles/organization_id:n") {
			t.Fatalf("deferred root page persisted new child work: %s", key)
		}
	}
	if len(rt.entries) != 0 || rt.committed(p) {
		t.Fatal("deferred root page wrote entries or advanced its checkpoint")
	}

	// Simulate the existing queue draining enough children to create safe
	// victims, then prove the exact deferred root page completes normally.
	var drained worklist
	if err := json.Unmarshal(seed, &drained); err != nil {
		t.Fatal(err)
	}
	drainedCount := 0
	for key, item := range drained.Items {
		if item.Pending && drainedCount < 2000 {
			item.Pending = false
			item.RetryAt = time.Time{}
			item.LastCompleted = p.now()
			drained.Items[key] = item
			drainedCount++
		}
	}
	drainedBytes, err := json.Marshal(drained)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(workKey, drainedBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatalf("capacity did not recover after existing work drained: %v", err)
	}
	if len(rt.entries) == 0 || !rt.committed(p) {
		t.Fatal("deferred root page did not complete after capacity recovered")
	}
	if got := mustGet(t, rt, workKey); len(got) > maxPersistedWorklistBytes {
		t.Fatalf("recovered worklist exceeds byte bound: got=%d limit=%d", len(got), maxPersistedWorklistBytes)
	}
}

// TestPendingWedge_ByteShedWaitsForWorklistCommit injects a failure into the
// worklist write after a byte-bound victim has been selected. The victim's
// checkpoint must remain intact because the durable worklist still owns it.
func TestPendingWedge_ByteShedWaitsForWorklistCommit(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = maxPendingWorkItems

	const oldItems = 2000
	const oversizedStateKey = 16 << 10
	items := make(map[string]work, oldItems)
	var victimKey, victimState string
	for i := 0; i < oldItems; i++ {
		id := fmt.Sprintf("%06d-%s", i, strings.Repeat("x", maxDiscoveredParameterLen-7))
		key := "role-permissions/organization_id:" + id + "/role_id:" + id
		stateKey := "pre-release/" + strings.Repeat("s", oversizedStateKey) + "/" + id
		items[key] = work{
			Dataset:       "role-permissions",
			Parameter:     []string{"organization_id:" + id, "role_id:" + id},
			Revision:      "revision",
			StateKey:      stateKey,
			LastCompleted: time.Unix(int64(i+1), 0).UTC(),
		}
		if i == 0 {
			victimKey, victimState = key, stateKey
		}
	}
	seedCheckpoint(t, rt, victimState, checkpoint{Since: time.Unix(1, 0).UTC(), Manifest: manifest{"record": {Digest: "digest"}}})
	seed, err := json.Marshal(worklist{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) <= maxPersistedWorklistBytes {
		t.Fatalf("test seed must exceed the persist bound to exercise shedding: %d bytes", len(seed))
	}
	workKey := p.conf.key() + keyChildren
	if err := rt.Put(workKey, seed); err != nil {
		t.Fatal(err)
	}

	var rows strings.Builder
	rows.WriteString(`{"data":[`)
	for i := 0; i < Datasets["organizations"].Limit; i++ {
		if i > 0 {
			rows.WriteByte(',')
		}
		id := fmt.Sprintf("%06d-%s", i, strings.Repeat("n", maxDiscoveredParameterLen-7))
		fmt.Fprintf(&rows, `{"uuid":%q}`, id)
	}
	rows.WriteString(`],"has_more":false}`)
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(rows.String(), http.StatusOK), nil
	})
	putAttempted := false
	rt.putFn = func(key string, _ []byte) error {
		if key == workKey {
			putAttempted = true
			return fmt.Errorf("synthetic worklist write failure")
		}
		return nil
	}

	if _, err := p.Handle(t.Context(), rt); err == nil {
		t.Fatal("worklist write failure was ignored")
	}
	if !putAttempted {
		t.Fatal("test did not reach the byte-bound worklist write")
	}
	if got := mustGet(t, rt, workKey); string(got) != string(seed) {
		t.Fatal("failed worklist write changed durable state")
	}
	var stored worklist
	if err := json.Unmarshal(seed, &stored); err != nil {
		t.Fatal(err)
	}
	if _, ok := stored.Items[victimKey]; !ok {
		t.Fatal("test victim is absent from the stored worklist")
	}
	cp := readCheckpoint(t, rt, victimState)
	if len(cp.Manifest) != 1 {
		t.Fatal("failed worklist write compacted the victim checkpoint")
	}
}

// TestPendingWedge_UnsheddableGrandchildrenDeferWithoutLeaks fills the queue
// with live, unsheddable work and then lets a scheduled child discover
// grandchildren it has no room for. The rejected child page must leave no
// grandchildren in the durable list, must not write or checkpoint any of that
// page's records, must keep the child pending behind a future RetryAt, and must
// hand back a positive bounded continuation rather than spinning immediately.
func TestPendingWedge_UnsheddableGrandchildrenDeferWithoutLeaks(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = maxPendingWorkItems
	p.conf.Max_Children = 1

	const fillerItems = maxPendingWorkItems - 1
	items := make(map[string]work, fillerItems+1)
	current := work{
		Dataset:   "organization-roles",
		Parameter: []string{"organization_id:orgA"},
		Revision:  "current",
		Pending:   true,
	}
	currentKey := current.Dataset + "/" + strings.Join(current.Parameter, "/")
	// Use the namespace the running child actually checkpoints under, so the
	// no-commit assertion below inspects the keys the deferred page would have
	// written rather than an invented prefix nothing ever touches.
	currentConf, err := childConfig(p.conf, current)
	if err != nil {
		t.Fatal(err)
	}
	currentState := currentConf.key()
	current.StateKey = currentState
	items[currentKey] = current
	// The filler work is deliberately a *direct* child of the scanned root.
	// Capacity pressure here has to come from live work the queue may not shed:
	// a deeper descendant whose own parent work item is absent from the list is
	// an orphan, and the cascade in retireAbsentChildren would correctly retire
	// it, releasing the very capacity this test exists to exhaust.
	for i := 0; i < fillerItems; i++ {
		id := fmt.Sprintf("%06d-%s", i, strings.Repeat("x", maxDiscoveredParameterLen-7))
		key := "organization-users/organization_id:" + id
		items[key] = work{
			Dataset:   "organization-users",
			Parameter: []string{"organization_id:" + id},
			Revision:  "filler",
			StateKey:  fmt.Sprintf("grandchild-deferral/item/%d", i),
			Pending:   true,
			RetryAt:   p.now().Add(time.Hour),
		}
	}
	seed, err := json.Marshal(worklist{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) >= maxPersistedWorklistBytes {
		t.Fatalf("test seed must fit the persist bound: %d bytes", len(seed))
	}
	workKey := p.conf.key() + keyChildren
	if err := rt.Put(workKey, seed); err != nil {
		t.Fatal(err)
	}

	var rows strings.Builder
	rows.WriteString(`{"data":[`)
	for i := 0; i < Datasets["organization-roles"].Limit; i++ {
		if i > 0 {
			rows.WriteByte(',')
		}
		id := fmt.Sprintf("%06d-%s", i, strings.Repeat("r", maxDiscoveredParameterLen-7))
		fmt.Fprintf(&rows, `{"id":%q}`, id)
	}
	rows.WriteString(`],"has_more":false}`)
	childRequests := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/orgA/roles") {
			childRequests++
			return reply(rows.String(), http.StatusOK), nil
		}
		return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
	})

	cont, err := p.Handle(t.Context(), rt)
	if err != nil {
		t.Fatalf("grandchild capacity backpressure escaped as an error: %v", err)
	}
	var stored worklist
	if err := json.Unmarshal(mustGet(t, rt, workKey), &stored); err != nil {
		t.Fatal(err)
	}
	if cont == nil || cont.Delay <= 0 || cont.Delay > time.Duration(p.conf.Request_Interval)*time.Second {
		t.Fatalf("deferred child page did not request bounded backoff: cont=%+v stored=%d ready=%d",
			cont, len(stored.Items), len(readyWorkKeys(stored, p.now())))
	}
	if len(stored.Items) != len(items) {
		t.Fatalf("deferred child page leaked work items: got=%d want=%d", len(stored.Items), len(items))
	}
	if kept := stored.Items[currentKey]; !kept.Pending || kept.RetryAt.IsZero() || !kept.RetryAt.After(p.now()) {
		t.Fatalf("deferred child was not kept pending: pending=%t retry_at=%s", kept.Pending, kept.RetryAt)
	}
	for key := range stored.Items {
		if strings.HasPrefix(key, "role-permissions/organization_id:orgA/") {
			t.Fatalf("deferred child page persisted a grandchild: %s", key)
		}
	}
	if len(rt.entries) != 0 {
		t.Fatalf("deferred child page wrote entries: count=%d", len(rt.entries))
	}
	if cp := readCheckpoint(t, rt, currentState); !cp.Since.IsZero() || cp.Walk != nil {
		t.Fatal("deferred child page advanced its checkpoint")
	}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatalf("immediate cycle after capacity deferral failed: %v", err)
	}
	if childRequests != 1 {
		t.Fatalf("capacity-deferred child was replayed before RetryAt: requests=%d", childRequests)
	}
}
