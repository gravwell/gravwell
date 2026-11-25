package microsoft

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"golang.org/x/time/rate"
)

const fairnessRequestInterval = 60

// newFairnessTestPlugin builds a stanza holding one Management Activity
// selector and one ordinary sibling selector against the same server.
func newFairnessTestPlugin(t *testing.T, server *httptest.Server, now func() time.Time, selectors ...string) *Microsoft {
	t.Helper()
	conf := &Config{
		Tenant_ID:            "11111111-1111-1111-1111-111111111111",
		Client_ID:            "22222222-2222-2222-2222-222222222222",
		Client_Secret:        "synthetic-client-secret",
		Publisher_Identifier: managementTestPublisher,
		Api:                  selectors,
		Graph_Host:           server.URL,
		ARM_Host:             server.URL,
		Defender_Host:        server.URL,
		Defender_XDR_Host:    server.URL,
		PowerBI_Host:         server.URL,
		Management_Host:      server.URL,
		Auth_Host:            server.URL,
		PollingConfig: hosted.PollingConfig{
			Lookback: 24, Requests_Per_Minute: 6000, Request_Interval: fairnessRequestInterval,
		},
	}
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	plugin := NewWithStateSync(conf, func() error { return nil })
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.retryBackoff = 0
	plugin.client = client
	plugin.now = now
	return plugin
}

