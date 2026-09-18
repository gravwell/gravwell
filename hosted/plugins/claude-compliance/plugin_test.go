package claudecompliance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"golang.org/x/time/rate"
	"io"
	"net/http"
	"net/url"
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
	putCounts            map[string]int
	warnings             int
	// warningLog additively captures each Warn call's rendered message and
	// KV fields, for tests that need to inspect what a warning contained
	// (e.g. proving a value was never logged), without disturbing the
	// existing warnings counter that other tests already rely on.
	warningLog []string
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
	if r.putCounts != nil {
		r.putCounts[key]++
	}
	if r.states != nil {
		r.states[key] = append([]byte(nil), b...)
	}
	return nil
}

func (r *runtime) Debug(string, ...rfc5424.SDParam) {}
func (r *runtime) Info(string, ...rfc5424.SDParam)  {}
func (r *runtime) Warn(msg string, params ...rfc5424.SDParam) {
	r.warnings++
	line := msg
	for _, p := range params {
		line += " " + p.Name + "=" + p.Value
	}
	r.warningLog = append(r.warningLog, line)
}
func (r *runtime) Error(string, ...rfc5424.SDParam)    {}
func (r *runtime) Critical(string, ...rfc5424.SDParam) {}
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
func TestFailuresBeforePageCommitNeverAdvanceState(t *testing.T) {
	for _, kind := range []string{"write", "403", "application", "missing-array", "missing-token", "size"} {
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

func TestRepeatedCursorRetainsCompletedPageCheckpoint(t *testing.T) {
	p, rt := setup(t, "activities")
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"x"}],"has_more":true,"last_id":"same"}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e == nil {
		t.Fatal("repeated cursor accepted")
	}
	if len(rt.entries) != 1 || rt.saved == nil {
		t.Fatal("completed page checkpoint was not retained")
	}
	var st state
	if e := json.Unmarshal(rt.saved, &st); e != nil || st.Traversal == nil || st.Traversal.Cursor != "same" {
		t.Fatal("missing resumable traversal state")
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

func TestPageCheckpointBoundsReplayAfterLaterFailure(t *testing.T) {
	for _, failure := range []string{"next-page", "state", "cancel-after-sync"} {
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
			if failure == "cancel-after-sync" {
				p.syncIngest = func(context.Context, time.Duration) error { cancel(); return nil }
			}
			if _, err := p.Handle(ctx, rt); err == nil {
				t.Fatal("failure accepted")
			}
			before := len(rt.entries)
			if failure == "next-page" {
				if rt.saved == nil {
					t.Fatal("completed page was not checkpointed")
				}
			} else if rt.saved != nil {
				t.Fatal("failed page advanced checkpoint")
			}
			failed, rt.failState = false, false
			p.syncIngest = rt.SyncContext
			if _, err := p.Handle(t.Context(), rt); err != nil {
				t.Fatal(err)
			}
			wantAdded := 2
			if failure == "next-page" {
				wantAdded = 1
			}
			if len(rt.entries) != before+wantAdded || rt.saved == nil {
				t.Fatal("unexpected replay after failure")
			}
		})
	}
}

func TestMaxPagesContinuesFromCommittedCursor(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Max_Pages = 1
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("after_id") == "a" {
			return reply(`{"data":[{"id":"b"}],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[{"id":"a"}],"has_more":true,"last_id":"a"}`, 200), nil
	})
	cont, e := p.Handle(t.Context(), rt)
	if e != nil || cont == nil || cont.Delay != 0 || len(rt.entries) != 1 || rt.saved == nil {
		t.Fatal("page limit did not return a committed immediate continuation")
	}
	restarted, e := New(p.conf, rt)
	if e != nil {
		t.Fatal(e)
	}
	restarted.http.Transport = p.http.Transport
	restarted.now, restarted.wait, restarted.limiter = p.now, p.wait, p.limiter
	cont, e = restarted.Handle(t.Context(), rt)
	if e != nil || cont == nil || cont.Delay == 0 || len(rt.entries) != 2 {
		t.Fatal("resumed traversal did not complete without replay")
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
		"old1": {Dataset: "group-members", Parameter: []string{"group_id:old1"}, Revision: "rev-old1", LastCompleted: p.now().Add(-time.Hour)},
		"old2": {Dataset: "group-members", Parameter: []string{"group_id:old2"}, Revision: "rev-old2", LastCompleted: p.now()},
	}}
	old1 := list.Items["old1"]
	old1Config, e := childConfig(p.conf, old1)
	if e != nil {
		t.Fatal(e)
	}
	old1.StateKey = old1Config.key()
	list.Items["old1"] = old1
	large, _ := json.Marshal(state{Since: p.now(), Manifest: manifest{"message": {Digest: strings.Repeat("a", 4096)}}})
	rt.states[old1.StateKey] = large
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
	var pruned state
	if e := json.Unmarshal(rt.states[old1.StateKey], &pruned); e != nil || len(pruned.Manifest) != 0 {
		t.Fatal("evicted child checkpoint was not compacted")
	}
	if pruned.Retired != "" {
		t.Fatal("capacity eviction must not tombstone the checkpoint; it would suppress an unchanged parent forever")
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
	before := len(rt.entries)
	cont, e := p.Handle(t.Context(), rt)
	if e != nil || cont == nil || cont.Delay != 0 {
		t.Fatal("pending capacity did not defer the root page")
	}
	if len(rt.entries) != before {
		t.Fatal("deferred root page was partially written")
	}
	var after worklist
	if e = json.Unmarshal(rt.states[p.conf.key()+"/child-work-v1"], &after); e != nil || len(after.Items) != 2 {
		t.Fatal("pending work was not preserved")
	}
}

