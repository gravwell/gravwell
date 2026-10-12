package microsoft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func matrixRecord(d Dataset, id, value string) map[string]any {
	r := map[string]any{"id": id, "displayName": value, "createdDateTime": "2020-01-01T00:00:00Z", "status": "active"}
	put := func(path string, value string) {
		if path == "" {
			return
		}
		parts := strings.Split(path, ".")
		m := r
		for _, part := range parts[:len(parts)-1] {
			child, ok := m[part].(map[string]any)
			if !ok {
				child = map[string]any{}
				m[part] = child
			}
			m = child
		}
		m[parts[len(parts)-1]] = value
	}
	put(d.IDField, id)
	for _, p := range d.IDFields {
		put(p, id)
	}
	put(d.TimeField, "2020-01-01T00:00:00Z")
	return r
}

// Enumerate every full-refresh catalog path with the real production builder,
// HTTP client and Bolt store. Fixtures prove implementation behavior; they do
// not assert that each vendor's mutable clock has a stronger contract.
func TestFullRefreshCatalogMutationRestartAndDeletion(t *testing.T) {
	for _, d := range Catalog() {
		if temporalDataset(d) {
			continue
		}
		t.Run(d.Name, func(t *testing.T) {
			cycle := 0
			s := auditServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Has("$filter") && d.Filter == "" {
					t.Errorf("full refresh sent temporal filter: %s", d.Name)
				}
				a, b := matrixRecord(d, "a", "original"), matrixRecord(d, "b", "original")
				if cycle >= 2 {
					a["displayName"] = "changed"
				}
				rows := []any{a, b}
				if cycle == 3 {
					rows = []any{}
				}
				response := map[string]any{}
				switch d.Kind {
				case KindObjectGET:
					if cycle == 3 {
						response = map[string]any{}
					} else {
						response = a
					}
				case KindResourceGraph:
					var body struct {
						Options map[string]any `json:"options"`
					}
					if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
						t.Error(e)
					}
					if cycle == 3 {
						response["data"] = rows
					} else if body.Options["$skipToken"] == "page2" {
						response["data"] = []any{b}
					} else {
						response["data"] = []any{a}
						response["$skipToken"] = "page2"
					}
					response["totalRecords"] = 0
				case KindAdvancedHunting:
					response["results"] = rows // no pagination exists for this API
				case KindParentChild:
					if r.URL.Path == d.ParentPath {
						response["value"] = []any{map[string]any{"id": "parent"}}
					} else if cycle == 3 {
						response["value"] = rows
					} else if r.URL.Query().Get("matrixPage") == "2" {
						response["value"] = []any{b}
					} else {
						response["value"] = []any{a}
						response["@odata.nextLink"] = serverURL(r) + r.URL.Path + "?matrixPage=2"
					}
				default:
					if cycle == 3 {
						response["value"] = rows
					} else if r.URL.Query().Get("matrixPage") == "2" {
						response["value"] = []any{b}
					} else {
						response["value"] = []any{a}
						response["@odata.nextLink"] = serverURL(r) + r.URL.Path + "?matrixPage=2"
					}
					response["@odata.count"] = 0
				}
				if e := json.NewEncoder(w).Encode(response); e != nil {
					t.Error(e)
				}
			})
			path := filepath.Join(t.TempDir(), "state")
			for cycle = 0; cycle < 5; cycle++ {
				rt, closeDB := auditRuntime(t, path)
				c := auditConfig(t, s.URL, d.Name)
				auditRun(t, c, rt, rt)
				if len(rt.errorLogs) > 0 {
					t.Fatalf("cycle %d: %v", cycle, rt.errorLogs)
				}
				want := 2
				if d.Kind == KindObjectGET {
					want = 1
				}
				switch cycle {
				case 1:
					want = 0
				case 2:
					want = 1
				case 3:
					want = 0
					if d.Kind == KindObjectGET {
						want = 1
					}
				case 4:
					if d.Mode != ModeSnapshot {
						want = 0
					}
					if d.Kind == KindObjectGET {
						want = 0
					}
				}
				if len(rt.entries) != want {
					t.Errorf("cycle %d wrote %d want %d", cycle, len(rt.entries), want)
				}
				for _, entry := range rt.entries {
					if !json.Valid(entry.Data) || strings.ContainsAny(string(entry.Data), "\r\n") {
						t.Error("record lost compact framing")
					}
				}
				closeDB()
			}
		})
	}
}

