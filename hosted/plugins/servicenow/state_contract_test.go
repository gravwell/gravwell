package servicenow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

type serviceNowContractRuntime struct {
	hosted.Runtime
	state             map[string][]byte
	entries           []entry.Entry
	writeCalls        int
	failWriteAt       int
	tagErr            error
	blockContextWrite bool
	writeStarted      chan struct{}
}

func newServiceNowContractRuntime() *serviceNowContractRuntime {
	return &serviceNowContractRuntime{state: map[string][]byte{}}
}

func (r *serviceNowContractRuntime) Get(key string) ([]byte, error) {
	value, ok := r.state[key]
	if !ok {
		return nil, storage.ErrStorageNotFound
	}
	return bytes.Clone(value), nil
}

func (r *serviceNowContractRuntime) Put(key string, value []byte) error {
	r.state[key] = bytes.Clone(value)
	return nil
}

func (r *serviceNowContractRuntime) Write(value entry.Entry) error {
	copy := value
	return r.WriteEntry(&copy)
}

func (r *serviceNowContractRuntime) WriteEntry(value *entry.Entry) error {
	r.writeCalls++
	if r.failWriteAt != 0 && r.writeCalls == r.failWriteAt {
		return errors.New("synthetic write failure")
	}
	copy := *value
	copy.Data = bytes.Clone(value.Data)
	r.entries = append(r.entries, copy)
	return nil
}

func (r *serviceNowContractRuntime) WriteEntryContext(ctx context.Context, value *entry.Entry) error {
	if r.blockContextWrite {
		if r.writeStarted != nil {
			select {
			case <-r.writeStarted:
			default:
				close(r.writeStarted)
			}
		}
		<-ctx.Done()
		return ctx.Err()
	}
	return r.WriteEntry(value)
}

func (r *serviceNowContractRuntime) WriteBatch(values []*entry.Entry) error {
	for _, value := range values {
		if err := r.WriteEntry(value); err != nil {
			return err
		}
	}
	return nil
}

func (r *serviceNowContractRuntime) WriteBatchContext(_ context.Context, values []*entry.Entry) error {
	return r.WriteBatch(values)
}

func (r *serviceNowContractRuntime) NegotiateTag(string) (entry.EntryTag, error) {
	return 42, r.tagErr
}
func (r *serviceNowContractRuntime) Debug(string, ...rfc5424.SDParam)    {}
func (r *serviceNowContractRuntime) Info(string, ...rfc5424.SDParam)     {}
func (r *serviceNowContractRuntime) Warn(string, ...rfc5424.SDParam)     {}
func (r *serviceNowContractRuntime) Error(string, ...rfc5424.SDParam)    {}
func (r *serviceNowContractRuntime) Critical(string, ...rfc5424.SDParam) {}

func serviceNowContractConfig(t *testing.T, instance string) *Config {
	t.Helper()
	return &Config{
		Instance: instance, Secret_File: secretFile(t), Normalization: "disabled",
		Page_Size: 100, Max_Pages: 10, Overlap: intPointer(300), Timeout: 5, Max_Retries: intPointer(1),
		PollingConfig: hosted.PollingConfig{Lookback: 24, Requests_Per_Minute: 60000, Request_Interval: 60},
	}
}

func TestIncrementalEmptyPollRetainsAnchorCatalogWide(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	anchor := now.Add(-24 * time.Hour)
	lateTimestamp := anchor.Add(time.Hour)
	for _, dataset := range Catalog() {
		dataset := dataset
		t.Run(dataset.Name, func(t *testing.T) {
			requests := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests <= 2 {
					_, _ = w.Write([]byte(`{"result":[]}`))
					return
				}
				_, _ = fmt.Fprintf(w, `{"result":[{"sys_id":"late","%s":"%s"}]}`, dataset.Timestamp, lateTimestamp.Format("2006-01-02 15:04:05"))
			}))
			defer server.Close()
			runtime := newServiceNowContractRuntime()
			config := serviceNowContractConfig(t, server.URL)
			client := NewClient(server.URL, config.Secret_File, time.Second, 1, 60000, server.Client().Transport)
			plugin := New(config, processors.NewProcessorSet(runtime))
			plugin.now = func() time.Time { return now }
			if err := plugin.collect(context.Background(), runtime, client, dataset); err != nil {
				t.Fatal(err)
			}
			key := config.StateNamespace() + "/" + dataset.Name
			state, err := loadCursor(runtime, key)
			if err != nil || !state.Checkpoint.Equal(anchor) || !state.WindowEnd.IsZero() {
				t.Fatalf("initial empty state=%+v err=%v", state, err)
			}

			established := New(config, processors.NewProcessorSet(runtime))
			established.now = func() time.Time { return now.Add(24 * time.Hour) }
			if err := established.collect(context.Background(), runtime, client, dataset); err != nil {
				t.Fatal(err)
			}
			state, err = loadCursor(runtime, key)
			if err != nil || !state.Checkpoint.Equal(anchor) || !state.WindowEnd.IsZero() {
				t.Fatalf("established empty state=%+v err=%v", state, err)
			}

			restarted := New(config, processors.NewProcessorSet(runtime))
			restarted.now = func() time.Time { return now.Add(48 * time.Hour) }
			if err := restarted.collect(context.Background(), runtime, client, dataset); err != nil {
				t.Fatal(err)
			}
			if len(runtime.entries) != 1 {
				t.Fatalf("late-visible entries=%d want=1", len(runtime.entries))
			}
			state, err = loadCursor(runtime, key)
			if err != nil || !state.Checkpoint.Equal(lateTimestamp) {
				t.Fatalf("late-visible state=%+v err=%v", state, err)
			}
		})
	}
}