// TestCapacityEvictedUnchangedParentIsRediscovered proves the fix for the
// tombstone-reuse defect: Max-Pending capacity eviction must compact a
// child's checkpoint without marking it Retired, because the parent's
// revision is unrelated to why it was evicted. A still-unchanged parent
// (identical revision) has to be rediscovered and its child work re-run the
// next time capacity allows it back in -- not permanently suppressed as if
// it were confirmed-deleted.
func TestCapacityEvictedUnchangedParentIsRediscovered(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 1
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}
	active := "g1"
	g1Calls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(fmt.Sprintf(`{"data":[{"id":%q}],"has_more":false}`, active), 200), nil
		}
		if strings.Contains(r.URL.Path, "g1") {
			g1Calls++
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	// Cycle 1: g1 is discovered and its membership completed.
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	workKey := p.conf.key() + "/child-work-v1"
	var list worklist
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil {
		t.Fatal(e)
	}
	g1, ok := list.Items["group-members/group_id:g1"]
	if !ok || g1.Pending {
		t.Fatal("g1 was not discovered and completed")
	}
	callsAfterCycle1 := g1Calls
	if callsAfterCycle1 == 0 {
		t.Fatal("g1's child work never ran")
	}

	// Cycle 2: g2 appears. With Max_Pending exhausted, g1 -- the only
	// non-pending candidate -- is evicted for capacity.
	active = "g2"
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	list = worklist{}
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil {
		t.Fatal(e)
	}
	if _, ok := list.Items["group-members/group_id:g1"]; ok {
		t.Fatal("g1 was not evicted for Max-Pending capacity")
	}
	var pruned state
	if e := json.Unmarshal(rt.states[g1.StateKey], &pruned); e != nil {
		t.Fatal(e)
	}
	if pruned.Retired != "" {
		t.Fatal("capacity eviction must not tombstone the checkpoint")
	}

	// Cycle 3: g1 reappears, content-identical (same revision) to before
	// its eviction. It must be rediscovered, not silently dropped.
	active = "g1"
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	list = worklist{}
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil {
		t.Fatal(e)
	}
	g1Again, ok := list.Items["group-members/group_id:g1"]
	if !ok {
		t.Fatal("unchanged capacity-evicted parent was never rediscovered")
	}
	if g1Again.Revision != g1.Revision {
		t.Fatal("test setup issue: g1's revision changed across cycles")
	}
	if g1Calls == callsAfterCycle1 {
		t.Fatal("rediscovered parent's child work never actually ran")
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

// TestInProgressChildIsNotPreemptedByNewDiscovery guards against a newly
// discovered child (LastAttempt zero) cutting ahead of an older child that
// is already mid-pagination (LastAttempt set, but not yet Pending=false).
// Under Max-Children=1 saturation, g1 must keep making progress on each
// eligible cycle instead of losing its turn to g2 every time g2 appears.
func TestInProgressChildIsNotPreemptedByNewDiscovery(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 1
	p.conf.Max_Pages = 1 // force group-members to need more than one Handle cycle
	rt.states = map[string][]byte{}

	addG2 := false
	g1Page := 0
	g1Calls, g2Calls := 0, 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/groups"):
			body := `{"data":[{"id":"g1"}`
			if addG2 {
				body += `,{"id":"g2"}`
			}
			return reply(body+`],"has_more":false}`, 200), nil
		case strings.Contains(r.URL.Path, "g1"):
			g1Calls++
			if g1Page == 0 {
				g1Page++
				return reply(`{"data":[{"id":"m1"}],"has_more":false,"next_page":"tok"}`, 200), nil
			}
			return reply(`{"data":[{"id":"m2"}],"has_more":false}`, 200), nil
		case strings.Contains(r.URL.Path, "g2"):
			g2Calls++
			return reply(`{"data":[],"has_more":false}`, 200), nil
		}
		t.Fatal("unexpected path")
		return nil, nil
	})

	// Cycle 1: discovers and makes the first attempt at g1. It is not done
	// (its own page cap was hit) so it remains Pending with a real LastAttempt.
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if g1Calls != 1 || g2Calls != 0 {
		t.Fatalf("unexpected cycle 1 calls: g1=%d g2=%d", g1Calls, g2Calls)
	}

	// Cycle 2: g2 is newly discovered in the same cycle g1 is next eligible.
	// g1 (older, already in progress) must keep its turn under Max-Children=1.
	addG2 = true
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if g1Calls != 2 || g2Calls != 0 {
		t.Fatalf("in-progress child g1 was preempted by newly-discovered child g2: g1Calls=%d g2Calls=%d", g1Calls, g2Calls)
	}

	// Cycle 3: g1 has completed; g2 must still get its turn.
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if g2Calls != 1 {
		t.Fatalf("newly-discovered child g2 never ran: g1Calls=%d g2Calls=%d", g1Calls, g2Calls)
	}
}

