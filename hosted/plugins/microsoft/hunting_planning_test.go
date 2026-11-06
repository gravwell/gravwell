package microsoft

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestHuntingSnapshotPlansPartitionsBeforePublishing(t *testing.T) {
	overflow := huntingOverflowResults()
	record := huntingSnapshotRecord("CVE-1")
	var calls, plan16Overflow, partition32Calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		query := huntingRequestQuery(t, r)
		switch {
		case !strings.Contains(query, "hash(strcat("):
			fmt.Fprintf(w, `{"results":%s}`, overflow)
		case huntingPartitionQuery(query, 16, 0):
			fmt.Fprintf(w, `{"results":[%s]}`, record)
		case huntingPartitionQuery(query, 16, 1):
			plan16Overflow++
			fmt.Fprintf(w, `{"results":%s}`, overflow)
		case huntingPartitionQuery(query, 32, 0):
			partition32Calls++
			fmt.Fprintf(w, `{"results":[%s]}`, record)
		case strings.Contains(query, ", 32) =="):
			partition32Calls++
			fmt.Fprint(w, `{"results":[]}`)
		default:
			t.Errorf("unexpected query %q", query)
			http.Error(w, "unexpected query", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	dataset := datasets["defender-xdr-vulnerability"]
	client.tokens[dataset.Scope] = cachedToken{Value: "token", Expires: time.Now().Add(time.Hour)}
	chunk, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !chunk.Complete || chunk.Continuation != nil || len(chunk.Records) != 1 {
		t.Fatalf("chunk complete=%t continuation=%+v records=%d", chunk.Complete, chunk.Continuation, len(chunk.Records))
	}
	if chunk.Records[0].ID != "device\x1fvendor\x1fname\x1f1\x1fCVE-1" {
		t.Fatalf("record ID=%q", chunk.Records[0].ID)
	}
	if plan16Overflow != 1 || partition32Calls != 64 || calls != 67 {
		t.Fatalf("calls=%d plan16Overflow=%d partition32Calls=%d", calls, plan16Overflow, partition32Calls)
	}
}

func TestHuntingAppendCompletesWhenWorkBudgetDrainsFinalWindow(t *testing.T) {
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"results":[]}`)
	}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	dataset := datasets["defender-xdr-device-events"]
	client.tokens[dataset.Scope] = cachedToken{Value: "token", Expires: time.Now().Add(time.Hour)}
	start := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	windows := make([]FetchWindow, defaultMaxPages)
	for i := range windows {
		windows[i] = FetchWindow{Start: start.Add(time.Duration(i) * time.Second), End: start.Add(time.Duration(i+1) * time.Second)}
	}
	chunk, err := client.fetchHuntingChunk(t.Context(), dataset, windows[0].Start, windows[len(windows)-1].End,
		&FetchContinuation{Version: fetchContinuationVersion, Kind: KindAdvancedHunting, HuntingWindows: windows})
	if err != nil {
		t.Fatal(err)
	}
	if !chunk.Complete || chunk.Continuation != nil || calls != defaultMaxPages {
		t.Fatalf("complete=%t continuation=%+v calls=%d", chunk.Complete, chunk.Continuation, calls)
	}
}

func TestHuntingSnapshotOverflowAfterSuccessfulPartitionPublishesNothing(t *testing.T) {
	overflow := huntingOverflowResults()
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		query := huntingRequestQuery(t, r)
		if huntingPartitionQuery(query, 16, 0) {
			fmt.Fprintf(w, `{"results":[%s]}`, huntingSnapshotRecord("CVE-1"))
			return
		}
		if huntingPartitionQuery(query, 16, 1) {
			fmt.Fprintf(w, `{"results":%s}`, overflow)
			return
		}
		t.Errorf("unexpected query %q", query)
		http.Error(w, "unexpected query", http.StatusBadRequest)
	}))
	defer server.Close()
	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	dataset := datasets["defender-xdr-vulnerability"]
	client.tokens[dataset.Scope] = cachedToken{Value: "token", Expires: time.Now().Add(time.Hour)}
	prior := &FetchContinuation{Version: fetchContinuationVersion, Kind: KindAdvancedHunting, HuntingPartitions: 16}
	chunk, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", prior)
	if err == nil || !strings.Contains(err.Error(), "exceeded its preflight capacity") {
		t.Fatalf("overflow error=%v", err)
	}
	if len(chunk.Records) != 0 || calls != 2 {
		t.Fatalf("overflow chunk records=%d calls=%d", len(chunk.Records), calls)
	}
}

func TestHuntingSnapshotRestartResumesPlannedPartition(t *testing.T) {
	var partitionZero, dataCalls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		dataCalls++
		query := huntingRequestQuery(t, r)
		if huntingPartitionQuery(query, 16, 0) {
			partitionZero++
		}
		if huntingPartitionQuery(query, 16, 1) {
			fmt.Fprintf(w, `{"results":[%s]}`, huntingSnapshotRecord("CVE-restart"))
			return
		}
		fmt.Fprint(w, `{"results":[]}`)
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime(t)
	p := newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"defender-xdr-vulnerability"}
	now := p.now()
	state := streamState{
		WindowStart: now.Add(-time.Hour), WindowEnd: now,
		Continuation: &FetchContinuation{
			Version: fetchContinuationVersion, Kind: KindAdvancedHunting,
			HuntingPartitions: 16, HuntingPartition: 1,
		},
	}
	if err := saveState(rt, "microsoft/defender-xdr-vulnerability", state); err != nil {
		t.Fatal(err)
	}
	// Recreate from durable state before running the remaining data partitions.
	p = newMicrosoftTestPlugin(t, server, rt)
	p.conf.Api = []string{"defender-xdr-vulnerability"}
	if _, err := p.Handle(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	if partitionZero != 0 || dataCalls != 15 || len(rt.Entries()) != 1 {
		t.Fatalf("partitionZero=%d dataCalls=%d entries=%d", partitionZero, dataCalls, len(rt.Entries()))
	}
	saved, err := loadState(rt, "microsoft/defender-xdr-vulnerability")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Continuation != nil {
		t.Fatalf("completed restart retained continuation: %+v", saved.Continuation)
	}
}

func huntingRequestQuery(t *testing.T, r *http.Request) string {
	t.Helper()
	var request map[string]string
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Errorf("decode hunting request: %v", err)
		return ""
	}
	return request["Query"]
}

func huntingPartitionQuery(query string, partitions, partition int) bool {
	return strings.Contains(query, fmt.Sprintf(", %d) == %d | take", partitions, partition))
}

func huntingOverflowResults() string {
	record := huntingSnapshotRecord("overflow")
	return "[" + strings.TrimSuffix(strings.Repeat(record+",", defaultPageSize*defaultMaxPages+1), ",") + "]"
}

func huntingSnapshotRecord(cve string) string {
	return fmt.Sprintf(`{"DeviceId":"device","SoftwareVendor":"vendor","SoftwareName":"name","SoftwareVersion":"1","CveId":%q}`, cve)
}
