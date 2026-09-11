package HttpIngester

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingesters/utils"

	"gravwell/e2e"
)

// a fresh, non-keepalive client per large request avoids any chance of
// connection-reuse confusion when a prior request on the same persistent
// connection wasn't fully drained.
var noKeepAliveClient = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

// sendLarge posts body and returns an error describing anything that went
// wrong. It deliberately does NOT call t.Fatal/t.Fatalf itself: it's called
// from multiple goroutines below, and the testing package requires FailNow
// (which Fatal calls) to only ever be invoked from the test's own goroutine.
func sendLarge(endpoint string, body string) error {
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(body))
	resp, err := noKeepAliveClient.Do(req)
	if err != nil {
		return err
	}
	defer utils.DrainResponse(resp)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("got status %d, want %d, body=%q", resp.StatusCode, http.StatusOK, string(b))
	}
	return nil
}

// TestSurvivesStalledIndexer is the true end-to-end companion to the unit-
// and ingest-package-level tests for gravwell/issues#2820
// (ingest/throttle_test.go, ingest/entryWriter_test.go, ingest/muxer_stall_test.go).
// Those exercise the fix against a raw conn, a fake indexer, and a fake
// protocol server; this one exercises the real ingest muxer, a real TCP
// connection, and a real Gravwell indexer process, to prove the fix holds up
// outside of test doubles too.
//
// Root cause recap: EntryWriter only set a write deadline when it explicitly
// called flush(); bufio.Writer's own internal auto-flush wrote straight to
// the underlying conn with no deadline at all, so a peer that stopped
// draining its socket (e.g. an indexer stuck on disk I/O) could block a
// write forever. The fix chunks writes into blocks and resets the deadline
// before each one (ingest/throttle.go).
//
// PauseInstance freezes the whole Gravwell container -- indexer included --
// via the Docker cgroup freezer, which stops it from reading its ingest
// socket without dropping the TCP connection. That is exactly the failure
// mode from the bug report, reproduced against a real indexer instead of a
// simulated one.
func TestSurvivesStalledIndexer(t *testing.T) {
	_, endpoint := setup(t, "stall")

	// Baseline: confirm the pipe works before doing anything adversarial.
	preData := fmt.Sprintf(`{"data": "before pause %d"}`, time.Now().UnixNano())
	SendHttpNoAuth(t, endpoint+"/ingest", strings.NewReader(preData))

	c := e2e.GetClient(t)
	e2e.WaitForEntries(t, c, "tag=http-stall words before pause", time.Minute, 1, 30*time.Second)

	// Freeze the indexer so it stops draining its ingest socket entirely,
	// without closing the connection.
	e2e.PauseInstance(t)
	// Safety net: if anything below Fatals (e.g. the duringPauseCount check
	// just below) before the explicit UnpauseInstance further down runs, this
	// still unpauses the shared container instead of leaving it frozen for
	// every later test. Guarded so the happy path -- which unpauses itself
	// once the freeze window is over -- doesn't double-unpause: "docker
	// unpause" on an already-running container exits non-zero, which would
	// otherwise fail an already-passing test right here in cleanup.
	paused := true
	defer func() {
		if paused {
			e2e.UnpauseInstance(t)
		}
	}()

	// Entries must be big enough to force EntryWriter past its 1MB bufio
	// buffer and into an actual auto-flush against the (now frozen) socket --
	// small entries just sit harmlessly in the buffer and never touch the
	// network at all, which would prove nothing either way. This padding
	// pushes each request comfortably past that threshold, while staying
	// under HttpIngester's own 4MB request-size cap (handlers.go, "request
	// too large"), which is unrelated to gravwell/issues#2820.
	//
	// Kept conservative on size/count on purpose: this test's job is to prove
	// the real stack delivers everything once the indexer recovers, not to
	// pin down the exact byte threshold where an OS-level socket write
	// blocks, which varies by platform/environment (see the unit-level tests
	// in ingest/throttle_test.go for that -- proving this same invariant
	// there took real iteration, including a CI-only flake, to get those
	// margins right).
	padding := strings.Repeat("x", 4*1024*1024-1024) // just under HttpIngester's 4MB cap, minus room for the prefix text

	// Found the hard way while tuning this test: HttpIngester checks
	// IngestMuxer.WillBlock() -- which reflects whether the current
	// connection is "hot", not queue depth -- before accepting a write, and
	// answers 507 immediately if not. Once the first stalled write actually
	// times out (our fix's bounded deadline) and the connection gets
	// recycled, the connection goes non-hot for the rest of the freeze (every
	// reconnect attempt just re-hits the same paused indexer), so anything
	// sent after that window gets an immediate 507 -- a real, separate
	// backpressure signal, not data loss, but it means requests sent too
	// slowly, one at a time, can arrive after the window closes. Firing them
	// all concurrently the instant we pause keeps them all inside it.
	const duringPauseCount = 3
	errs := make([]error, duringPauseCount)
	var wg sync.WaitGroup
	for i := 0; i < duringPauseCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			data := fmt.Sprintf("during pause %d %d %s", i, time.Now().UnixNano(), padding)
			errs[i] = sendLarge(endpoint+"/ingest", data)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("during-pause request %d failed: %v", i, err)
		}
	}

	// Hold the freeze comfortably longer than the ingest muxer's per-block
	// write deadline (10s in production) so the write relay routine actually
	// hits the stall, times out, and goes through at least one full
	// recycle/reconnect cycle while still frozen -- proving it doesn't wedge
	// permanently on the connection that was live when the freeze hit.
	time.Sleep(25 * time.Second)

	e2e.UnpauseInstance(t)
	paused = false

	// Every entry sent while paused must still show up -- no data loss, and
	// delivery actually resumes once the indexer can drain again. Generous
	// wait: after a 25s freeze, the write relay routine may still need to
	// finish an in-flight recycle/reconnect cycle before it can even start
	// draining the backlog, on top of normal indexing/search latency.
	ents := e2e.WaitForEntries(t, c, "tag=http-stall words during pause", 2*time.Minute, duringPauseCount, 90*time.Second)
	if len(ents) != duringPauseCount {
		e2e.Fatalf(t, "got %d entries after unpausing, want %d -- data sent during the stall was lost", len(ents), duringPauseCount)
	}
}