// TestLegacyZeroLastAttemptWorkItemIsNotStuckFirstForever confirms that a
// child-work item persisted before the fairness fix (LastAttempt left at
// its Go zero value) does not perpetually cut ahead of items that have a
// real recorded attempt time, and that it still runs (and thereby acquires
// a real LastAttempt) once nothing newer is competing for the slot.
func TestLegacyZeroLastAttemptWorkItemIsNotStuckFirstForever(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 1
	rt.states = map[string][]byte{}

	legacyConfig, e := childConfig(p.conf, work{Dataset: "group-members", Parameter: []string{"group_id:legacy"}})
	if e != nil {
		t.Fatal(e)
	}
	attempted := work{Dataset: "group-members", Parameter: []string{"group_id:attempted"}, Revision: "rev-attempted", Pending: true, LastAttempt: p.now().Add(-time.Minute)}
	attemptedConfig, e := childConfig(p.conf, attempted)
	if e != nil {
		t.Fatal(e)
	}
	list := worklist{Items: map[string]work{
		"group-members/group_id:legacy":    {Dataset: "group-members", Parameter: []string{"group_id:legacy"}, Revision: "rev-legacy", Pending: true, StateKey: legacyConfig.key()}, // LastAttempt left zero, as pre-fix persisted state would have it
		"group-members/group_id:attempted": {Dataset: "group-members", Parameter: []string{"group_id:attempted"}, Revision: "rev-attempted", Pending: true, LastAttempt: attempted.LastAttempt, StateKey: attemptedConfig.key()},
	}}
	b, _ := json.Marshal(list)
	rt.states[p.conf.key()+"/child-work-v1"] = b

	var order []string
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[],"has_more":false}`, 200), nil
		}
		switch {
		case strings.Contains(r.URL.Path, "legacy"):
			order = append(order, "legacy")
		case strings.Contains(r.URL.Path, "attempted"):
			order = append(order, "attempted")
		default:
			t.Fatal("unexpected child path")
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})

	// The already-attempted item must run before the never-attempted
	// (zero LastAttempt) legacy item under Max-Children=1.
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(order) != 1 || order[0] != "attempted" {
		t.Fatalf("legacy zero-LastAttempt item cut ahead of already-attempted work: order=%v", order)
	}
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(order) != 2 || order[1] != "legacy" {
		t.Fatalf("legacy zero-LastAttempt item never got its turn: order=%v", order)
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

func TestRecordCapContinuesFromCommittedCursor(t *testing.T) {
	p, rt := setup(t, "activities")
	p.maxRecords = 1
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("after_id") == "a" {
			return reply(`{"data":[{"id":"b"}],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[{"id":"a"}],"has_more":true,"last_id":"a"}`, 200), nil
	})
	cont, e := p.Handle(t.Context(), rt)
	if e != nil || cont == nil || cont.Delay != 0 || len(rt.entries) != 1 {
		t.Fatal("record cap did not preserve a resumable page boundary")
	}
	cont, e = p.Handle(t.Context(), rt)
	if e != nil || cont == nil || cont.Delay == 0 || len(rt.entries) != 2 {
		t.Fatal("record-cap continuation replayed or skipped data")
	}
}

func TestPendingCapacityDefersPageWithoutDuplicateRootWrites(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pending = 2
	p.conf.Max_Children = 2
	rt.states = map[string][]byte{}
	children := map[string]int{}
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g1"},{"id":"g2"},{"id":"g3"}],"has_more":false}`, 200), nil
		}
		for _, id := range []string{"g1", "g2", "g3"} {
			if strings.Contains(r.URL.Path, id) {
				children[id]++
				return reply(`{"data":[],"has_more":false}`, 200), nil
			}
		}
		t.Fatal("unexpected child path")
		return nil, nil
	})
	cont, e := p.Handle(t.Context(), rt)
	if e != nil || cont == nil || cont.Delay != 0 || len(rt.entries) != 0 {
		t.Fatal("capacity-limited page was partially accepted")
	}
	cont, e = p.Handle(t.Context(), rt)
	if e != nil || cont == nil || cont.Delay == 0 || len(rt.entries) != 3 {
		t.Fatal("deferred root page did not complete exactly once")
	}
	for _, id := range []string{"g1", "g2", "g3"} {
		if children[id] != 1 {
			t.Fatalf("child %s calls = %d", id, children[id])
		}
	}
}

func TestChildWorkPersistenceIsBatchedPerPage(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	rt.putCounts = map[string]int{}
	var body strings.Builder
	body.WriteString(`{"data":[`)
	for i := 0; i < 500; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"id":"a-%d"}`, i)
	}
	body.WriteString(`],"has_more":false}`)
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(body.String(), 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if got := rt.putCounts[p.conf.key()+"/child-work-v1"]; got != 0 {
		t.Fatalf("activity records wrote child worklist %d times", got)
	}
	if got := rt.putCounts[p.conf.key()]; got != 1 {
		t.Fatalf("dataset checkpoint writes = %d", got)
	}
}

func TestOversizedSessionContextDoesNotBlockMessages(t *testing.T) {
	p, rt := setup(t, "local-session-messages")
	session := strings.Repeat("s", entry.MaxEvDataLength+1)
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"session":{"id":"`+session+`"},"data":[{"id":"m","content":"retained"}],"next_page":null}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 1 || rt.saved == nil || rt.warnings != 1 {
		t.Fatal("oversized session blocked message delivery")
	}
	if _, ok := rt.entries[0].GetEnumeratedValue("_session"); ok {
		t.Fatal("oversized session was attached as an enumerated value")
	}
	if !bytes.Contains(rt.entries[0].Data, []byte(`"content":"retained"`)) {
		t.Fatal("native message fields were discarded")
	}
}

func TestDiscoveredChatCollectsHistoryBeforeUsingWindow(t *testing.T) {
	p, rt := setup(t, "chats")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	childQueries := []url.Values{}
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/chats") {
			return reply(`{"data":[{"id":"c1","updated_at":"2020-01-01T00:00:00Z"}],"has_more":false}`, 200), nil
		}
		childQueries = append(childQueries, r.URL.Query())
		return reply(`{"session":{"id":"c1"},"chat_messages":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(childQueries) != 1 || childQueries[0].Has("updated_at.gte") || childQueries[0].Has("updated_at.lte") {
		t.Fatal("initial discovered chat history was windowed")
	}
	oldNow := p.now()
	p.now = func() time.Time { return oldNow.Add(2 * time.Hour) }
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(childQueries) != 2 || !childQueries[1].Has("updated_at.gte") || !childQueries[1].Has("updated_at.lte") {
		t.Fatal("completed chat history did not resume bounded polling")
	}
}

func TestLegacyDiscoveredChatCheckpointGetsHistoryBackfill(t *testing.T) {
	p, rt := setup(t, "chat-messages")
	p.conf.discovered = true
	rt.states = map[string][]byte{}
	legacy, e := json.Marshal(state{
		Since:    p.now().Add(-time.Hour),
		Manifest: manifest{"old": {Digest: "digest"}},
	})
	if e != nil {
		t.Fatal(e)
	}
	rt.states[p.conf.key()] = legacy
	var query url.Values
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		query = r.URL.Query()
		return reply(`{"session":{"id":"c1"},"chat_messages":[],"has_more":false}`, 200), nil
	})
	if _, e = p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if query.Has("updated_at.gte") || query.Has("updated_at.lte") {
		t.Fatal("legacy discovered chat checkpoint skipped corrective history backfill")
	}
	var upgraded state
	if e = json.Unmarshal(rt.states[p.conf.key()], &upgraded); e != nil {
		t.Fatal(e)
	}
	if !upgraded.HistoryComplete {
		t.Fatal("corrective history backfill was not recorded")
	}
}

