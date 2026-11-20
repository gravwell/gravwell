package microsoft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

type closeProbe struct {
	io.Reader
	closed bool
}

func (b *closeProbe) Close() error {
	b.closed = true
	return nil
}

type staticDoer func(*http.Request) (*http.Response, error)

func (f staticDoer) Do(r *http.Request) (*http.Response, error) { return f(r) }

func syntheticResponse(body string) *http.Response {
	return syntheticBody(io.NopCloser(strings.NewReader(body)))
}

func syntheticBody(body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}
}

// stalledDispatch reports what net/http reports when a request is abandoned
// because its context ended: a *url.Error wrapping the context error rather
// than the sentinel itself.
func stalledDispatch(r *http.Request) error {
	return &url.Error{Op: r.Method, URL: r.URL.String(), Err: r.Context().Err()}
}

func TestSharedRetryClientStopsWhenContextIsCanceled(t *testing.T) {
	conf := testClientConfig(t, "https://127.0.0.1:1")
	client, err := NewClient(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	dataset := datasets["entra-users"]
	client.tokens[dataset.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err = client.request(ctx, http.MethodGet, conf.Graph_Host+dataset.Path, dataset.Scope, nil)
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("shared retry did not stop at context deadline: error=%v context=%v", err, ctx.Err())
	}
}

func TestFetchChunkDeadlineBoundsPermanentRetryableResponses(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			// The fetch deadline abandons an attempt while its handler may still
			// be running, so the counter must be safe to read from the test
			// goroutine without a happens-before edge from the response.
			var calls atomic.Int64
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
			}))
			defer server.Close()
			prior := http.DefaultTransport
			http.DefaultTransport = server.Client().Transport
			t.Cleanup(func() { http.DefaultTransport = prior })
			client, err := NewClient(testClientConfig(t, server.URL), nil)
			if err != nil {
				t.Fatal(err)
			}
			client.limiter = rate.NewLimiter(rate.Inf, 1)
			client.fetchDeadline = 25 * time.Millisecond
			client.retryTimeout = time.Second
			client.retryBackoff = time.Millisecond
			dataset := datasets["entra-users"]
			client.tokens[dataset.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
			started := time.Now()
			chunk, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", nil)
			// The deadline still bounds the retry loop, and the page it gave up
			// on is now checkpointed instead of discarded: the plugin owns this
			// deadline, so reaching it is a pacing yield rather than a failure.
			if err != nil {
				t.Fatalf("status %d error=%v, want a deferred checkpoint", status, err)
			}
			if chunk.Complete || chunk.Continuation == nil || chunk.Continuation.NextURL == "" || len(chunk.Records) != 0 {
				t.Fatalf("status %d chunk=%+v cursor=%+v, want an incomplete chunk holding a resumable cursor", status, chunk, chunk.Continuation)
			}
			if seen := calls.Load(); seen < 2 || time.Since(started) > time.Second {
				t.Fatalf("status %d calls=%d duration=%s", status, seen, time.Since(started))
			}
		})
	}
}

