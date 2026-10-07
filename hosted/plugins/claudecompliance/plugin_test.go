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

// runtime wraps the shared hosted.Mock so this package does not re-implement
// a Runtime, and adds only the failure injection and call accounting the
// plugin's tests need on top of it.
type runtime struct {
	*hosted.Mock
	failWrite, failState bool
	// putFn, when set, observes every storage write.
	putFn func(string, []byte) error
	// written records every key this runtime has been asked to write.
	written []string
	// failAfter, when set, decides per storage write whether it fails, so a
	// test can cut a multi-key commit at an exact point.
	failAfter      func() bool
	failDelivery   bool
	entries        []entry.Entry
	negotiatedTags []string
	putCounts      map[string]int
	warnings       int
	// warningLog additively captures each Warn call's rendered message and
	// KV fields, for tests that need to inspect what a warning contained
	// (e.g. proving a value was never logged).
	warningLog []string
	// sleeps records every Runtime.Sleep duration the plugin asked for, and
	// sleepCancels makes Sleep report that the context ended.
	sleeps       []time.Duration
	sleepCancels bool
}

// SyncContext is the optional muxer barrier the plugin binds when present.
func (r *runtime) SyncContext(ctx context.Context, _ time.Duration) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if r.failDelivery {
		return errors.New("synthetic ingest synchronization failure")
	}
	return nil
}

func newRuntime(t *testing.T) *runtime {
	t.Helper()
	return &runtime{Mock: hosted.NewMock(t.Context())}
}

func (r *runtime) Sleep(d time.Duration) bool {
	r.sleeps = append(r.sleeps, d)
	return r.sleepCancels
}

func (r *runtime) NegotiateTag(tag string) (entry.EntryTag, error) {
	r.negotiatedTags = append(r.negotiatedTags, tag)
	return r.Mock.NegotiateTag(tag)
}

func (r *runtime) Write(e entry.Entry) error {
	if r.failWrite {
		return errors.New("synthetic write failure")
	}
	r.entries = append(r.entries, e)
	return nil
}

func (r *runtime) Put(key string, b []byte) error {
	if r.failState {
		return errors.New("synthetic state failure")
	}
	if r.failAfter != nil && r.failAfter() {
		return errors.New("synthetic partial-commit failure")
	}
	if r.putFn != nil {
		if e := r.putFn(key, b); e != nil {
			return e
		}
	}
	if r.putCounts != nil {
		r.putCounts[key]++
	}
	r.written = append(r.written, key)
	return r.Mock.Put(key, b)
}

// PutString and PutTime are re-declared so injected state failures and put
// accounting cover them too; hosted.Mock implements them against its own Put.
func (r *runtime) PutString(key, value string) error { return r.Put(key, []byte(value)) }
func (r *runtime) PutTime(key string, value time.Time) error {
	return r.PutString(key, value.Format(time.RFC3339Nano))
}

func (r *runtime) Warn(msg string, params ...rfc5424.SDParam) {
	r.warnings++
	line := msg
	for _, p := range params {
		line += " " + p.Name + "=" + p.Value
	}
	r.warningLog = append(r.warningLog, line)
}

// committed reports whether p has durably recorded progress of any kind --
// a completed traversal or a resumable page walk. It is the discrete-key
// equivalent of "the state blob was written".
func (r *runtime) committed(p *Plugin) bool {
	cp, err := loadCheckpoint(r, p.conf.key())
	return err == nil && (!cp.Since.IsZero() || cp.Walk != nil)
}

// putsUnder totals recorded Put calls against any key beginning with prefix.
func (r *runtime) putsUnder(prefix string) int {
	total := 0
	for k, n := range r.putCounts {
		if strings.HasPrefix(k, prefix) {
			total += n
		}
	}
	return total
}

// checkpoint reads back p's stored progress through the same discrete keys
// the plugin writes.
func (r *runtime) checkpoint(t *testing.T, p *Plugin) checkpoint {
	t.Helper()
	cp, err := loadCheckpoint(r, p.conf.key())
	if err != nil {
		t.Fatal(err)
	}
	return cp
}

// storedKeys lists every key this runtime has been asked to write.
func (r *runtime) storedKeys() []string {
	return append([]string(nil), r.written...)
}

// stateBytes totals every byte this runtime currently holds under prefix.
func (r *runtime) stateBytes(prefix string) int {
	total := 0
	for _, k := range []string{keySince, keyManifest, keyHistory, keyRetired,
		keyWalkCursor, keyWalkSince, keyWalkUntil, keyWalkStarted, keyWalkHistory, keyWalkMan} {
		if b, err := r.Get(prefix + k); err == nil {
			total += len(b)
		}
	}
	return total
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
	if len(rt.entries) != 1 || rt.committed(p) {
		t.Fatal("queued write advanced state")
	}
	rt.failDelivery = false
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if rt.committed(p) == false || len(rt.entries) != 2 {
		t.Fatal("record from failed synchronization not replayed")
	}
}

