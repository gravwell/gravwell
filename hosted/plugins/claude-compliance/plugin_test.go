package claudecompliance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"golang.org/x/time/rate"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type runtime struct {
	hosted.Runtime
	saved                []byte
	entries              []entry.Entry
	failWrite, failState bool
	failDelivery         bool
	states               map[string][]byte
	negotiatedTags       []string
}

func (r *runtime) SyncContext(ctx context.Context, timeout time.Duration) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if r.failDelivery {
		return errors.New("synthetic ingest synchronization failure")
	}
	return nil
}

func TestFailedIngestSyncDoesNotCommitState(t *testing.T) {
	p, rt := setup(t, "activities")
	rt.failDelivery = true
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"a"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e == nil {
		t.Fatal("failed synchronization accepted")
	}
	if len(rt.entries) != 1 || rt.saved != nil {
		t.Fatal("queued write advanced state")
	}
	rt.failDelivery = false
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if rt.saved == nil || len(rt.entries) != 2 {
		t.Fatal("record from failed synchronization not replayed")
	}
}

func TestMissingSynchronizationCapabilityFailsAtConstruction(t *testing.T) {
	p, _ := setup(t, "activities")
	if _, e := New(p.conf, nil); e == nil {
		t.Fatal("missing capability accepted")
	}
}