// TestLongManagementCatchUpDoesNotStarveSiblingSelectors pins the scheduling
// fairness bound. Handle re-arms immediate-drain mode on every pass that still
// has pending work and a drain pass skips every target with none, while a
// Management Activity catch-up reports pending work for its whole backlog. The
// two together starved every sibling selector in the stanza for the entire
// drain, which for sibling Management selectors is permanent audit-history loss
// once their checkpoints age past the source's seven-day retention. A complete
// pass must therefore still happen at least once per configured poll interval,
// and the catch-up must keep running immediately in between.
func TestLongManagementCatchUpDoesNotStarveSiblingSelectors(t *testing.T) {
	var siblingCalls, listCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			_, _ = io.WriteString(w, `{"access_token":"token","expires_in":3600}`)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			// Every catch-up window lists empty, so the backlog advances one
			// bounded window per pass without any content delivery.
			listCalls.Add(1)
			_, _ = io.WriteString(w, `[]`)
		case strings.HasSuffix(r.URL.Path, "/v1.0/users"):
			siblingCalls.Add(1)
			_, _ = io.WriteString(w, `{"value":[{"id":"user-1","displayName":"One"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	clock := managementTestNow
	rt := newMicrosoftTestRuntime(t)
	plugin := newFairnessTestPlugin(t, server, func() time.Time { return clock },
		"microsoft365-audit-exchange", "entra-users")

	// Six days of backlog inside the source's retention window. Each pass
	// finalizes one bounded window, so the drain spans several passes.
	managementKey := managementStateKey("microsoft", "microsoft365-audit-exchange")
	encoded, err := json.Marshal(managementState{
		Version: managementStateVersion, CompletedThrough: managementTestNow.Add(-6 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Put(managementKey, encoded); err != nil {
		t.Fatal(err)
	}

	// The clock advances less than the poll interval per pass, so fairness
	// cannot be satisfied by wall-clock drift alone.
	const step = (fairnessRequestInterval / 3) * time.Second
	type pass struct {
		immediate      bool
		siblingPolls   int64
		listPolls      int64
		completedThrou time.Time
	}
	var passes []pass
	previousSibling, previousList := int64(0), int64(0)
	for i := 0; i < 7; i++ {
		continuation, err := plugin.Handle(t.Context(), rt)
		if err != nil {
			t.Fatalf("pass %d: %v", i+1, err)
		}
		if continuation == nil {
			t.Fatalf("pass %d returned no continuation", i+1)
		}
		state, err := loadManagementState(rt, managementKey)
		if err != nil {
			t.Fatal(err)
		}
		sibling, list := siblingCalls.Load(), listCalls.Load()
		passes = append(passes, pass{
			immediate:      continuation.Delay == 0,
			siblingPolls:   sibling - previousSibling,
			listPolls:      list - previousList,
			completedThrou: state.CompletedThrough,
		})
		previousSibling, previousList = sibling, list
		clock = clock.Add(step)
	}

	// The Management selector is polled on every pass, including the complete
	// ones, and its checkpoint advances every time: fairness must not restart
	// or pause the catch-up.
	for i, got := range passes {
		if got.listPolls == 0 {
			t.Fatalf("pass %d did not poll the Management Activity selector: %+v", i+1, passes)
		}
		if i > 0 && !passes[i-1].completedThrou.Before(got.completedThrou) && passes[i-1].immediate {
			t.Fatalf("pass %d did not advance the catch-up checkpoint: %+v", i+1, passes)
		}
	}

	// While the backlog is outstanding the plugin keeps running immediately:
	// fairness must not reintroduce Request-Interval throttling of catch-up.
	drained := len(passes)
	for i, got := range passes {
		if !got.immediate {
			drained = i
			break
		}
	}
	if drained < 4 {
		t.Fatalf("catch-up drained in %d immediate passes, too few to demonstrate starvation: %+v", drained, passes)
	}

	// The sibling selector has no pending work of its own, so before the repair
	// it was polled on the first pass and then skipped for the whole drain.
	polled := []int{}
	for i, got := range passes[:drained] {
		if got.siblingPolls > 0 {
			polled = append(polled, i+1)
		}
	}
	if len(polled) < 2 {
		t.Fatalf("sibling selector was polled on passes %v during a %d-pass drain; it must be polled at least once per interval", polled, drained)
	}
	// One full interval of simulated time elapses every three passes.
	for i := 1; i < len(polled); i++ {
		if gap := polled[i] - polled[i-1]; gap > fairnessRequestInterval/int(step/time.Second)+1 {
			t.Fatalf("sibling selector went %d passes without a poll (polled on %v)", gap, polled)
		}
	}
	// The first pass that stops running immediately is the one that reached the
	// planned catch-up target, so the drain terminated instead of being cut off.
	if drained >= len(passes) {
		t.Fatalf("catch-up never finished within %d passes: %+v", len(passes), passes)
	}
	if got := passes[drained].completedThrou; !got.Equal(managementTestNow) {
		t.Fatalf("catch-up finished at %s, want the planned target %s", got, managementTestNow)
	}
}

// TestImmediateDrainSequenceIsBoundedByThePollInterval pins the bound itself
// without any transport: a drain sequence that keeps being re-armed must still
// yield a complete pass once the interval has elapsed, and the elapsed window
// must be measured from the start of the sequence rather than from the last
// time it was re-armed.
func TestImmediateDrainSequenceIsBoundedByThePollInterval(t *testing.T) {
	clock := managementTestNow
	plugin := New(&Config{PollingConfig: hosted.PollingConfig{Request_Interval: fairnessRequestInterval}})
	plugin.now = func() time.Time { return clock }

	if plugin.takeImmediateDrain() {
		t.Fatal("the first pass must be a complete pass")
	}
	plugin.scheduleImmediateDrain()
	for elapsed := time.Duration(0); elapsed < fairnessRequestInterval*time.Second; elapsed += 10 * time.Second {
		clock = managementTestNow.Add(elapsed)
		if !plugin.takeImmediateDrain() {
			t.Fatalf("pass at +%s gave up the drain early", elapsed)
		}
		plugin.scheduleImmediateDrain()
	}
	clock = managementTestNow.Add(fairnessRequestInterval * time.Second)
	if plugin.takeImmediateDrain() {
		t.Fatal("a drain sequence longer than the poll interval must yield a complete pass")
	}
	// The next sequence starts fresh from here.
	plugin.scheduleImmediateDrain()
	clock = clock.Add(fairnessRequestInterval / 2 * time.Second)
	if !plugin.takeImmediateDrain() {
		t.Fatal("the following drain sequence was not given its own interval")
	}
}