func TestTemporalCatalogMonotonicEqualSubsecondWatermarks(t *testing.T) {
	end := time.Date(2026, 9, 21, 12, 0, 0, 500000000, time.UTC)
	previous := end.Add(-time.Hour)
	start := previous.Add(-15 * time.Minute)
	for _, d := range Catalog() {
		if !temporalDataset(d) {
			continue
		}
		t.Run(d.Name, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				stamps []time.Time
				want   time.Time
			}{
				{"empty", nil, previous}, {"zero", []time.Time{{}}, previous}, {"older", []time.Time{previous.Add(-time.Nanosecond)}, previous},
				{"equal", []time.Time{previous, previous}, previous}, {"subsecond", []time.Time{previous.Add(time.Nanosecond)}, previous.Add(time.Nanosecond)},
				{"future", []time.Time{end.Add(time.Hour)}, end},
			} {
				var records []FetchedRecord
				for _, stamp := range tc.stamps {
					records = append(records, FetchedRecord{Timestamp: stamp})
				}
				if got := nextWatermark(d, previous, start, end, records); !got.Equal(tc.want) {
					t.Errorf("%s: %s want %s", tc.name, got, tc.want)
				}
			}
			if got := nextWatermark(d, previous, start, previous.Add(-time.Minute), nil); !got.Equal(previous) {
				t.Fatal("clock rollback regressed state")
			}
		})
	}
}

func TestPaginationCapRecoveryAndIndependentSource(t *testing.T) {
	for _, total := range []string{"absent", "zero", "stale"} {
		t.Run(total, func(t *testing.T) {
			s := auditServer(t, func(w http.ResponseWriter, r *http.Request) {
				obj := map[string]any{}
				if total == "zero" {
					obj["@odata.count"] = 0
				}
				if total == "stale" {
					obj["@odata.count"] = 1
				}
				if strings.HasSuffix(r.URL.Path, "/groups") {
					obj["value"] = []any{map[string]any{"id": "group", "displayName": "Independent group"}}
				} else if r.URL.Path == "/page2" {
					obj["value"] = []any{map[string]any{"id": "b", "displayName": "Second user"}}
				} else {
					obj["value"] = []any{map[string]any{"id": "a", "displayName": "First user"}}
					obj["@odata.nextLink"] = serverURL(r) + "/page2"
				}
				if e := json.NewEncoder(w).Encode(obj); e != nil {
					t.Error(e)
				}
			})
			path := filepath.Join(t.TempDir(), "state")
			for run := 0; run < 3; run++ {
				rt, closeDB := auditRuntime(t, path)
				c := auditConfig(t, s.URL, "entra-users")
				c.Api = []string{"entra-users", "entra-groups"}
				c.Max_Pages = 1
				if run > 0 {
					c.Max_Pages = 2
				}
				auditRun(t, c, rt, rt)
				want := 1
				if run == 1 {
					want = 2
				}
				if run == 2 {
					want = 0
				}
				if len(rt.entries) != want {
					t.Errorf("run %d entries=%d want=%d", run, len(rt.entries), want)
				}
				if run == 0 && len(rt.errorLogs) != 1 {
					t.Errorf("cap error count=%d", len(rt.errorLogs))
				}
				if run > 0 && len(rt.errorLogs) > 0 {
					t.Errorf("cap stayed latched: %v", rt.errorLogs)
				}
				closeDB()
			}
		})
	}
}

func TestPriorNamespaceAndRepeatedRoutingPreserveState(t *testing.T) {
	s := auditServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"value":[{"id":"prior","displayName":"Unchanged user"}]}`)
	})
	c := auditConfig(t, s.URL, "entra-users")
	path := filepath.Join(t.TempDir(), "state")
	rt, closeDB := auditRuntime(t, path)
	// Seed exactly the prior JSON shape and raw routed namespace, without a new
	// pending field, then repeatedly switch schema and normalization modes.
	oldHash, e := recordDigest([]byte(`{"id":"prior","displayName":"Unchanged user"}`), datasets["entra-users"])
	if e != nil {
		t.Fatal(e)
	}
	old, _ := json.Marshal(map[string]any{"watermark": time.Now().Add(-time.Hour), "records": map[string]recordState{"prior": {Hash: oldHash, SeenAt: time.Now()}}})
	if e := rt.Put("microsoft/entra-users", old); e != nil {
		t.Fatal(e)
	}
	closeDB()
	for i, mode := range []string{"disabled", "disabled", "enabled", "disabled", "enabled"} {
		rt, closeDB = auditRuntime(t, path)
		c.Normalization = mode
		if i%2 == 0 {
			c.Tag_Schema = TagSchemaLegacy
		} else {
			c.Tag_Schema = TagSchemaConsolidated
		}
		if e := c.Verify(); e != nil {
			t.Fatal(e)
		}
		auditRun(t, c, rt, rt)
		want := 0
		if i == 2 {
			want = 1
		}
		if len(rt.entries) != want {
			t.Errorf("switch %d mode=%s emitted=%d want=%d", i, mode, len(rt.entries), want)
		}
		closeDB()
	}
}

func TestPartialReceiptCompletionAndIntentionalRepeats(t *testing.T) {
	s := auditServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"value":[{"id":"a","displayName":"First"},{"id":"b","displayName":"Second"}]}`)
	})
	path := filepath.Join(t.TempDir(), "state")
	for run := 0; run < 4; run++ {
		rt, closeDB := auditRuntime(t, path)
		c := auditConfig(t, s.URL, "entra-users")
		c.Ingest_Unchanged = true
		var writer processorWriter = rt
		if run < 2 {
			writer = &auditPartialWriter{rt}
		}
		auditRun(t, c, rt, writer)
		want := []int{1, 0, 1, 2}[run]
		if len(rt.entries) != want {
			t.Errorf("run %d entries=%d want=%d", run, len(rt.entries), want)
		}
		state, e := loadState(rt, "microsoft/entra-users")
		if e != nil {
			t.Fatal(e)
		}
		if run < 2 {
			if state.Pending == nil || len(state.Pending.Receipts) != 1 || !state.Watermark.IsZero() {
				t.Error("partial receipt or complete watermark incorrect")
			}
		} else if state.Pending != nil {
			t.Error("completed poll retained pending receipts")
		}
		closeDB()
	}
}