func TestMicrosoftRetryAfterOutlivesLocalBackoff(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := NewClient(testClientConfig(t, server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.fetchDeadline = 30 * time.Millisecond
	client.retryBackoff = time.Millisecond
	dataset := datasets["entra-users"]
	client.tokens[dataset.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}

	chunk, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", nil)
	if err != nil {
		t.Fatalf("error=%v, want a deferred checkpoint", err)
	}
	if chunk.Complete || chunk.Continuation == nil || len(chunk.Records) != 0 {
		t.Fatalf("chunk=%+v, want an incomplete resumable chunk", chunk)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls=%d, want Retry-After to prevent a retry before the fetch deadline", got)
	}
}

// TestFetchDeadlinePreservesCompletedState pins what one pass keeps when an
// HTTP request crosses the plugin-owned fetch deadline in flight, part way
// through a multi-page window: every page already read is delivered, the cursor
// that resumes the abandoned page is persisted, and the completed-window
// watermark stays where it was until the window actually finishes. Discarding
// the chunk instead -- which is what context.DeadlineExceeded reaching the page
// loop did -- threw away the completed pages and left a slow window re-reading
// the same ones on every pass without ever checkpointing.
func TestFetchDeadlinePreservesCompletedState(t *testing.T) {
	const pages = 4
	const auditPath = "/v1.0/auditLogs/directoryAudits"
	var requests, stalled atomic.Int64
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Fprint(w, `{"access_token":"token","expires_in":3600}`)
			return
		}
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				http.Error(w, "unreadable page", http.StatusBadRequest)
				return
			}
			page = parsed
		}
		requests.Add(1)
		// Page 3 stalls the first time it is asked for, so the fetch deadline
		// arrives while that request is in flight and pages 1 and 2 have already
		// been decoded into the chunk.
		if page == 3 && stalled.Add(1) == 1 {
			<-r.Context().Done()
			return
		}
		next := ""
		if page < pages {
			next = fmt.Sprintf(`,"@odata.nextLink":%q`, server.URL+auditPath+"?page="+strconv.Itoa(page+1))
		}
		fmt.Fprintf(w, `{"value":[{"id":"audit-%d","activityDateTime":"2026-10-07T10:30:00Z"}]%s}`, page, next)
	}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	conf.Api = []string{"entra-directory-audits"}
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	// Two instant pages and a stalled third inside one second: the stall is what
	// ends the pass, not the pages before it.
	client.fetchDeadline = time.Second
	client.retryBackoff = time.Millisecond
	rt := newMicrosoftTestRuntime(t)
	watermark := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	if err := rt.PutTime("microsoft/entra-directory-audits/timestamp", watermark); err != nil {
		t.Fatal(err)
	}
	p := New(conf)
	p.client = client
	p.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }

	continuation, err := p.Handle(t.Context(), rt)
	if err != nil || continuation == nil || continuation.Delay != 0 {
		t.Fatalf("stalled pass error=%v continuation=%v, want an immediate retry", err, continuation)
	}
	if got := len(rt.Entries()); got != 2 {
		t.Fatalf("stalled pass wrote %d entries, want the 2 pages it completed", got)
	}
	got, err := rt.GetTime("microsoft/entra-directory-audits/timestamp")
	if err != nil || !got.Equal(watermark) {
		t.Fatalf("watermark=%s error=%v, want unchanged %s", got, err, watermark)
	}
	state, err := loadState(rt, "microsoft/entra-directory-audits")
	if err != nil || state.Continuation == nil || !strings.Contains(state.Continuation.NextURL, "page=3") {
		t.Fatalf("stalled pass state=%+v cursor=%+v error=%v, want a cursor resuming page 3", state, state.Continuation, err)
	}
	if state.WindowEnd.IsZero() || !state.WindowStart.Before(state.WindowEnd) {
		t.Fatalf("stalled pass window=[%s,%s], want the window it was reading", state.WindowStart, state.WindowEnd)
	}

	if continuation, err = p.Handle(t.Context(), rt); err != nil || continuation == nil || continuation.Delay == 0 {
		t.Fatalf("resumed pass error=%v continuation=%v, want a completed window", err, continuation)
	}
	delivered := map[string]int{}
	for _, item := range rt.Entries() {
		delivered[string(item.Data)]++
	}
	if len(delivered) != pages {
		t.Fatalf("delivered %d distinct records across both passes, want %d", len(delivered), pages)
	}
	for payload, count := range delivered {
		if count != 1 {
			t.Fatalf("record %s delivered %d times", payload, count)
		}
	}
	for page := 1; page <= pages; page++ {
		want := fmt.Sprintf(`"id":"audit-%d"`, page)
		found := false
		for payload := range delivered {
			if strings.Contains(payload, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("page %d was never delivered", page)
		}
	}
	// Only the stalled page is re-read, and the window closed past the pages the
	// first pass had already committed.
	if got := requests.Load(); got != pages+1 {
		t.Fatalf("source requests=%d, want %d: a page was skipped or re-read", got, pages+1)
	}
	final, err := loadState(rt, "microsoft/entra-directory-audits")
	if err != nil || final.Continuation != nil || !final.Watermark.After(watermark) {
		t.Fatalf("resumed state=%+v error=%v, want a cleared cursor and an advanced watermark", final, err)
	}
}

