package claudecompliance

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDeletedChatDrainsIncompleteTranscriptBeforeTombstone(t *testing.T) {
	p, rt := setup(t, "chats")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pages = 1
	deleted := false
	messagePages := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/chats") {
			deletedAt := "null"
			if deleted {
				deletedAt = `"2026-09-08T01:00:00Z"`
			}
			return reply(`{"data":[{"id":"c1","deleted_at":`+deletedAt+`}],"has_more":false}`, 200), nil
		}
		messagePages++
		if r.URL.Query().Get("after_id") == "m1" {
			return reply(`{"session":{"id":"c1"},"chat_messages":[{"id":"m2"}],"has_more":false}`, 200), nil
		}
		return reply(`{"session":{"id":"c1"},"chat_messages":[{"id":"m1"}],"has_more":true,"last_id":"m1"}`, 200), nil
	})

	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	deleted = true
	p.now = func() time.Time { return time.Date(2026, 9, 8, 2, 0, 0, 0, time.UTC) }
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if messagePages != 2 {
		t.Fatalf("deleted chat discarded its in-flight transcript after %d message page(s)", messagePages)
	}
	var sawM2 bool
	for _, ent := range rt.entries {
		sawM2 = sawM2 || strings.Contains(string(ent.Data), `"id":"m2"`)
	}
	if !sawM2 {
		t.Fatal("final transcript page was not ingested before deletion tombstoned the child")
	}
}

func TestDeletedProjectDrainsEveryIncompleteChildBeforeTombstone(t *testing.T) {
	p, rt := setup(t, "projects")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pages = 1
	deleted := false
	childPages := map[string]int{}
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/projects") {
			deletedAt := "null"
			if deleted {
				deletedAt = `"2026-09-08T01:00:00Z"`
			}
			return reply(`{"data":[{"id":"p1","deleted_at":`+deletedAt+`}],"has_more":false}`, 200), nil
		}
		var row string
		var family string
		switch {
		case strings.HasSuffix(r.URL.Path, "/attachments"):
			family, row = "attachments", `{"id":"a1"}`
		case strings.HasSuffix(r.URL.Path, "/collaborators"):
			family, row = "collaborators", `{"type":"user","user_id":"u1"}`
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		childPages[family]++
		if r.URL.Query().Get("page") == "next" {
			return reply(`{"data":[`+row+`],"next_page":null}`, 200), nil
		}
		return reply(`{"data":[`+row+`],"next_page":"next"}`, 200), nil
	})

	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	deleted = true
	p.now = func() time.Time { return time.Date(2026, 9, 8, 2, 0, 0, 0, time.UTC) }
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	for _, family := range []string{"attachments", "collaborators"} {
		if childPages[family] != 2 {
			t.Fatalf("deleted project %s were not drained: pages=%d", family, childPages[family])
		}
	}
}