func TestIncrementalCatalogStableTieAndSubsecondBoundary(t *testing.T) {
	checkpoint := time.Date(2026, 9, 21, 10, 0, 0, 987654321, time.UTC)
	windowEnd := checkpoint.Add(time.Hour)
	for _, dataset := range Catalog() {
		query := WindowQuery(dataset, checkpoint, windowEnd, 300)
		for _, want := range []string{
			dataset.Timestamp + ">2026-09-21 09:55:00",
			dataset.Timestamp + "<=2026-09-21 11:00:00",
			"ORDERBY" + dataset.Timestamp,
			"ORDERBYsys_id",
		} {
			if !strings.Contains(query, want) {
				t.Errorf("dataset %s query %q missing %q", dataset.Name, query, want)
			}
		}
	}
}

func TestIncrementalEqualAndSubsecondBoundaryDeduplicatesByIdentity(t *testing.T) {
	response := `{"result":[{"sys_id":"one","sys_updated_on":"2026-09-21T10:00:00.100Z"},{"sys_id":"two","sys_updated_on":"2026-09-21T10:00:00.900Z"}]}`
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	runtime := newServiceNowContractRuntime()
	config := serviceNowContractConfig(t, server.URL)
	client := NewClient(server.URL, config.Secret_File, time.Second, 1, 60000, server.Client().Transport)
	plugin := New(config, processors.NewProcessorSet(runtime))
	plugin.now = func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }
	if err := plugin.collect(context.Background(), runtime, client, catalog["incidents"]); err != nil {
		t.Fatal(err)
	}
	response = `{"result":[{"sys_id":"one","sys_updated_on":"2026-09-21T10:00:00.100Z"},{"sys_id":"two","sys_updated_on":"2026-09-21T10:00:00.900Z"},{"sys_id":"three","sys_updated_on":"2026-09-21T10:00:00.900Z"}]}`
	if err := plugin.collect(context.Background(), runtime, client, catalog["incidents"]); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 3 {
		t.Fatalf("entries=%d want=3", len(runtime.entries))
	}
	state, err := loadCursor(runtime, config.StateNamespace()+"/incidents")
	if err != nil || !state.Checkpoint.Equal(time.Date(2026, 9, 21, 10, 0, 0, 900000000, time.UTC)) {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestPartialPageFailureUsesDurableItemDeduplication(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":[{"sys_id":"one","sys_updated_on":"2026-09-21 10:00:00"},{"sys_id":"two","sys_updated_on":"2026-09-21 10:01:00"}]}`))
	}))
	defer server.Close()
	runtime := newServiceNowContractRuntime()
	runtime.failWriteAt = 2
	config := serviceNowContractConfig(t, server.URL)
	client := NewClient(server.URL, config.Secret_File, time.Second, 1, 60000, server.Client().Transport)
	plugin := New(config, processors.NewProcessorSet(runtime))
	plugin.now = func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }
	if err := plugin.collect(context.Background(), runtime, client, catalog["incidents"]); err == nil {
		t.Fatal("synthetic partial write succeeded")
	}
	if len(runtime.entries) != 1 {
		t.Fatalf("accepted entries=%d want=1", len(runtime.entries))
	}

	runtime.failWriteAt = 0
	restarted := New(config, processors.NewProcessorSet(runtime))
	restarted.now = plugin.now
	if err := restarted.collect(context.Background(), runtime, client, catalog["incidents"]); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 2 || string(runtime.entries[0].Data) == string(runtime.entries[1].Data) {
		t.Fatalf("partial replay entries=%d data=%q", len(runtime.entries), []string{string(runtime.entries[0].Data), string(runtime.entries[len(runtime.entries)-1].Data)})
	}
}

