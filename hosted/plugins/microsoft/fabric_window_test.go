package microsoft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestFabricCapacityProtectsCurrentScan(t *testing.T) {
	now := time.Now()
	records := make(map[string]recordState, maximumStateRecords+1)
	active := make(map[string]bool, maximumStateRecords)
	for i := 0; i < maximumStateRecords; i++ {
		id := fmt.Sprint(i)
		records[id] = recordState{Hash: "same", SeenAt: now}
		active[id] = true
	}
	records["absent"] = recordState{Hash: "old", SeenAt: now}
	if err := pruneFabricRecords(records, active, now); err != nil {
		t.Fatal(err)
	}
	if len(records) != maximumStateRecords {
		t.Fatalf("boundary size=%d", len(records))
	}
	for id := range active {
		if _, ok := records[id]; !ok {
			t.Fatalf("active identity evicted: %s", id)
		}
	}
	active["over-cap"] = true
	records["over-cap"] = recordState{Hash: "new", SeenAt: now}
	if err := pruneFabricRecords(records, active, now); err == nil {
		t.Fatal("over-cap scan accepted")
	}
	if len(records) != maximumStateRecords+1 {
		t.Fatal("failure mutated candidate map")
	}
}

func TestFabricCapacityFailsBeforeDeliveryOrStateMutation(t *testing.T) {
	var body bytes.Buffer
	body.WriteString(`{"activityEventEntities":[`)
	for i := 0; i <= maximumStateRecords; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"Id":"%d"}`, i)
	}
	body.WriteString(`]}`)
	s := auditServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body.Bytes()) })
	rt := newMicrosoftTestRuntime()
	p := newMicrosoftTestPlugin(t, s, rt)
	p.conf.Api = []string{"fabric-activity"}
	now := p.now()
	key := stateKey(p.conf.StateNamespace(), "fabric-activity", "")
	prior := streamState{Watermark: now.Add(-time.Hour), Records: map[string]recordState{"prior": {Hash: "saved", SeenAt: now}}, Pending: &deliveryProgress{Start: now.Add(-time.Hour), End: now, Receipts: map[string]bool{"prior:saved": true}}}
	if err := saveState(rt, key, prior); err != nil {
		t.Fatal(err)
	}
	encoded := append([]byte(nil), rt.values[key]...)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := p.Handle(ctx, rt); err != nil {
		t.Fatal(err)
	}
	assertMicrosoftSelectorError(t, rt, "fabric-activity", false, "identity state capacity")
	if len(rt.entries) != 0 || !bytes.Equal(encoded, rt.values[key]) {
		t.Fatal("capacity failure wrote entries or changed prior durable progress")
	}
}

func TestFabricRescanSuppressesUnchangedEvenWhenFlagEnabled(t *testing.T) {
	for _, flag := range []bool{false, true} {
		t.Run(fmt.Sprint(flag), func(t *testing.T) {
			now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
			events := []fabricTestEvent{{"first", now.Add(-30 * time.Minute)}}
			s := auditServer(t, func(w http.ResponseWriter, r *http.Request) { fabricReply(t, w, r, now, events, nil) })
			c := auditConfig(t, s.URL, "fabric-activity")
			c.Ingest_Unchanged = flag
			c.Lookback = 1
			path := filepath.Join(t.TempDir(), "state")
			for poll := 0; poll < 4; poll++ {
				if poll == 2 {
					events = append(events, fabricTestEvent{"late", now.Add(-50 * time.Minute)})
				}
				if poll == 3 {
					events[0].at = events[0].at.Add(time.Second)
				}
				rt, closeDB := auditRuntime(t, path)
				fabricRun(t, c, rt, rt, rt.bucket.Sync, now)
				want := 1
				if poll == 1 {
					want = 0
				}
				if len(rt.entries) != want || len(rt.errorLogs) != 0 {
					t.Fatalf("poll %d entries=%d want=%d errors=%v", poll, len(rt.entries), want, rt.errorLogs)
				}
				closeDB()
				now = now.Add(time.Minute)
			}
		})
	}
}

func TestFabricMaximumLookbackEmptyRestart(t *testing.T) {
	s := auditServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"activityEventEntities":[]}`)
	})
	c := auditConfig(t, s.URL, "fabric-activity")
	c.Lookback = 28 * 24
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state")
	for poll := 0; poll < 2; poll++ {
		rt, closeDB := auditRuntime(t, path)
		auditRun(t, c, rt, rt)
		closeDB()
		if len(rt.errorLogs) > 0 {
			t.Fatalf("poll %d failed: %v", poll, rt.errorLogs)
		}
	}
}

const fabricTestKey = "microsoft/fabric-activity"