// TestAbsenceRetirementNeverPreemptsDrainRetirement makes the drain path the
// explicit sole owner of a deleted parent's child. A project the vendor reports
// deleted hands its children to RetireAfterDrain, which finishes the traversal
// already in flight (or accepts an authoritative 404) and only then compacts the
// checkpoint *and* writes the deletion tombstone that stops the deleted parent
// from being rediscovered. "projects" is also one of the absence-tracked parent
// kinds, so that very same child is in reach of absence retirement -- which
// deliberately writes no tombstone, because absence is not content-derived.
// Letting absence retirement reach drain work would substitute the weaker
// retirement for the stronger one and throw away confirmed-deletion evidence on
// nothing more than the parent having also dropped out of the listing, so it
// must skip such work outright however long the parent has been missing.
func TestAbsenceRetirementNeverPreemptsDrainRetirement(t *testing.T) {
	p, rt := setup(t, "projects")
	p.conf.Follow_Children = "enabled"
	if !absenceTrackedParents[p.conf.dataset.Name] {
		t.Fatal("precondition: the parent kind must be absence-tracked for this to cover anything")
	}
	draining := work{
		Dataset:   "project-attachments",
		Parameter: []string{"project_id:p1"},
		Revision:  "deleted-revision",
		Pending:   true, RetireAfterDrain: true,
		// Already past the absence threshold: a deleted parent that has since
		// dropped out of the listing altogether.
		AbsentStreak: absentRetirementThreshold + 1,
		LastAttempt:  p.now().Add(-time.Hour),
		// Not due yet, so the cycles below are the absence pass alone.
		RetryAt: p.now().Add(time.Hour),
	}
	child, err := childConfig(p.conf, draining)
	if err != nil {
		t.Fatal(err)
	}
	draining.StateKey = child.key()
	seedCheckpoint(t, rt, draining.StateKey, checkpoint{
		Since: p.now(), Manifest: manifest{"identity": {Digest: "d"}},
	})
	const key = "project-attachments/project_id:p1"
	workKey := p.conf.key() + keyChildren
	seed, err := json.Marshal(worklist{Items: map[string]work{key: draining}})
	if err != nil {
		t.Fatal(err)
	}
	if err = rt.Put(workKey, seed); err != nil {
		t.Fatal(err)
	}
	attachmentCalls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/projects") {
			// A complete, clean, unwindowed pass in which p1 is simply gone.
			return reply(`{"data":[],"next_page":null}`, http.StatusOK), nil
		}
		if !strings.HasSuffix(r.URL.Path, "/projects/p1/attachments") {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		// The deleted project's attachments are gone with it: the drain path's
		// own authoritative 404, available only after a successful root listing.
		attachmentCalls++
		return reply(`{"error":{"type":"not_found_error"}}`, http.StatusNotFound), nil
	})

	// Enough complete absent passes to retire ordinary child work several times
	// over.
	for cycle := 0; cycle < absentRetirementThreshold+1; cycle++ {
		if _, err = p.Handle(t.Context(), rt); err != nil {
			t.Fatalf("cycle %d: %v", cycle, err)
		}
	}
	if attachmentCalls != 0 {
		t.Fatalf("the drain request ran before its retry time: calls=%d", attachmentCalls)
	}
	var list worklist
	if err = json.Unmarshal(mustGet(t, rt, workKey), &list); err != nil {
		t.Fatal(err)
	}
	held, ok := list.Items[key]
	if !ok {
		t.Fatal("absence retirement removed work the drain path owns, discarding its deletion tombstone")
	}
	if !held.RetireAfterDrain || !held.Pending || held.AbsentStreak != absentRetirementThreshold+1 {
		t.Fatalf("the absence pass rewrote work it does not own: %+v", held)
	}
	if tasks, e := loadCleanup(rt, p.conf.key()+keyCleanup); e != nil || len(tasks) != 0 {
		t.Fatalf("absence retirement staged a reclamation for drain work: %#v %v", tasks, e)
	}
	kept := readCheckpoint(t, rt, draining.StateKey)
	if len(kept.Manifest) != 1 {
		t.Fatalf("absence retirement compacted a checkpoint the drain path still owns: %+v", kept)
	}
	if tombstones, e := loadTombstones(rt, p.conf.key()+keyTombstones); e != nil || retiredIdentity(tombstones, key, draining.Revision) {
		t.Fatalf("nothing has confirmed the drain, but a tombstone exists: %v %v", tombstones, e)
	}

	// The drain path itself still retires it, and still leaves behind the
	// tombstone absence retirement must never substitute for.
	p.now = func() time.Time { return held.RetryAt.Add(time.Minute) }
	if _, err = p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if attachmentCalls != 1 {
		t.Fatalf("the drain request did not run once its retry time had passed: calls=%d", attachmentCalls)
	}
	list = worklist{}
	if err = json.Unmarshal(mustGet(t, rt, workKey), &list); err != nil {
		t.Fatal(err)
	}
	if _, still := list.Items[key]; still {
		t.Fatal("an authoritative 404 did not complete the drain retirement")
	}
	drained := readCheckpoint(t, rt, draining.StateKey)
	tombstones, err := loadTombstones(rt, p.conf.key()+keyTombstones)
	if err != nil || !retiredIdentity(tombstones, key, draining.Revision) {
		t.Fatalf("drain retirement lost the deletion tombstone: %v %v", tombstones, err)
	}
	if len(drained.Manifest) != 0 || drained.Walk != nil {
		t.Fatal("drain retirement did not compact the child checkpoint")
	}

	// The deliberately bounded stuck-pending capacity path is untouched: drain
	// work that never reaches its 404 is still evictable once it has exhausted
	// bounded retries, so excluding it from absence retirement cannot wedge the
	// queue.
	stuck := draining
	stuck.Failures = maxChildFailures
	if got := capacityVictim(worklist{Items: map[string]work{key: stuck}}, nil); got != key {
		t.Fatalf("stuck drain work is no longer evictable under capacity pressure: %q", got)
	}
}