func TestCanceledProcessorWriteDoesNotAdvanceStateAndRetries(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":[{"sys_id":"00000000000000000000000000000001","sys_updated_on":"2026-09-21 10:00:00"}]}`))
	}))
	defer server.Close()

	runtime := newServiceNowContractRuntime()
	runtime.blockContextWrite = true
	runtime.writeStarted = make(chan struct{})
	config := serviceNowContractConfig(t, server.URL)
	client := NewClient(server.URL, config.Secret_File, time.Second, 1, 60000, server.Client().Transport)
	plugin := New(config, processors.NewProcessorSet(runtime))
	plugin.now = func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- plugin.collect(ctx, runtime, client, catalog["incidents"])
	}()
	select {
	case <-runtime.writeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("processor write did not block")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled collect error=%v want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled processor write did not return")
	}
	if len(runtime.entries) != 0 {
		t.Fatalf("canceled write delivered %d entries", len(runtime.entries))
	}
	key := config.StateNamespace() + "/incidents"
	if _, exists, err := loadCursorIfExists(runtime, key); err != nil || exists {
		t.Fatalf("canceled write advanced state: exists=%v err=%v", exists, err)
	}

	runtime.blockContextWrite = false
	restarted := New(config, processors.NewProcessorSet(runtime))
	restarted.now = plugin.now
	if err := restarted.collect(context.Background(), runtime, client, catalog["incidents"]); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 1 {
		t.Fatalf("retried entries=%d want=1", len(runtime.entries))
	}
	state, err := loadCursor(runtime, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Hashes) != 1 || state.Checkpoint.IsZero() {
		t.Fatalf("retry state hashes=%d checkpoint=%v", len(state.Hashes), state.Checkpoint)
	}
}

func TestTablePageCeilingResumesPersistedKeyset(t *testing.T) {
	ids := []string{
		"00000000000000000000000000000001",
		"00000000000000000000000000000002",
		"00000000000000000000000000000003",
	}
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.URL.Query().Get("sysparm_offset"); got != "0" {
			t.Fatalf("offset=%q want=0", got)
		}
		query := r.URL.Query().Get("sysparm_query")
		if requests == 1 {
			if strings.Contains(query, "^NQ") {
				t.Fatalf("initial query unexpectedly contains keyset: %q", query)
			}
			_, _ = fmt.Fprintf(w, `{"result":[{"sys_id":"%s","sys_updated_on":"2026-09-21 10:00:00"},{"sys_id":"%s","sys_updated_on":"2026-09-21 10:01:00"}]}`, ids[0], ids[1])
			return
		}
		for _, part := range []string{"sys_updated_on>2026-09-21 10:01:00", "^NQ", "sys_updated_on=2026-09-21 10:01:00", "sys_id>" + ids[1]} {
			if !strings.Contains(query, part) {
				t.Fatalf("resumed query %q missing %q", query, part)
			}
		}
		_, _ = fmt.Fprintf(w, `{"result":[{"sys_id":"%s","sys_updated_on":"2026-09-21 10:02:00"}]}`, ids[2])
	}))
	defer server.Close()
	runtime := newServiceNowContractRuntime()
	config := serviceNowContractConfig(t, server.URL)
	config.Page_Size = 2
	config.Max_Pages = 1
	client := NewClient(server.URL, config.Secret_File, time.Second, 1, 60000, server.Client().Transport)
	plugin := New(config, processors.NewProcessorSet(runtime))
	plugin.now = func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }
	if err := plugin.collect(context.Background(), runtime, client, catalog["incidents"]); err != nil {
		t.Fatal(err)
	}
	state, err := loadCursor(runtime, config.StateNamespace()+"/incidents")
	if err != nil || state.Offset != 0 || state.PageID != ids[1] || state.PageTimestamp.IsZero() || state.WindowEnd.IsZero() {
		t.Fatalf("ceiling state=%+v err=%v", state, err)
	}
	restarted := New(config, processors.NewProcessorSet(runtime))
	restarted.now = plugin.now
	if err := restarted.collect(context.Background(), runtime, client, catalog["incidents"]); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 3 {
		t.Fatalf("entries=%d want=3", len(runtime.entries))
	}
	state, err = loadCursor(runtime, config.StateNamespace()+"/incidents")
	if err != nil || state.Offset != 0 || state.PageID != "" || !state.PageTimestamp.IsZero() || !state.WindowEnd.IsZero() {
		t.Fatalf("drained state=%+v err=%v", state, err)
	}
}

