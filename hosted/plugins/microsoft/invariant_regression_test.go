package microsoft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const auditUUID = "550e8400-e29b-41d4-a716-446655440000"

func auditTemporal(d Dataset) bool {
	return d.Mode != ModeSnapshot && (d.Kind == KindAzureActivity || d.Kind == KindAdvancedHunting || d.Kind == KindFabricActivity || d.Service == ServiceGraph && d.TimeField != "")
}

func auditServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		handler(w, r)
	}))
	prior := http.DefaultTransport
	http.DefaultTransport = s.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = prior; s.Close() })
	return s
}

func auditRuntime(t *testing.T, path string) (*persistentTestRuntime, func()) {
	t.Helper()
	db, e := storage.OpenBoltHandler(path, true)
	if e != nil {
		t.Fatal(e)
	}
	b, e := db.GetBucketWriter(auditUUID)
	if e != nil {
		t.Fatal(e)
	}
	r := &persistentTestRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(), bucket: b}
	r.stopOnSleep = true
	return r, func() {
		if e := db.Close(); e != nil {
			t.Fatal(e)
		}
	}
}

func auditConfig(t *testing.T, host, name string) *Config {
	c := testClientConfig(t, host)
	c.Api = []string{name}
	c.Ingester_UUID = auditUUID
	c.Subscription_ID = []string{"33333333-3333-4333-8333-333333333333"}
	if e := c.Verify(); e != nil {
		t.Fatal(e)
	}
	return c
}

func auditRun(t *testing.T, c *Config, r *persistentTestRuntime, writer processorWriter) {
	t.Helper()
	job, e := NewBuilder("invariant-audit", c).Build(writer, r.bucket.Sync)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := job.Run(ctx, r); e != nil {
		t.Fatal(e)
	}
}

