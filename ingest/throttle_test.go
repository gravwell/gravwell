/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// dialLoopback sets up a real TCP loopback connection. It attempts to shrink
// the OS send/receive buffers, but that has proven unreliable to depend on
// precisely (observed: a 256KB write against an unread socket completed
// instantly even after requesting 4KB buffers, likely because the resize
// either lost a race with the first write or was clamped/ignored by the OS).
// Tests MUST size payloads generously (multiple MB) so they reliably exceed
// even an unshrunk, possibly-autotuned buffer, rather than relying on this
// resize taking effect. This stands in for gravwell/issues#2820's indexer
// that has stopped (or slowed to a crawl) draining its socket.
func dialLoopback(t *testing.T) (cli, srv net.Conn, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		if tc, ok := c.(*net.TCPConn); ok {
			tc.SetReadBuffer(4096)
		}
		accepted <- c
	}()

	cli, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		ln.Close()
		t.Fatalf("failed to dial: %v", err)
	}
	if tc, ok := cli.(*net.TCPConn); ok {
		tc.SetWriteBuffer(4096)
	}

	srv = <-accepted

	cleanup = func() {
		cli.Close()
		srv.Close()
		ln.Close()
	}
	return cli, srv, cleanup
}

// writeWithWatchdog runs a blocking Write in a goroutine and fails the test
// if it doesn't return within budget, so a regression fails the test loudly
// instead of hanging the whole suite forever.
func writeWithWatchdog(t *testing.T, budget time.Duration, fn func() (int, error)) (int, error) {
	t.Helper()
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		n, err := fn()
		done <- result{n, err}
	}()
	select {
	case r := <-done:
		return r.n, r.err
	case <-time.After(budget):
		t.Fatalf("Write did not return within %v", budget)
		return 0, nil
	}
}

// stalledPeerPayloadSize is deliberately far larger than any plausible
// combined OS send+receive buffer capacity (default ~128KB each side on
// macOS, autotuning up to ~4MB each per observed sysctls), so a write
// against a peer that never reads is guaranteed to genuinely block rather
// than being silently absorbed by kernel buffering.
const stalledPeerPayloadSize = 16 * 1024 * 1024

// ---------------------------------------------------------------------
// A fully stalled peer must be caught, not blocked on forever.
// ---------------------------------------------------------------------

func TestFullSpeedWriteTimesOutOnStalledPeer(t *testing.T) {
	cli, _, cleanup := dialLoopback(t)
	defer cleanup()

	const writeTimeout = 150 * time.Millisecond
	fs := newUnthrottledConn(cli, writeTimeout, 4096)
	payload := make([]byte, stalledPeerPayloadSize)

	n, err := writeWithWatchdog(t, writeTimeout+10*time.Second, func() (int, error) {
		return fs.Write(payload)
	})
	if err == nil {
		t.Fatalf("expected a write timeout error against a peer that never reads, got n=%d err=nil", n)
	}
	if !isTimeout(err) {
		t.Fatalf("expected a timeout error, got: %v", err)
	}
	t.Logf("fullSpeed.Write correctly returned after a bounded timeout: n=%d err=%v", n, err)
}

func TestThrottleConnWriteTimesOutOnStalledPeer(t *testing.T) {
	cli, _, cleanup := dialLoopback(t)
	defer cleanup()

	const writeTimeout = 150 * time.Millisecond
	p := newParent(100*1024*1024, 1) // huge burst: a single chunk would span the whole payload pre-blockSize-cap
	tc := p.newThrottleConn(cli, writeTimeout, 4096)
	payload := make([]byte, stalledPeerPayloadSize)

	n, err := writeWithWatchdog(t, writeTimeout+10*time.Second, func() (int, error) {
		return tc.Write(payload)
	})
	if err == nil {
		t.Fatalf("expected a write timeout error against a peer that never reads, got n=%d err=nil", n)
	}
	if !isTimeout(err) {
		t.Fatalf("expected a timeout error, got: %v", err)
	}
	t.Logf("throttleConn.Write correctly returned after a bounded timeout: n=%d err=%v", n, err)
}

