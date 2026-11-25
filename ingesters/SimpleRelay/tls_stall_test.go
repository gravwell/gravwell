/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

func testTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	}
}

// TestPumperStalledTLSHandshake connects to a TLS listener and never sends a
// ClientHello.  The first pumped read times out inside the TLS handshake, which
// makes crypto/tls return that same timeout error instantly on every later
// read.  The pumper must surface that as an error instead of treating each one
// as an idle tick, otherwise the scanner above it spins a core forever.
func TestPumperStalledTLSHandshake(t *testing.T) {
	lst, err := tls.Listen("tcp", "127.0.0.1:0", testTLSConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer lst.Close()

	cli, err := net.Dial("tcp", lst.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close() // held open, never speaks TLS

	srv, err := lst.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	const interval = 50 * time.Millisecond
	pumper := newReadTimeoutPumper(srv, interval)
	buff := make([]byte, 1024)
	var reads int
	deadline := time.Now().Add(20 * interval)
	for time.Now().Before(deadline) {
		if _, err := pumper.Read(buff); err != nil {
			t.Logf("pumper returned %v after %d reads", err, reads)
			return
		}
		reads++
	}
	// a healthy idle connection ticks once per interval, so ~20 reads here
	t.Fatalf("pumper never failed a dead TLS handshake: %d reads in %v", reads, 20*interval)
}

// TestPumperIdleTick makes sure a healthy but silent connection still gets the
// artificial newline once per interval, that is the whole point of the pumper.
func TestPumperIdleTick(t *testing.T) {
	cli, srv := net.Pipe()
	defer cli.Close()
	defer srv.Close()

	const interval = 50 * time.Millisecond
	pumper := newReadTimeoutPumper(srv, interval)
	buff := make([]byte, 16)
	for i := range 3 {
		start := time.Now()
		n, err := pumper.Read(buff)
		if err != nil {
			t.Fatalf("idle read %d failed: %v", i, err)
		} else if n != 1 || buff[0] != '\n' {
			t.Fatalf("idle read %d returned %q", i, buff[:n])
		} else if time.Since(start) < interval/2 {
			t.Fatalf("idle read %d returned early after %v", i, time.Since(start))
		}
	}
}

// captureWriter stands in for the ingest muxer and records what was ingested
type captureWriter struct {
	sync.Mutex
	ents []string
}

func (cw *captureWriter) WriteEntry(e *entry.Entry) error {
	cw.Lock()
	cw.ents = append(cw.ents, string(e.Data))
	cw.Unlock()
	return nil
}

func (cw *captureWriter) WriteEntryContext(_ context.Context, e *entry.Entry) error {
	return cw.WriteEntry(e)
}

func (cw *captureWriter) WriteBatch(ents []*entry.Entry) error {
	for _, e := range ents {
		cw.WriteEntry(e)
	}
	return nil
}

func (cw *captureWriter) WriteBatchContext(_ context.Context, ents []*entry.Entry) error {
	return cw.WriteBatch(ents)
}

func (cw *captureWriter) entries() []string {
	cw.Lock()
	defer cw.Unlock()
	return append([]string(nil), cw.ents...)
}

const (
	testPumpInterval     = 50 * time.Millisecond
	testHandshakeTimeout = time.Second
)

var testMsgs = []string{
	"<34>1 2026-11-25T10:00:00Z host app - - - message one",
	"<35>1 2026-11-25T10:00:01Z host app - - - message two",
	"<36>1 2026-11-25T10:00:02Z host app - - - message three",
}

// testHandlerConfig sets up the package globals the handlers lean on, shortens
// the pump and handshake timers, and hands back a handler config that writes
// into a captureWriter.  Timers are restored only after wg has drained so that
// no handler is still reading them.
func testHandlerConfig(t *testing.T) (handlerConfig, *captureWriter) {
	t.Helper()
	lg = log.New(os.Stderr)
	mtx.Lock()
	if connClosers == nil {
		connClosers = make(map[int]closer, 1)
	}
	mtx.Unlock()

	wg := &sync.WaitGroup{}
	origPump, origHandshake := defaultReaderPumpInterval, tlsHandshakeTimeout
	defaultReaderPumpInterval, tlsHandshakeTimeout = testPumpInterval, testHandshakeTimeout
	t.Cleanup(func() {
		// deferred so the timers are restored even if the wait gives up
		defer func() {
			defaultReaderPumpInterval, tlsHandshakeTimeout = origPump, origHandshake
		}()
		waitTimeout(t, wg, 10*time.Second)
	})

	cw := &captureWriter{}
	return handlerConfig{
		name:             "test",
		lrt:              rfc5424Reader,
		ignoreTimestamps: true,
		wg:               wg,
		proc:             processors.NewProcessorSet(cw),
		ctx:              context.Background(),
	}, cw
}

func waitTimeout(t *testing.T, wg *sync.WaitGroup, to time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(to):
		t.Fatalf("goroutines still running after %v", to)
	}
}

// startTestAcceptor runs the real acceptor on a loopback listener, TLS when
// tcfg is set.  The listener is closed and the acceptor reaped on cleanup.
func startTestAcceptor(t *testing.T, cfg handlerConfig, tcfg *tls.Config) net.Addr {
	t.Helper()
	var lst net.Listener
	var err error
	tp := bindType(0)
	if tcfg != nil {
		lst, err = tls.Listen("tcp", "127.0.0.1:0", tcfg)
	} else {
		lst, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	cfg.wg.Add(1)
	go acceptor(lst, addConn(lst), nil, cfg, tp)
	t.Cleanup(func() { lst.Close() })
	return lst.Addr()
}

func waitForEntries(t *testing.T, cw *captureWriter, want []string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var got []string
	for time.Now().Before(deadline) {
		if got = cw.entries(); len(got) >= len(want) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, wanted %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d is %q, wanted %q", i, got[i], want[i])
		}
	}
}

// TestRFC5424HandlerStalledTLSHandshake hands the RFC5424 handler a TLS
// connection whose client never handshakes, skipping the acceptor's up front
// handshake.  The handler has to give up on its own rather than spin.
func TestRFC5424HandlerStalledTLSHandshake(t *testing.T) {
	cfg, cw := testHandlerConfig(t)
	lst, err := tls.Listen("tcp", "127.0.0.1:0", testTLSConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer lst.Close()

	cli, err := net.Dial("tcp", lst.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	srv, err := lst.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	done := make(chan struct{})
	go func() {
		rfc5424ConnHandlerTCP(srv, cfg)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler is still running against a dead TLS handshake")
	}
	if ents := cw.entries(); len(ents) != 0 {
		t.Fatalf("ingested %d entries from a client that sent nothing: %q", len(ents), ents)
	}
}

// TestAcceptorDropsSilentTLSClient runs the full acceptor path and checks that
// a client which connects to a TLS listener and says nothing gets disconnected
// and leaves no handler behind.
func TestAcceptorDropsSilentTLSClient(t *testing.T) {
	cfg, cw := testHandlerConfig(t)
	addr := startTestAcceptor(t, cfg, testTLSConfig(t))
	base := connCount()

	cli, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	// the server should hang up on us shortly after the handshake timeout
	cli.SetReadDeadline(time.Now().Add(10 * testHandshakeTimeout))
	if _, err = cli.Read(make([]byte, 16)); err == nil {
		t.Fatal("server sent data to a client that never handshook")
	} else if isTimeout(err) {
		t.Fatal("server never dropped a client that did not handshake")
	}
	// the hangup can reach us a hair before the server finishes its bookkeeping
	deadline := time.Now().Add(2 * time.Second)
	for connCount() != base && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := connCount(); n != base {
		t.Fatalf("connection count is %d, wanted %d", n, base)
	}
	if ents := cw.entries(); len(ents) != 0 {
		t.Fatalf("ingested %d entries from a client that sent nothing: %q", len(ents), ents)
	}
}

// TestAcceptorTLSSlowHandshake makes sure a client that takes several pump
// intervals to start its handshake is still served, and that idle gaps between
// messages do not disturb what gets ingested.
func TestAcceptorTLSSlowHandshake(t *testing.T) {
	cfg, cw := testHandlerConfig(t)
	addr := startTestAcceptor(t, cfg, testTLSConfig(t))

	raw, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	time.Sleep(4 * testPumpInterval) // dawdle, but stay inside the handshake timeout

	cli := tls.Client(raw, &tls.Config{InsecureSkipVerify: true})
	if err = cli.Handshake(); err != nil {
		t.Fatalf("handshake failed after a slow start: %v", err)
	}
	for _, m := range testMsgs {
		if _, err = io.WriteString(cli, m+"\n"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * testPumpInterval) // let the pumper tick between messages
	}
	cli.Close()
	waitForEntries(t, cw, testMsgs)
}

// TestAcceptorPlainIdleGaps is the same idle gap check over plain TCP, where
// the handshake step has to be a no-op.
func TestAcceptorPlainIdleGaps(t *testing.T) {
	cfg, cw := testHandlerConfig(t)
	addr := startTestAcceptor(t, cfg, nil)

	cli, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	for _, m := range testMsgs {
		if _, err = io.WriteString(cli, m+"\n"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * testPumpInterval)
	}
	cli.Close()
	waitForEntries(t, cw, testMsgs)
}

// TestAcceptorSurvivesBadHandshakes parks several silent clients on a TLS
// listener and checks that a real client is accepted and served while they are
// still stalled, then again after they have been dropped.  A failed or pending
// handshake must never block or take down the accept loop.
func TestAcceptorSurvivesBadHandshakes(t *testing.T) {
	cfg, cw := testHandlerConfig(t)
	addr := startTestAcceptor(t, cfg, testTLSConfig(t))

	for range 3 {
		silent, err := net.Dial("tcp", addr.String())
		if err != nil {
			t.Fatal(err)
		}
		defer silent.Close()
	}

	send := func(msg string) {
		t.Helper()
		d := &net.Dialer{Timeout: 2 * testHandshakeTimeout}
		cli, err := tls.DialWithDialer(d, "tcp", addr.String(), &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("listener did not serve a real client: %v", err)
		}
		defer cli.Close()
		if _, err = io.WriteString(cli, msg+"\n"); err != nil {
			t.Fatal(err)
		}
	}

	// a serial accept loop would make us wait out all three silent clients
	start := time.Now()
	send(testMsgs[0])
	waitForEntries(t, cw, testMsgs[:1])
	if d := time.Since(start); d >= 2*testHandshakeTimeout {
		t.Fatalf("real client waited %v behind stalled handshakes", d)
	}

	// let the silent clients time out, the listener has to still be alive
	time.Sleep(testHandshakeTimeout + testHandshakeTimeout/2)
	send(testMsgs[1])
	waitForEntries(t, cw, testMsgs[:2])
}

// silentTLSConn returns the server side of a TLS connection whose client has
// connected and will never send a ClientHello.
func silentTLSConn(t *testing.T) net.Conn {
	t.Helper()
	lst, err := tls.Listen("tcp", "127.0.0.1:0", testTLSConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lst.Close() })
	cli, err := net.Dial("tcp", lst.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cli.Close() })
	srv, err := lst.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return srv
}

// mustReturn fails the test if f is still running well after the handshake
// timeout should have fired.
func mustReturn(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		f()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * testHandshakeTimeout):
		t.Fatalf("%s is still running against a dead TLS handshake", what)
	}
}