func (r *runtime) Get(key string) ([]byte, error) {
	if r.states != nil {
		if b, ok := r.states[key]; ok {
			return b, nil
		}
		return nil, storage.ErrStorageNotFound
	}
	if r.saved == nil {
		return nil, storage.ErrStorageNotFound
	}
	return r.saved, nil
}
func (r *runtime) Put(key string, b []byte) error {
	if r.failState {
		return errors.New("synthetic state failure")
	}
	r.saved = append([]byte(nil), b...)
	if r.states != nil {
		r.states[key] = append([]byte(nil), b...)
	}
	return nil
}
func (r *runtime) Write(e entry.Entry) error {
	if r.failWrite {
		return errors.New("synthetic write failure")
	}
	r.entries = append(r.entries, e)
	return nil
}
func (r *runtime) NegotiateTag(tag string) (entry.EntryTag, error) {
	r.negotiatedTags = append(r.negotiatedTags, tag)
	return 1, nil
}
func setup(t *testing.T, name string) (*Plugin, *runtime) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key")
	if e := os.WriteFile(path, []byte("synthetic-test-key"), 0600); e != nil {
		t.Fatal(e)
	}
	c := &Config{Dataset: name, Credential_File: path, Scope_Identity: "synthetic-" + t.Name(), Page_Size: 100, Max_Pages: 5, Max_Retries: 1, Follow_Children: "disabled"}
	c.BaseConfig.Ingester_UUID = "00000000-0000-4000-8000-000000000321"
	d, _ := lookup(name)
	for _, m := range placeholder.FindAllStringSubmatch(d.Path, -1) {
		c.Parameter = append(c.Parameter, m[1]+":synthetic-id")
	}
	if e := c.Verify(); e != nil {
		t.Fatal(e)
	}
	rt := &runtime{}
	p, err := New(c, rt)
	if err != nil {
		t.Fatal(err)
	}
	p.limiter = rate.NewLimiter(rate.Inf, 1000)
	p.now = func() time.Time { return time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC) }
	p.wait = func(context.Context, time.Duration) error { return nil }
	return p, rt
}
func reply(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestEveryJSONOperationFramesOneNativeObject(t *testing.T) {
	for _, d := range Catalog() {
		t.Run(d.Name, func(t *testing.T) {
			p, rt := setup(t, d.Name)
			p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.Header.Get("x-api-key") != "synthetic-test-key" || r.Header.Get("anthropic-version") != "2023-06-01" {
					t.Fatal("invalid request")
				}
				raw := `{ "id": "synthetic", "content":"line\nbreak", "provenance":{"type":"unknown"} }`
				if d.Rows != "" {
					raw = `{"` + d.Rows + `": [` + raw + `],"has_more":false,"next_page":null}`
				}
				return reply(raw, 200), nil
			})
			if _, e := p.Handle(t.Context(), rt); e != nil {
				t.Fatal(e)
			}
			if len(rt.entries) != 1 || rt.saved == nil {
				t.Fatal("missing entry or state")
			}
			b := rt.entries[0].Data
			if !json.Valid(b) || bytes.ContainsAny(b, "\r\n") {
				t.Fatal("invalid framing")
			}
			if v, ok := rt.entries[0].GetEnumeratedValue("_source"); !ok || v != d.Name {
				t.Fatal("missing discriminator")
			}
			var v map[string]any
			_ = json.Unmarshal(b, &v)
			if v["content"] != "line\nbreak" {
				t.Fatal("native content changed")
			}
		})
	}
}
func TestEmptyPageWithContinuationIsDrained(t *testing.T) {
	p, rt := setup(t, "local-session-messages")
	n := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		n++
		if n == 1 {
			return reply(`{"session":{"id":"s"},"data":[],"next_page":"opaque"}`, 200), nil
		}
		if r.URL.Query().Get("page") != "opaque" {
			t.Fatal("wrong opaque token")
		}
		return reply(`{"session":{"id":"s"},"data":[{"id":"m","content":[{"type":"tool_use","input":"{truncated","truncated":true}]}],"next_page":null}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if n != 2 || len(rt.entries) != 1 {
		t.Fatal("empty page terminated traversal")
	}
}
func TestChatMessagesUseCorrectEnvelope(t *testing.T) {
	p, rt := setup(t, "chat-messages")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"wrong"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e == nil || rt.saved != nil {
		t.Fatal("accepted wrong messages envelope")
	}
}
func TestFailuresNeverAdvanceState(t *testing.T) {
	for _, kind := range []string{"write", "403", "application", "missing-array", "repeated", "missing-token", "size"} {
		t.Run(kind, func(t *testing.T) {
			p, rt := setup(t, "activities")
			rt.failWrite = kind == "write"
			if kind == "size" {
				p.conf.Max_Entry_Bytes = 1
			}
			p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
				s := `{"data":[{"id":"x"}],"has_more":false}`
				code := 200
				switch kind {
				case "403":
					code = 403
					s = `{"error":{"message":"sensitive"}}`
				case "application":
					s = `{"error":{"type":"permission_error"}}`
				case "missing-array":
					s = `{"has_more":false}`
				case "repeated":
					s = `{"data":[],"has_more":true,"last_id":"same"}`
				case "missing-token":
					s = `{"data":[],"has_more":true}`
				}
				return reply(s, code), nil
			})
			_, e := p.Handle(t.Context(), rt)
			if e == nil || rt.saved != nil {
				t.Fatal("failure advanced state")
			}
			if strings.Contains(e.Error(), "sensitive") {
				t.Fatal("error leaked response")
			}
		})
	}
}
func TestRetryAfterAndNoRetryHeader(t *testing.T) {
	for _, noRetry := range []bool{false, true} {
		p, rt := setup(t, "activities")
		n := 0
		var delay time.Duration
		p.wait = func(_ context.Context, d time.Duration) error { delay = d; return nil }
		p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
			n++
			if n == 1 {
				code := 429
				if noRetry {
					code = 500
				}
				r := reply(`{}`, code)
				r.Header.Set("retry-after", "120")
				if noRetry {
					r.Header.Set("x-should-retry", "false")
				}
				return r, nil
			}
			return reply(`{"data":[],"has_more":false}`, 200), nil
		})
		_, e := p.Handle(t.Context(), rt)
		if noRetry {
			if n != 1 || e == nil {
				t.Fatal("retried prohibited response")
			}
		} else if e != nil || n != 2 || delay < 120*time.Second {
			t.Fatal("Retry-After not honored")
		}
	}
}
func TestRestartDeduplicatesAndRetainsWindow(t *testing.T) {
	p, rt := setup(t, "chats")
	n := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		n++
		if r.URL.Query().Get("order_by") != "updated_at" || r.URL.Query().Has("user_ids[]") {
			t.Fatal("invalid org-wide update filter")
		}
		return reply(`{"data":[{"id":"chat","updated_at":"2026-09-07T22:00:00Z"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	before := append([]byte(nil), rt.saved...)
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 1 || n != 2 || !bytes.Equal(before, rt.saved) {
		t.Fatal("restart replayed unchanged records")
	}
	old := p.conf.key()
	p.conf.Scope_Identity = "different-scope"
	if old == p.conf.key() {
		t.Fatal("scope not bound to state")
	}
}
func TestCancellationAndTimestampFallback(t *testing.T) {
	p, rt := setup(t, "activities")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e := p.Handle(ctx, rt); e == nil || rt.saved != nil {
		t.Fatal("ignored cancellation")
	}
	now := p.now()
	if sourceTime([]byte(`{"created_at":"2026-09-08T00:00:00"}`), now) != now {
		t.Fatal("invented timezone")
	}
}

type persistedRuntime struct {
	hosted.Runtime
	bucket *storage.BucketWriter
}

func (r persistedRuntime) Get(k string) ([]byte, error) { return r.bucket.Get(k) }
func (r persistedRuntime) Put(k string, v []byte) error { return r.bucket.Put(k, v) }

func TestStandardRuntimeDoesNotNeedPrivateMethods(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"a"}],"has_more":false}`, 200), nil
	})
	// The static embedded interface intentionally hides the mock's SyncContext.
	wrapped := struct{ hosted.Runtime }{rt}
	if _, err := p.Handle(t.Context(), wrapped); err != nil {
		t.Fatal(err)
	}
	if rt.saved == nil {
		t.Fatal("missing checkpoint")
	}
}

func TestPartialPageAndStateFailuresReplay(t *testing.T) {
	for _, failure := range []string{"next-page", "state", "cancel-after-sync", "max-pages"} {
		t.Run(failure, func(t *testing.T) {
			p, rt := setup(t, "activities")
			failed := true
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("after_id") == "a" {
					if failed && failure == "next-page" {
						return reply(`{}`, 403), nil
					}
					return reply(`{"data":[{"id":"b"}],"has_more":false}`, 200), nil
				}
				return reply(`{"data":[{"id":"a"}],"has_more":true,"last_id":"a"}`, 200), nil
			})
			rt.failState = failure == "state"
			if failure == "max-pages" {
				p.conf.Max_Pages = 1
			}
			if failure == "cancel-after-sync" {
				p.syncIngest = func(context.Context, time.Duration) error { cancel(); return nil }
			}
			if _, err := p.Handle(ctx, rt); err == nil || rt.saved != nil {
				t.Fatal("failure advanced checkpoint")
			}
			before := len(rt.entries)
			failed, rt.failState = false, false
			p.conf.Max_Pages = 5
			p.syncIngest = rt.SyncContext
			if _, err := p.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			if len(rt.entries) != before+2 || rt.saved == nil {
				t.Fatal("partial cycle was not replayed")
			}
		})
	}
}

func TestLookbackStartAndCheckpointPrecedence(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Lookback = 48
	want := p.now().Add(-48 * time.Hour)
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.Query().Get("created_at.gte"); got != want.Format(time.RFC3339Nano) {
			t.Fatalf("lower bound %s, want %s", got, want)
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	want = p.now().Add(-300 * time.Second)
	p.conf.Lookback = 720
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	rt.saved = nil
	p.conf.Start_Time = "2026-09-01T00:00:00Z"
	want, _ = time.Parse(time.RFC3339, p.conf.Start_Time)
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointPersistsAcrossBoltReopen(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"a"}],"has_more":false}`, 200), nil
	})
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := storage.OpenBoltHandler(path, true)
	if err != nil {
		t.Fatal(err)
	}
	bw, err := db.GetBucketWriter("claude-test")
	if err != nil {
		t.Fatal(err)
	}
	wrapped := persistedRuntime{rt, bw}
	if _, err := p.Handle(t.Context(), wrapped); err != nil {
		t.Fatal(err)
	}
	if err := bw.Sync(); err != nil {
		t.Fatal(err)
	}
	before, err := bw.Get(p.conf.key())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = storage.OpenBoltHandler(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bw, err = db.GetBucketWriter("claude-test")
	if err != nil {
		t.Fatal(err)
	}
	wrapped.bucket = bw
	if _, err := p.Handle(t.Context(), wrapped); err != nil {
		t.Fatal(err)
	}
	after, err := bw.Get(p.conf.key())
	if err != nil || !bytes.Equal(before, after) || len(rt.entries) != 1 {
		t.Fatal("restart lost checkpoint or replayed unchanged event")
	}
}