// ---------------------------------------------------------------------
// The invariant Kris flagged on gravwell/issues#2820: a slow-but-alive peer
// must not be killed just because the whole transfer takes longer than a
// single writeTimeout window. Only "no progress within writeTimeout" should
// fail the write, not "total transfer time exceeds writeTimeout".
// ---------------------------------------------------------------------

// slowReader reads from c in chunks with a fixed delay between reads,
// forever (until c is closed or errors). Used to simulate a peer that is
// alive and making progress, just slowly -- e.g. a real but
// bandwidth-constrained link, not a dead one.
func slowReader(c net.Conn, chunk int, delay time.Duration) {
	buf := make([]byte, chunk)
	for {
		time.Sleep(delay)
		if _, err := c.Read(buf); err != nil {
			return
		}
	}
}

// Parameters below were arrived at empirically across three rounds of CI
// failures (see conversation history / gravwell/gravwell#2756), not just
// derived on paper.
//
// Round 1: an 800ms writeTimeout reliably passed locally but flaked in CI
// (actions run 34393112885) because a loaded/shared runner's goroutine
// scheduling latency alone can eat several hundred ms before the reader's
// first wakeup -- nothing to do with the code under test. Fixed by widening
// slowPeerWriteTimeout to a large (15x+) margin over slowPeerReadDelay. This
// is a ceiling, not something writes normally wait out, so it doesn't slow
// the test down when things work.
//
// Round 2: a small blockSize (4096) then caused a genuine 30s+ hang on the
// actual GitHub Actions Linux runner (actions run 34494493307), while staying
// fast (~5s) locally. Misdiagnosed the cause as "too many blocks" and fixed
// it by growing blockSize to 256KB -- which promptly made every block time
// out at exactly 5s on the next CI run (actions run 34496306372, "expected
// the slow but alive peer's write to succeed ... after 5.000759676s").
//
// The real mechanism, understood after that: what actually gates how much
// gets through per read cycle is the KERNEL's receive buffer (set via
// dialLoopback's SetReadBuffer(4096) on the accepting side), not slowReader's
// own per-call read size (slowPeerReadChunk). On Linux that request is
// apparently honored close to literally, so each read only frees roughly
// 4KB of window regardless of how big slowReader's own buffer is. A bigger
// blockSize doesn't reduce read cycles needed -- it means each individual
// block needs MORE of them, making it MORE likely to blow its own per-block
// writeTimeout, not less. Round 2 had it backwards.
//
// Fixed for real by going back to a small blockSize (so each block completes
// in the 1-2 read cycles its size actually requires against a ~4KB kernel
// buffer, comfortably inside writeTimeout) and shrinking the total payload
// instead, to bound the cumulative time across the resulting larger block
// count: 256KB / 4096 = 64 blocks, at most ~200ms of real waiting each in the
// worst case = ~12.8s worst-case total, comfortably inside both writeTimeout
// per block and the outer per-test watchdog below.
const (
	slowPeerWriteTimeout = 5 * time.Second
	slowPeerBlockSize    = 4096
	slowPeerPayloadSize  = 256 * 1024
	slowPeerReadChunk    = 65536
	slowPeerReadDelay    = 200 * time.Millisecond // well under writeTimeout: no single gap should ever trip the deadline
	// slowPeerMinRealisticDuration is the "this wasn't just buffered instantly"
	// sanity floor for the survives-a-slow-peer tests. Kept deliberately lenient
	// (not derived from slowPeerWriteTimeout or slowPeerReadDelay) because how
	// much of slowPeerPayloadSize gets buffered "for free" before any real
	// waiting is needed varies a lot by platform (observed ~306ms end-to-end
	// for the full payload on macOS/OrbStack, where kernel buffers run larger
	// than requested) -- this just needs to rule out a literal no-op write.
	slowPeerMinRealisticDuration = 100 * time.Millisecond
)

