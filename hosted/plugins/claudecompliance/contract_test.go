package claudecompliance

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// These tests pin the documented Compliance API contract: pagination
// termination, indexing lag, cursor lifetime, windowed manifests, per-endpoint
// identity and timestamp fields, and discovery semantics.

func TestAfterIDEndpointWithoutHasMoreIsComplete(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		// has_more is optional and defaults to false on the Activity Feed.
		return reply(`{"data":[{"id":"a","created_at":"2026-09-07T23:50:00Z"}],"first_id":"a","last_id":"a"}`, 200), nil
	})
	cont, e := p.Handle(t.Context(), rt)
	if e != nil || cont == nil || cont.Delay == 0 || len(rt.entries) != 1 {
		t.Fatalf("absent has_more was not treated as complete: err=%v entries=%d", e, len(rt.entries))
	}
}

func TestPageEndpointStopsWhenHasMoreIsFalse(t *testing.T) {
	p, rt := setup(t, "groups")
	n := 0
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		n++
		return reply(`{"data":[{"id":"g"}],"has_more":false,"next_page":"ignored"}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if n != 1 {
		t.Fatalf("followed next_page after has_more=false: %d requests", n)
	}
}

func TestSessionPagesContinueUntilNextPageIsNull(t *testing.T) {
	p, rt := setup(t, "remote-sessions")
	n := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		n++
		if r.URL.Query().Get("page") == "" {
			return reply(`{"data":[{"id":"s1","status":"archived"}],"next_page":"page_2"}`, 200), nil
		}
		return reply(`{"data":[{"id":"s2","status":"archived"}],"next_page":null}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if n != 2 || len(rt.entries) != 2 {
		t.Fatalf("session pagination without has_more: requests=%d entries=%d", n, len(rt.entries))
	}
}

func TestWindowUpperBoundTrailsIndexingLag(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Overlap_Seconds = 1
	var lte string
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		lte = r.URL.Query().Get("created_at.lte")
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if want := p.now().Add(-time.Minute).Format(time.RFC3339Nano); lte != want {
		t.Fatalf("upper bound %s, want %s", lte, want)
	}
	st := rt.checkpoint(t, p)
	// Even a one-second overlap resumes behind the lagged bound, so a
	// record indexed up to a minute late is never skipped.
	if want := p.now().Add(-time.Minute - time.Second); !st.Since.Equal(want) {
		t.Fatalf("next lower bound %s, want %s", st.Since, want)
	}
}

func TestExpiredStoredWalkRestartsWithoutDuplicates(t *testing.T) {
	p, rt := setup(t, "local-session-messages")
	p.conf.Max_Pages = 1
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Query().Get("page") {
		case "":
			return reply(`{"session":{"id":"s"},"data":[{"id":"m1","created_at":"2026-09-07T10:00:00Z"}],"next_page":"page_2"}`, 200), nil
		case "page_2":
			return reply(`{"session":{"id":"s"},"data":[{"id":"m2","created_at":"2026-09-07T11:00:00Z"}],"next_page":null}`, 200), nil
		}
		t.Fatal("unexpected cursor")
		return nil, nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil || len(rt.entries) != 1 {
		t.Fatalf("first page: err=%v entries=%d", e, len(rt.entries))
	}
	// The runner is down for longer than the vendor's 24-hour cursor life.
	start := p.now()
	p.now = func() time.Time { return start.Add(25 * time.Hour) }
	p.conf.Max_Pages = 5
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 2 || rt.warnings != 1 {
		t.Fatalf("restarted walk replayed or skipped: entries=%d warnings=%d", len(rt.entries), rt.warnings)
	}
}

func TestRejectedStoredCursorRestartsWalkOnce(t *testing.T) {
	p, rt := setup(t, "local-session-messages")
	p.conf.Max_Pages = 1
	expired := false
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Query().Get("page") {
		case "":
			return reply(`{"session":{"id":"s"},"data":[{"id":"m1"}],"next_page":"page_2"}`, 200), nil
		case "page_2":
			if expired {
				return reply(`{"error":{"type":"invalid_request_error","message":"The page cursor has expired."}}`, 400), nil
			}
			return reply(`{"session":{"id":"s"},"data":[{"id":"m2"}],"next_page":null}`, 200), nil
		}
		return nil, fmt.Errorf("unexpected cursor")
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	expired = true
	p.conf.Max_Pages = 2
	// The stored cursor is rejected; the walk restarts, re-reads page one
	// without rewriting m1, then reaches the (still expired) cursor again
	// within this walk, which is an ordinary failure rather than a loop.
	if _, e := p.Handle(t.Context(), rt); e == nil || httpStatus(e) != http.StatusBadRequest {
		t.Fatalf("second rejection was not surfaced: %v", e)
	}
	if len(rt.entries) != 1 || rt.warnings != 1 {
		t.Fatalf("restart duplicated records: entries=%d warnings=%d", len(rt.entries), rt.warnings)
	}
	expired = false
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 2 {
		t.Fatalf("walk did not complete after recovery: entries=%d", len(rt.entries))
	}
}

func TestWindowedManifestDropsIdentitiesBeforeNextWindow(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"old","created_at":"2026-09-07T12:00:00Z"},{"id":"recent","created_at":"2026-09-07T23:57:00Z"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	st := rt.checkpoint(t, p)
	if _, ok := st.Manifest["id:old"]; ok {
		t.Fatal("identity outside every later window was retained")
	}
	if _, ok := st.Manifest["id:recent"]; !ok {
		t.Fatal("identity inside the overlap was dropped")
	}
}

func TestFullInventoryManifestIsNotPrunedByTime(t *testing.T) {
	p, rt := setup(t, "groups")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"g","updated_at":"2020-01-01T00:00:00Z"}],"has_more":false}`, 200), nil
	})
	for i := 0; i < 2; i++ {
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatal(e)
		}
	}
	if len(rt.entries) != 1 {
		t.Fatalf("unchanged inventory record replayed: %d", len(rt.entries))
	}
}

func TestChatsRootPollsIncrementallyWithDiscovery(t *testing.T) {
	p, rt := setup(t, "chats")
	p.conf.Follow_Children = "enabled"
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/chats") {
			q := r.URL.Query()
			if q.Get("order_by") != "updated_at" || !q.Has("updated_at.gte") || !q.Has("updated_at.lte") {
				t.Fatalf("chat root is not an incremental updated_at poll: %v", q)
			}
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
}

func TestDeletedProjectTombstonesEveryChild(t *testing.T) {
	d := Datasets["projects"]
	raw := []byte(`{"id":"p1","deleted_at":"2026-09-07T00:00:00Z","updated_at":"2026-09-07T00:00:00Z"}`)
	if rows, e := childWork(d, nil, raw); e != nil || len(rows) != 0 {
		t.Fatal("deleted project scheduled child content")
	}
	retired, ok, e := deletedChildWork(d, nil, raw)
	if e != nil || !ok || len(retired) != 2 {
		t.Fatalf("deleted project children not retired: %v %v %d", e, ok, len(retired))
	}
	names := map[string]bool{}
	for _, w := range retired {
		names[w.Dataset] = true
		if w.Revision == "" || len(w.Parameter) != 1 || w.Parameter[0] != "project_id:p1" {
			t.Fatalf("unexpected retired work %+v", w)
		}
	}
	if !names["project-attachments"] || !names["project-collaborators"] {
		t.Fatalf("missing retired child families: %v", names)
	}
}

func TestPendingRemoteSessionSchedulesNoTranscript(t *testing.T) {
	d := Datasets["remote-sessions"]
	if rows, e := childWork(d, nil, []byte(`{"id":"cse_1","status":"pending"}`)); e != nil || len(rows) != 0 {
		t.Fatal("pending remote session scheduled a transcript that 404s")
	}
	if rows, e := childWork(d, nil, []byte(`{"id":"cse_1","status":"active"}`)); e != nil || len(rows) != 1 {
		t.Fatal("active remote session was not scheduled")
	}
}

func TestUnchangedLocalSessionIsNotReReadHourly(t *testing.T) {
	p, rt := setup(t, "local-sessions")
	p.conf.Follow_Children = "enabled"
	updated := "2026-09-07T20:00:00Z"
	transcripts := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/local") {
			return reply(`{"data":[{"id":"clls_1","updated_at":"`+updated+`"}],"next_page":null}`, 200), nil
		}
		transcripts++
		return reply(`{"session":{"id":"clls_1"},"data":[],"next_page":null}`, 200), nil
	})
	start := p.now()
	for i, offset := range []time.Duration{0, 2 * time.Hour} {
		p.now = func() time.Time { return start.Add(offset) }
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatal(i, e)
		}
	}
	if transcripts != 1 {
		t.Fatalf("unchanged local session transcript re-read: %d", transcripts)
	}
	// A new inference call advances updated_at and so the revision.
	updated = "2026-09-08T01:00:00Z"
	p.now = func() time.Time { return start.Add(3 * time.Hour) }
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if transcripts != 2 {
		t.Fatalf("changed local session was not revisited: %d", transcripts)
	}
}

func TestStableIdentityPerEndpoint(t *testing.T) {
	cases := []struct {
		dataset, raw, want string
	}{
		{"organizations", `{"uuid":"o1","name":"n"}`, "uuid:o1"},
		{"group-members", `{"user_id":"user_1","email":"a@example.com"}`, "user_id:user_1"},
		{"role-permissions", `{"action":"claude_code","resource_id":"r1","resource_type":"organization"}`, "resource_type:organization\x1fresource_id:r1\x1faction:claude_code"},
		{"project-collaborators", `{"type":"group","group_id":"rbac_group_1","role":"viewer"}`, "type:group\x1fgroup_id:rbac_group_1"},
		{"project-collaborators", `{"type":"user","user_id":null,"role":"viewer"}`, "sha256:digest"},
		{"artifact-metadata", `{"id":"claude_artifact_1","version_id":"claude_artifact_version_2"}`, "version_id:claude_artifact_version_2"},
		{"organization-settings", `{"type":"effective_organization_settings","organization_id":"o1"}`, "organization_id:o1"},
		{"activities", `{"id":"activity_1"}`, "id:activity_1"},
	}
	for _, c := range cases {
		d, _ := Datasets[c.dataset]
		if got := identity([]byte(c.raw), d.Identity, "digest"); got != c.want {
			t.Errorf("%s identity %q, want %q", c.dataset, got, c.want)
		}
	}
}

func TestChangedMemberKeepsOneIdentity(t *testing.T) {
	p, rt := setup(t, "group-members")
	email := "a@example.com"
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"user_id":"user_1","email":"`+email+`","updated_at":null}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	email = "b@example.com"
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	st := rt.checkpoint(t, p)
	if len(rt.entries) != 2 || len(st.Manifest) != 1 {
		t.Fatalf("member change: entries=%d manifest=%d", len(rt.entries), len(st.Manifest))
	}
}

func TestSnapshotTimestampsUseChangeTimeOrCollectionTime(t *testing.T) {
	collected := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		dataset, raw string
		want         time.Time
	}{
		// Account creation is not the time a user record changed.
		{"organization-users", `{"id":"user_1","created_at":"2023-01-01T00:00:00Z"}`, collected},
		{"organizations", `{"uuid":"o1","created_at":"2023-01-01T00:00:00Z"}`, collected},
		{"organization-roles", `{"id":"r","created_at":"2023-01-01T00:00:00Z","updated_at":null}`, collected},
		{"organization-roles", `{"id":"r","created_at":"2023-01-01T00:00:00Z","updated_at":"2026-09-01T00:00:00Z"}`, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
		{"activities", `{"id":"a","created_at":"2026-09-07T12:00:00Z"}`, time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)},
		{"chat-messages", `{"id":"m","created_at":"2026-09-07T12:00:00Z"}`, time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)},
		{"remote-sessions", `{"id":"s","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-09-07T12:00:00Z"}`, time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		d, _ := Datasets[c.dataset]
		if got := sourceTime([]byte(c.raw), d.Time, collected); !got.Equal(c.want) {
			t.Errorf("%s %s: timestamp %s, want %s", c.dataset, c.raw, got, c.want)
		}
	}
}

func TestEveryDatasetDeclaresTimeAndIdentityContract(t *testing.T) {
	for _, d := range Datasets {
		if len(d.Identity) == 0 {
			t.Errorf("%s has no identity contract", d.Name)
		}
		if d.Window != "" && d.Rows == "" {
			t.Errorf("%s windows a single-object endpoint", d.Name)
		}
	}
	if len(Datasets) != 26 {
		t.Fatalf("catalog has %d operations, want 26", len(Datasets))
	}
}

// Traversal, discovery, and framing behavior under the conditions that make
// each one easy to get wrong.

func TestBacklogOverMaxPagesIsNotReplayed(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Max_Pages = 3
	n := 0
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		n++
		return reply(fmt.Sprintf(`{"data":[{"id":"a%d"}],"has_more":true,"last_id":"c%d"}`, n, n), 200), nil
	})
	for i := 0; i < 3; i++ {
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatal(e)
		}
	}
	if len(rt.entries) != 9 || rt.committed(p) == false {
		t.Fatalf("backlog replayed or uncheckpointed: entries=%d", len(rt.entries))
	}
	seen := map[string]bool{}
	for _, e := range rt.entries {
		if seen[string(e.Data)] {
			t.Fatalf("duplicate entry %s", e.Data)
		}
		seen[string(e.Data)] = true
	}
}

func TestChildrenBeyondMaxPendingDoNotBrickRoot(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 2
	p.conf.Max_Children = 2
	members := map[string]int{}
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g1"},{"id":"g2"},{"id":"g3"}],"has_more":false}`, 200), nil
		}
		members[r.URL.Path]++
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	start := p.now()
	for i := 0; i < 4; i++ {
		p.now = func() time.Time { return start.Add(time.Duration(i) * time.Minute) }
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatalf("poll %d: %v", i, e)
		}
	}
	groups := 0
	for _, e := range rt.entries {
		if v, _ := e.GetEnumeratedValue("_source"); v == "groups" {
			groups++
		}
	}
	if groups != 3 {
		t.Fatalf("root groups written %d times, want exactly 3", groups)
	}
	if members["/v1/compliance/groups/g3/members"] == 0 {
		t.Fatalf("g3 members never fetched: %v", members)
	}
}