// Only the wall clock is controlled. The production Builder, HTTP client,
// processor dispatch, job adapter, and native Bolt bucket remain in use.
func fabricRun(t *testing.T, c *Config, rt hosted.Runtime, writer processorWriter, syncState func() error, now time.Time) {
	t.Helper()
	b := NewBuilder("fabric-window-regression", c)
	b.now = func() time.Time { return now }
	job, err := b.Build(writer, syncState)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := job.Run(ctx, rt); err != nil {
		t.Fatal(err)
	}
}

type fabricTestEvent struct {
	id string
	at time.Time
}

func fabricReply(t *testing.T, w http.ResponseWriter, r *http.Request, now time.Time, events []fabricTestEvent, starts *[]time.Time) {
	t.Helper()
	start, e1 := time.Parse(time.RFC3339Nano, strings.Trim(r.URL.Query().Get("startDateTime"), "'"))
	end, e2 := time.Parse(time.RFC3339Nano, strings.Trim(r.URL.Query().Get("endDateTime"), "'"))
	if e1 != nil || e2 != nil {
		t.Errorf("invalid Fabric request bounds")
		w.WriteHeader(400)
		return
	}
	if start.Before(now.Add(-28*24*time.Hour)) || end.After(now) || start.After(end) || start.Format("2006-01-02") != end.Format("2006-01-02") {
		t.Errorf("out-of-contract Fabric bounds %s/%s at %s", start, end, now)
		w.WriteHeader(400)
		return
	}
	if starts != nil {
		*starts = append(*starts, start)
	}
	rows := []any{}
	for _, ev := range events {
		if !ev.at.Before(start) && !ev.at.After(end) {
			rows = append(rows, map[string]any{"Id": ev.id, "CreationTime": ev.at.Format(time.RFC3339Nano), "Operation": "ViewReport", "Workload": "PowerBI", "Activity": "ViewReport"})
		}
	}
	if err := json.NewEncoder(w).Encode(map[string]any{"activityEventEntities": rows}); err != nil {
		t.Error(err)
	}
}

func TestFabricRetainedCoverageBeyondCapAndLookbackChange(t *testing.T) {
	for _, lookback := range []int{24, 28 * 24} {
		t.Run(fmt.Sprint(lookback), func(t *testing.T) {
			t0 := time.Date(2026, 7, 1, 12, 0, 0, 123456789, time.UTC)
			now := t0
			var events []fabricTestEvent
			var starts []time.Time
			s := auditServer(t, func(w http.ResponseWriter, r *http.Request) { fabricReply(t, w, r, now, events, &starts) })
			c := auditConfig(t, s.URL, "fabric-activity")
			c.Lookback = LookbackHours(lookback)
			if err := c.Verify(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "state")
			anchor := t0.Add(-time.Duration(lookback) * time.Hour)
			var prior streamState
			for i, advance := range []time.Duration{0, time.Hour, 29 * 24 * time.Hour, 60 * 24 * time.Hour, 61 * 24 * time.Hour, 62 * 24 * time.Hour, 63 * 24 * time.Hour} {
				now = t0.Add(advance)
				starts = nil
				if i > 0 {
					c.Lookback = 1
				}
				if i == 4 {
					events = []fabricTestEvent{{"newer", now.Add(-time.Hour)}}
				}
				// An older record appears after a newer event advanced Watermark.
				if i == 5 {
					events = append(events, fabricTestEvent{"late", now.Add(-27 * 24 * time.Hour)})
				}
				rt, closeDB := auditRuntime(t, path)
				fabricRun(t, c, rt, rt, rt.bucket.Sync, now)
				if len(rt.errorLogs) != 0 {
					t.Fatalf("poll%d: %v", i, rt.errorLogs)
				}
				state, err := loadState(rt, fabricTestKey)
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if i == 4 || i == 5 {
					want = 1
				}
				if len(rt.entries) != want {
					t.Fatalf("poll%d entries=%d want=%d", i, len(rt.entries), want)
				}
				if state.Fabric == nil || !state.Fabric.Anchor.Equal(anchor) {
					t.Fatalf("anchor lost: %+v", state.Fabric)
				}
				if len(starts) == 0 {
					t.Fatalf("poll%d made no request", i)
				}
				if state.Watermark.Before(prior.Watermark) {
					t.Fatal("event watermark regressed")
				}
				if i < 4 && !state.Watermark.Equal(anchor) {
					t.Fatal("empty poll advanced event watermark")
				}
				if i > 0 && (state.Fabric.Through.Before(prior.Fabric.Through) || state.Fabric.AvailableSince.Before(prior.Fabric.AvailableSince)) {
					t.Fatal("scan metadata regressed")
				}
				prior = state
				closeDB()
			}
		})
	}
}