func TestFetchDeadlineCancelsSlowRequest(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()
	prior := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = prior })
	client, err := NewClient(testClientConfig(t, server.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.fetchDeadline = 5 * time.Second
	dataset := datasets["entra-users"]
	client.tokens[dataset.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
	chunk, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", nil)
	// The request is still abandoned at the plugin-owned deadline. What the
	// chunk carries back is a cursor the next pass can resume, not an error that
	// throws the window away.
	if err != nil {
		t.Fatalf("slow request error=%v, want a deferred checkpoint", err)
	}
	if chunk.Complete || chunk.Continuation == nil || chunk.Continuation.NextURL == "" {
		t.Fatalf("slow request chunk=%+v cursor=%+v, want a resumable cursor", chunk, chunk.Continuation)
	}
	select {
	case <-started:
	default:
		t.Fatal("slow request was not dispatched")
	}
}

// TestInFlightDeadlineKeepsCompletedPagesOnTheDirectHTTPPath exercises the
// httpDoer branch of Client.do with an in-memory page set, so the pages the
// chunk completes before the deadline are fixed rather than a function of host
// timing. The cursor is round-tripped through JSON between passes because that
// is how the runner actually resumes it.
func TestInFlightDeadlineKeepsCompletedPagesOnTheDirectHTTPPath(t *testing.T) {
	const pages = 3
	const host = "https://graph.example.invalid"
	client, err := NewClient(testClientConfig(t, host), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.fetchDeadline = 60 * time.Millisecond
	dataset := datasets["entra-users"]
	client.tokens[dataset.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
	pageURL := func(page int) string { return host + "/v1.0/users?page=" + strconv.Itoa(page) }
	// Do runs on the calling goroutine, so this bookkeeping needs no
	// synchronization of its own.
	var requested []string
	stalls := 0
	client.http = staticDoer(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				return nil, err
			}
			page = parsed
		}
		if page == pages && stalls == 0 {
			stalls++
			<-r.Context().Done()
			return nil, stalledDispatch(r)
		}
		next := ""
		if page < pages {
			next = fmt.Sprintf(`,"@odata.nextLink":%q`, pageURL(page+1))
		}
		return syntheticResponse(fmt.Sprintf(`{"value":[{"id":"user-%d"}]%s}`, page, next)), nil
	})

	chunk, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", nil)
	if err != nil {
		t.Fatalf("stalled dispatch error=%v, want a deferred checkpoint", err)
	}
	if chunk.Complete || chunk.Continuation == nil || len(chunk.Records) != pages-1 {
		t.Fatalf("chunk=%+v cursor=%+v, want the %d completed pages and a cursor", chunk, chunk.Continuation, pages-1)
	}
	for i, record := range chunk.Records {
		if want := fmt.Sprintf("user-%d", i+1); record.ID != want {
			t.Fatalf("record %d ID=%q, want %q", i, record.ID, want)
		}
	}
	if chunk.Continuation.NextURL != pageURL(pages) {
		t.Fatalf("cursor NextURL=%q, want the stalled page %q", chunk.Continuation.NextURL, pageURL(pages))
	}

	encoded, err := json.Marshal(chunk.Continuation)
	if err != nil {
		t.Fatal(err)
	}
	var resumed FetchContinuation
	if err := json.Unmarshal(encoded, &resumed); err != nil {
		t.Fatal(err)
	}
	final, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", &resumed)
	if err != nil || !final.Complete || len(final.Records) != 1 || final.Records[0].ID != fmt.Sprintf("user-%d", pages) {
		t.Fatalf("resumed chunk=%+v error=%v, want page %d and completion", final, err, pages)
	}
	if len(requested) != pages+1 || requested[len(requested)-1] != pageURL(pages) {
		t.Fatalf("requested=%v, want each page once and the stalled page twice", requested)
	}
}

// TestDeadlineAfterAResponseDefersAndClosesTheBody covers the post-response
// deadline check in Client.request: the response arrived, the plugin-owned
// deadline is already gone, and the chunk must checkpoint without leaving the
// body it will not decode open.
func TestDeadlineAfterAResponseDefersAndClosesTheBody(t *testing.T) {
	client, err := NewClient(testClientConfig(t, "https://graph.example.invalid"), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.fetchDeadline = 40 * time.Millisecond
	dataset := datasets["entra-users"]
	client.tokens[dataset.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
	body := &closeProbe{Reader: strings.NewReader(`{"value":[{"id":"user-1"}]}`)}
	client.http = staticDoer(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return syntheticBody(body), nil
	})
	chunk, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", nil)
	if err != nil {
		t.Fatalf("late response error=%v, want a deferred checkpoint", err)
	}
	if chunk.Complete || chunk.Continuation == nil || chunk.Continuation.NextURL == "" || len(chunk.Records) != 0 {
		t.Fatalf("chunk=%+v cursor=%+v, want an undecoded page and a resumable cursor", chunk, chunk.Continuation)
	}
	if !body.closed {
		t.Fatal("the deadline yield left the response body open")
	}
}

// TestObjectFetchDeadlineStaysAHardFailure pins the one fetch shape that has no
// cursor to resume. A single-request object fetch cannot checkpoint, so the
// deadline has to stay the failure it has always been instead of leaking the
// internal pacing yield to a caller that would read it as progress.
func TestObjectFetchDeadlineStaysAHardFailure(t *testing.T) {
	client, err := NewClient(testClientConfig(t, "https://defender.example.invalid"), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.fetchDeadline = 40 * time.Millisecond
	dataset := datasets["defender-endpoint-exposure-score"]
	if dataset.Kind != KindObjectGET {
		t.Fatalf("dataset kind=%q, want %q", dataset.Kind, KindObjectGET)
	}
	client.tokens[dataset.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
	client.http = staticDoer(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, stalledDispatch(r)
	})
	chunk, err := client.FetchChunk(t.Context(), dataset, time.Time{}, time.Time{}, "", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("object fetch error=%v, want a deadline failure", err)
	}
	if errors.Is(err, errFetchPacing) {
		t.Fatal("object fetch leaked the internal pacing yield to its caller")
	}
	if chunk.Complete || chunk.Continuation != nil || len(chunk.Records) != 0 {
		t.Fatalf("object fetch chunk=%+v, want nothing deliverable", chunk)
	}
}

// TestCallerOwnedDeadlineIsNotDeferred pins the ownership boundary. Only the
// deadline withFetchDeadline installs becomes a pacing yield; a caller whose own
// deadline is at least as tight is asking the fetch to stop, not to resume.
func TestCallerOwnedDeadlineIsNotDeferred(t *testing.T) {
	client, err := NewClient(testClientConfig(t, "https://graph.example.invalid"), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.fetchDeadline = 5 * time.Second
	dataset := datasets["entra-users"]
	client.tokens[dataset.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
	client.http = staticDoer(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, stalledDispatch(r)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	chunk, err := client.FetchChunk(ctx, dataset, time.Time{}, time.Time{}, "", nil)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errFetchPacing) {
		t.Fatalf("caller deadline error=%v, want context.DeadlineExceeded", err)
	}
	if chunk.Continuation != nil || len(chunk.Records) != 0 {
		t.Fatalf("caller deadline chunk=%+v, want no checkpoint", chunk)
	}
}

func TestFetchHonorsCancellationBeforeAndDuringDataRequest(t *testing.T) {
	var calls int
	dataStarted := make(chan struct{}, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		dataStarted <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()
	prior := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = prior })
	client, err := NewClient(testClientConfig(t, server.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	dataset := datasets["entra-users"]
	client.tokens[dataset.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.FetchChunk(canceled, dataset, time.Time{}, time.Time{}, "", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled fetch error=%v", err)
	}
	if calls != 0 {
		t.Fatalf("pre-canceled fetch dispatched %d requests", calls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := client.FetchChunk(ctx, dataset, time.Time{}, time.Time{}, "", nil)
		done <- err
	}()
	<-dataStarted
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight cancellation error=%v", err)
	}
}

func TestFetchHonorsCancellationDuringAuthentication(t *testing.T) {
	authStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authStarted <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	prior := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = prior })
	client, err := NewClient(testClientConfig(t, server.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := client.FetchChunk(ctx, datasets["entra-users"], time.Time{}, time.Time{}, "", nil)
		done <- err
	}()
	<-authStarted
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("authentication cancellation error=%v", err)
	}
}

func TestAuthFailureDeadlineDoesNotBlockLaterSelector(t *testing.T) {
	var managementAuth, graphAuth, graphData int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "management.azure.com") {
				managementAuth++
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			graphAuth++
			fmt.Fprint(w, `{"access_token":"graph-token","expires_in":3600}`)
			return
		}
		if r.URL.Path == datasets["entra-users"].Path {
			graphData++
			fmt.Fprint(w, `{"value":[{"id":"user"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	prior := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = prior })
	conf := testClientConfig(t, server.URL)
	conf.Api = []string{"azure-activity", "entra-users"}
	conf.Subscription_ID = []string{"33333333-3333-4333-8333-333333333333"}
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.fetchDeadline = 20 * time.Millisecond
	client.retryBackoff = time.Millisecond
	p := New(conf)
	p.client = client
	p.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
	rt := newMicrosoftTestRuntime(t)
	// The ARM selector spends its whole fetch deadline retrying a failing token
	// endpoint and yields a resumable checkpoint, exactly as it would have at the
	// pacing or work budget. The Graph selector behind it still runs to
	// completion in the same pass, which is what this regression is about.
	continuation, err := p.Handle(t.Context(), rt)
	if err != nil || continuation == nil || continuation.Delay != 0 {
		t.Fatalf("Handle error=%v continuation=%v, want the ARM selector deferred for an immediate retry", err, continuation)
	}
	if managementAuth < 2 || graphAuth < 1 || graphData != 1 || len(rt.Entries()) != 1 {
		t.Fatalf("managementAuth=%d graphAuth=%d graphData=%d entries=%d", managementAuth, graphAuth, graphData, len(rt.Entries()))
	}
	armState, err := loadState(rt, "microsoft/azure-activity/33333333-3333-4333-8333-333333333333")
	if err != nil || armState.Continuation == nil || armState.Continuation.NextURL == "" {
		t.Fatalf("ARM state=%+v error=%v, want the failed selector to keep a resumable cursor", armState, err)
	}
	if graphState, err := loadState(rt, "microsoft/entra-users"); err != nil || graphState.Continuation != nil || graphState.Watermark.IsZero() {
		t.Fatalf("Graph state=%+v error=%v, want a completed window behind the deferred selector", graphState, err)
	}
}

func TestSharedRetryReplaysOAuthPostBody(t *testing.T) {
	var bodies []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"synthetic","expires_in":3600}`)
	}))
	defer server.Close()
	prior := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = prior })
	client, err := NewClient(testClientConfig(t, server.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.retryBackoff = time.Millisecond
	if _, err := client.token(t.Context(), "synthetic-scope"); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || bodies[0] == "" || bodies[1] != bodies[0] {
		t.Fatalf("OAuth retry bodies=%q", bodies)
	}
}

func TestSharedRetryReplaysDataPostBody(t *testing.T) {
	var bodies []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	prior := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = prior })
	client, err := NewClient(testClientConfig(t, server.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.retryBackoff = time.Millisecond
	scope := "https://graph.microsoft.com/.default"
	client.tokens[scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
	want := `{"query":"DeviceEvents | take 1"}`
	if _, err := client.request(t.Context(), http.MethodPost, server.URL+"/post", scope, []byte(want)); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || bodies[0] != want || bodies[1] != want {
		t.Fatalf("data retry bodies=%q", bodies)
	}
}

func TestResponseBodyClosesOnHTTPFailure(t *testing.T) {
	client, err := NewClient(testClientConfig(t, "https://source.invalid"), nil)
	if err != nil {
		t.Fatal(err)
	}
	dataset := datasets["entra-users"]
	client.tokens[dataset.Scope] = cachedToken{Value: "synthetic", Expires: time.Now().Add(time.Hour)}
	body := &closeProbe{Reader: strings.NewReader(`{"error":"synthetic"}`)}
	client.http = staticDoer(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: body}, nil
	})
	if _, err := client.request(t.Context(), http.MethodGet, "https://source.invalid/v1.0/users", dataset.Scope, nil); err == nil {
		t.Fatal("HTTP failure accepted")
	}
	if !body.closed {
		t.Fatal("HTTP failure body was not closed")
	}
}

func TestContinuationCannotSwitchToAnotherConfiguredService(t *testing.T) {
	conf := testClientConfig(t, "https://graph.example.invalid")
	conf.ARM_Host = "https://management.example.invalid"
	client, err := NewClient(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.resolveContinuation("https://graph.example.invalid/v1.0/users", "https://management.example.invalid/next"); err == nil {
		t.Fatal("continuation can send the Graph token to another configured origin")
	}
}