func TestMissingParentIdentityIsLoggedAndSkipped(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	children := map[string]int{}
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g1"},{"name":"missing-id"},{"id":"g2"}],"has_more":false}`, 200), nil
		}
		for _, id := range []string{"g1", "g2"} {
			if strings.Contains(r.URL.Path, id) {
				children[id]++
				return reply(`{"data":[],"has_more":false}`, 200), nil
			}
		}
		t.Fatal("unexpected child path")
		return nil, nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 3 || rt.saved == nil || rt.warnings != 1 {
		t.Fatal("malformed parent blocked the root dataset")
	}
	if children["g1"] != 1 || children["g2"] != 1 {
		t.Fatal("valid siblings were not collected")
	}
}

func TestCredentialReadsFullFileAndRejectsUnsafeContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	want := strings.Repeat("k", 16384)
	if e := os.WriteFile(path, []byte(want), 0600); e != nil {
		t.Fatal(e)
	}
	if got, e := credential(path); e != nil || got != want {
		t.Fatal("full credential file was not read")
	}
	if e := os.WriteFile(path, []byte("key\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if got, e := credential(path); e != nil || got != "key" {
		t.Fatal("single trailing newline was not handled")
	}
	if e := os.WriteFile(path, []byte("first\nsecond"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := credential(path); e == nil {
		t.Fatal("multiline credential accepted")
	}
	if e := os.WriteFile(path, []byte(strings.Repeat("x", 16385)), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := credential(path); e == nil {
		t.Fatal("oversized credential accepted")
	}
}

func TestDeletedChatCompactsCheckpointAndRemovesWork(t *testing.T) {
	p, rt := setup(t, "chats")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	deleted := false
	childCalls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/chats") {
			if deleted {
				return reply(`{"data":[{"id":"c1","deleted_at":"2026-09-08T01:00:00Z"}],"has_more":false}`, 200), nil
			}
			return reply(`{"data":[{"id":"c1","deleted_at":null}],"has_more":false}`, 200), nil
		}
		childCalls++
		return reply(`{"session":{"id":"c1"},"chat_messages":[{"id":"m1"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	var list worklist
	workKey := p.conf.key() + "/child-work-v1"
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil || len(list.Items) != 1 {
		t.Fatal("chat child work was not retained")
	}
	var child work
	for _, child = range list.Items {
	}
	deleted = true
	oldNow := p.now()
	p.now = func() time.Time { return oldNow.Add(2 * time.Hour) }
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	list = worklist{}
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil || len(list.Items) != 0 {
		t.Fatal("deleted chat retained child work")
	}
	var pruned state
	if e := json.Unmarshal(rt.states[child.StateKey], &pruned); e != nil || len(pruned.Manifest) != 0 || pruned.Retired == "" {
		t.Fatal("deleted chat checkpoint was not compacted")
	}
	if childCalls != 1 {
		t.Fatal("deleted chat transcript was fetched again")
	}
}

// TestStillDeletedChatRemainsSuppressed proves the tombstone-reuse fix did
// not weaken the one case where a revision tombstone is actually correct:
// a chat that stays deleted across further cycles (identical deleted_at
// content, hence identical revision) must remain suppressed forever, with
// its transcript never refetched.
func TestStillDeletedChatRemainsSuppressed(t *testing.T) {
	p, rt := setup(t, "chats")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	childCalls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/chats") {
			return reply(`{"data":[{"id":"c1","deleted_at":"2026-09-08T01:00:00Z"}],"has_more":false}`, 200), nil
		}
		childCalls++
		return reply(`{"session":{"id":"c1"},"chat_messages":[{"id":"m1"}],"has_more":false}`, 200), nil
	})
	oldNow := p.now()
	for i := 0; i < 3; i++ {
		delay := time.Duration(i) * 2 * time.Hour
		p.now = func() time.Time { return oldNow.Add(delay) }
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatal(e)
		}
	}
	workKey := p.conf.key() + "/child-work-v1"
	var list worklist
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil || len(list.Items) != 0 {
		t.Fatal("still-deleted chat should have no child work")
	}
	if childCalls != 0 {
		t.Fatal("still-deleted chat's transcript should never be fetched")
	}
}

// TestUndeletedChatBecomesDiscoverable proves the tombstone-reuse fix did
// not weaken genuine vendor deletion: once a deleted chat's record actually
// changes (deleted_at reverts to null), its revision changes too, so the
// tombstone correctly no longer matches and the chat becomes discoverable
// again.
func TestUndeletedChatBecomesDiscoverable(t *testing.T) {
	p, rt := setup(t, "chats")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	deleted := true
	childCalls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/chats") {
			if deleted {
				return reply(`{"data":[{"id":"c1","deleted_at":"2026-09-08T01:00:00Z"}],"has_more":false}`, 200), nil
			}
			return reply(`{"data":[{"id":"c1","deleted_at":null}],"has_more":false}`, 200), nil
		}
		childCalls++
		return reply(`{"session":{"id":"c1"},"chat_messages":[{"id":"m1"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	workKey := p.conf.key() + "/child-work-v1"
	var list worklist
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil || len(list.Items) != 0 {
		t.Fatal("deleted chat should have no child work")
	}
	if childCalls != 0 {
		t.Fatal("deleted chat's transcript should never be fetched")
	}

	deleted = false
	oldNow := p.now()
	p.now = func() time.Time { return oldNow.Add(2 * time.Hour) }
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	list = worklist{}
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil || len(list.Items) != 1 {
		t.Fatal("undeleted chat was not rediscovered")
	}
	if childCalls == 0 {
		t.Fatal("rediscovered chat's transcript was never fetched")
	}
}

// TestManifestGrowthIsBounded proves the primary dataset checkpoint's
// identity/digest manifest cannot grow past the configured bound, and that
// repeatedly exceeding the bound (as a full, unwindowed "organizations"
// scan does on every cycle once the tenant has more entities than the
// bound) never turns into an error or a replay loop.
func TestManifestGrowthIsBounded(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.maxManifestEntries = 5
	const total = 20
	var body strings.Builder
	body.WriteString(`{"data":[`)
	for i := 0; i < total; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"uuid":"org-%02d","updated_at":"2026-01-01T00:00:%02dZ"}`, i, i)
	}
	body.WriteString(`],"has_more":false}`)
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(body.String(), 200), nil
	})
	for cycle := 0; cycle < 6; cycle++ {
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatalf("cycle %d: reaching the manifest bound caused an error instead of evicting: %v", cycle, e)
		}
	}
	var st state
	if e := json.Unmarshal(rt.saved, &st); e != nil {
		t.Fatal(e)
	}
	if len(st.Manifest) > 5 {
		t.Fatalf("manifest retained %d entries, exceeding the configured bound of 5", len(st.Manifest))
	}
	if len(rt.saved) > 4<<10 {
		t.Fatalf("checkpoint blob grew unexpectedly large for a 5-entry bound: %d bytes", len(rt.saved))
	}
}

// TestManifestBoundPreservesDedupAcrossRestart confirms that, as long as the
// working set fits within the configured manifest bound, restart-and-resume
// still recognizes unchanged records and does not replay them.
func TestManifestBoundPreservesDedupAcrossRestart(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.maxManifestEntries = 10
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"uuid":"o1","updated_at":"2026-01-01T00:00:00Z"},{"uuid":"o2","updated_at":"2026-01-01T00:00:01Z"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 2 {
		t.Fatalf("first scan must write both new records, got %d", len(rt.entries))
	}
	restarted, e := New(p.conf, rt)
	if e != nil {
		t.Fatal(e)
	}
	restarted.maxManifestEntries = p.maxManifestEntries
	restarted.now, restarted.wait, restarted.limiter = p.now, p.wait, p.limiter
	restarted.http.Transport = p.http.Transport
	if _, e := restarted.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 2 {
		t.Fatalf("restart replayed unchanged records within the manifest bound, entries = %d", len(rt.entries))
	}
}

// TestManifestEvictionRewritesStaleRecordsInsteadOfDroppingData proves the
// eviction direction is safe: once the bound is exceeded, only the identity
// the vendor reports as least-recently-updated stops being deduplicated, and
// it is rewritten (a bounded, deterministic duplicate) rather than lost.
// Records that remain within the bound stay correctly deduplicated.
func TestManifestEvictionRewritesStaleRecordsInsteadOfDroppingData(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.maxManifestEntries = 2
	const body = `{"data":[{"uuid":"o1","updated_at":"2020-01-01T00:00:00Z"},{"uuid":"o2","updated_at":"2020-01-02T00:00:00Z"},{"uuid":"o3","updated_at":"2020-01-03T00:00:00Z"}],"has_more":false}`
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(body, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 3 {
		t.Fatalf("first scan must write every record, got %d", len(rt.entries))
	}
	for cycle := 0; cycle < 3; cycle++ {
		before := len(rt.entries)
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatalf("cycle %d: %v", cycle, e)
		}
		// Exactly the oldest identity (o1, evicted to respect the bound of 2)
		// must be rewritten each cycle; o2/o3 stay cached and deduplicated.
		if got := len(rt.entries) - before; got != 1 {
			t.Fatalf("cycle %d: want exactly 1 rewritten stale record, got %d", cycle, got)
		}
		var st state
		if e := json.Unmarshal(rt.saved, &st); e != nil {
			t.Fatal(e)
		}
		if len(st.Manifest) > 2 {
			t.Fatalf("cycle %d: manifest exceeded its bound of 2: %d entries", cycle, len(st.Manifest))
		}
	}
}

// TestLegacyPlainStringManifestShapeStillLoads confirms a checkpoint written
// before this bound existed -- where each manifest value was a bare digest
// string rather than {"d":"...","s":...} -- still loads, and that its
// digest is still honored for unchanged-record detection.
func TestLegacyPlainStringManifestShapeStillLoads(t *testing.T) {
	p, rt := setup(t, "organizations")
	rt.states = map[string][]byte{}
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"uuid":"o1"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("first scan must write the new record, got %d", len(rt.entries))
	}
	var st state
	if e := json.Unmarshal(rt.saved, &st); e != nil {
		t.Fatal(e)
	}
	if len(st.Manifest) != 1 {
		t.Fatalf("expected exactly one manifest entry, got %d", len(st.Manifest))
	}
	var key, digest string
	for k, v := range st.Manifest {
		key, digest = k, v.Digest
	}

	// Rewrite the persisted checkpoint into the plain {"key":"digest"} shape
	// that builds before this fix wrote, to prove the upgraded reader still
	// loads it and still honors its dedup digest.
	legacy, e := json.Marshal(struct {
		Since    time.Time         `json:"since"`
		Manifest map[string]string `json:"manifest"`
	}{Since: st.Since, Manifest: map[string]string{key: digest}})
	if e != nil {
		t.Fatal(e)
	}
	rt.states[p.conf.key()] = legacy

	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatalf("legacy plain-string manifest failed to load: %v", e)
	}
	if len(rt.entries) != 1 {
		t.Fatal("legacy manifest digest match was not honored; unchanged record was rewritten")
	}
	if e := json.Unmarshal(rt.states[p.conf.key()], &st); e != nil {
		t.Fatal(e)
	}
	if st.Manifest[key].Digest != digest {
		t.Fatal("legacy manifest entry was not upgraded to the current shape")
	}
}

// --- Bounded absence-based retirement for parent kinds with no vendor
// deletion signal (organizations, groups, projects, local-sessions; see
// absenceTrackedParents). ---

func TestAbsentParentBelowThresholdDoesNotRetireChild(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	present := true
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			if present {
				return reply(`{"data":[{"id":"g1"}],"has_more":false}`, 200), nil
			}
			return reply(`{"data":[],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	present = false
	// Two consecutive complete scans in which the parent is absent must not
	// retire its child work; the threshold requires three.
	for i := 0; i < 2; i++ {
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatal(e)
		}
	}
	workKey := p.conf.key() + "/child-work-v1"
	var list worklist
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil {
		t.Fatal(e)
	}
	w, ok := list.Items["group-members/group_id:g1"]
	if !ok {
		t.Fatal("child work was retired before the required consecutive absences")
	}
	if w.AbsentStreak != 2 {
		t.Fatalf("expected AbsentStreak=2 after two complete absent scans, got %d", w.AbsentStreak)
	}
}

func TestAbsentParentReappearanceResetsStreak(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	present := true
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			if present {
				return reply(`{"data":[{"id":"g1"}],"has_more":false}`, 200), nil
			}
			return reply(`{"data":[],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	step := func() {
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatal(e)
		}
	}
	step() // discovered, present
	present = false
	step() // absent, streak -> 1
	present = true
	step() // reappears, streak resets to 0
	present = false
	step() // absent again, streak -> 1 (not 3: proves the reset took)

	workKey := p.conf.key() + "/child-work-v1"
	var list worklist
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil {
		t.Fatal(e)
	}
	w, ok := list.Items["group-members/group_id:g1"]
	if !ok {
		t.Fatal("child work was retired despite a reappearance resetting its streak")
	}
	if w.AbsentStreak != 1 {
		t.Fatalf("expected AbsentStreak=1 after a reappearance reset the streak, got %d", w.AbsentStreak)
	}
}

func TestAbsentParentRetiredAfterConsecutiveCompleteScans(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	present := true
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			if present {
				return reply(`{"data":[{"id":"g1"}],"has_more":false}`, 200), nil
			}
			return reply(`{"data":[],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	workKey := p.conf.key() + "/child-work-v1"
	var list worklist
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil {
		t.Fatal(e)
	}
	child, ok := list.Items["group-members/group_id:g1"]
	if !ok || child.StateKey == "" || child.Revision == "" {
		t.Fatal("child work was not discovered as expected")
	}

	present = false
	for i := 0; i < absentRetirementThreshold; i++ {
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatal(e)
		}
	}
	list = worklist{}
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil {
		t.Fatal(e)
	}
	if _, ok := list.Items["group-members/group_id:g1"]; ok {
		t.Fatalf("child work was not retired after %d consecutive complete absent scans", absentRetirementThreshold)
	}
	var pruned state
	if e := json.Unmarshal(rt.states[child.StateKey], &pruned); e != nil {
		t.Fatal(e)
	}
	if pruned.Retired != "" {
		t.Fatalf("absence retirement must not tombstone the child checkpoint (revision is unrelated to why it was retired): retired=%q", pruned.Retired)
	}
	if len(pruned.Manifest) != 0 || pruned.Traversal != nil {
		t.Fatal("retirement did not compact the child checkpoint")
	}
}

func TestPartialAndErroredScansNeverAdvanceAbsenceRetirement(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pages = 1
	rt.states = map[string][]byte{}

	// Discover g1 with one clean, complete scan.
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"g1"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	workKey := p.conf.key() + "/child-work-v1"
	streak := func() uint {
		var list worklist
		if e := json.Unmarshal(rt.states[workKey], &list); e != nil {
			t.Fatal(e)
		}
		return list.Items["group-members/group_id:g1"].AbsentStreak
	}

	// A chunked scan that never completes (Max-Pages=1, has_more always
	// true, g1 never present on any page fetched so far) must never be
	// mistaken for a confirmed absence: it never reaches a complete pass.
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[],"has_more":true,"next_page":"tok"}`, 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	for i := 0; i < 4; i++ {
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatal(e)
		}
	}
	if s := streak(); s != 0 {
		t.Fatalf("a chunked, never-completing scan advanced AbsentStreak to %d", s)
	}

	// A request failure must also never advance the streak.
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"error":"boom"}`, 500), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	for i := 0; i < 3; i++ {
		if _, e := p.Handle(t.Context(), rt); e == nil {
			t.Fatal("expected the request failure to surface as an error")
		}
	}
	if s := streak(); s != 0 {
		t.Fatalf("a failing request advanced AbsentStreak to %d", s)
	}
}

func TestAbsentParentRetirementLeavesUnrelatedChildWorkAlone(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	g1Present := true
	g2Calls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/groups"):
			if g1Present {
				return reply(`{"data":[{"id":"g1"},{"id":"g2"}],"has_more":false}`, 200), nil
			}
			return reply(`{"data":[{"id":"g2"}],"has_more":false}`, 200), nil
		case strings.Contains(r.URL.Path, "g2"):
			g2Calls++
			return reply(`{"data":[],"has_more":false}`, 200), nil
		default:
			return reply(`{"data":[],"has_more":false}`, 200), nil
		}
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	g1Present = false
	for i := 0; i < absentRetirementThreshold; i++ {
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatal(e)
		}
	}
	workKey := p.conf.key() + "/child-work-v1"
	var list worklist
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil {
		t.Fatal(e)
	}
	if _, ok := list.Items["group-members/group_id:g1"]; ok {
		t.Fatal("g1's child work should have been retired")
	}
	if _, ok := list.Items["group-members/group_id:g2"]; !ok {
		t.Fatal("unrelated g2 child work was removed alongside g1's retirement")
	}
	if g2Calls == 0 {
		t.Fatal("g2's child work never made progress")
	}
}