func TestChildlessRecordsDoNotRewriteWorklist(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Follow_Children = "enabled"
	rt.putCounts = map[string]int{}
	rows := make([]string, 500)
	for i := range rows {
		rows[i] = fmt.Sprintf(`{"id":"a%d","created_at":"2026-09-07T23:50:00Z"}`, i)
	}
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[`+strings.Join(rows, ",")+`],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	total := 0
	for _, n := range rt.putCounts {
		total += n
	}
	// A completed traversal commits a small fixed set of discrete keys and
	// nothing per record, so state writes must not scale with the page.
	if total == 0 || total > 8 || len(rt.entries) != 500 {
		t.Fatalf("state writes=%d entries=%d", total, len(rt.entries))
	}
}

func TestLargeSessionEnvelopeDoesNotFailEntries(t *testing.T) {
	p, rt := setup(t, "remote-session-messages")
	blob := strings.Repeat("x", 70000)
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"session":{"id":"s","pad":"`+blob+`"},"data":[{"id":"m"}],"next_page":null}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 1 || rt.committed(p) == false {
		t.Fatal("70000-byte session envelope blocked the message")
	}
	if len(blob) <= entry.MaxEvDataLength {
		t.Fatal("test blob no longer exceeds the enumerated-value limit")
	}
}