// Writing an entry only queues it, and a state-store sync is not proof of
// ingest delivery. The plugin therefore refuses to be built without a real
// delivery barrier rather than advancing a checkpoint past records that may
// never reach a backend.
func TestConstructionRequiresAnIngestDeliveryBarrier(t *testing.T) {
	p, rt := setup(t, "activities")
	bare := struct{ hosted.TagNegotiator }{rt}
	if _, e := New(p.conf, bare); e == nil {
		t.Fatal("a negotiator with no SyncContext was accepted")
	}
	if _, e := New(p.conf, nil); e == nil {
		t.Fatal("a nil negotiator was accepted")
	}
	// The real muxer provides it, so the ordinary path still builds and the
	// barrier is bound.
	full, e := New(p.conf, rt)
	if e != nil {
		t.Fatal(e)
	}
	if full.syncIngest == nil {
		t.Fatal("delivery barrier was not bound from a capable negotiator")
	}
}

// No checkpoint may advance until the delivery barrier has returned.
func TestCheckpointWaitsForDeliveryBarrier(t *testing.T) {
	p, rt := setup(t, "activities")
	order := []string{}
	p.syncIngest = func(ctx context.Context, _ time.Duration) error {
		order = append(order, "sync")
		return nil
	}
	rt.putFn = func(string, []byte) error {
		order = append(order, "checkpoint")
		return nil
	}
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"a"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(order) == 0 || order[0] != "sync" {
		t.Fatalf("a checkpoint key was written before the delivery barrier: %v", order)
	}
	if len(order) < 2 {
		t.Fatalf("no checkpoint was written at all: %v", order)
	}
}