func TestFullSpeedWriteSurvivesSlowButAlivePeer(t *testing.T) {
	cli, srv, cleanup := dialLoopback(t)
	defer cleanup()

	go slowReader(srv, slowPeerReadChunk, slowPeerReadDelay)

	fs := newUnthrottledConn(cli, slowPeerWriteTimeout, slowPeerBlockSize)
	payload := make([]byte, slowPeerPayloadSize)

	start := time.Now()
	n, err := writeWithWatchdog(t, 60*time.Second, func() (int, error) {
		return fs.Write(payload)
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected the slow but alive peer's write to succeed, got err=%v after %v", err, elapsed)
	}
	if n != slowPeerPayloadSize {
		t.Fatalf("expected to write all %d bytes, wrote %d", slowPeerPayloadSize, n)
	}
	// The read-cadence-driven transfer time is a hard floor regardless of how
	// generous writeTimeout is -- if this finishes suspiciously fast, the
	// reader's pacing didn't actually create real backpressure, so it isn't
	// proving anything either way.
	if elapsed < slowPeerMinRealisticDuration {
		t.Fatalf("write finished in %v, expected at least %v -- "+
			"test may not be exercising real backpressure, so it isn't proving anything", elapsed, slowPeerMinRealisticDuration)
	}
	t.Logf("write of %d bytes succeeded in %v against a peer slower than a single writeTimeout (%v) -- "+
		"proves the per-block deadline doesn't kill a legitimately slow transfer", slowPeerPayloadSize, elapsed, slowPeerWriteTimeout)
}

func TestThrottleConnWriteSurvivesSlowButAlivePeer(t *testing.T) {
	cli, srv, cleanup := dialLoopback(t)
	defer cleanup()

	go slowReader(srv, slowPeerReadChunk, slowPeerReadDelay)

	p := newParent(100*1024*1024, 1)
	tc := p.newThrottleConn(cli, slowPeerWriteTimeout, slowPeerBlockSize)
	payload := make([]byte, slowPeerPayloadSize)

	start := time.Now()
	n, err := writeWithWatchdog(t, 60*time.Second, func() (int, error) {
		return tc.Write(payload)
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected the slow but alive peer's write to succeed, got err=%v after %v", err, elapsed)
	}
	if n != slowPeerPayloadSize {
		t.Fatalf("expected to write all %d bytes, wrote %d", slowPeerPayloadSize, n)
	}
	if elapsed < slowPeerMinRealisticDuration {
		t.Fatalf("write finished in %v, expected at least %v -- "+
			"test may not be exercising real backpressure", elapsed, slowPeerMinRealisticDuration)
	}
	t.Logf("write of %d bytes succeeded in %v against a peer slower than a single writeTimeout (%v)", slowPeerPayloadSize, elapsed, slowPeerWriteTimeout)
}

// ---------------------------------------------------------------------
// A peer that goes silent mid-transfer must be caught within roughly one
// block's worth of silence, not "however long the rest of the payload would
// have taken" -- proving the deadline is genuinely per-block, not just
// eventually-consistent over the whole write.
// ---------------------------------------------------------------------

func TestFullSpeedWriteDetectsPeerGoingSilentMidTransfer(t *testing.T) {
	cli, srv, cleanup := dialLoopback(t)
	defer cleanup()

	const (
		writeTimeout     = 300 * time.Millisecond
		blockSize        = 256 * 1024
		payloadSize      = 16 * 1024 * 1024 // large enough that most of it is still unsent when the peer dies
		readsBeforeDeath = 3
	)

	go func() {
		buf := make([]byte, blockSize)
		for range readsBeforeDeath {
			if _, err := srv.Read(buf); err != nil {
				return
			}
		}
		// go silent forever -- simulates a peer that was fine, then stalled
	}()

	fs := newUnthrottledConn(cli, writeTimeout, blockSize)
	payload := make([]byte, payloadSize)

	start := time.Now()
	_, err := writeWithWatchdog(t, 30*time.Second, func() (int, error) {
		return fs.Write(payload)
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected a timeout once the peer went silent mid-transfer, got no error after %v", elapsed)
	}
	if !isTimeout(err) {
		t.Fatalf("expected a timeout error, got: %v", err)
	}
	if elapsed > writeTimeout*10 {
		t.Fatalf("took %v to detect a peer that went silent, expected roughly one writeTimeout (%v) -- "+
			"detection latency should be bounded by a single block, not the remaining payload size", elapsed, writeTimeout)
	}
	t.Logf("detected a peer going silent mid-transfer within %v of a %v writeTimeout", elapsed, writeTimeout)
}

// ---------------------------------------------------------------------
// Regression coverage for a real bug caught in review: fullSpeed/throttleConn
// only get sane defaults from their constructors (newUnthrottledConn /
// newThrottleConn). Neither Write method itself defends against a zero-value
// writeTimeout or blockSize, which a test (or any direct construction) can
// easily produce. Two distinct, isolated failure modes exist today:
//   - blockSize == 0 with a healthy writeTimeout: Write spins forever issuing
//     zero-length writes (confirmed for throttleConn).
//   - writeTimeout == 0: SetWriteDeadline(time.Now().Add(0)) sets an
//     already-past deadline, so every write fails immediately (confirmed for
//     fullSpeed).
// These tests isolate each field independently and expect success -- they
// will fail until Write() itself falls back to sane defaults the way the
// constructors already do.
// ---------------------------------------------------------------------

func TestFullSpeedWriteDoesNotSpinWithZeroBlockSize(t *testing.T) {
	cli, srv, cleanup := dialLoopback(t)
	defer cleanup()
	go io.Copy(io.Discard, srv) // always draining, never stalls

	fs := fullSpeed{Conn: cli, writeTimeout: 10 * time.Second} // blockSize deliberately left zero
	payload := make([]byte, 64*1024)

	done := make(chan error, 1)
	go func() {
		_, err := fs.Write(payload)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected a zero blockSize to fall back to a sane default and succeed, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Write spun/hung with a zero blockSize instead of falling back to a sane default")
	}
}

func TestFullSpeedWriteDoesNotFailWithZeroWriteTimeout(t *testing.T) {
	cli, srv, cleanup := dialLoopback(t)
	defer cleanup()
	go io.Copy(io.Discard, srv) // always draining, never stalls

	fs := fullSpeed{Conn: cli, blockSize: 4096} // writeTimeout deliberately left zero
	payload := make([]byte, 64*1024)

	done := make(chan error, 1)
	go func() {
		_, err := fs.Write(payload)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected a zero writeTimeout to fall back to a sane default and succeed, got: %v "+
				"(a zero timeout sets an already-past deadline, so every write fails immediately)", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Write hung with a zero writeTimeout instead of falling back to a sane default")
	}
}

func TestThrottleConnWriteDoesNotSpinWithZeroBlockSize(t *testing.T) {
	cli, srv, cleanup := dialLoopback(t)
	defer cleanup()
	go io.Copy(io.Discard, srv)

	p := newParent(100*1024*1024, 1)
	tc := p.newThrottleConn(cli, 0, 0) // writeTimeout set by newThrottleConn...
	tc.blockSize = 0                   // ...but deliberately zeroed out here to simulate a caller that skipped it
	payload := make([]byte, 64*1024)

	done := make(chan error, 1)
	go func() {
		_, err := tc.Write(payload)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected a zero blockSize to fall back to a sane default and succeed, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Write spun/hung with a zero blockSize instead of falling back to a sane default")
	}
}

func TestThrottleConnWriteDoesNotFailWithZeroWriteTimeout(t *testing.T) {
	cli, srv, cleanup := dialLoopback(t)
	defer cleanup()
	go io.Copy(io.Discard, srv)

	p := newParent(100*1024*1024, 1)
	tc := p.newThrottleConn(cli, 0, 0)
	tc.writeTimeout = 0 // deliberately zeroed out to simulate a caller that skipped it
	payload := make([]byte, 64*1024)

	done := make(chan error, 1)
	go func() {
		_, err := tc.Write(payload)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected a zero writeTimeout to fall back to a sane default and succeed, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Write hung with a zero writeTimeout instead of falling back to a sane default")
	}
}

// ---------------------------------------------------------------------
// Regression coverage for a bug caught in review (gravwell/pull/2756):
// throttleConn.Write's error paths return without adding the just-written r
// bytes to n, unlike its fullSpeed.Write sibling fixed in the same diff. A
// partial write that then hits a deadline/error still delivered bytes to the
// peer -- under-reporting n makes higher level retry logic
// (syncAndCloseConnection -> flush()) believe those bytes were never sent,
// so it resends them on the next connection and duplicates data at the
// indexer.
// ---------------------------------------------------------------------

// partialWriteErrConn is a minimal net.Conn stand-in that reports writing
// exactly partial bytes (regardless of how many the caller asked for) and
// then returns failErr, simulating a real partial write that was cut short
// by a deadline or a reset peer.
type partialWriteErrConn struct {
	net.Conn
	partial int
	failErr error
}

func (c *partialWriteErrConn) Write(b []byte) (int, error) {
	n := min(len(b), c.partial)
	return n, c.failErr
}

func (c *partialWriteErrConn) SetWriteDeadline(time.Time) error { return nil }

func TestThrottleConnWriteReportsBytesWrittenBeforeError(t *testing.T) {
	const partial = 65482
	failErr := errors.New("synthetic write failure")

	p := newParent(1<<30, 1) // effectively unlimited, so WaitN never blocks or errors
	tc := p.newThrottleConn(&partialWriteErrConn{partial: partial, failErr: failErr}, 0, 0)

	// Bigger than one block so the first (and only, since it errors) chunk
	// written is capped by blockSize, not by len(payload).
	payload := make([]byte, defaultWriteBlockSize*4)

	n, err := tc.Write(payload)
	if err == nil {
		t.Fatalf("expected the synthetic write failure to propagate")
	}
	if n != partial {
		t.Fatalf("Write reported n=%d after a failed write that actually delivered %d bytes to the peer -- "+
			"a caller that trusts this return value will resend data the peer already has", n, partial)
	}
}

// ---------------------------------------------------------------------
// Regression coverage for a bug caught in review (gravwell/pull/2756):
// throttleConn.Write feeds w.burst directly into the min() that picks each
// chunk size. A zero (or negative) burst -- the zero value of a literal, or a
// misconfigured rate limit with bps <= 0 -- makes every chunk a zero-length
// write that "succeeds" without advancing n, spinning forever. Identical
// failure mode to the blockSize == 0 case above, just via a different field.
//
// Substituting blockSize for the bad burst is only half the fix, and the
// "burst zeroed by hand" case below hides the other half: it leaves the
// parent's limiter holding a real burst, so WaitN still works. A parent built
// from bps <= 0 has a limiter whose burst is zero too, and rate.Limiter
// rejects any WaitN larger than its own burst -- so the write fails anyway,
// after blockSize bytes have already gone out on the wire. Both shapes have to
// pass, which means Write has to skip an unusable limiter entirely.
// ---------------------------------------------------------------------

func TestThrottleConnWriteDoesNotSpinWithZeroBurst(t *testing.T) {
	tests := []struct {
		name string
		conn func(net.Conn) *throttleConn
	}{
		{
			// a caller that skipped the constructor, or otherwise stomped burst
			name: "burst zeroed on an otherwise healthy limiter",
			conn: func(c net.Conn) *throttleConn {
				tc := newParent(100*1024*1024, 1).newThrottleConn(c, 10*time.Second, 4096)
				tc.burst = 0
				return tc
			},
		},
		{
			// the real thing: bps <= 0 zeroes the limiter's burst as well
			name: "parent built from a bps <= 0 rate limit",
			conn: func(c net.Conn) *throttleConn {
				return newParent(0, 0).newThrottleConn(c, 10*time.Second, 4096)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cli, srv, cleanup := dialLoopback(t)
			defer cleanup()
			go io.Copy(io.Discard, srv)

			tc := tt.conn(cli)
			payload := make([]byte, 64*1024)
			n, err := writeWithWatchdog(t, 3*time.Second, func() (int, error) { return tc.Write(payload) })
			if err != nil {
				t.Fatalf("expected an unusable rate limit to be skipped entirely, got: %v (after %d of %d "+
					"bytes had already gone out on the wire)", err, n, len(payload))
			}
			if n != len(payload) {
				t.Fatalf("wrote %d bytes, want %d", n, len(payload))
			}
		})
	}
}

// ---------------------------------------------------------------------
// Regression coverage for a bug caught in review (gravwell/pull/2756):
// newThrottleConn always constructed the returned throttleConn with the
// package defaults (defaultFlushTimeout/defaultWriteBlockSize), ignoring
// whatever FlushTimeout/WriteBlockSize the EntryWriter was actually
// configured with. In muxer.go, a rate-limited connection is built by
// wrapping an already-configured EntryWriter's conn with newThrottleConn once
// a THROTTLE command arrives mid-stream -- so any non-default configuration
// silently reverted back to the defaults at that point.
// ---------------------------------------------------------------------

func TestNewThrottleConnPreservesConfiguredValues(t *testing.T) {
	cli, _, cleanup := dialLoopback(t)
	defer cleanup()

	p := newParent(1024, 1)

	const writeTimeout = 42 * time.Second
	const blockSize = 12345
	tc := p.newThrottleConn(cli, writeTimeout, blockSize)
	if tc.writeTimeout != writeTimeout {
		t.Fatalf("expected newThrottleConn to preserve writeTimeout=%v, got %v", writeTimeout, tc.writeTimeout)
	}
	if tc.blockSize != blockSize {
		t.Fatalf("expected newThrottleConn to preserve blockSize=%d, got %d", blockSize, tc.blockSize)
	}
}

func TestNewThrottleConnDefaultsZeroedValues(t *testing.T) {
	cli, _, cleanup := dialLoopback(t)
	defer cleanup()

	p := newParent(1024, 1)

	tc := p.newThrottleConn(cli, 0, 0)
	if tc.writeTimeout != defaultFlushTimeout {
		t.Fatalf("expected newThrottleConn to default a zero writeTimeout to %v, got %v", defaultFlushTimeout, tc.writeTimeout)
	}
	if tc.blockSize != defaultWriteBlockSize {
		t.Fatalf("expected newThrottleConn to default a zero blockSize to %d, got %d", defaultWriteBlockSize, tc.blockSize)
	}
}

// ---------------------------------------------------------------------
// Coverage gap identified in review: every "stalled peer" test above uses a
// peer that never completes at all, and every "slow but alive peer" test
// uses a margin far wider (20-75x) than writeTimeout, both deliberately, to
// avoid the real-socket/kernel-buffer flakiness documented at length on
// slowPeerReadDelay/slowPeerBlockSize above. Neither actually proves anything
// about behavior close to the deadline boundary itself. The two tests below
// do, using a synthetic conn with a controlled, deterministic write duration
// instead of real socket timing, so the margins can stay tight (a few hundred
// ms on each side of the deadline, not 25x+) without becoming flaky.
// ---------------------------------------------------------------------

// controlledWriteConn is a net.Conn stand-in whose Write "completes" after a
// fixed, caller-chosen writeDuration rather than depending on real socket/
// kernel timing. It still honors SetWriteDeadline the way a real conn would:
// if the installed deadline fires before writeDuration elapses, Write returns
// a timeout error instead of waiting out the rest of writeDuration.
type controlledWriteConn struct {
	net.Conn
	writeDuration time.Duration

	mtx      sync.Mutex
	deadline time.Time
}

func (c *controlledWriteConn) SetWriteDeadline(t time.Time) error {
	c.mtx.Lock()
	c.deadline = t
	c.mtx.Unlock()
	return nil
}

func (c *controlledWriteConn) Write(b []byte) (int, error) {
	c.mtx.Lock()
	deadline := c.deadline
	c.mtx.Unlock()

	if deadline.IsZero() {
		time.Sleep(c.writeDuration)
		return len(b), nil
	}

	select {
	case <-time.After(c.writeDuration):
		return len(b), nil
	case <-time.After(time.Until(deadline)):
		return 0, writeDeadlineExceededErr{}
	}
}

// writeDeadlineExceededErr satisfies net.Error (Timeout() == true) so it is
// recognized by isTimeout the same way a real deadline-exceeded error is.
type writeDeadlineExceededErr struct{}

func (writeDeadlineExceededErr) Error() string   { return "controlledWriteConn: write deadline exceeded" }
func (writeDeadlineExceededErr) Timeout() bool   { return true }
func (writeDeadlineExceededErr) Temporary() bool { return true }

func TestFullSpeedWriteDeadlineBoundary(t *testing.T) {
	const writeTimeout = 400 * time.Millisecond

	t.Run("completes_just_under_deadline_succeeds", func(t *testing.T) {
		// 100ms of margin under a 400ms deadline -- comfortably clear of
		// typical goroutine-scheduling jitter, but close to the boundary, not
		// the 25x+ margins used by the slow-but-alive tests above.
		const writeDuration = 300 * time.Millisecond
		c := &controlledWriteConn{writeDuration: writeDuration}
		fs := newUnthrottledConn(c, writeTimeout, 64*1024)
		payload := make([]byte, 1024) // smaller than blockSize: exactly one underlying Write call

		n, err := writeWithWatchdog(t, 2*time.Second, func() (int, error) {
			return fs.Write(payload)
		})
		if err != nil {
			t.Fatalf("expected a write finishing %v under a %v deadline to succeed, got: %v",
				writeTimeout-writeDuration, writeTimeout, err)
		}
		if n != len(payload) {
			t.Fatalf("expected all %d bytes written, got %d", len(payload), n)
		}
	})

	t.Run("stalls_just_over_deadline_fails", func(t *testing.T) {
		// The write "would" take 150ms longer than the deadline allows --
		// close to the boundary, not an indefinite stall -- and must still be
		// caught right at writeTimeout, not after the full writeDuration.
		const writeDuration = 550 * time.Millisecond
		c := &controlledWriteConn{writeDuration: writeDuration}
		fs := newUnthrottledConn(c, writeTimeout, 64*1024)
		payload := make([]byte, 1024)

		start := time.Now()
		_, err := writeWithWatchdog(t, 2*time.Second, func() (int, error) {
			return fs.Write(payload)
		})
		elapsed := time.Since(start)

		if err == nil {
			t.Fatalf("expected a write that stalls %v past a %v deadline to fail", writeDuration-writeTimeout, writeTimeout)
		}
		if !isTimeout(err) {
			t.Fatalf("expected a timeout error, got: %v", err)
		}
		// Must be caught at roughly writeTimeout, not writeDuration -- proves
		// the deadline actually bounds the write instead of just eventually
		// erroring once the underlying write "finishes" on its own.
		if elapsed > writeTimeout+250*time.Millisecond {
			t.Fatalf("took %v to fail against a %v deadline (would have taken %v to complete on its own) -- "+
				"deadline was not actually enforced close to its boundary", elapsed, writeTimeout, writeDuration)
		}
	})
}

func TestThrottleConnWriteDeadlineBoundary(t *testing.T) {
	const writeTimeout = 400 * time.Millisecond
	p := newParent(1<<30, 1) // effectively unlimited, so burst/WaitN never gate timing

	t.Run("completes_just_under_deadline_succeeds", func(t *testing.T) {
		const writeDuration = 300 * time.Millisecond
		c := &controlledWriteConn{writeDuration: writeDuration}
		tc := p.newThrottleConn(c, writeTimeout, 64*1024)
		payload := make([]byte, 1024)

		n, err := writeWithWatchdog(t, 2*time.Second, func() (int, error) {
			return tc.Write(payload)
		})
		if err != nil {
			t.Fatalf("expected a write finishing %v under a %v deadline to succeed, got: %v",
				writeTimeout-writeDuration, writeTimeout, err)
		}
		if n != len(payload) {
			t.Fatalf("expected all %d bytes written, got %d", len(payload), n)
		}
	})

	t.Run("stalls_just_over_deadline_fails", func(t *testing.T) {
		const writeDuration = 550 * time.Millisecond
		c := &controlledWriteConn{writeDuration: writeDuration}
		tc := p.newThrottleConn(c, writeTimeout, 64*1024)
		payload := make([]byte, 1024)

		start := time.Now()
		_, err := writeWithWatchdog(t, 2*time.Second, func() (int, error) {
			return tc.Write(payload)
		})
		elapsed := time.Since(start)

		if err == nil {
			t.Fatalf("expected a write that stalls %v past a %v deadline to fail", writeDuration-writeTimeout, writeTimeout)
		}
		if !isTimeout(err) {
			t.Fatalf("expected a timeout error, got: %v", err)
		}
		if elapsed > writeTimeout+250*time.Millisecond {
			t.Fatalf("took %v to fail against a %v deadline (would have taken %v to complete on its own) -- "+
				"deadline was not actually enforced close to its boundary", elapsed, writeTimeout, writeDuration)
		}
	})
}

// ---------------------------------------------------------------------
// Coverage gap identified in review: blockSize is exercised throughout this
// file (fallback defaults, preserved config, etc.) purely as a struct-field
// equality check, or indirectly via its effect on stall-detection latency.
// Nothing actually asserts that a payload is split into blockSize-sized
// underlying Write calls in the first place -- these two tests do, by
// recording the size of every call the conn actually receives.
// ---------------------------------------------------------------------

// recordingConn is a net.Conn stand-in that records the size of every Write
// call it receives instead of doing any real I/O, so a test can assert
// directly on how a payload was chunked rather than inferring it indirectly
// from timing.
type recordingConn struct {
	net.Conn
	writes []int
}

func (c *recordingConn) Write(b []byte) (int, error) {
	c.writes = append(c.writes, len(b))
	return len(b), nil
}

func (c *recordingConn) SetWriteDeadline(time.Time) error { return nil }

// assertChunkedAtBlockSize is shared by the fullSpeed/throttleConn chunking
// tests below: it checks that recorded per-call write sizes are each
// positive and at most blockSize, that they sum to payloadSize, and that the
// call count matches the expected ceil(payloadSize/blockSize).
func assertChunkedAtBlockSize(t *testing.T, writes []int, blockSize, payloadSize int) {
	t.Helper()
	wantCalls := (payloadSize + blockSize - 1) / blockSize
	if len(writes) != wantCalls {
		t.Fatalf("expected the payload to be split into %d underlying Write calls of at most %d bytes each, "+
			"got %d calls: %v", wantCalls, blockSize, len(writes), writes)
	}
	var total int
	for i, sz := range writes {
		if sz <= 0 {
			t.Fatalf("call %d wrote %d bytes, expected a positive chunk", i, sz)
		}
		if sz > blockSize {
			t.Fatalf("call %d wrote %d bytes, exceeding blockSize %d", i, sz, blockSize)
		}
		total += sz
	}
	if total != payloadSize {
		t.Fatalf("recorded per-call write sizes summed to %d, expected %d", total, payloadSize)
	}
}

func TestFullSpeedWriteChunksAtBlockSize(t *testing.T) {
	const blockSize = 4096
	// A few multiples of blockSize plus a remainder, so the last recorded
	// call is expected to be a smaller, partial chunk.
	const payloadSize = blockSize*3 + 777

	c := &recordingConn{}
	fs := newUnthrottledConn(c, 10*time.Second, blockSize)
	payload := make([]byte, payloadSize)

	n, err := fs.Write(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != payloadSize {
		t.Fatalf("expected %d bytes written, got %d", payloadSize, n)
	}
	assertChunkedAtBlockSize(t, c.writes, blockSize, payloadSize)
}

func TestThrottleConnWriteChunksAtBlockSize(t *testing.T) {
	const blockSize = 4096
	const payloadSize = blockSize*3 + 777

	c := &recordingConn{}
	p := newParent(1<<30, 1) // huge burst, so burst never gates chunk size below blockSize
	tc := p.newThrottleConn(c, 10*time.Second, blockSize)
	payload := make([]byte, payloadSize)

	n, err := tc.Write(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != payloadSize {
		t.Fatalf("expected %d bytes written, got %d", payloadSize, n)
	}
	assertChunkedAtBlockSize(t, c.writes, blockSize, payloadSize)
}