// TestAbsenceRetiredUnchangedParentIsRediscovered proves the fix for the
// tombstone-reuse defect: absence-based retirement must compact a child's
// checkpoint without marking it Retired, because absence is not a
// content-derived signal -- a parent that reappears unchanged after being
// retired presents the very same revision. It must be rediscovered and its
// child work re-run, not permanently suppressed as if it were
// confirmed-deleted.
func TestAbsenceRetiredUnchangedParentIsRediscovered(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	present := true
	g1Calls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			if present {
				return reply(`{"data":[{"id":"g1"}],"has_more":false}`, 200), nil
			}
			return reply(`{"data":[],"has_more":false}`, 200), nil
		}
		g1Calls++
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	workKey := p.conf.key() + "/child-work-v1"
	var list worklist
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil {
		t.Fatal(e)
	}
	child, ok := list.Items["group-members/group_id:g1"]
	if !ok || child.StateKey == "" || child.Revision == "" {
		t.Fatal("child work was not discovered as expected")
	}
	callsBeforeRetirement := g1Calls
	if callsBeforeRetirement == 0 {
		t.Fatal("g1's child work never ran")
	}

	present = false
	for i := 0; i < absentRetirementThreshold; i++ {
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatal(e)
		}
	}
	list = worklist{}
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil {
		t.Fatal(e)
	}
	if _, ok := list.Items["group-members/group_id:g1"]; ok {
		t.Fatalf("child work was not retired after %d consecutive complete absent scans", absentRetirementThreshold)
	}
	var pruned state
	if e := json.Unmarshal(rt.states[child.StateKey], &pruned); e != nil {
		t.Fatal(e)
	}
	if pruned.Retired != "" {
		t.Fatal("absence retirement must not tombstone the checkpoint")
	}

	// g1 reappears, content-identical (same revision) to before retirement.
	present = true
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	list = worklist{}
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil {
		t.Fatal(e)
	}
	again, ok := list.Items["group-members/group_id:g1"]
	if !ok {
		t.Fatal("unchanged absence-retired parent was never rediscovered")
	}
	if again.Revision != child.Revision {
		t.Fatal("test setup issue: g1's revision changed across cycles")
	}
	if g1Calls == callsBeforeRetirement {
		t.Fatal("rediscovered parent's child work never actually ran")
	}
}

