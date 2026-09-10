/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"io"
	"net"
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
	tc := p.newThrottleConn(cli)
	tc.writeTimeout = writeTimeout
	tc.blockSize = 4096
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

// Parameters below were arrived at empirically across two rounds of CI
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
// Round 2: a *small* blockSize (4096) then caused a genuine 30s+ hang on the
// actual GitHub Actions Linux runner (actions run 34494493307), while
// staying fast (~5s) locally. Best explanation: Linux honors a requested
// small SetReadBuffer far more literally than macOS/OrbStack does locally,
// so the effective OS buffer there is genuinely tiny -- meaning nearly every
// one of the ~512 blocks needed its own dedicated read cycle, multiplying
// total time far past the 30s watchdog even though each individual block was
// still correctly bounded by writeTimeout. The number of blocks, not their
// size, was driving total time, and that count is what varies unpredictably
// by platform. Fixed by using a much larger blockSize (few blocks total) so
// worst-case total time is bounded and platform-independent: with
// blockSize=256KB and readChunk=64KB, even a maximally pessimistic (tiny
// buffer) block needs at most a handful of read cycles -- comfortably under
// writeTimeout -- and only 8 such blocks are needed for the whole payload.
const (
	slowPeerWriteTimeout = 5 * time.Second
	slowPeerBlockSize    = 256 * 1024
	slowPeerPayloadSize  = 2 * 1024 * 1024
	slowPeerReadChunk    = 64 * 1024
	slowPeerReadDelay    = 200 * time.Millisecond // well under writeTimeout: no single gap should ever trip the deadline
	// slowPeerMinRealisticDuration is the "this wasn't just buffered instantly"
	// sanity floor for the survives-a-slow-peer tests. Deliberately NOT derived
	// from slowPeerWriteTimeout (a much larger ceiling used only to tolerate CI
	// scheduling jitter, see above) -- it's a floor on the read-cadence-driven
	// duration instead, which is what actually proves real backpressure occurred.
	slowPeerMinRealisticDuration = 1 * time.Second
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
	tc := p.newThrottleConn(cli)
	tc.writeTimeout = slowPeerWriteTimeout
	tc.blockSize = slowPeerBlockSize
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
		for i := 0; i < readsBeforeDeath; i++ {
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
	tc := p.newThrottleConn(cli) // writeTimeout set by newThrottleConn...
	tc.blockSize = 0             // ...but deliberately zeroed out here to simulate a caller that skipped it
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
	tc := p.newThrottleConn(cli)
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