func TestNoOffsetPageCeilingResumesPersistedContinuation(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"result":[{"id":"two","updated":"2026-09-21T10:01:00Z"}]}`))
			return
		}
		w.Header().Set("Link", `<`+server.URL+r.URL.Path+`?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`{"result":[{"id":"one","updated":"2026-09-21T10:00:00Z"}]}`))
	}))
	defer server.Close()
	runtime := newServiceNowContractRuntime()
	config := serviceNowContractConfig(t, server.URL)
	config.Max_Pages = 1
	dataset := Dataset{Name: "no-offset", Product: "ITSM", Tag: "servicenow-itsm", Timestamp: "updated", REST: &RESTSpec{Path: "/api/no-offset", ResultPath: "result", IDField: "id"}}
	client := NewClient(server.URL, config.Secret_File, time.Second, 1, 60000, server.Client().Transport)
	plugin := New(config, processors.NewProcessorSet(runtime))
	plugin.now = func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }
	if err := plugin.collect(context.Background(), runtime, client, dataset); err != nil {
		t.Fatal(err)
	}
	key := config.StateNamespace() + "/" + dataset.Name
	state, err := loadCursor(runtime, key)
	if err != nil || state.NextURL == "" || state.WindowEnd.IsZero() {
		t.Fatalf("ceiling state=%+v err=%v", state, err)
	}
	restarted := New(config, processors.NewProcessorSet(runtime))
	restarted.now = plugin.now
	if err := restarted.collect(context.Background(), runtime, client, dataset); err != nil {
		t.Fatal(err)
	}
	if len(runtime.entries) != 2 {
		t.Fatalf("entries=%d want=2", len(runtime.entries))
	}
	state, err = loadCursor(runtime, key)
	if err != nil || state.NextURL != "" || !state.WindowEnd.IsZero() {
		t.Fatalf("drained state=%+v err=%v", state, err)
	}
}

func TestMutableSnapshotMutationRepeatAndDeletionContract(t *testing.T) {
	response := `{"result":[{"id":"one","created":"2026-09-20T10:00:00Z","value":"first"}]}`
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	runtime := newServiceNowContractRuntime()
	config := serviceNowContractConfig(t, server.URL)
	dataset := Dataset{Name: "snapshot", Product: "ITSM", Tag: "servicenow-itsm", Timestamp: "created", REST: &RESTSpec{Path: "/api/snapshot", ResultPath: "result", IDField: "id"}}
	client := NewClient(server.URL, config.Secret_File, time.Second, 1, 60000, server.Client().Transport)
	current := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	poll := func() {
		t.Helper()
		plugin := New(config, processors.NewProcessorSet(runtime))
		plugin.now = func() time.Time { return current }
		if err := plugin.collect(context.Background(), runtime, client, dataset); err != nil {
			t.Fatal(err)
		}
		current = current.Add(time.Second)
	}
	poll()
	response = `{"result":[{"id":"one","created":"2026-09-20T10:00:00Z","value":"changed"}]}`
	poll()
	poll()
	if len(runtime.entries) != 2 {
		t.Fatalf("mutation/repeat entries=%d want=2", len(runtime.entries))
	}
	response = `{"result":[]}`
	poll()
	state, err := loadCursor(runtime, config.StateNamespace()+"/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := state.Hashes["one"]; exists || len(runtime.entries) != 2 {
		t.Fatalf("snapshot deletion state=%+v entries=%d", state, len(runtime.entries))
	}
	response = `{"result":[{"id":"one","created":"2026-09-20T10:00:00Z","value":"changed"}]}`
	poll()
	if len(runtime.entries) != 3 {
		t.Fatalf("reappearing snapshot entries=%d want=3", len(runtime.entries))
	}
}