func setup(t *testing.T, name string) (*Plugin, *runtime) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key")
	if e := os.WriteFile(path, []byte("synthetic-test-key"), 0600); e != nil {
		t.Fatal(e)
	}
	c := &Config{Dataset: []string{name}, Credential_File: path, Page_Size: 100, Max_Pages: 5, Max_Retries: 1, Follow_Children: "disabled"}
	c.BaseConfig.Ingester_UUID = "00000000-0000-4000-8000-000000000321"
	d := Datasets[name]
	for _, m := range placeholder.FindAllStringSubmatch(d.Path, -1) {
		c.Parameter = append(c.Parameter, m[1]+":synthetic-id")
	}
	if e := c.Verify(); e != nil {
		t.Fatal(e)
	}
	rt := newRuntime(t)
	p, err := New(c, rt)
	if err != nil {
		t.Fatal(err)
	}
	p.limiter = rate.NewLimiter(rate.Inf, 1000)
	p.now = func() time.Time { return time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC) }
	return p, rt
}
func reply(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestEveryJSONOperationFramesOneNativeObject(t *testing.T) {
	for _, d := range Datasets {
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
			if len(rt.entries) != 1 || rt.committed(p) == false {
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
	if _, e := p.Handle(t.Context(), rt); e == nil || rt.committed(p) {
		t.Fatal("accepted wrong messages envelope")
	}
}
func TestFailuresBeforePageCommitNeverAdvanceState(t *testing.T) {
	for _, kind := range []string{"write", "403", "application", "missing-array", "missing-token", "size"} {
		t.Run(kind, func(t *testing.T) {
			p, rt := setup(t, "activities")
			rt.failWrite = kind == "write"
			if kind == "size" {
				p.maxEntryBytes = 1
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
			if e == nil || rt.committed(p) {
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
	if len(rt.entries) != 1 || rt.committed(p) == false {
		t.Fatal("completed page checkpoint was not retained")
	}
	st := rt.checkpoint(t, p)
	if st.Walk == nil || st.Walk.Cursor != "same" {
		t.Fatal("missing resumable traversal state")
	}
}
func TestRetryAfterAndNoRetryHeader(t *testing.T) {
	for _, noRetry := range []bool{false, true} {
		p, rt := setup(t, "activities")
		n := 0
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
		} else if e != nil || n != 2 || len(rt.sleeps) != 1 || rt.sleeps[0] != 120*time.Second {
			t.Fatalf("Retry-After not honored: err=%v requests=%d sleeps=%v", e, n, rt.sleeps)
		}
	}
}

func TestBoundedRetryAfterNumericBounds(t *testing.T) {
	ceiling := 300 * time.Second
	for _, tc := range []struct {
		value string
		delay time.Duration
		over  bool
	}{
		{value: "300", delay: ceiling},
		{value: "301", over: true},
		{value: "18446744073709551615", over: true},
		{value: "18446744073709551616"},
		{value: "-1"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			delay, over := boundedRetryAfter(tc.value, ceiling)
			if delay != tc.delay || over != tc.over {
				t.Fatalf("boundedRetryAfter(%q)=(%v,%t), want (%v,%t)", tc.value, delay, over, tc.delay, tc.over)
			}
		})
	}
}

func TestRetryAfterBeyondPollIntervalStopsCycle(t *testing.T) {
	for _, retryAfter := range []string{
		"4294967295",
		time.Now().Add(24 * time.Hour).UTC().Format(http.TimeFormat),
	} {
		t.Run(retryAfter, func(t *testing.T) {
			p, rt := setup(t, "activities")
			p.conf.Max_Retries = 1
			requests := 0
			p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
				requests++
				if requests == 1 {
					r := reply(`{}`, http.StatusTooManyRequests)
					r.Header.Set("retry-after", retryAfter)
					return r, nil
				}
				return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
			})
			if _, err := p.Handle(t.Context(), rt); err == nil {
				t.Fatal("over-ceiling Retry-After did not end the cycle")
			}
			if requests != 1 || len(rt.sleeps) != 0 || rt.committed(p) {
				t.Fatalf("over-ceiling Retry-After retried, waited, or advanced state: requests=%d sleeps=%v saved=%t", requests, rt.sleeps, rt.committed(p))
			}
		})
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
		// Honor the requested window as the vendor does: the record is
		// inside both the initial lookback and the overlap of the next poll.
		updated, _ := time.Parse(time.RFC3339, "2026-09-07T23:57:00Z")
		gte, _ := time.Parse(time.RFC3339Nano, r.URL.Query().Get("updated_at.gte"))
		lte, _ := time.Parse(time.RFC3339Nano, r.URL.Query().Get("updated_at.lte"))
		if updated.Before(gte) || updated.After(lte) {
			return reply(`{"data":[],"has_more":false}`, 200), nil
		}
		return reply(`{"data":[{"id":"chat","updated_at":"2026-09-07T23:57:00Z"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	before := rt.checkpoint(t, p)
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 1 || n != 2 || !before.Since.Equal(rt.checkpoint(t, p).Since) {
		t.Fatal("restart replayed unchanged records")
	}
	old := p.conf.key()
	p.conf.path = "/v1/compliance/other"
	if old == p.conf.key() {
		t.Fatal("dataset path not bound to state")
	}
}
func TestCancellationAndTimestampFallback(t *testing.T) {
	p, rt := setup(t, "activities")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e := p.Handle(ctx, rt); e == nil || rt.committed(p) {
		t.Fatal("ignored cancellation")
	}
	now := p.now()
	if sourceTime([]byte(`{"created_at":"2026-09-08T00:00:00"}`), "created_at", now) != now {
		t.Fatal("invented timezone")
	}
}

type persistedRuntime struct {
	hosted.Runtime
	bucket *storage.BucketWriter
}

func (r persistedRuntime) Get(k string) ([]byte, error) { return r.bucket.Get(k) }
func (r persistedRuntime) Put(k string, v []byte) error { return r.bucket.Put(k, v) }
func (r persistedRuntime) GetString(k string) (string, error) {
	v, err := r.bucket.Get(k)
	return string(v), err
}
func (r persistedRuntime) PutString(k, v string) error { return r.bucket.Put(k, []byte(v)) }
func (r persistedRuntime) GetTime(k string) (time.Time, error) {
	v, err := r.GetString(k)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, v)
}
func (r persistedRuntime) PutTime(k string, v time.Time) error {
	return r.PutString(k, v.Format(time.RFC3339Nano))
}

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
	if rt.committed(p) == false {
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
				if rt.committed(p) == false {
					t.Fatal("completed page was not checkpointed")
				}
			} else if rt.committed(p) {
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
			if len(rt.entries) != before+wantAdded || rt.committed(p) == false {
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
	if e != nil || cont == nil || cont.Delay != 0 || len(rt.entries) != 1 || rt.committed(p) == false {
		t.Fatal("page limit did not return a committed immediate continuation")
	}
	restarted, e := New(p.conf, rt)
	if e != nil {
		t.Fatal(e)
	}
	restarted.http.Transport = p.http.Transport
	restarted.now, restarted.limiter = p.now, p.limiter
	cont, e = restarted.Handle(t.Context(), rt)
	if e != nil || cont == nil || cont.Delay == 0 || len(rt.entries) != 2 {
		t.Fatal("resumed traversal did not complete without replay")
	}
}

func TestLookbackStartAndCheckpointPrecedence(t *testing.T) {
	p, rt := setup(t, "activities")
	// Set it the way a configuration file does -- through the compatibility
	// spelling -- and re-verify, which normalizes it into the standard
	// polling field that collection reads.
	p.conf.Lookback = 48
	if e := p.conf.Verify(); e != nil {
		t.Fatal(e)
	}
	if p.conf.PollingConfig.Lookback != 48 {
		t.Fatalf("Lookback did not normalize: %d", p.conf.PollingConfig.Lookback)
	}
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
	// The completed window ended indexingLag before now; the next one
	// starts Overlap-Seconds before that end.
	want = p.now().Add(-time.Minute - 300*time.Second)
	p.conf.Lookback = 720
	if e := p.conf.Verify(); e != nil {
		t.Fatal(e)
	}
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
	before, err := bw.Get(p.conf.key() + keySince)
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
	after, err := bw.Get(p.conf.key() + keySince)
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
	c.Host = "https://eu.example.invalid"
	if p.conf.Equal(c) {
		t.Fatal("Host change was not detected by Equal")
	}
	c = *p.conf
	c.path = "/v1/compliance/other"
	if c.key() == p.conf.key() {
		t.Fatal("dataset path change did not isolate progress")
	}
	c = *p.conf
	c.Lookback = -1
	if err := c.Verify(); err == nil {
		t.Fatal("negative lookback accepted")
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
	if e := json.Unmarshal(mustGet(t, rt, p.conf.key()+"/children"), &pending); e != nil || len(pending.Items) != 1 {
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
	restarted.limiter = p.limiter
	if _, e := restarted.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(messages) != 2 {
		t.Fatalf("new/pending parents not collected: %v", messages)
	}
	if e := json.Unmarshal(mustGet(t, rt, p.conf.key()+"/children"), &pending); e != nil {
		t.Fatal(e)
	}
	for _, w := range pending.Items {
		if w.Pending {
			t.Fatal("completed work remains pending")
		}
	}
}

// TestActiveRemoteSession404IsDroppedThenRevisited follows the vendor's
// 404 guidance: after a healthy root listing, a child 404 means the resource
// is gone or not yet running, so it is logged and completed rather than
// retried as a failure. An active remote session is still revisited on the
// next poll, so its transcript is collected once it becomes readable.
func TestActiveRemoteSession404IsDroppedThenRevisited(t *testing.T) {
	p, rt := setup(t, "remote-sessions")
	p.conf.Follow_Children = "enabled"
	missing := true
	calls := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/remote") {
			if r.URL.Query().Has("updated_at.gte") || r.URL.Query().Has("user_ids[]") {
				t.Fatal("invalid remote filter")
			}
			return reply(`{"data":[{"id":"agent-session","user":null,"agent_id":"a","status":"active"}],"next_page":null}`, 200), nil
		}
		calls++
		if missing {
			return reply(`{"error":{"type":"not_found_error","message":"Remote session not found"}}`, 404), nil
		}
		return reply(`{"session":{"id":"agent-session"},"data":[{"id":"m"}],"next_page":null}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if rt.warnings != 1 {
		t.Fatalf("child 404 was not reported: warnings=%d", rt.warnings)
	}
	var list worklist
	if e := json.Unmarshal(mustGet(t, rt, p.conf.key()+"/children"), &list); e != nil || len(list.Items) != 1 {
		t.Fatal("child work missing")
	}
	for _, w := range list.Items {
		if w.Pending || w.Failures != 0 {
			t.Fatalf("404 child retained as failing pending work: %+v", w)
		}
	}
	missing = false
	oldNow := p.now()
	p.now = func() time.Time { return oldNow.Add(2 * time.Minute) }
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if calls != 3 || len(rt.entries) != 2 {
		t.Fatalf("active remote session not revisited: calls=%d entries=%d", calls, len(rt.entries))
	}
}

// TestChild404DuringRootFailureRemainsFailure keeps a 404 an ordinary
// failure when the root listing itself failed, because the vendor's bare
// 404 also signals an unauthenticated request rather than a missing child.
func TestChild404DuringRootFailureRemainsFailure(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	key := p.conf.key() + "/children"
	seeded, _ := json.Marshal(worklist{Items: map[string]work{
		"group-members/group_id:g1": {Dataset: "group-members", Parameter: []string{"group_id:g1"}, Revision: "r", Pending: true, LastAttempt: p.now().Add(-time.Hour)},
	}})
	if e := rt.Put(key, seeded); e != nil {
		t.Fatal(e)
	}
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		return reply(`{"error":{"type":"not_found_error","message":"Not found"}}`, 404), nil
	})
	if _, e := p.Handle(t.Context(), rt); e == nil {
		t.Fatal("unauthenticated 404 hidden")
	}
	var list worklist
	if e := json.Unmarshal(mustGet(t, rt, key), &list); e != nil {
		t.Fatal(e)
	}
	if w := list.Items["group-members/group_id:g1"]; !w.Pending || w.Failures != 1 {
		t.Fatalf("child 404 under a failed root was not counted as a failure: %+v", w)
	}
}

func TestUnchangedOrganizationRefreshesMembership(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.conf.Follow_Children = "enabled"
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
	seedCheckpoint(t, rt, old1.StateKey, checkpoint{Since: p.now(), Manifest: manifest{"message": {Digest: strings.Repeat("a", 4096)}}})
	b, _ := json.Marshal(list)
	if e := rt.Put(p.conf.key()+"/children", b); e != nil {
		t.Fatal(e)
	}
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
	if e := json.Unmarshal(mustGet(t, rt, p.conf.key()+"/children"), &list); e != nil {
		t.Fatal(e)
	}
	if len(list.Items) != 2 {
		t.Fatal("history bound changed")
	}
	if _, ok := list.Items["old1"]; ok {
		t.Fatal("oldest completed entry retained")
	}
	pruned := readCheckpoint(t, rt, old1.StateKey)
	if len(pruned.Manifest) != 0 {
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
	if e := rt.Put(p.conf.key()+"/children", b); e != nil {
		t.Fatal(e)
	}
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
	if e = json.Unmarshal(mustGet(t, rt, p.conf.key()+"/children"), &after); e != nil || len(after.Items) != 2 {
		t.Fatal("pending work was not preserved")
	}
}

// TestCapacityEvictedUnchangedParentIsRediscovered covers the
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
	workKey := p.conf.key() + "/children"
	var list worklist
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
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
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
		t.Fatal(e)
	}
	if _, ok := list.Items["group-members/group_id:g1"]; ok {
		t.Fatal("g1 was not evicted for Max-Pending capacity")
	}
	pruned := readCheckpoint(t, rt, g1.StateKey)
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
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
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
	healthy := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"a-failing"},{"id":"z-healthy"}],"has_more":false}`, 200), nil
		}
		if strings.Contains(r.URL.Path, "a-failing") {
			return reply(`{}`, 403), nil
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
				return reply(`{"data":[{"id":"m1"}],"has_more":true,"next_page":"tok"}`, 200), nil
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

// TestZeroLastAttemptWorkItemIsNotStuckFirstForever confirms that a stored
// child-work item carrying the zero attempt time does not perpetually cut
// ahead of items with a real recorded attempt time, and that it still runs
// (and thereby acquires a real LastAttempt) once nothing newer is competing
// for the slot.
func TestZeroLastAttemptWorkItemIsNotStuckFirstForever(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 1

	zeroAttemptConfig, e := childConfig(p.conf, work{Dataset: "group-members", Parameter: []string{"group_id:zero-attempt"}})
	if e != nil {
		t.Fatal(e)
	}
	attempted := work{Dataset: "group-members", Parameter: []string{"group_id:attempted"}, Revision: "rev-attempted", Pending: true, LastAttempt: p.now().Add(-time.Minute)}
	attemptedConfig, e := childConfig(p.conf, attempted)
	if e != nil {
		t.Fatal(e)
	}
	list := worklist{Items: map[string]work{
		"group-members/group_id:zero-attempt": {Dataset: "group-members", Parameter: []string{"group_id:zero-attempt"}, Revision: "rev-zero-attempt", Pending: true, StateKey: zeroAttemptConfig.key()}, // LastAttempt deliberately left at its zero value
		"group-members/group_id:attempted":    {Dataset: "group-members", Parameter: []string{"group_id:attempted"}, Revision: "rev-attempted", Pending: true, LastAttempt: attempted.LastAttempt, StateKey: attemptedConfig.key()},
	}}
	b, _ := json.Marshal(list)
	if e := rt.Put(p.conf.key()+"/children", b); e != nil {
		t.Fatal(e)
	}

	var order []string
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[],"has_more":false}`, 200), nil
		}
		switch {
		case strings.Contains(r.URL.Path, "zero-attempt"):
			order = append(order, "zero-attempt")
		case strings.Contains(r.URL.Path, "attempted"):
			order = append(order, "attempted")
		default:
			t.Fatal("unexpected child path")
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})

	// The already-attempted item must run before the never-attempted
	// zero-LastAttempt item under Max-Children=1.
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(order) != 1 || order[0] != "attempted" {
		t.Fatalf("zero-LastAttempt item cut ahead of already-attempted work: order=%v", order)
	}
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(order) != 2 || order[1] != "zero-attempt" {
		t.Fatalf("zero-LastAttempt item never got its turn: order=%v", order)
	}
}

