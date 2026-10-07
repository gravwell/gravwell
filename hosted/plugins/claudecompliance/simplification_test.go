package claudecompliance

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// --- One stanza, several datasets ---

// TestOneStanzaCoversSeveralDatasets proves a single stanza collects every
// dataset it lists, each against its own checkpoint, without needing a
// config stanza per endpoint.
func TestOneStanzaCoversSeveralDatasets(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Dataset = []string{"activities", "organizations", "groups"}
	if e := p.conf.Verify(); e != nil {
		t.Fatal(e)
	}
	var paths []string
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		return reply(`{"data":[{"id":"r","uuid":"r"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"/v1/compliance/activities", "/v1/compliance/organizations", "/v1/compliance/groups"} {
		found := false
		for _, got := range paths {
			found = found || got == want
		}
		if !found {
			t.Fatalf("stanza did not collect %s: %v", want, paths)
		}
	}
	if len(rt.entries) != 3 {
		t.Fatalf("expected one entry per dataset, got %d", len(rt.entries))
	}
	// Each dataset must checkpoint independently.
	seen := map[string]bool{}
	for _, name := range p.conf.Dataset {
		b, e := p.conf.bind(name)
		if e != nil {
			t.Fatal(e)
		}
		if seen[b.key()] {
			t.Fatalf("%s shares a checkpoint namespace with another dataset", name)
		}
		seen[b.key()] = true
		if cp := readCheckpoint(t, rt, b.key()); cp.Since.IsZero() {
			t.Fatalf("%s did not checkpoint", name)
		}
	}
}

// TestStanzaTagsCoverEverySelectedFamily proves tag negotiation covers every
// family the stanza can write to, de-duplicated and stable.
func TestStanzaTagsCoverEverySelectedFamily(t *testing.T) {
	p, _ := setup(t, "activities")
	p.conf.Dataset = []string{"activities", "organizations", "groups", "chats"}
	if e := p.conf.Verify(); e != nil {
		t.Fatal(e)
	}
	got := p.conf.Tags()
	want := []string{"claude-compliance-activities", "claude-compliance-conversations", "claude-compliance-directory"}
	if len(got) != len(want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tags = %v, want %v", got, want)
		}
	}
	// Tag-Name pins every selected dataset to one tag.
	p.conf.Tag_Name = "claude"
	if e := p.conf.Verify(); e != nil {
		t.Fatal(e)
	}
	if tags := p.conf.Tags(); len(tags) != 1 || tags[0] != "claude" {
		t.Fatalf("Tag-Name did not pin a single tag: %v", tags)
	}
}

// TestPerDatasetPageSizeIsClampedNotRejected proves one shared Page-Size is
// clamped to each endpoint's documented maximum instead of forcing the
// operator to pick a value that fits every endpoint at once.
func TestPerDatasetPageSizeIsClampedNotRejected(t *testing.T) {
	p, _ := setup(t, "activities")
	p.conf.Dataset = []string{"activities", "projects"}
	p.conf.Page_Size = 5000 // legal for activities, 50x the projects maximum
	if e := p.conf.Verify(); e != nil {
		t.Fatalf("shared page size rejected: %v", e)
	}
	activities, e := p.conf.bind("activities")
	if e != nil {
		t.Fatal(e)
	}
	projects, e := p.conf.bind("projects")
	if e != nil {
		t.Fatal(e)
	}
	if activities.Page_Size != 5000 {
		t.Fatalf("activities page size clamped unnecessarily: %d", activities.Page_Size)
	}
	if projects.Page_Size != Datasets["projects"].Limit {
		t.Fatalf("projects page size = %d, want %d", projects.Page_Size, Datasets["projects"].Limit)
	}
}

func TestMultiDatasetParameterValidation(t *testing.T) {
	base := func() *Config {
		p, _ := setup(t, "activities")
		return p.conf
	}
	c := base()
	c.Dataset = []string{"group-members"}
	c.Parameter = nil
	if e := c.Verify(); e == nil || !strings.Contains(e.Error(), "missing Parameter group_id") {
		t.Fatalf("missing placeholder accepted: %v", e)
	}
	c = base()
	c.Dataset = []string{"activities", "activities"}
	if e := c.Verify(); e == nil {
		t.Fatal("duplicate Dataset accepted")
	}
	c = base()
	c.Dataset = nil
	if e := c.Verify(); e == nil {
		t.Fatal("empty Dataset accepted")
	}
	c = base()
	c.Dataset = []string{"activities"}
	c.Parameter = []string{"group_id:g"}
	if e := c.Verify(); e == nil {
		t.Fatal("unused Parameter accepted")
	}
	// A parameter used by only one of several datasets is still used.
	c = base()
	c.Dataset = []string{"activities", "group-members"}
	c.Parameter = []string{"group_id:g"}
	if e := c.Verify(); e != nil {
		t.Fatalf("parameter shared across datasets rejected: %v", e)
	}
}

// --- Event versus Record ---

// TestEveryDatasetDeclaresAKind pins the classification so a new endpoint
// cannot be added without deciding which execution path it belongs on.
func TestEveryDatasetDeclaresAKind(t *testing.T) {
	for name, d := range Datasets {
		switch d.Kind {
		case Event:
			if d.Time == "" {
				t.Errorf("%s is an Event with no occurrence time", name)
			}
		case Record:
		default:
			t.Errorf("%s has an unknown Kind", name)
		}
	}
	// Messages and the activity feed are append-only; everything a directory
	// lists is mutable.
	for _, name := range []string{"activities", "chat-messages", "local-session-messages", "remote-session-messages"} {
		if Datasets[name].Kind != Event {
			t.Errorf("%s should be an Event", name)
		}
	}
	for _, name := range []string{"organizations", "groups", "projects", "chats", "organization-users"} {
		if Datasets[name].Kind != Record {
			t.Errorf("%s should be a Record", name)
		}
	}
}

// TestEventIdentityIsImmutableAcrossDigestChange proves the Event path keys
// solely on identity: an append-only record the vendor replays with altered
// bytes is not re-ingested, because an event cannot legitimately change.
func TestEventIdentityIsImmutableAcrossDigestChange(t *testing.T) {
	p, rt := setup(t, "activities")
	if Datasets["activities"].Kind != Event {
		t.Fatal("precondition: activities must be an Event")
	}
	body := `{"data":[{"id":"a1","created_at":"2026-09-07T23:59:00Z","note":"first"}],"has_more":false}`
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(body, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("first poll wrote %d entries", len(rt.entries))
	}
	// Same identity, different bytes.
	body = `{"data":[{"id":"a1","created_at":"2026-09-07T23:59:00Z","note":"rewritten"}],"has_more":false}`
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("event replayed with altered bytes was re-ingested: %d entries", len(rt.entries))
	}
}

// TestRecordDigestChangeIsIngested proves the Record path still keys on the
// digest, so a mutable object edited in place is collected again.
func TestRecordDigestChangeIsIngested(t *testing.T) {
	p, rt := setup(t, "organizations")
	if Datasets["organizations"].Kind != Record {
		t.Fatal("precondition: organizations must be a Record")
	}
	body := `{"data":[{"uuid":"o1","name":"before"}],"has_more":false}`
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(body, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("unchanged record was rewritten: %d entries", len(rt.entries))
	}
	body = `{"data":[{"uuid":"o1","name":"after"}],"has_more":false}`
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 2 {
		t.Fatalf("edited record was not re-ingested: %d entries", len(rt.entries))
	}
}

// --- Discrete storage keys ---

// TestStateIsHeldInDiscreteTypedKeys proves progress is readable key by key
// through the runtime's own KV rather than as one opaque object.
func TestStateIsHeldInDiscreteTypedKeys(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"a","created_at":"2026-09-07T23:59:00Z"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	prefix := p.conf.key()
	since, e := rt.GetTime(prefix + keySince)
	if e != nil || since.IsZero() {
		t.Fatalf("since is not its own typed key: %v %v", since, e)
	}
	cursor, e := rt.GetString(prefix + keyWalkCursor)
	if e != nil || cursor != "" {
		t.Fatalf("walk cursor is not its own key: %q %v", cursor, e)
	}
	if _, e := rt.Get(prefix + keyManifest); e != nil {
		t.Fatalf("manifest is not its own key: %v", e)
	}
	// The dedup manifest and the child worklist are the only values that
	// stay whole, because hosted.Storage has no Delete and no prefix listing
	// to shrink or enumerate a per-element encoding with.
	var storage hostedStorage = rt
	_ = storage
}

// hostedStorage pins the exact storage surface a plugin is given. If Delete
// or a listing operation is ever added, the manifest and worklist should be
// revisited; until then they cannot be spread over one key per element.
type hostedStorage interface {
	Get(string) ([]byte, error)
	Put(string, []byte) error
	GetString(string) (string, error)
	PutString(string, string) error
	GetInt64(string) (int64, error)
	PutInt64(string, int64) error
	GetTime(string) (time.Time, error)
	PutTime(string, time.Time) error
}

// TestInterruptedCommitNeverSkipsData walks every prefix of a dataset commit
// and of a page commit, failing the store part way through, and proves that
// no partial write can advance the committed lower bound past data that was
// not written. A torn commit may cost duplicates; it may never lose records.
func TestInterruptedCommitNeverSkipsData(t *testing.T) {
	for cut := 0; cut < 10; cut++ {
		t.Run(string(rune('a'+cut)), func(t *testing.T) {
			p, rt := setup(t, "activities")
			page := 0
			p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				page++
				if page == 1 {
					return reply(`{"data":[{"id":"a1","created_at":"2026-09-07T23:50:00Z"}],"has_more":true,"last_id":"a1"}`, 200), nil
				}
				return reply(`{"data":[{"id":"a2","created_at":"2026-09-07T23:51:00Z"}],"has_more":false}`, 200), nil
			})
			// Fail the store after `cut` successful writes.
			writes := 0
			rt.failAfter = func() bool {
				writes++
				return writes > cut
			}
			_, _ = p.Handle(t.Context(), rt)
			rt.failAfter = nil

			// Whatever survived, a fresh cycle must still be able to reach
			// both records, and the committed lower bound must never sit
			// after a record that was never written.
			cp := readCheckpoint(t, rt, p.conf.key())
			written := map[string]bool{}
			for _, ent := range rt.entries {
				written[string(ent.Data)] = true
			}
			for _, rec := range []struct{ id, ts string }{
				{"a1", "2026-09-07T23:50:00Z"}, {"a2", "2026-09-07T23:51:00Z"},
			} {
				body := `{"id":"` + rec.id + `","created_at":"` + rec.ts + `"}`
				if written[body] {
					continue
				}
				at, _ := time.Parse(time.RFC3339, rec.ts)
				if !cp.Since.IsZero() && cp.Since.After(at) {
					t.Fatalf("cut=%d: lower bound %v advanced past unwritten record %s", cut, cp.Since, rec.id)
				}
			}
		})
	}
}
