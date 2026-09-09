/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"context"
	"math"
	"net"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultBurstMultiplier = 1
	// defaultWriteBlockSize bounds how much data a single underlying Write
	// call pushes before the deadline gets reset. Entries can legitimately be
	// very large (up to entry.MaxDataSize, approx 1GB) and a slow-but-healthy
	// connection may take minutes to push one. A single deadline spanning the
	// whole transfer cannot tell a slowly progressing peer from a fully stalled
	// one. Chunking into blocks and resetting the deadline before each one bounds
	// "no progress within the deadline", not "total transfer time", which is the
	// invariant we actually want.
	defaultWriteBlockSize int = 256 * 1024
)

type parent struct {
	burst int
	lm    *rate.Limiter
}

type throttleConn struct {
	net.Conn
	burst int
	lm    *rate.Limiter
	to    time.Duration
	ctx   context.Context
	cncl  func()
	// writeTimeout/blockSize configure the per-block deadline behavior in Write
	// below. Set to sane defaults by newThrottleConn. Tests construct a throttleCon
	// literal directly to override them per-instance instead of mutating shared
	// package state.
	writeTimeout time.Duration
	blockSize    int
}

type conn interface {
	net.Conn
	SetReadTimeout(time.Duration) error
	SetWriteTimeout(time.Duration) error
	ClearWriteTimeout() error
	ClearReadTimeout() error
}

func newParent(bps int64, burstMult int) *parent {
	if burstMult <= 0 {
		burstMult = defaultBurstMultiplier
	}
	burst := bps * int64(burstMult)
	if burst > math.MaxInt || burst < 0 {
		burst = math.MaxInt
	}
	return &parent{
		burst: int(burst),
		lm:    rate.NewLimiter(rate.Limit(bps), int(burst)),
	}
}

func (p *parent) newThrottleConn(c net.Conn) *throttleConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &throttleConn{
		Conn:         c,
		burst:        p.burst,
		lm:           p.lm,
		cncl:         cancel,
		ctx:          ctx,
		writeTimeout: defaultFlushTimeout,
		blockSize:    defaultWriteBlockSize,
	}
}

func (w *throttleConn) Close() error {
	if w.cncl != nil {
		w.cncl()
	}
	return w.Conn.Close()
}

func (w *throttleConn) SetReadTimeout(to time.Duration) error {
	return w.Conn.SetReadDeadline(time.Now().Add(to))
}

func (w *throttleConn) ClearReadTimeout() error {
	return w.Conn.SetReadDeadline(time.Time{})
}

func (w *throttleConn) SetWriteTimeout(to time.Duration) error {
	w.to = to
	return w.Conn.SetWriteDeadline(time.Now().Add(to))
}

func (w *throttleConn) ClearWriteTimeout() error {
	w.to = 0
	return w.Conn.SetWriteDeadline(time.Time{})
}

func (w *throttleConn) Write(b []byte) (n int, err error) {
	var r int
	ctx := w.ctx
	if w.to > 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(w.ctx, w.to)
		defer cancel()
	}

	// A caller that constructs a throttleConn literal directly (tests do
	// exactly this, see throttle_test.go) can leave these values zeroed.
	// Without this fallback, blockSize == 0 makes every chunk a zero-length
	// write that "succeeds" without advancing n. An infite spin, not a clean
	// failure. writeTimeout == 0 sets an already-past deadline, so every write
	// fails immediately instead of using a sane default.
	blockSize := w.blockSize
	if blockSize <= 0 {
		blockSize = defaultWriteBlockSize
	}
	writeTimeout := w.writeTimeout
	if writeTimeout <= 0 {
		writeTimeout = defaultFlushTimeout
	}

	for n < len(b) {
		// Cap each underlying write at w.blockSize regardless of the
		// configured rate-limit burst, so the deadline below actually
		// gets reset often enough to detect a stall promptly even when
		// a large burst is configured.
		sz := min(len(b)-n, w.burst, blockSize)

		// Reset the floor write deadline before every block, so a stalled
		// peer is caught within one block's worth of silence. Not blocked
		// forever, and not killed early on a slow but still progressing
		// transfer.
		if err = w.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
			return
		}

		if r, err = w.Conn.Write(b[n : n+sz]); err != nil {
			return
		}
		if err = w.lm.WaitN(ctx, r); err != nil {
			return
		}
		n += r
	}
	return
}

type fullSpeed struct {
	net.Conn
	// writeTimeout/blockSize configure the per-block deadline behavior in
	// Write below. Set to sane defaults by newUnthrottledConn. Tests construct
	// a fullSpeed literal directly to override them per-instance instead of
	// mutating shared package state.
	writeTimeout time.Duration
	blockSize    int
}

// Write guarantees that any write reaching the wire through this conn, whether
// an explicit EntryWriter.flush(), or a bufio.Writer's own internal auto-flush
// (which is triggered by ex: EntryWriter.writeAll or any other direct write
// to ew.bIO), can never block forever, without also killing a large slowly
// progressing transfer early. It pushes at most blockSize bytes per underlying
// Write call and resets the writeTimeout deadline before each one. This is so
// the bound is "no progress within writeTimeout", not "total transfer time".
func (fs fullSpeed) Write(b []byte) (n int, err error) {
	// Same fallback as throttleConn.Write, for the same reason.
	blockSize := fs.blockSize
	if blockSize <= 0 {
		blockSize = defaultWriteBlockSize
	}

	writeTimeout := fs.writeTimeout
	if writeTimeout <= 0 {
		writeTimeout = defaultFlushTimeout
	}

	for n < len(b) {
		if err = fs.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
			return
		}

		var r int
		end := min(n+blockSize, len(b))

		if r, err = fs.Conn.Write(b[n:end]); err != nil {
			n += r
			return
		}

		n += r
	}

	return
}

func (fs fullSpeed) SetReadTimeout(to time.Duration) error {
	return fs.Conn.SetReadDeadline(time.Now().Add(to))
}

func (fs fullSpeed) ClearReadTimeout() error {
	return fs.Conn.SetReadDeadline(time.Time{})
}

func (fs fullSpeed) SetWriteTimeout(to time.Duration) error {
	return fs.Conn.SetWriteDeadline(time.Now().Add(to))
}

func (fs fullSpeed) ClearWriteTimeout() error {
	return fs.Conn.SetWriteDeadline(time.Time{})
}

// newUnthrottledConn wraps c with fullSpeed's per-block write deadline
// behavior. A zero writeTimeout/blockSize (ex: a caller that skipped
// EntryReaderWriterConfig.validate()) falls back to the same defaults
// validate() would have set, so this is safe to call directly too.
func newUnthrottledConn(c net.Conn, writeTimeout time.Duration, blockSize int) fullSpeed {
	if writeTimeout <= 0 {
		writeTimeout = defaultFlushTimeout
	}
	if blockSize <= 0 {
		blockSize = defaultWriteBlockSize
	}

	return fullSpeed{
		Conn:         c,
		writeTimeout: writeTimeout,
		blockSize:    blockSize,
	}
}