type fabricFaultRuntime struct {
	*persistentTestRuntime
	failure  string
	warnings int
}

func (r *fabricFaultRuntime) Warn(string, ...rfc5424.SDParam) { r.warnings++ }
func (r *fabricFaultRuntime) Put(key string, value []byte) error {
	var state streamState
	if err := json.Unmarshal(value, &state); err != nil {
		return err
	}
	if r.failure == "put" || (r.failure == "final-put" && state.Pending == nil) {
		return errors.New("synthetic storage failure")
	}
	return r.persistentTestRuntime.Put(key, value)
}
func (r *fabricFaultRuntime) WriteEntryContext(ctx context.Context, e *entry.Entry) error {
	if r.failure == "write" || r.failure == "second-write" && strings.Contains(string(e.Data), `"Id":"b"`) {
		return errors.New("synthetic write failure")
	}
	return r.persistentTestRuntime.WriteEntryContext(ctx, e)
}
func (r *fabricFaultRuntime) SyncContext(context.Context, time.Duration) error {
	if r.failure == "delivery-sync" {
		return errors.New("synthetic delivery sync failure")
	}
	return nil
}

func TestFabricFailureStateAndExpiredPendingRestart(t *testing.T) {
	for _, failure := range []string{"request", "write", "delivery-sync", "put", "final-put", "state-sync", "second-write"} {
		t.Run(failure, func(t *testing.T) {
			now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
			anchor := now.Add(-60 * 24 * time.Hour)
			old := streamState{Watermark: anchor, Records: map[string]recordState{}}
			old.Fabric = &fabricScan{Anchor: anchor, AvailableSince: anchor, Through: anchor.Add(time.Hour)}
			phase := 0
			events := []fabricTestEvent{{"a", now.Add(-time.Hour)}, {"b", now.Add(-time.Minute)}}
			s := auditServer(t, func(w http.ResponseWriter, r *http.Request) {
				if phase == 0 && failure == "request" {
					w.WriteHeader(503)
					return
				}
				fabricReply(t, w, r, now, events, nil)
			})
			c := auditConfig(t, s.URL, "fabric-activity")
			path := filepath.Join(t.TempDir(), "state")
			rt, closeDB := auditRuntime(t, path)
			if err := saveState(rt, fabricTestKey, old); err != nil {
				t.Fatal(err)
			}
			fault := &fabricFaultRuntime{persistentTestRuntime: rt, failure: failure}
			syncState := func() error {
				if phase == 0 && failure == "state-sync" {
					return errors.New("synthetic state sync failure")
				}
				return rt.bucket.Sync()
			}
			fabricRun(t, c, fault, fault, syncState, now)
			failed, err := loadState(rt, fabricTestKey)
			if err != nil {
				t.Fatal(err)
			}
			if !failed.Watermark.Equal(old.Watermark) || !reflect.DeepEqual(failed.Fabric, old.Fabric) || !reflect.DeepEqual(failed.Records, old.Records) {
				t.Fatal("failed poll advanced completed state")
			}
			if len(rt.errorLogs) == 0 {
				t.Fatal("failure was not reported")
			}
			if fault.warnings == 0 {
				t.Fatal("expired history was silently discarded")
			}
			closeDB()
			// All incomplete windows are now wholly expired. The new run must
			// recover from persisted R2-compatible state, not requery that window.
			phase = 1
			now = now.Add(40 * 24 * time.Hour)
			events = []fabricTestEvent{{"current", now.Add(-time.Hour)}}
			c.Lookback = 1
			rt, closeDB = auditRuntime(t, path)
			fabricRun(t, c, rt, rt, rt.bucket.Sync, now)
			if len(rt.errorLogs) != 0 || len(rt.entries) != 1 {
				t.Fatalf("recovery logs=%v entries=%d", rt.errorLogs, len(rt.entries))
			}
			saved, err := loadState(rt, fabricTestKey)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Pending != nil || saved.Fabric == nil || !saved.Watermark.Equal(events[0].at) {
				t.Fatalf("recovery state %+v", saved)
			}
			closeDB()
		})
	}
}