func TestProductionBuilderPartialReceiptCallsStateSyncOnFailure(t *testing.T) {
	s := auditServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"value":[{"id":"a"},{"id":"b"}]}`) })
	rt, closeDB := auditRuntime(t, filepath.Join(t.TempDir(), "state"))
	defer closeDB()
	c := auditConfig(t, s.URL, "entra-users")
	calls := 0
	job, e := NewBuilder("sync-partial", c).Build(&auditPartialWriter{rt}, func() error { calls++; return rt.bucket.Sync() })
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := job.Run(ctx, rt); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatalf("partial failure called storage sync %d times want=1", calls)
	}
}

type matrixSyncFailureWriter struct {
	*persistentTestRuntime
	calls int
}

func (w *matrixSyncFailureWriter) SyncContext(context.Context, time.Duration) error {
	w.calls++
	if w.calls == 2 {
		return errors.New("synthetic second delivery sync failure")
	}
	return nil
}

func TestPartialBatchSyncFailurePreservesEarlierReceipts(t *testing.T) {
	s := auditServer(t, func(w http.ResponseWriter, r *http.Request) {
		rows := make([]any, 130)
		for i := range rows {
			rows[i] = map[string]any{"id": fmt.Sprint(i), "displayName": "Synthetic inventory record"}
		}
		if e := json.NewEncoder(w).Encode(map[string]any{"value": rows}); e != nil {
			t.Error(e)
		}
	})
	path := filepath.Join(t.TempDir(), "state")
	for run := 0; run < 2; run++ {
		rt, closeDB := auditRuntime(t, path)
		c := auditConfig(t, s.URL, "entra-users")
		var writer processorWriter = rt
		if run == 0 {
			writer = &matrixSyncFailureWriter{persistentTestRuntime: rt}
		}
		auditRun(t, c, rt, writer)
		state, e := loadState(rt, "microsoft/entra-users")
		if e != nil {
			t.Fatal(e)
		}
		if run == 0 {
			if state.Pending == nil || len(state.Pending.Receipts) != 64 || !state.Watermark.IsZero() {
				t.Fatal("failed sync persisted unconfirmed progress")
			}
			if len(rt.entries) != 128 {
				t.Fatalf("accepted=%d want128", len(rt.entries))
			}
		} else {
			if len(rt.entries) != 66 {
				t.Fatalf("retry accepted=%d want66 (64 uncertain + 2 new)", len(rt.entries))
			}
			if state.Pending != nil {
				t.Fatal("completed retry retains pending state")
			}
		}
		closeDB()
	}
}

func TestTemporalCatalogEstablishedEmptyLateRestartAndTies(t *testing.T) {
	for _, d := range Catalog() {
		if !temporalDataset(d) {
			continue
		}
		t.Run(d.Name, func(t *testing.T) {
			anchor := time.Now().UTC().Add(-48 * time.Hour)
			eventTime := anchor.Add(time.Hour)
			phase := 0
			s := auditServer(t, func(w http.ResponseWriter, r *http.Request) {
				var startText, endText string
				switch d.Kind {
				case KindAdvancedHunting:
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					parts := strings.Split(body["Timespan"], "/")
					if len(parts) == 2 {
						startText, endText = parts[0], parts[1]
					}
				case KindFabricActivity:
					startText, endText = r.URL.Query().Get("startDateTime"), r.URL.Query().Get("endDateTime")
				default:
					parts := strings.Fields(r.URL.Query().Get("$filter"))
					if len(parts) >= 7 {
						startText, endText = parts[2], parts[6]
					}
				}
				lower, e1 := time.Parse(time.RFC3339Nano, strings.Trim(startText, "'"))
				upper, e2 := time.Parse(time.RFC3339Nano, strings.Trim(endText, "'"))
				if e1 != nil || e2 != nil {
					t.Errorf("missing or invalid temporal request: %s/%s", startText, endText)
				}
				rows := []any{}
				stamp := eventTime
				if phase == 3 {
					stamp = stamp.Add(time.Nanosecond)
				}
				if phase > 0 && !stamp.Before(lower) && !stamp.After(upper) {
					record := matrixRecord(d, "late", "Late visible native record")
					record[d.TimeField] = stamp.Format(time.RFC3339Nano)
					rows = append(rows, record)
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"value": rows, "results": rows, "activityEventEntities": rows}); err != nil {
					t.Error(err)
				}
			})
			path := filepath.Join(t.TempDir(), "state")
			c := auditConfig(t, s.URL, d.Name)
			sub := ""
			if strings.Contains(d.Path, "{subscriptionId}") {
				sub = c.Subscription_ID[0]
			}
			key := stateKey(c.StateNamespace(), d.Name, sub)
			rt, closeDB := auditRuntime(t, path)
			if err := saveState(rt, key, streamState{Watermark: anchor, Records: map[string]recordState{}}); err != nil {
				t.Fatal(err)
			}
			closeDB()
			previous := anchor
			for phase = 0; phase < 4; phase++ {
				rt, closeDB = auditRuntime(t, path)
				if phase > 0 {
					c.Lookback = 1
				}
				auditRun(t, c, rt, rt)
				if len(rt.errorLogs) > 0 {
					t.Fatalf("phase %d errors=%v", phase, rt.errorLogs)
				}
				want := []int{0, 1, 0, 1}[phase]
				if len(rt.entries) != want {
					t.Errorf("phase %d entries=%d want=%d", phase, len(rt.entries), want)
				}
				state, err := loadState(rt, key)
				if err != nil {
					t.Fatal(err)
				}
				if state.Watermark.Before(previous) {
					t.Error("watermark moved backward")
				}
				if phase == 0 && !state.Watermark.Equal(anchor) {
					t.Error("empty established anchor moved")
				}
				previous = state.Watermark
				closeDB()
			}
		})
	}
}

func TestAuthoritativeContinuationFamiliesIgnoreOptionalTotals(t *testing.T) {
	for _, name := range []string{"entra-users", "azure-resource-inventory", "fabric-activity"} {
		for _, total := range []string{"absent", "zero", "stale"} {
			t.Run(name+"/"+total, func(t *testing.T) {
				d := datasets[name]
				requests := 0
				s := auditServer(t, func(w http.ResponseWriter, r *http.Request) {
					requests++
					second := r.URL.Query().Get("matrixPage") == "2"
					if d.Kind == KindResourceGraph {
						var b struct {
							Options map[string]any `json:"options"`
						}
						if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
							t.Error(e)
						}
						second = b.Options["$skipToken"] == "two"
					}
					id := "a"
					if second {
						id = "b"
					}
					record := matrixRecord(d, id, "Representative native record")
					reply := map[string]any{}
					if total != "absent" {
						n := 0
						if total == "stale" {
							n = 1
						}
						reply["@odata.count"] = n
						reply["totalRecords"] = n
						reply["count"] = n
					}
					switch d.Kind {
					case KindResourceGraph:
						reply["data"] = []any{record}
						reply["resultTruncated"] = !second
						if !second {
							reply["$skipToken"] = "two"
						}
					case KindFabricActivity:
						reply["activityEventEntities"] = []any{record}
						if !second {
							reply["continuationUri"] = serverURL(r) + r.URL.Path + "?matrixPage=2"
						}
					default:
						reply["value"] = []any{record}
						if !second {
							reply["@odata.nextLink"] = serverURL(r) + r.URL.Path + "?matrixPage=2"
						}
					}
					if e := json.NewEncoder(w).Encode(reply); e != nil {
						t.Error(e)
					}
				})
				conf := auditConfig(t, s.URL, name)
				conf.Max_Pages = 2
				c, e := NewClient(conf, nil)
				if e != nil {
					t.Fatal(e)
				}
				end := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
				records, e := c.Fetch(context.Background(), d, end.Add(-time.Hour), end, "")
				if e != nil || len(records) != 2 || requests != 2 {
					t.Fatalf("records=%d requests=%d err=%v", len(records), requests, e)
				}
			})
		}
	}
}