func TestConfigIdentityAndBounds(t *testing.T) {
	p, _ := setup(t, "activities")
	c := *p.conf
	if !p.conf.Equal(c) || !p.conf.Equal(&c) || p.conf.Equal(nil) || p.conf.Equal((*Config)(nil)) || p.conf.Equal("wrong") {
		t.Fatal("config equality contract")
	}
	c.Scope_Identity = "another-scope"
	if p.conf.Equal(c) || c.key() == p.conf.key() {
		t.Fatal("scope change did not isolate progress")
	}
	for _, hours := range []int{-1, 1 << 30} {
		c = *p.conf
		c.Lookback = lookbackHours(hours)
		if err := c.Verify(); err == nil {
			t.Fatal("invalid lookback accepted")
		}
	}
	c = *p.conf
	c.Credential_File = filepath.Join(t.TempDir(), "missing")
	if err := c.Verify(); err == nil {
		t.Fatal("missing key accepted")
	}
}

func TestNewParentsAndFailedTranscriptSurviveRestart(t *testing.T) {
	p, rt := setup(t, "local-sessions")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	fail := true
	newParent := false
	messages := map[string]int{}
	tr := transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1/compliance/apps/sessions/local" {
			body := `{"data":[{"id":"s1","updated_at":"2026-09-08T00:00:00Z"}`
			if newParent {
				body += `,{"id":"s2","updated_at":"2026-09-08T00:01:00Z"}`
			}
			return reply(body+`],"next_page":null}`, 200), nil
		}
		if strings.Contains(r.URL.Path, "/messages") {
			if r.URL.Query().Has("page") {
				t.Fatal("resumed expired token")
			}
			if fail {
				return reply(`{}`, 503), nil
			}
			messages[r.URL.Path]++
			return reply(`{"session":{"id":"s"},"data":[{"id":"m","provenance":{"type":"content_unavailable"}}],"next_page":null}`, 200), nil
		}
		t.Fatal("unexpected discovery path")
		return nil, nil
	})
	p.http.Transport = tr
	if _, e := p.Handle(t.Context(), rt); e == nil {
		t.Fatal("missing failure")
	}
	var pending worklist
	if e := json.Unmarshal(rt.states[p.conf.key()+"/child-work-v1"], &pending); e != nil || len(pending.Items) != 1 {
		t.Fatal("child work not persisted")
	}
	fail = false
	newParent = true
	oldNow := p.now()
	p.now = func() time.Time { return oldNow.Add(2 * time.Minute) }
	restarted, err := New(p.conf, rt)
	if err != nil {
		t.Fatal(err)
	}
	restarted.http.Transport = tr
	restarted.now = p.now
	restarted.wait = p.wait
	restarted.limiter = p.limiter
	if _, e := restarted.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(messages) != 2 {
		t.Fatalf("new/pending parents not collected: %v", messages)
	}
	if e := json.Unmarshal(rt.states[p.conf.key()+"/child-work-v1"], &pending); e != nil {
		t.Fatal(e)
	}
	for _, w := range pending.Items {
		if w.Pending {
			t.Fatal("completed work remains pending")
		}
	}
}
func TestActiveRemoteSessionsAreRevisitedAndPending404Retained(t *testing.T) {
	p, rt := setup(t, "remote-sessions")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	pending := true
	calls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/remote") {
			if r.URL.Query().Has("updated_at.gte") || r.URL.Query().Has("user_ids[]") {
				t.Fatal("invalid remote filter")
			}
			return reply(`{"data":[{"id":"agent-session","user":null,"agent_id":"a","status":"active"}],"next_page":null}`, 200), nil
		}
		calls++
		if pending {
			return reply(`{}`, 404), nil
		}
		return reply(`{"session":{"id":"agent-session"},"data":[{"id":"m"}],"next_page":null}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e == nil {
		t.Fatal("pending 404 hidden")
	}
	pending = false
	oldNow := p.now()
	p.now = func() time.Time { return oldNow.Add(2 * time.Minute) }
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if calls != 3 {
		t.Fatal("active remote session not revisited")
	}
}

func TestUnchangedOrganizationRefreshesMembership(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	members := 1
	calls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/organizations"):
			return reply(`{"data":[{"uuid":"o"}],"has_more":false}`, 200), nil
		case strings.HasSuffix(r.URL.Path, "/users"):
			calls++
			rows := `{"data":[{"id":"u1"}`
			if members == 2 {
				rows += `,{"id":"u2"}`
			}
			return reply(rows+`],"has_more":false}`, 200), nil
		case strings.HasSuffix(r.URL.Path, "/roles"):
			return reply(`{"data":[],"has_more":false}`, 200), nil
		}
		t.Fatal("unexpected child path")
		return nil, nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	members = 2
	oldNow := p.now()
	p.now = func() time.Time { return oldNow.Add(2 * time.Hour) }
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatal("unchanged parent suppressed new member")
	}
}

func TestCompletedHistoryRetiresWithoutEvictingPending(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 2
	p.conf.Max_Children = 2
	rt.states = map[string][]byte{}
	list := worklist{Items: map[string]work{
		"old1": {Dataset: "group-members", Parameter: []string{"group_id:old1"}, LastCompleted: p.now().Add(-time.Hour)},
		"old2": {Dataset: "group-members", Parameter: []string{"group_id:old2"}, LastCompleted: p.now()},
	}}
	b, _ := json.Marshal(list)
	rt.states[p.conf.key()+"/child-work-v1"] = b
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"new"}],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	list = worklist{}
	if e := json.Unmarshal(rt.states[p.conf.key()+"/child-work-v1"], &list); e != nil {
		t.Fatal(e)
	}
	if len(list.Items) != 2 {
		t.Fatal("history bound changed")
	}
	if _, ok := list.Items["old1"]; ok {
		t.Fatal("oldest completed entry retained")
	}
	for k, w := range list.Items {
		w.Pending = true
		w.RetryAt = p.now().Add(time.Hour)
		list.Items[k] = w
	}
	b, _ = json.Marshal(list)
	rt.states[p.conf.key()+"/child-work-v1"] = b
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"different"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e == nil {
		t.Fatal("pending work silently evicted")
	}
}

func TestFailedChildDoesNotStarveHealthyWork(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}
	healthy := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"a-failing"},{"id":"z-healthy"}],"has_more":false}`, 200), nil
		}
		if strings.Contains(r.URL.Path, "a-failing") {
			return reply(`{}`, 404), nil
		}
		healthy++
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e == nil {
		t.Fatal("failure hidden")
	}
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if healthy != 1 {
		t.Fatal("failed child starved healthy child")
	}
}