// --- Bounding the combined primary + in-flight traversal manifest as a
// single shared budget. ---

func TestCombinedManifestNeverExceedsConfiguredLimitDuringTraversal(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.conf.Max_Pages = 1
	p.conf.Page_Size = 1
	p.maxManifestEntries = 5
	rt.states = map[string][]byte{}

	// Prime a primary manifest already at the bound; the forced multi-page
	// traversal below revisits exactly these five identities, letting
	// compaction keep pace with insertion as it goes.
	prior := manifest{}
	for i := 0; i < 5; i++ {
		prior[fmt.Sprintf("uuid:o%d", i)] = manifestEntry{Digest: "will-not-match", Seen: int64(i)}
	}
	primed, e := json.Marshal(state{Since: p.now().Add(-time.Hour), Manifest: prior})
	if e != nil {
		t.Fatal(e)
	}
	rt.states[p.conf.key()] = primed

	call := 0
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`{"data":[{"uuid":"o%d","updated_at":"2026-01-01T00:00:%02dZ"}],"has_more":%v`, call, call, call < 4)
		if call < 4 {
			body += fmt.Sprintf(`,"next_page":"c%d"`, call+1)
		}
		body += `}`
		call++
		return reply(body, 200), nil
	})

	for i := 0; i < 5; i++ {
		if _, e := p.Handle(t.Context(), rt); e != nil {
			t.Fatal(e)
		}
		var st state
		if e := json.Unmarshal(rt.saved, &st); e != nil {
			t.Fatal(e)
		}
		total := len(st.Manifest)
		if st.Traversal != nil {
			total += len(st.Traversal.Manifest)
		}
		if total > p.maxManifestEntries {
			t.Fatalf("cycle %d: combined manifest entries %d exceeded the configured limit %d", i, total, p.maxManifestEntries)
		}
	}
}