func TestUnknownStoredChildIsRetiredWithoutBlockingHealthySibling(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 10
	rootRaw := []byte(`{"id":"g1"}`)
	revision := fmt.Sprintf("%x", sha256.Sum256(rootRaw))
	list := worklist{Items: map[string]work{
		"removed-dataset/id:poison": {
			Dataset: "removed-dataset", Parameter: []string{"id:poison"}, StateKey: "dataset/removed", Pending: true,
			LastAttempt: time.Unix(1, 0),
		},
		"group-members/group_id:g1": {
			Dataset: "group-members", Parameter: []string{"group_id:g1"}, Revision: revision, Pending: true,
			LastAttempt: time.Unix(2, 0),
		},
	}}
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if err = rt.Put(p.conf.key()+keyChildren, b); err != nil {
		t.Fatal(err)
	}
	healthyCalls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[`+string(rootRaw)+`],"has_more":false}`, 200), nil
		}
		healthyCalls++
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, err = p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if healthyCalls != 1 {
		t.Fatalf("poisoned work blocked healthy sibling: calls=%d", healthyCalls)
	}
	var after worklist
	if err = json.Unmarshal(mustGet(t, rt, p.conf.key()+keyChildren), &after); err != nil {
		t.Fatal(err)
	}
	if _, exists := after.Items["removed-dataset/id:poison"]; exists {
		t.Fatal("unprocessible stored work was not retired")
	}
}

func TestTransientCredentialFailurePreservesChildWorkAndCheckpoint(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g1"}],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[{"user_id":"u1"}],"has_more":false}`, 200), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	workKey := p.conf.key() + keyChildren
	beforeWork := string(mustGet(t, rt, workKey))
	var list worklist
	if err := json.Unmarshal([]byte(beforeWork), &list); err != nil {
		t.Fatal(err)
	}
	w := list.Items["group-members/group_id:g1"]
	if w.StateKey == "" {
		t.Fatal("child state key was not persisted")
	}
	beforeManifest := string(mustGet(t, rt, w.StateKey+keyManifest))
	beforeEntries := len(rt.entries)

	credentialPath := p.conf.Credential_File
	if err := os.Remove(credentialPath); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(t.Context(), rt); err == nil || !strings.Contains(err.Error(), "credential file") {
		t.Fatalf("missing credential did not fail safely: %v", err)
	}
	if got := string(mustGet(t, rt, workKey)); got != beforeWork {
		t.Fatal("transient credential failure changed the child worklist")
	}
	if got := string(mustGet(t, rt, w.StateKey+keyManifest)); got != beforeManifest {
		t.Fatal("transient credential failure compacted the child checkpoint")
	}

	if err := os.WriteFile(credentialPath, []byte("synthetic-test-key"), 0600); err != nil {
		t.Fatal(err)
	}
	p.now = func() time.Time { return time.Date(2026, 9, 8, 2, 0, 0, 0, time.UTC) }
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != beforeEntries {
		t.Fatalf("recovery replayed child history: entries before=%d after=%d", beforeEntries, len(rt.entries))
	}
}

func TestStoredChildWithoutNamespaceIsBoundToFiniteSlot(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 1
	p.conf.Max_Pending = 100

	workKey := p.conf.key() + keyChildren
	identity := "group-members/group_id:g1"
	legacy := work{
		Dataset:   "group-members",
		Parameter: []string{"group_id:g1"},
		Revision:  "r1",
		Pending:   true,
	}
	b, err := json.Marshal(worklist{Items: map[string]work{identity: legacy}, LastRootScan: p.now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if err = rt.Put(workKey, b); err != nil {
		t.Fatal(err)
	}
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups/g1/members") {
			return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
		}
		return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
	})

	if _, err = p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	var stored worklist
	if err = json.Unmarshal(mustGet(t, rt, workKey), &stored); err != nil {
		t.Fatal(err)
	}
	got := stored.Items[identity]
	wantKey := childStateKey(p.conf.key(), 1)
	if got.Slot != 1 || got.StateKey != wantKey {
		t.Fatalf("legacy child bound to slot=%d key=%q, want slot=1 key=%q", got.Slot, got.StateKey, wantKey)
	}
	configured, err := childConfig(p.conf, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if got.StateKey == configured.key() {
		t.Fatal("legacy child reused the configured-selector namespace")
	}
}

