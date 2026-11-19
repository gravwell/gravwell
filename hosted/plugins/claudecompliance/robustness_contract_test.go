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
		if !ok || parent != "role_id:role-1" {
			t.Fatalf("unexpected discovery parent context: %q", parent)
		}
		return
	}
	t.Fatal("role-permissions entry was not emitted")
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