func TestMalformedParentIsSkippedNotBlocking(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g1"},{"name":"no-id"},{"id":"g3"}],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	for i := 0; i < 2; i++ {
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatalf("poll %d: %v", i, e)
		}
	}
	if len(rt.entries) != 3 {
		t.Fatalf("id-less parent blocked or duplicated the page: entries=%d", len(rt.entries))
	}
}

func TestTransportFailureIsRetriedWithBackoff(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Max_Retries = 2
	n := 0
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		n++
		if n < 3 {
			return nil, errors.New("synthetic connection reset")
		}
		return reply(`{"data":[{"id":"a"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if n != 3 || len(rt.entries) != 1 || len(rt.sleeps) != 2 || rt.sleeps[0] != time.Second || rt.sleeps[1] != 2*time.Second {
		t.Fatalf("transport retry: requests=%d entries=%d sleeps=%v", n, len(rt.entries), rt.sleeps)
	}
}

func TestTransportFailureExhaustsWithoutAdvancingState(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Max_Retries = 1
	n := 0
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		n++
		return nil, errors.New("synthetic connection reset")
	})
	if _, e := p.Handle(t.Context(), rt); e == nil || rt.committed(p) {
		t.Fatal("exhausted transport retries advanced state")
	}
	if n != 2 {
		t.Fatalf("transport attempts=%d, want Max-Retries+1", n)
	}
}

func TestExpiredStoredListWalkRestartsInsteadOfSkippingSessions(t *testing.T) {
	// A list page token older than 24 hours is still accepted but is
	// re-evaluated against the current retention boundary, so resuming it
	// can silently skip sessions. The walk must restart without page.
	p, rt := setup(t, "local-sessions")
	p.conf.Max_Pages = 1
	var pages []string
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		pages = append(pages, r.URL.Query().Get("page"))
		if r.URL.Query().Get("page") == "" {
			return reply(`{"data":[{"id":"clls_1","updated_at":"2026-09-07T23:00:00Z"}],"next_page":"page_2"}`, 200), nil
		}
		return reply(`{"data":[{"id":"clls_2","updated_at":"2026-09-07T22:00:00Z"}],"next_page":null}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	start := p.now()
	p.now = func() time.Time { return start.Add(24 * time.Hour) }
	p.conf.Max_Pages = 5
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(pages) != 3 || pages[1] != "" || pages[2] != "page_2" {
		t.Fatalf("stale list token was resumed: %q", pages)
	}
	if len(rt.entries) != 2 {
		t.Fatalf("restarted list walk duplicated or skipped: entries=%d", len(rt.entries))
	}
}