func TestInvalidParentIdentitySkipsOnlyThatChildExpansion(t *testing.T) {
	for _, id := range []string{"bad/id", "bad?id", "bad#id", `bad\id`} {
		t.Run(fmt.Sprintf("%x", id), func(t *testing.T) {
			p, rt := setup(t, "groups")
			p.conf.Follow_Children = "enabled"
			childCalls := 0
			p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/groups") {
					body, err := json.Marshal(map[string]any{
						"data":     []map[string]string{{"id": id}, {"id": "good"}},
						"has_more": false,
					})
					if err != nil {
						t.Fatal(err)
					}
					return reply(string(body), 200), nil
				}
				childCalls++
				if !strings.HasSuffix(r.URL.Path, "/groups/good/members") {
					t.Fatalf("invalid identity reached a child request: %s", r.URL.Path)
				}
				return reply(`{"data":[],"has_more":false}`, 200), nil
			})
			if _, err := p.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			if childCalls != 1 {
				t.Fatalf("healthy sibling was not processed exactly once: calls=%d", childCalls)
			}
			if rt.warnings == 0 {
				t.Fatal("invalid parent identity was not reported")
			}
		})
	}
}

func TestSuccessfulContinuationClearsStaleFailureClassification(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pages = 1
	list := worklist{Items: map[string]work{
		"group-members/group_id:g1": {
			Dataset: "group-members", Parameter: []string{"group_id:g1"}, Revision: "old", Pending: true,
			Failures: maxChildFailures, LastAttempt: time.Unix(1, 0),
		},
	}}
	b, _ := json.Marshal(list)
	if err := rt.Put(p.conf.key()+keyChildren, b); err != nil {
		t.Fatal(err)
	}
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g1"}],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[{"user_id":"u1"}],"has_more":true,"next_page":"p2"}`, 200), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	var after worklist
	if err := json.Unmarshal(mustGet(t, rt, p.conf.key()+keyChildren), &after); err != nil {
		t.Fatal(err)
	}
	w := after.Items["group-members/group_id:g1"]
	if w.Failures != 0 || !w.Pending {
		t.Fatalf("successful continuation retained stale failure state: %+v", w)
	}
}

func TestSingleObjectRecordMayContainErrorField(t *testing.T) {
	p, rt := setup(t, "local-session")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"id":"s1","updated_at":"2026-09-07T12:00:00Z","error":"record field"}`, 200), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("single-object record was not ingested: entries=%d", len(rt.entries))
	}
}

func TestSingleObjectEndpointsRejectApplicationErrorEnvelopes(t *testing.T) {
	for name, d := range Datasets {
		if d.Rows != "" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			p, rt := setup(t, name)
			p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
				return reply(`{"type":"error","error":{"type":"not_found_error","message":"not found"}}`, http.StatusOK), nil
			})
			if _, err := p.Handle(t.Context(), rt); err == nil {
				t.Fatal("application error envelope was accepted")
			}
			if len(rt.entries) != 0 || rt.committed(p) {
				t.Fatal("application error envelope was ingested or checkpointed")
			}
		})
	}
}

func TestNonObjectSessionContextDoesNotBlockMessages(t *testing.T) {
	p, rt := setup(t, "local-session-messages")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"session":"c1","data":[{"id":"m1","created_at":"2026-09-08T00:00:00Z"}],"next_page":null}`, http.StatusOK), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != 1 || !rt.committed(p) {
		t.Fatal("invalid envelope metadata blocked message delivery or checkpointing")
	}
	if _, ok := rt.entries[0].GetEnumeratedValue("_session"); ok {
		t.Fatal("non-object session context was attached")
	}
	if rt.warnings != 1 || strings.Contains(strings.Join(rt.warningLog, "\n"), "c1") {
		t.Fatalf("warning count or sanitization is wrong: %v", rt.warningLog)
	}
}