func TestDiscoveryDoesNotFilterOutUnchangedProjects(t *testing.T) {
	p, rt := setup(t, "projects")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Has("updated_at.gte") || r.URL.Query().Has("updated_at.lte") {
			t.Fatal("child discovery excludes unchanged project")
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
}
func TestDeletedChatDoesNotScheduleContentAndSharedBudget(t *testing.T) {
	d, _ := lookup("chats")
	rows, e := childWork(d, nil, []byte(`{"id":"x","deleted_at":"2026-09-08T00:00:00Z"}`))
	if e != nil || len(rows) != 0 {
		t.Fatal("scheduled deleted content")
	}
	if sharedLimiter("one-parent", 30) != sharedLimiter("one-parent", 60) {
		t.Fatal("budget multiplied per child")
	}
}

func TestCustomTagIsPreservedForParentAndDiscoveredChildren(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Tag_Name = "tenant-compliance-directory"
	rt.states = map[string][]byte{}
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g"}],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[{"id":"member"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.negotiatedTags) != 2 {
		t.Fatalf("parent/child negotiations: %v", rt.negotiatedTags)
	}
	for _, tag := range rt.negotiatedTags {
		if tag != p.conf.Tags()[0] {
			t.Fatalf("negotiated undeclared tag %q", tag)
		}
	}
}