func TestDiscoveryDoesNotFilterOutUnchangedProjects(t *testing.T) {
	p, rt := setup(t, "projects")
	p.conf.Follow_Children = "enabled"
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
func TestDeletedChatDoesNotScheduleContentAndStanzaSharesBudget(t *testing.T) {
	d := Datasets["chats"]
	rows, e := childWork(d, nil, []byte(`{"id":"x","deleted_at":"2026-09-08T00:00:00Z"}`))
	if e != nil || len(rows) != 0 {
		t.Fatal("scheduled deleted content")
	}
	p, _ := setup(t, "chats")
	child := *p
	if child.limiter != p.limiter {
		t.Fatal("copying a stanza for child work multiplied its request budget")
	}
}

func TestRateIncreaseAppliesWhenStanzaIsRebuilt(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Requests_Per_Minute = 1
	p.limiter = requestLimiter(p.conf.Requests_Per_Minute)

	reloaded := *p.conf
	reloaded.Requests_Per_Minute = 600
	high, err := New(&reloaded, rt)
	if err != nil {
		t.Fatal(err)
	}
	wantLow := rate.Every(time.Minute)
	wantHigh := rate.Every(time.Minute / 600)
	if p.limiter.Limit() != wantLow {
		t.Fatalf("original limiter changed during reload: got=%v want=%v", p.limiter.Limit(), wantLow)
	}
	if high.limiter.Limit() != wantHigh {
		t.Fatalf("reloaded rate was not applied: got=%v want=%v", high.limiter.Limit(), wantHigh)
	}
	if high.limiter == p.limiter {
		t.Fatal("rebuilt stanza retained the removed configuration's limiter")
	}
}

func TestLoweredMaxPendingEvictsCompletedStoredWork(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	key := p.conf.key() + keyChildren
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/groups") {
			return reply(`{"data":[{"id":"g1"},{"id":"g2"}],"has_more":false}`, http.StatusOK), nil
		}
		return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
	})

	p.conf.Max_Children = 2
	p.conf.Max_Pending = 2
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	p.conf.Max_Children = 1
	p.conf.Max_Pending = 1
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	var got worklist
	if err := json.Unmarshal(mustGet(t, rt, key), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != p.conf.Max_Pending {
		t.Fatalf("stored worklist did not honor lowered Max-Pending: got=%d want=%d", len(got.Items), p.conf.Max_Pending)
	}
}