func TestNonMessageOperationIgnoresSessionContext(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"session":{"id":"irrelevant"},"data":[{"id":"a1","created_at":"2026-09-08T00:00:00Z"}],"has_more":false}`, http.StatusOK), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != 1 || !rt.committed(p) {
		t.Fatal("valid activity was not delivered and checkpointed")
	}
	if _, ok := rt.entries[0].GetEnumeratedValue("_session"); ok {
		t.Fatal("non-message operation attached an irrelevant _session field")
	}
}

func TestPendingChildrenDrainWithoutRelistingRoot(t *testing.T) {
	p, rt := setup(t, "projects")
	p.conf.Follow_Children = "enabled"
	p.conf.Page_Size = 2
	p.conf.Max_Children = 2
	p.conf.Max_Pending = 20
	rootRequests := 0
	childRequests := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/projects") {
			rootRequests++
			page := r.URL.Query().Get("page")
			switch page {
			case "":
				return reply(`{"data":[{"id":"p1"},{"id":"p2"}],"has_more":true,"next_page":"p2"}`, http.StatusOK), nil
			case "p2":
				return reply(`{"data":[{"id":"p3"},{"id":"p4"}],"has_more":true,"next_page":"p3"}`, http.StatusOK), nil
			case "p3":
				return reply(`{"data":[{"id":"p5"},{"id":"p6"}],"has_more":false}`, http.StatusOK), nil
			default:
				t.Fatalf("unexpected root cursor %q", page)
			}
		}
		childRequests++
		return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
	})

	for cycle := 0; cycle < 6; cycle++ {
		if _, err := p.Handle(t.Context(), rt); err != nil {
			t.Fatalf("cycle %d: %v", cycle, err)
		}
	}
	if rootRequests != 3 {
		t.Fatalf("root inventory requests=%d, want one three-page listing", rootRequests)
	}
	if childRequests != 12 {
		t.Fatalf("child requests=%d, want 12", childRequests)
	}
}

func TestContinuingChildCannotSuppressPeriodicRootDiscovery(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 1
	p.conf.Max_Pending = maxPendingWorkItems
	p.conf.Max_Pages = 1
	now := p.now()
	p.now = func() time.Time { return now }
	rootRequests, childRequests := 0, 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			rootRequests++
			return reply(`{"data":[{"id":"g1"}],"has_more":false}`, http.StatusOK), nil
		}
		childRequests++
		return reply(fmt.Sprintf(`{"data":[{"user_id":"m%d"}],"has_more":true,"next_page":"c%d"}`, childRequests, childRequests), http.StatusOK), nil
	})

	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if rootRequests != 1 {
		t.Fatalf("root was re-listed before its interval: requests=%d", rootRequests)
	}
	now = now.Add(time.Duration(p.conf.Request_Interval) * time.Second)
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if rootRequests != 2 {
		t.Fatalf("continuing child suppressed the due root scan: requests=%d", rootRequests)
	}
	if childRequests != 3 {
		t.Fatalf("periodic root scan blocked child progress: requests=%d", childRequests)
	}
}

func TestConfiguredParameterIsNotEmittedAsParentContext(t *testing.T) {
	p, rt := setup(t, "organization-users")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"u1"}],"has_more":false}`, 200), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("entries=%d, want 1", len(rt.entries))
	}
	if _, exists := rt.entries[0].GetEnumeratedValue("_parent"); exists {
		t.Fatal("operator-configured parameter was exposed as discovered parent context")
	}
}

func TestDiscoveredParentContextExcludesConfiguredAncestors(t *testing.T) {
	p, rt := setup(t, "organization-roles")
	p.conf.Follow_Children = "enabled"
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/roles") {
			return reply(`{"data":[{"id":"role-1"}],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[{"action":"read"}],"has_more":false}`, 200), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	for _, ent := range rt.entries {
		if source, ok := ent.GetEnumeratedValue("_source"); !ok || source != "role-permissions" {
			continue
		}
		parent, ok := ent.GetEnumeratedValue("_parent")
		if !ok || parent != `{"role_id":"role-1"}` {
			t.Fatalf("unexpected discovery parent context: %q", parent)
		}
		return
	}
	t.Fatal("role-permissions entry was not emitted")
}

