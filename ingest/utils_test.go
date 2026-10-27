/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// TestEmergencyQueueClearNilBatchEntry ensures that clear() does not panic when
// a batch pulled from the emergency queue contains nil entries interspersed
// with entries that fail tag translation. Previously the tag-reversal loop
// that runs when a mid-batch translation fails did not skip nil entries,
// causing a nil pointer dereference (see the AzureEventHubs ingester panic in
// ingest.(*emergencyQueue).clear -> tt.reverse).
func TestEmergencyQueueClearNilBatchEntry(t *testing.T) {
	eq := newEmergencyQueue()

	tt := &tagTrans{
		active: []entry.EntryTag{5}, // only tag 0 is negotiated
	}

	blk := []*entry.Entry{
		nil,
		{Tag: 0}, // translates fine, gets reversed when the batch bails below
		{Tag: 1}, // not negotiated, forces the reversal loop to run
	}
	eq.push(nil, blk)

	if ok := eq.clear(nil, tt); ok {
		t.Fatalf("expected clear to report failure for untranslatable tag")
	}
}

func TestTagBitMask(t *testing.T) {
	var tmt tagMaskTracker
	//make sure its empty
	for i := 0; i < 0x10000; i++ {
		tg := entry.EntryTag(i)
		if tmt.has(tg) {
			t.Fatalf("tag  %d set without being set", i)
		}
		tmt.add(tg)
		if !tmt.has(tg) {
			t.Fatalf("tag %d not set after being set", i)
		}
	}
	for i := 0xffff; i >= 0; i-- {
		tg := entry.EntryTag(i)
		if !tmt.has(tg) {
			t.Fatalf("tag %d not set", i)
		}
		tmt.clear(tg)
		if tmt.has(tg) {
			t.Fatalf("tag %d set after clear", i)
		}
	}
}

// memListener is an in-memory stand in for a TCP listener.  Dialing it hands
// the caller one end of a net.Pipe and queues the other end for Accept.  It
// exists so muxer level tests can run inside a synctest bubble: a pipe blocks
// on channels and keeps its deadlines on timers, which the bubble's fake
// clock controls, where a real socket read or write is invisible to it.
type memListener struct {
	addr  *net.TCPAddr
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

var (
	memListeners sync.Map // address string -> *memListener
	memNextLst   uint32   // listener ports, see memPort
	memNextCli   uint32   // client side ports, see memPort
)

// memPort hands out ports from a counter, wrapping inside [lo, hi) so a long
// run never walks past 65535 and the addresses keep looking real.
func memPort(ctr *uint32, lo, hi uint32) int {
	return int(lo + atomic.AddUint32(ctr, 1)%(hi-lo))
}

func newMemListener() *memListener {
	l := &memListener{
		addr:  &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: memPort(&memNextLst, 40000, 65536)},
		conns: make(chan net.Conn),
		done:  make(chan struct{}),
	}
	memListeners.Store(l.addr.String(), l)
	return l
}

func (l *memListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

// Close stops accepting and deregisters the address.  A late dial from a
// muxer still in its reconnect loop is refused by installMemDialer's lookup.
func (l *memListener) Close() error {
	l.once.Do(func() {
		close(l.done)
		memListeners.Delete(l.addr.String())
	})
	return nil
}

func (l *memListener) Addr() net.Addr { return l.addr }

// dial connects to the listener, blocking until the accept side takes the
// connection the way a real dial waits on the kernel backlog.
func (l *memListener) dial() (net.Conn, error) {
	cli, srv := net.Pipe()
	local := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: memPort(&memNextCli, 10000, 40000)}
	select {
	case l.conns <- &addrConn{Conn: srv, local: l.addr, remote: local}:
		return &addrConn{Conn: cli, local: local, remote: l.addr}, nil
	case <-l.done:
		cli.Close()
		srv.Close()
		return nil, errors.New("connection refused")
	}
}

// addrConn gives a pipe end real looking TCP addresses, newTcpConn parses the
// local one for the ingester's source IP.
type addrConn struct {
	net.Conn
	local, remote net.Addr
}

func (c *addrConn) LocalAddr() net.Addr  { return c.local }
func (c *addrConn) RemoteAddr() net.Addr { return c.remote }

// installMemDialer routes tcpDial through the in-memory listeners for the
// rest of the test.  Anything not registered is refused rather than dialed
// for real: a socket is invisible to a synctest bubble's clock, and a stray
// loopback port might have something listening on it.  It swaps a package
// variable, so tests that call it must not run in parallel with other muxer
// tests.
func installMemDialer(t *testing.T) {
	t.Helper()
	orig := tcpDial
	tcpDial = func(dst string) (net.Conn, error) {
		if v, ok := memListeners.Load(dst); ok {
			return v.(*memListener).dial()
		}
		return nil, fmt.Errorf("no in-memory listener registered for %s", dst)
	}
	t.Cleanup(func() { tcpDial = orig })
}