func TestAuditInitialEmptyCatalog(t *testing.T) {
	for _, d := range Catalog() {
		if !auditTemporal(d) {
			continue
		}
		t.Run(d.Name, func(t *testing.T) {
			s := auditServer(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"value":[],"results":[],"activityEventEntities":[]}`)
			})
			c := auditConfig(t, s.URL, d.Name)
			r, closeDB := auditRuntime(t, filepath.Join(t.TempDir(), "state"))
			defer closeDB()
			initial := time.Now().UTC().Add(-time.Duration(c.Lookback) * time.Hour)
			auditRun(t, c, r, r)
			if len(r.errorLogs) > 0 {
				t.Fatalf("unexpected source error: %v", r.errorLogs)
			}
			sub := ""
			if strings.Contains(d.Path, "{subscriptionId}") {
				sub = c.Subscription_ID[0]
			}
			state, e := loadState(r, stateKey(c.StateNamespace(), d.Name, sub))
			if e != nil {
				t.Fatal(e)
			}
			if state.Watermark.IsZero() || state.Watermark.After(initial.Add(time.Minute)) {
				t.Errorf("empty initial poll lost lower anchor: initial=%s saved=%s", initial.Format(time.RFC3339Nano), state.Watermark.Format(time.RFC3339Nano))
			}
		})
	}
}

func TestAuditEstablishedEmptyRestartLateVisibility(t *testing.T) {
	anchor := time.Now().UTC().Add(-48 * time.Hour)
	eventTime := anchor.Add(time.Hour)
	phase := 0
	s := auditServer(t, func(w http.ResponseWriter, r *http.Request) {
		fields := strings.Fields(r.URL.Query().Get("$filter"))
		if len(fields) < 3 {
			t.Errorf("missing time filter")
			fmt.Fprint(w, `{"value":[]}`)
			return
		}
		lower, e := time.Parse(time.RFC3339Nano, fields[2])
		if e != nil {
			t.Error(e)
		}
		if phase == 1 && !eventTime.Before(lower) {
			fmt.Fprintf(w, `{"value":[{"id":"late","activityDateTime":%q,"activityDisplayName":"Add user"}]}`, eventTime.Format(time.RFC3339Nano))
			return
		}
		fmt.Fprint(w, `{"value":[]}`)
	})
	path := filepath.Join(t.TempDir(), "state")
	c := auditConfig(t, s.URL, "entra-directory-audits")
	r, closeDB := auditRuntime(t, path)
	key := stateKey(c.StateNamespace(), c.Api[0], "")
	if e := saveState(r, key, streamState{Watermark: anchor, Records: map[string]recordState{}}); e != nil {
		t.Fatal(e)
	}
	auditRun(t, c, r, r)
	saved, e := loadState(r, key)
	if e != nil {
		t.Fatal(e)
	}
	closeDB()
	phase = 1
	r, closeDB = auditRuntime(t, path)
	auditRun(t, c, r, r)
	closeDB()
	if !saved.Watermark.Equal(anchor) || len(r.entries) != 1 {
		t.Fatalf("empty poll advanced %s -> %s; after Bolt reopen late records delivered=%d want=1", anchor.Format(time.RFC3339Nano), saved.Watermark.Format(time.RFC3339Nano), len(r.entries))
	}
}

type auditPartialWriter struct{ *persistentTestRuntime }

func (w *auditPartialWriter) WriteEntryContext(ctx context.Context, e *entry.Entry) error {
	if bytes.Contains(e.Data, []byte(`"id":"b"`)) {
		return errors.New("synthetic persistent second-record write failure")
	}
	return w.persistentTestRuntime.WriteEntryContext(ctx, e)
}
func TestAuditPartialWriteReplayAcrossRestart(t *testing.T) {
	s := auditServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/page2" {
			fmt.Fprint(w, `{"value":[{"id":"b","activityDisplayName":"Add group"}]}`)
			return
		}
		fmt.Fprintf(w, `{"value":[{"id":"a","activityDisplayName":"Add user"}],"@odata.nextLink":%q}`, serverURL(r)+"/page2")
	})
	path := filepath.Join(t.TempDir(), "state")
	total := 0
	for attempt := 0; attempt < 3; attempt++ {
		r, closeDB := auditRuntime(t, path)
		c := auditConfig(t, s.URL, "entra-directory-audits")
		auditRun(t, c, r, &auditPartialWriter{r})
		total += len(r.entries)
		if len(r.errorLogs) == 0 {
			t.Fatal("failure not exercised")
		}
		closeDB()
	}
	if total > 1 {
		t.Fatalf("first-page record accepted %d times over 3 persisted-state restarts; no durable dedup/progress committed before deterministic second-record failure", total)
	}
}

func TestAuditEverySelectorRejectsIntervalOverflow(t *testing.T) {
	for _, d := range Catalog() {
		t.Run(d.Name, func(t *testing.T) {
			c := auditConfig(t, "https://fixture.example.invalid", d.Name)
			c.Request_Interval = int(^uint(0) >> 1)
			if e := c.Verify(); e == nil {
				t.Fatalf("Request-Interval=%d accepted; native duration=%s", c.Request_Interval, time.Duration(c.Request_Interval)*time.Second)
			}
		})
	}
}

func TestAuditCrossOriginCredentialedRedirects(t *testing.T) {
	for _, token := range []bool{false, true} {
		for _, status := range []int{301, 302, 303, 307, 308} {
			t.Run(fmt.Sprintf("token=%v/status=%d", token, status), func(t *testing.T) {
				var hits atomic.Int32
				target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
				defer target.Close()
				s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/token") && !token {
						fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
						return
					}
					http.Redirect(w, r, target.URL, status)
				}))
				defer s.Close()
				c, e := NewClient(testClientConfig(t, s.URL), nil)
				if e != nil {
					t.Fatal(e)
				}
				c.http.Transport = s.Client().Transport
				_, e = c.Fetch(context.Background(), datasets["entra-directory-audits"], time.Now().Add(-time.Hour), time.Now(), "")
				if e == nil || hits.Load() != 0 {
					t.Fatalf("redirect err=%v forbidden target hits=%d", e, hits.Load())
				}
			})
		}
	}
}

func TestAuditRetryIntegerBoundaries(t *testing.T) {
	maxSeconds := int64((time.Duration(1<<63 - 1)) / time.Second)
	for _, v := range []string{strconv.FormatInt(maxSeconds, 10), strconv.FormatInt(maxSeconds+1, 10), "18446744073709551615", "18446744073709551616"} {
		got := retryDelay(0, v)
		if got <= 0 {
			t.Fatalf("%s overflowed to %s", v, got)
		}
		if v != strconv.FormatInt(maxSeconds, 10) && got != time.Duration(1<<63-1) {
			t.Fatalf("%s not deferred", v)
		}
	}
}

func TestAuditDuplicateKeyFidelity(t *testing.T) {
	raw := []byte(`{"id":"same","status":"first","status":"last"}`)
	d := datasets["entra-users"]
	plain, _, e := formatRecord(raw, "same", time.Now(), d, "disabled")
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(plain, raw) {
		t.Fatal("raw fidelity changed")
	}
	norm, _, e := formatRecord(raw, "same", time.Now(), d, "enabled")
	if e != nil {
		t.Fatal(e)
	}
	var out map[string]json.RawMessage
	if e := json.Unmarshal(norm, &out); e != nil {
		t.Fatal(e)
	}
	t.Logf("raw=%s normalized.record=%s", plain, out["record"])
	if bytes.Count(out["record"], []byte(`"status"`)) != 1 || !bytes.Contains(out["record"], []byte(`"status":"last"`)) {
		t.Fatal("normalization duplicate-key policy must retain the last key")
	}
	first, e := recordDigest(raw, d)
	if e != nil {
		t.Fatal(e)
	}
	last, e := recordDigest([]byte(`{"id":"same","status":"last"}`), d)
	if e != nil {
		t.Fatal(e)
	}
	if first != last {
		t.Fatal("digest duplicate-key policy must match map-based normalization")
	}
}