func TestManifestCompactionRestartResumesWithoutSkippingOrDuplicating(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.conf.Max_Pages = 1
	p.conf.Page_Size = 1
	p.maxManifestEntries = 2
	rt.states = map[string][]byte{}

	call := 0
	makeTransport := func() transport {
		return transport(func(*http.Request) (*http.Response, error) {
			body := fmt.Sprintf(`{"data":[{"uuid":"o%d","updated_at":"2026-01-01T00:00:%02dZ"}],"has_more":%v`, call, call, call < 3)
			if call < 3 {
				body += fmt.Sprintf(`,"next_page":"c%d"`, call+1)
			}
			body += `}`
			call++
			return reply(body, 200), nil
		})
	}
	p.http.Transport = makeTransport()

	if _, e := p.Handle(t.Context(), rt); e != nil { // o0
		t.Fatal(e)
	}
	if _, e := p.Handle(t.Context(), rt); e != nil { // o1
		t.Fatal(e)
	}
	if len(rt.entries) != 2 {
		t.Fatalf("expected 2 entries before restart, got %d", len(rt.entries))
	}

	// Simulate a restart mid-traversal: a fresh Plugin sharing only the
	// backing state store.
	restarted, e := New(p.conf, rt)
	if e != nil {
		t.Fatal(e)
	}
	restarted.maxManifestEntries = p.maxManifestEntries
	restarted.now, restarted.wait, restarted.limiter = p.now, p.wait, p.limiter
	restarted.http.Transport = makeTransport()

	if _, e := restarted.Handle(t.Context(), rt); e != nil { // o2
		t.Fatal(e)
	}
	if _, e := restarted.Handle(t.Context(), rt); e != nil { // o3, scan completes
		t.Fatal(e)
	}
	if len(rt.entries) != 4 {
		t.Fatalf("expected all 4 records written across the restart with none skipped or duplicated, got %d", len(rt.entries))
	}

	// A re-poll of the still-cached, unchanged o3 (the manifest bound of 2
	// keeps only the two most recently seen identities: o2 and o3) must not
	// duplicate it.
	before := len(rt.entries)
	restarted.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"uuid":"o3","updated_at":"2026-01-01T00:00:03Z"}],"has_more":false}`, 200), nil
	})
	if _, e := restarted.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != before {
		t.Fatal("re-polling an unchanged, still-cached record duplicated it")
	}
}

func TestManifestCompactionStillEmitsGenuinelyChangedRecords(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.maxManifestEntries = 2
	rt.states = map[string][]byte{}
	body := `{"data":[{"uuid":"o1","updated_at":"2026-01-01T00:00:00Z"},{"uuid":"o2","updated_at":"2026-01-01T00:00:01Z"}],"has_more":false}`
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(body, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 2 {
		t.Fatalf("first scan must write both records, got %d", len(rt.entries))
	}
	// o1's content genuinely changes. Even though the manifest is at its
	// bound and subject to compaction, the change must still be detected
	// and written -- never silently skipped.
	body = `{"data":[{"uuid":"o1","updated_at":"2026-01-01T01:00:00Z"},{"uuid":"o2","updated_at":"2026-01-01T00:00:01Z"}],"has_more":false}`
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 3 {
		t.Fatalf("a genuinely changed record was not emitted (and/or the unchanged sibling was duplicated): entries=%d", len(rt.entries))
	}
	if !bytes.Contains(rt.entries[2].Data, []byte("01:00:00")) {
		t.Fatal("the newly written entry was not the changed o1 record")
	}
}

// --- Preventing oversized discovered identities from stalling children. ---

func TestOversizedDiscoveredParentDoesNotBlockValidSibling(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	hugeID := strings.Repeat("a", maxDiscoveredParameterLen+1)
	memberCalls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/groups"):
			return reply(fmt.Sprintf(`{"data":[{"id":%q},{"id":"g2"}],"has_more":false}`, hugeID), 200), nil
		case strings.Contains(r.URL.Path, "g2"):
			memberCalls++
			return reply(`{"data":[],"has_more":false}`, 200), nil
		}
		t.Fatalf("unexpected request to %s: the oversized identity must never reach a child request", r.URL.Path)
		return nil, nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if memberCalls != 1 {
		t.Fatalf("valid sibling g2 was blocked by the oversized parent: memberCalls=%d", memberCalls)
	}
	workKey := p.conf.key() + "/child-work-v1"
	var list worklist
	if e := json.Unmarshal(rt.states[workKey], &list); e != nil {
		t.Fatal(e)
	}
	if _, ok := list.Items["group-members/group_id:g2"]; !ok {
		t.Fatal("valid sibling's child work is missing")
	}
	for k := range list.Items {
		if strings.Contains(k, hugeID) {
			t.Fatal("the oversized parent produced a persisted work item, which would retry forever")
		}
	}
	if rt.warnings == 0 {
		t.Fatal("expected a warning for the skipped oversized parent")
	}
	for _, line := range rt.warningLog {
		if strings.Contains(line, hugeID) {
			t.Fatal("warning logged the oversized value itself")
		}
	}
}

func TestLegacyOversizedPersistedWorkItemIsRetiredSafely(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	rt.states = map[string][]byte{}
	hugeID := strings.Repeat("b", maxDiscoveredParameterLen+1)
	badParam := "group_id:" + hugeID
	childConf, e := childConfig(p.conf, work{Dataset: "group-members", Parameter: []string{badParam}})
	if e != nil {
		t.Fatal(e)
	}
	list := worklist{Items: map[string]work{
		"group-members/" + badParam: {Dataset: "group-members", Parameter: []string{badParam}, Revision: "rev", Pending: true, StateKey: childConf.key()},
	}}
	b, e := json.Marshal(list)
	if e != nil {
		t.Fatal(e)
	}
	rt.states[p.conf.key()+"/child-work-v1"] = b

	attempts := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[],"has_more":false}`, 200), nil
		}
		attempts++
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})

	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if attempts != 0 {
		t.Fatalf("legacy oversized work item was attempted instead of retired: attempts=%d", attempts)
	}
	var after worklist
	if e := json.Unmarshal(rt.states[p.conf.key()+"/child-work-v1"], &after); e != nil {
		t.Fatal(e)
	}
	if _, ok := after.Items["group-members/"+badParam]; ok {
		t.Fatal("legacy oversized work item was not removed")
	}
	var pruned state
	if e := json.Unmarshal(rt.states[childConf.key()], &pruned); e != nil {
		t.Fatal(e)
	}
	if pruned.Retired != "rev" {
		t.Fatalf("legacy oversized work item was not tombstoned: retired=%q", pruned.Retired)
	}
}

func TestOversizedParentEnumeratedValueWarnsAndOmitsInsteadOfFailing(t *testing.T) {
	p, rt := setup(t, "organization-users")
	huge := strings.Repeat("c", entry.MaxEvDataLength+1)
	p.conf.Parameter = []string{"organization_id:" + huge}
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"u1"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatalf("an oversized _parent value must not fail an otherwise valid entry: %v", e)
	}
	if len(rt.entries) != 1 {
		t.Fatalf("expected the entry to still be written, got %d entries", len(rt.entries))
	}
	if _, ok := rt.entries[0].GetEnumeratedValue("_parent"); ok {
		t.Fatal("oversized _parent should have been omitted, not attached")
	}
	if v, ok := rt.entries[0].GetEnumeratedValue("_vendor"); !ok || v != "Anthropic" {
		t.Fatal("unrelated enumerated values must still be attached")
	}
	if rt.warnings == 0 {
		t.Fatal("expected a warning for the oversized _parent value")
	}
	for _, line := range rt.warningLog {
		if strings.Contains(line, huge) {
			t.Fatal("warning logged the oversized _parent value itself")
		}
	}
}