func TestEndpointCatalogMutableSnapshotContract(t *testing.T) {
	for _, dataset := range EndpointCatalog() {
		dataset := dataset
		t.Run(dataset.Name, func(t *testing.T) {
			value := "first"
			present := true
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !present {
					_, _ = w.Write([]byte(`{"result":[]}`))
					return
				}
				if dataset.REST.StaticID != "" {
					_, _ = fmt.Fprintf(w, `{"result":{"stats":{"count":"1"},"sys_updated_on":"2026-09-20 10:00:00","value":%q}}`, value)
					return
				}
				_, _ = fmt.Fprintf(w, `{"result":[{"sys_id":"one","sys_updated_on":"2026-09-20 10:00:00","value":%q}]}`, value)
			}))
			defer server.Close()
			runtime := newServiceNowContractRuntime()
			config := serviceNowContractConfig(t, server.URL)
			client := NewClient(server.URL, config.Secret_File, time.Second, 1, 60000, server.Client().Transport)
			current := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
			poll := func() {
				t.Helper()
				plugin := New(config, processors.NewProcessorSet(runtime))
				plugin.now = func() time.Time { return current }
				if err := plugin.collect(context.Background(), runtime, client, dataset); err != nil {
					t.Fatal(err)
				}
				current = current.Add(time.Second)
			}
			poll()
			value = "changed"
			poll()
			poll()
			if len(runtime.entries) != 2 {
				t.Fatalf("mutation/repeat entries=%d want=2", len(runtime.entries))
			}
			present = false
			poll()
			if len(runtime.entries) != 2 {
				t.Fatalf("deletion emitted tombstone entries=%d", len(runtime.entries))
			}
			state, err := loadCursor(runtime, config.StateNamespace()+"/"+dataset.Name)
			if err != nil {
				t.Fatal(err)
			}
			identity := "one"
			if dataset.REST.StaticID != "" {
				identity = dataset.REST.StaticID
			}
			if _, exists := state.Hashes[identity]; exists {
				t.Fatalf("absent snapshot identity %q retained", identity)
			}
			present = true
			poll()
			if len(runtime.entries) != 3 {
				t.Fatalf("reappearing snapshot entries=%d want=3", len(runtime.entries))
			}
		})
	}
}

func TestNormalizedFallbackStateCopiesWithoutDeletingSourceState(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer server.Close()
	runtime := newServiceNowContractRuntime()
	checkpoint := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	fallbackKey := "servicenow-normalization-v2/incidents"
	encoded, err := json.Marshal(cursorState{Checkpoint: checkpoint, Seen: map[string]time.Time{}, Hashes: map[string][32]byte{}})
	if err != nil {
		t.Fatal(err)
	}
	runtime.state[fallbackKey] = encoded
	config := serviceNowContractConfig(t, server.URL)
	config.Normalization = "enabled"
	client := NewClient(server.URL, config.Secret_File, time.Second, 1, 60000, server.Client().Transport)
	plugin := New(config, processors.NewProcessorSet(runtime))
	plugin.now = func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }
	if err := plugin.collect(context.Background(), runtime, client, catalog["incidents"]); err != nil {
		t.Fatal(err)
	}
	currentKey := config.StateNamespace() + "/incidents"
	state, err := loadCursor(runtime, currentKey)
	if err != nil || !state.Checkpoint.Equal(checkpoint) {
		t.Fatalf("copied state=%+v err=%v", state, err)
	}
	if _, exists := runtime.state[fallbackKey]; !exists {
		t.Fatal("fallback source state was deleted")
	}
}

func TestSourceFailureDoesNotStarveLaterDataset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/now/table/incident" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"result":[{"sys_id":"problem","sys_updated_on":"2026-09-21 10:00:00"}]}`))
	}))
	defer server.Close()
	runtime := newServiceNowContractRuntime()
	config := serviceNowContractConfig(t, server.URL)
	config.Table = []string{"incident", "problem"}
	plugin := New(config, processors.NewProcessorSet(runtime))
	plugin.now = func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }
	if _, err := plugin.Handle(context.Background(), runtime); err == nil {
		t.Fatal("failing source was not reported")
	}
	if len(runtime.entries) != 1 || !bytes.Contains(runtime.entries[0].Data, []byte(`"problem"`)) {
		t.Fatalf("later source entries=%d data=%v", len(runtime.entries), runtime.entries)
	}
}

func TestTagNegotiationFailurePreventsDeliveryAndCheckpointAdvance(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":[{"sys_id":"one","sys_updated_on":"2026-09-21 10:00:00"}]}`))
	}))
	defer server.Close()
	runtime := newServiceNowContractRuntime()
	runtime.tagErr = errors.New("synthetic tag failure")
	config := serviceNowContractConfig(t, server.URL)
	client := NewClient(server.URL, config.Secret_File, time.Second, 1, 60000, server.Client().Transport)
	plugin := New(config, processors.NewProcessorSet(runtime))
	plugin.now = func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }
	if err := plugin.collect(context.Background(), runtime, client, catalog["incidents"]); !errors.Is(err, runtime.tagErr) {
		t.Fatalf("error=%v", err)
	}
	if len(runtime.entries) != 0 {
		t.Fatalf("entries=%d", len(runtime.entries))
	}
	if _, exists := runtime.state[config.StateNamespace()+"/incidents"]; exists {
		t.Fatal("tag failure persisted checkpoint state")
	}
}