// TestDuplicateDecisionFieldsFailClosed covers ambiguous top-level JSON. A
// repeated field leaves every decision that reads it -- identity, timestamp,
// eviction window, pagination, application error -- depending on which copy a
// decoder happened to keep, while the bytes this ingester emits still contain
// all of them. The page must be rejected before any of those decisions is
// taken, with nothing written and no checkpoint advanced.
func TestDuplicateDecisionFieldsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name, dataset, body string
	}{
		{
			name: "record identity", dataset: "activities",
			body: `{"data":[{"id":"a1","id":"a2","created_at":"2026-09-07T12:00:00Z"}],"has_more":false}`,
		},
		{
			name: "record timestamp", dataset: "activities",
			body: `{"data":[{"id":"a1","created_at":"2026-09-07T12:00:00Z","created_at":"2026-09-08T12:00:00Z"}],"has_more":false}`,
		},
		{
			name: "composite identity component", dataset: "role-permissions",
			body: `{"data":[{"resource_type":"organization","resource_type":"project","resource_id":"r1","action":"read"}],"has_more":false}`,
		},
		{
			name: "pagination flag", dataset: "activities",
			body: `{"data":[{"id":"a1"}],"has_more":false,"has_more":true,"last_id":"c1"}`,
		},
		{
			name: "pagination token", dataset: "activities",
			body: `{"data":[{"id":"a1"}],"has_more":true,"last_id":"c1","last_id":"c2"}`,
		},
		{
			name: "application error marker", dataset: "activities",
			body: `{"data":[],"type":"list","type":"error","error":{"type":"permission_error"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, rt := setup(t, tc.dataset)
			p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
				return reply(tc.body, http.StatusOK), nil
			})
			_, err := p.Handle(t.Context(), rt)
			if err == nil {
				t.Fatal("an ambiguous duplicate decision field was accepted")
			}
			if len(rt.entries) != 0 || rt.committed(p) {
				t.Fatalf("ambiguous page wrote %d entries or advanced its checkpoint (%v)", len(rt.entries), rt.committed(p))
			}
		})
	}
}

// TestDuplicateDeletionFieldNeitherRetiresNorTombstonesAChild is the
// consequential half: a repeated deleted_at decides whether a child's
// checkpoint is compacted and permanently tombstoned, so it must fail closed
// before any retirement bookkeeping happens.
func TestDuplicateDeletionFieldNeitherRetiresNorTombstonesAChild(t *testing.T) {
	p, rt := setup(t, "chats")
	p.conf.Follow_Children = "enabled"
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/chats") {
			return reply(`{"data":[{"id":"c1","deleted_at":null,"deleted_at":"2026-09-08T01:00:00Z","updated_at":"2026-09-07T23:00:00Z"}],"has_more":false}`, http.StatusOK), nil
		}
		t.Fatalf("an ambiguous parent reached child collection: %s", r.URL.Path)
		return nil, nil
	})
	if _, err := p.Handle(t.Context(), rt); err == nil {
		t.Fatal("an ambiguous deletion marker was accepted")
	}
	if len(rt.entries) != 0 || rt.committed(p) {
		t.Fatal("ambiguous page wrote entries or advanced its checkpoint")
	}
	if b, err := rt.Get(p.conf.key() + keyChildren); err == nil && len(b) != 0 {
		var list worklist
		if err := json.Unmarshal(b, &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Items) != 0 {
			t.Fatalf("ambiguous parent scheduled or retired child work: %#v", list.Items)
		}
	}
	if tombstones, err := loadTombstones(rt, p.conf.key()+keyTombstones); err != nil || len(tombstones) != 0 {
		t.Fatalf("ambiguous deletion marker produced tombstones: %v %v", tombstones, err)
	}
}

func TestNonObjectRowFailsClosedWithoutCheckpoint(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"a1"},null],"has_more":false}`, 200), nil
	})
	if _, err := p.Handle(t.Context(), rt); err == nil {
		t.Fatal("malformed page was accepted")
	}
	if len(rt.entries) != 0 || rt.committed(p) {
		t.Fatal("malformed page partially wrote or advanced its checkpoint")
	}
}