func TestFabricPredecessorPendingAndReceiptContinuity(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	anchor := now.Add(-28 * 24 * time.Hour)
	events := []fabricTestEvent{{"a", now.Add(-time.Hour)}, {"b", now.Add(-time.Minute)}}
	s := auditServer(t, func(w http.ResponseWriter, r *http.Request) { fabricReply(t, w, r, now, events, nil) })
	c := auditConfig(t, s.URL, "fabric-activity")
	path := filepath.Join(t.TempDir(), "state")
	rt, closeDB := auditRuntime(t, path)
	// Exact predecessor layout: no fabric_scan or fabric_anchor fields.
	legacy := fmt.Sprintf(`{"watermark":%q,"records":{},"pending":{"start":%q,"end":%q,"receipts":{}}}`, anchor.Format(time.RFC3339Nano), anchor.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err := rt.Put(fabricTestKey, []byte(legacy)); err != nil {
		t.Fatal(err)
	}
	fault := &fabricFaultRuntime{persistentTestRuntime: rt, failure: "second-write"}
	fabricRun(t, c, fault, fault, rt.bucket.Sync, now)
	state, err := loadState(rt, fabricTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if state.Pending == nil || len(state.Pending.Receipts) != 1 || state.Fabric != nil || !state.Watermark.Equal(anchor) {
		t.Fatalf("partial predecessor state %+v", state)
	}
	closeDB()
	now = now.Add(time.Hour)
	c.Lookback = 1
	rt, closeDB = auditRuntime(t, path)
	fabricRun(t, c, rt, rt, rt.bucket.Sync, now)
	if len(rt.errorLogs) > 0 || len(rt.entries) != 1 || !strings.Contains(string(rt.entries[0].Data), `"Id":"b"`) {
		t.Fatalf("pending receipt replay: entries=%d errors=%v", len(rt.entries), rt.errorLogs)
	}
	state, err = loadState(rt, fabricTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if state.Fabric == nil || !state.Fabric.Anchor.Equal(anchor) || state.Pending != nil {
		t.Fatalf("state initialization lost anchor: %+v", state)
	}
	closeDB()
	// A later-visible event older than the new event watermark remains covered.
	events = append(events, fabricTestEvent{"late", now.Add(-27 * 24 * time.Hour)})
	rt, closeDB = auditRuntime(t, path)
	fabricRun(t, c, rt, rt, rt.bucket.Sync, now)
	if len(rt.errorLogs) > 0 || len(rt.entries) != 1 || !strings.Contains(string(rt.entries[0].Data), `"Id":"late"`) {
		t.Fatalf("late delivery: entries=%d errors=%v", len(rt.entries), rt.errorLogs)
	}
	closeDB()
}

func TestFabricWindowClockRollbackAndMillisecondLimit(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 999999, time.UTC)
	anchor := now.Add(-40 * 24 * time.Hour)
	state := streamState{Watermark: now, Fabric: &fabricScan{Anchor: anchor, AvailableSince: now.Add(-28 * 24 * time.Hour), Through: now}}
	for _, clock := range []time.Time{now, now.Add(-time.Hour), now.Add(time.Hour)} {
		scan, start, end := fabricWindow(state, anchor, clock, clock)
		if end.Sub(start) > 28*24*time.Hour || start.Before(clock.Add(-28*24*time.Hour)) || end.After(clock) || start.After(end) {
			t.Fatalf("invalid range %s/%s", start, end)
		}
		if !scan.Anchor.Equal(anchor) || scan.Through.Before(state.Fabric.Through) || scan.AvailableSince.Before(state.Fabric.AvailableSince) {
			t.Fatalf("scan regressed %+v", scan)
		}
	}
}

func TestFabricMigratedSubmillisecondRetentionEdge(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 123456789, time.UTC)
	for _, offset := range []time.Duration{0, 100 * time.Microsecond, 600 * time.Microsecond, time.Millisecond} {
		t.Run(offset.String(), func(t *testing.T) {
			var starts []time.Time
			s := auditServer(t, func(w http.ResponseWriter, r *http.Request) { fabricReply(t, w, r, now, nil, &starts) })
			c := auditConfig(t, s.URL, "fabric-activity")
			anchor := now.Add(-28 * 24 * time.Hour).Add(offset)
			watermark := anchor.Add(time.Duration(c.Overlap) * time.Second)
			rt, closeDB := auditRuntime(t, filepath.Join(t.TempDir(), "state"))
			defer closeDB()
			if err := saveState(rt, fabricTestKey, streamState{Watermark: watermark, Records: map[string]recordState{}}); err != nil {
				t.Fatal(err)
			}
			fabricRun(t, c, rt, rt, rt.bucket.Sync, now)
			if len(rt.errorLogs) > 0 || len(starts) == 0 {
				t.Fatalf("migrated boundary poll failed: %v", rt.errorLogs)
			}
			state, err := loadState(rt, fabricTestKey)
			if err != nil {
				t.Fatal(err)
			}
			if state.Fabric == nil || !state.Fabric.Anchor.Equal(anchor) || !state.Watermark.Equal(watermark) {
				t.Fatal("empty state initialization changed event progress or original anchor")
			}
			if starts[0].Before(now.Add(-28 * 24 * time.Hour)) {
				t.Fatal("transport rounding escaped retention")
			}
		})
	}
}