// TestRegexHandlerStalledTLSHandshake and TestJSONHandlerStalledTLSHandshake
// check that the regex and JSON listeners drop a client that never handshakes
// instead of parking a handler on it forever.
func TestRegexHandlerStalledTLSHandshake(t *testing.T) {
	base, _ := testHandlerConfig(t)
	cfg := regexHandlerConfig{
		name:  "test",
		wg:    base.wg,
		ctx:   base.ctx,
		proc:  base.proc,
		regex: "X",
	}
	srv := silentTLSConn(t)
	mustReturn(t, "regex handler", func() { regexConnHandler(srv, cfg, nil) })
}

func TestJSONHandlerStalledTLSHandshake(t *testing.T) {
	base, _ := testHandlerConfig(t)
	cfg := jsonHandlerConfig{
		name:             "test",
		wg:               base.wg,
		ctx:              base.ctx,
		proc:             base.proc,
		ignoreTimestamps: true,
		maxObjectSize:    1024 * 1024, // the handler bails before reading without one
	}
	srv := silentTLSConn(t)
	mustReturn(t, "json handler", func() { jsonConnHandler(srv, cfg, nil) })
}

// TestPumperPendingTLSHandshake covers a pumped read that waits out its whole
// deadline on a TLS connection that never handshakes.  The wait was genuine,
// but the connection is dead and must not be reported as an idle tick.
func TestPumperPendingTLSHandshake(t *testing.T) {
	srv := silentTLSConn(t)
	pumper := newReadTimeoutPumper(srv, testPumpInterval)
	if n, err := pumper.Read(make([]byte, 16)); err == nil {
		t.Fatalf("pumper reported an idle tick (%d bytes) on an unfinished TLS handshake", n)
	}
}