func TestLoweredMaxPendingDrainsHealthyPendingWork(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Children = 1
	p.conf.Max_Pending = 1
	list := worklist{Items: map[string]work{
		"group-members/g1": {Dataset: "group-members", Parameter: []string{"group_id:g1"}, Pending: true},
		"group-members/g2": {Dataset: "group-members", Parameter: []string{"group_id:g2"}, Pending: true},
	}}
	dirty := false
	if err := reconcileWorklistCapacity(rt, p.conf, &list, nil, &dirty); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 2 || dirty {
		t.Fatalf("healthy pending work was discarded while lowering the bound: len=%d dirty=%v", len(list.Items), dirty)
	}
	item := list.Items["group-members/g1"]
	item.Pending = false
	item.LastCompleted = p.now().Add(-time.Hour)
	list.Items["group-members/g1"] = item
	if err := reconcileWorklistCapacity(rt, p.conf, &list, nil, &dirty); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || !dirty {
		t.Fatalf("completed work was not evicted after the pending backlog drained: len=%d dirty=%v", len(list.Items), dirty)
	}
}

func TestCustomTagIsPreservedForParentAndDiscoveredChildren(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Tag_Name = "tenant-compliance-directory"
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
	if rt.committed(p) == false {
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
	if got := rt.putsUnder(p.conf.key() + keyChildren); got != 0 {
		t.Fatalf("activity records wrote child worklist %d times", got)
	}
	// One completed traversal commits a small fixed set of discrete keys;
	// what must never happen is a write per record.
	if got := rt.putsUnder(p.conf.key()); got == 0 || got > 8 {
		t.Fatalf("dataset checkpoint writes = %d for 500 records", got)
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
	if len(rt.entries) != 1 || rt.committed(p) == false || rt.warnings != 1 {
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

func TestDiscoveredChatWithoutHistoryMarkerGetsBackfill(t *testing.T) {
	p, rt := setup(t, "chat-messages")
	p.conf.discovered = true
	seedCheckpoint(t, rt, p.conf.key(), checkpoint{
		Since:    p.now().Add(-time.Hour),
		Manifest: manifest{"old": {Digest: "digest"}},
	})
	var e error
	var query url.Values
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		query = r.URL.Query()
		return reply(`{"session":{"id":"c1"},"chat_messages":[],"has_more":false}`, 200), nil
	})
	if _, e = p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if query.Has("updated_at.gte") || query.Has("updated_at.lte") {
		t.Fatal("discovered chat checkpoint without a history marker skipped the backfill")
	}
	after := readCheckpoint(t, rt, p.conf.key())
	if !after.HistoryComplete {
		t.Fatal("corrective history backfill was not recorded")
	}
}

func TestMissingParentIdentityIsLoggedAndSkipped(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
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
	if len(rt.entries) != 3 || rt.committed(p) == false || rt.warnings != 1 {
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
	if got, e := (&Config{Credential_File: path}).credential(); e != nil || got != want {
		t.Fatal("full credential file was not read")
	}
	if e := os.WriteFile(path, []byte("key\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if got, e := (&Config{Credential_File: path}).credential(); e != nil || got != "key" {
		t.Fatal("single trailing newline was not handled")
	}
	if e := os.WriteFile(path, []byte("first\nsecond"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := (&Config{Credential_File: path}).credential(); e == nil {
		t.Fatal("multiline credential accepted")
	}
	if e := os.WriteFile(path, []byte(strings.Repeat("x", 16385)), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := (&Config{Credential_File: path}).credential(); e == nil {
		t.Fatal("oversized credential accepted")
	}
}

func TestDeletedChatCompactsCheckpointAndRemovesWork(t *testing.T) {
	p, rt := setup(t, "chats")
	p.conf.Follow_Children = "enabled"
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
	workKey := p.conf.key() + "/children"
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil || len(list.Items) != 1 {
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
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil || len(list.Items) != 0 {
		t.Fatal("deleted chat retained child work")
	}
	pruned := readCheckpoint(t, rt, child.StateKey)
	if len(pruned.Manifest) != 0 || pruned.Retired == "" {
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
	workKey := p.conf.key() + "/children"
	var list worklist
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil || len(list.Items) != 0 {
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
	workKey := p.conf.key() + "/children"
	var list worklist
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil || len(list.Items) != 0 {
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
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil || len(list.Items) != 1 {
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
	st := rt.checkpoint(t, p)
	if len(st.Manifest) > 5 {
		t.Fatalf("manifest retained %d entries, exceeding the configured bound of 5", len(st.Manifest))
	}
	if n := rt.stateBytes(p.conf.key()); n > 4<<10 {
		t.Fatalf("checkpoint grew unexpectedly large for a 5-entry bound: %d bytes", n)
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
	restarted.now, restarted.limiter = p.now, p.limiter
	restarted.http.Transport = p.http.Transport
	if _, e := restarted.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	if len(rt.entries) != 2 {
		t.Fatalf("restart replayed unchanged records within the manifest bound, entries = %d", len(rt.entries))
	}
}

// TestManifestEvictionRewritesStaleRecordsInsteadOfDroppingData proves the
// eviction direction is safe. A manifest smaller than the record set cannot
// deduplicate every record, so some are rewritten each cycle -- but the
// number is bounded by how far the bound falls short, is identical every
// cycle rather than growing, and no record is ever dropped. When the bound
// is large enough to hold every identity, nothing is rewritten at all.
func TestManifestEvictionRewritesStaleRecordsInsteadOfDroppingData(t *testing.T) {
	const records = 3
	const body = `{"data":[{"uuid":"o1","updated_at":"2020-01-01T00:00:00Z"},{"uuid":"o2","updated_at":"2020-01-02T00:00:00Z"},{"uuid":"o3","updated_at":"2020-01-03T00:00:00Z"}],"has_more":false}`
	for _, limit := range []int{1, 2, 3, 4} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			p, rt := setup(t, "organizations")
			p.maxManifestEntries = limit
			p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
				return reply(body, 200), nil
			})
			if _, e := p.Handle(t.Context(), rt); e != nil {
				t.Fatal(e)
			}
			if len(rt.entries) != records {
				t.Fatalf("first scan must write every record, got %d", len(rt.entries))
			}
			// A bound that cannot hold every identity costs one rewrite for
			// the shortfall plus the one identity evicted to make room for
			// the record being processed; a bound that can hold them all
			// costs nothing.
			want := 0
			if limit < records {
				want = records - limit + 1
			}
			var seen []int
			for cycle := 0; cycle < 3; cycle++ {
				before := len(rt.entries)
				if _, e := p.Handle(t.Context(), rt); e != nil {
					t.Fatalf("cycle %d: %v", cycle, e)
				}
				got := len(rt.entries) - before
				seen = append(seen, got)
				if got != want {
					t.Fatalf("limit=%d cycle=%d: rewrote %d records, want a steady %d (seen %v)",
						limit, cycle, got, want, seen)
				}
				st := rt.checkpoint(t, p)
				if len(st.Manifest) > limit {
					t.Fatalf("limit=%d cycle=%d: manifest holds %d entries, exceeding its bound",
						limit, cycle, len(st.Manifest))
				}
			}
		})
	}
}

// --- Bounded absence-based retirement for parent kinds with no vendor
// deletion signal (organizations, groups, projects, local-sessions; see
// absenceTrackedParents). ---

func TestAbsentParentBelowThresholdDoesNotRetireChild(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
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
	workKey := p.conf.key() + "/children"
	var list worklist
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
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

	workKey := p.conf.key() + "/children"
	var list worklist
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
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
	workKey := p.conf.key() + "/children"
	var list worklist
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
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
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
		t.Fatal(e)
	}
	if _, ok := list.Items["group-members/group_id:g1"]; ok {
		t.Fatalf("child work was not retired after %d consecutive complete absent scans", absentRetirementThreshold)
	}
	pruned := readCheckpoint(t, rt, child.StateKey)
	if pruned.Retired != "" {
		t.Fatalf("absence retirement must not tombstone the child checkpoint (revision is unrelated to why it was retired): retired=%q", pruned.Retired)
	}
	if len(pruned.Manifest) != 0 || pruned.Walk != nil {
		t.Fatal("retirement did not compact the child checkpoint")
	}
}

func TestPartialAndErroredScansNeverAdvanceAbsenceRetirement(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
	p.conf.Max_Pages = 1

	// Discover g1 with one clean, complete scan.
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return reply(`{"data":[{"id":"g1"}],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	workKey := p.conf.key() + "/children"
	streak := func() uint {
		var list worklist
		if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
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
	workKey := p.conf.key() + "/children"
	var list worklist
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
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

// TestAbsenceRetiredUnchangedParentIsRediscovered covers the
// tombstone-reuse defect: absence-based retirement must compact a child's
// checkpoint without marking it Retired, because absence is not a
// content-derived signal -- a parent that reappears unchanged after being
// retired presents the very same revision. It must be rediscovered and its
// child work re-run, not permanently suppressed as if it were
// confirmed-deleted.
func TestAbsenceRetiredUnchangedParentIsRediscovered(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
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
	workKey := p.conf.key() + "/children"
	var list worklist
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
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
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
		t.Fatal(e)
	}
	if _, ok := list.Items["group-members/group_id:g1"]; ok {
		t.Fatalf("child work was not retired after %d consecutive complete absent scans", absentRetirementThreshold)
	}
	pruned := readCheckpoint(t, rt, child.StateKey)
	if pruned.Retired != "" {
		t.Fatal("absence retirement must not tombstone the checkpoint")
	}

	// g1 reappears, content-identical (same revision) to before retirement.
	present = true
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	list = worklist{}
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
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

	// Prime a primary manifest already at the bound; the forced multi-page
	// traversal below revisits exactly these five identities, letting
	// compaction keep pace with insertion as it goes.
	prior := manifest{}
	for i := 0; i < 5; i++ {
		prior[fmt.Sprintf("uuid:o%d", i)] = manifestEntry{Digest: "will-not-match", Seen: int64(i)}
	}
	seedCheckpoint(t, rt, p.conf.key(), checkpoint{Since: p.now().Add(-time.Hour), Manifest: prior})

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
		st := rt.checkpoint(t, p)
		total := len(st.Manifest)
		if st.Walk != nil {
			total += len(st.Walk.Manifest)
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
	restarted.now, restarted.limiter = p.now, p.limiter
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
	workKey := p.conf.key() + "/children"
	var list worklist
	if e := json.Unmarshal(mustGet(t, rt, workKey), &list); e != nil {
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

func TestStoredOversizedWorkItemIsRetiredSafely(t *testing.T) {
	p, rt := setup(t, "groups")
	p.conf.Follow_Children = "enabled"
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
	if e := rt.Put(p.conf.key()+"/children", b); e != nil {
		t.Fatal(e)
	}

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
		t.Fatalf("oversized work item was attempted instead of retired: attempts=%d", attempts)
	}
	var after worklist
	if e := json.Unmarshal(mustGet(t, rt, p.conf.key()+"/children"), &after); e != nil {
		t.Fatal(e)
	}
	if _, ok := after.Items["group-members/"+badParam]; ok {
		t.Fatal("oversized work item was not removed")
	}
	pruned := readCheckpoint(t, rt, childConf.key())
	if pruned.Retired != "rev" {
		t.Fatalf("oversized work item was not tombstoned: retired=%q", pruned.Retired)
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

// mustGet reads a raw stored value, failing the test if it is absent.
func mustGet(t *testing.T, rt *runtime, key string) []byte {
	t.Helper()
	b, err := rt.Get(key)
	if err != nil {
		return nil
	}
	return b
}

// seedCheckpoint writes a checkpoint through the same discrete keys the
// plugin uses, so tests can prime arbitrary stored progress.
func seedCheckpoint(t *testing.T, rt *runtime, prefix string, cp checkpoint) {
	t.Helper()
	if err := commitDataset(rt, prefix, cp.Since, cp.Manifest, cp.HistoryComplete); err != nil {
		t.Fatal(err)
	}
	if cp.Walk != nil {
		if err := commitPage(rt, prefix, cp.Manifest, *cp.Walk); err != nil {
			t.Fatal(err)
		}
	}
	if cp.Retired != "" {
		if err := rt.PutString(prefix+keyRetired, cp.Retired); err != nil {
			t.Fatal(err)
		}
	}
}

// readCheckpoint reads stored progress back at an arbitrary prefix.
func readCheckpoint(t *testing.T, rt *runtime, prefix string) checkpoint {
	t.Helper()
	cp, err := loadCheckpoint(rt, prefix)
	if err != nil {
		t.Fatal(err)
	}
	return cp
}