func TestSingleUnderscoreMetadataPreservesNativeRecord(t *testing.T) {
	p, rt := setup(t, "local-session-messages")
	const native = `{"id":"message-1","content":"unchanged","__meta":{"native":true},"__disabled":false,"__custom":"keep"}`
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"session":{"id":"session-1"},"data":[`+native+`],"next_page":null}`, 200), nil
	})
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(rt.entries))
	}
	ent := rt.entries[0]
	if !bytes.Equal(ent.Data, []byte(native)) {
		t.Fatalf("native JSON changed: %s", ent.Data)
	}
	want := map[string]string{
		"_vendor": "Anthropic", "_product": "Claude Enterprise Compliance",
		"_source": "local-session-messages", "_recordType": "local-session-messages",
		"_endpoint": "/v1/compliance" + p.conf.dataset.Path, "_apiVersion": "2023-06-01",
		"_parent": "session_id:synthetic-id", "_session": `{"id":"session-1"}`,
	}
	for key, value := range want {
		if got, ok := ent.GetEnumeratedValue(key); !ok || got != value {
			t.Errorf("%s = %v (present=%v), want %s", key, got, ok, value)
		}
		if _, ok := ent.GetEnumeratedValue("_" + key); ok {
			t.Errorf("obsolete metadata key emitted: _%s", key)
		}
	}
	if rt.saved == nil {
		t.Fatal("acknowledged record did not retain normal checkpoint behavior")
	}
}
