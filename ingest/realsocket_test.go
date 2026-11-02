//go:build realsocket

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"net"
	"testing"
	"time"
)

// The stalled and slow peer tests run over net.Pipe on a fake clock, which is
// fast and deterministic but never touches a kernel socket buffer.  This file
// keeps one real socket variant of each shape so the per block write deadline
// is still exercised against a partially draining TCP peer somewhere.  They
// take real seconds and depend on kernel buffer tuning, so they are behind a
// build tag:
//
//	go test -tags realsocket -run RealSocket ./ingest

// dialTCPLoopback sets up a real TCP loopback connection with the socket
// buffers shrunk as far as the OS will allow.  The resize is best effort, see
// the history of gravwell/gravwell#2756: tests must size payloads to exceed
// an unshrunk, autotuned buffer rather than rely on it.
func dialTCPLoopback(t *testing.T) (cli, srv net.Conn, cleanup func()) {
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

// realSocketStalledPayload is far larger than any plausible combined send and
// receive buffer (macOS autotunes up to ~4MB a side), so a write against a
// peer that never reads genuinely blocks instead of being absorbed.
const realSocketStalledPayload = 16 * 1024 * 1024

func TestFullSpeedWriteTimesOutOnStalledPeerRealSocket(t *testing.T) {
	cli, _, cleanup := dialTCPLoopback(t)
	defer cleanup()

	const writeTimeout = 150 * time.Millisecond
	fs := newUnthrottledConn(cli, writeTimeout, 4096)
	payload := make([]byte, realSocketStalledPayload)

	n, err := writeWithWatchdog(t, writeTimeout+10*time.Second, func() (int, error) {
		return fs.Write(payload)
	})
	if err == nil {
		t.Fatalf("expected a write timeout error against a peer that never reads, got n=%d err=nil", n)
	}
	if !isTimeout(err) {
		t.Fatalf("expected a timeout error, got: %v", err)
	}
}

// TestFullSpeedWriteSurvivesSlowButAlivePeerRealSocket is the kernel buffered
// twin of TestFullSpeedWriteSurvivesSlowButAlivePeer.  How much of the payload
// the kernel absorbs before any real waiting happens varies by platform, so
// the duration floor only rules out a no-op write.
func TestFullSpeedWriteSurvivesSlowButAlivePeerRealSocket(t *testing.T) {
	cli, srv, cleanup := dialTCPLoopback(t)
	defer cleanup()

	stop := make(chan struct{})
	defer close(stop)
	go slowReader(srv, slowPeerReadChunk, slowPeerReadDelay, stop)

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
	if elapsed < 100*time.Millisecond {
		t.Fatalf("write finished in %v, the reader's pacing did not create real backpressure", elapsed)
	}
	t.Logf("write of %d bytes succeeded in %v against a kernel buffered peer slower than a single writeTimeout (%v)",
		slowPeerPayloadSize, elapsed, slowPeerWriteTimeout)
}
