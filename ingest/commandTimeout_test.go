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
	"errors"
	"io"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// These tests cover the post-auth command exchanges in the EntryWriter.  Each
// one bounds a single read at two seconds, but the indexer keepalive fires
// every second, so a connection that is up but never answers a command used
// to spin forever.  See issue 2923.
//
// Every test runs inside a synctest bubble so the real production timeouts
// are exercised against a fake clock: the fake indexer keepalives at the real
// one second cadence and the ten second budget elapses instantly.  net.Pipe
// blocks on channels and implements its deadlines with timers, so the whole
// exchange lives inside the bubble.

// commandTimeoutSlack is the grace allowed on top of a timeout before a
// command is declared hung.  It is fake time, so it costs nothing.
const commandTimeoutSlack = 5 * time.Second

// setupCommands are the exchanges that go through readCommandsUntil (plus
// SendIngesterAPIVersion, which used to have its own copy of the loop).  Ping
// is the one caller left out on purpose: it waits for PONG_MAGIC, so a
// keepalive is a valid answer and it cannot be stalled this way.
var setupCommands = []struct {
	name string
	op   func(ew *EntryWriter) error
	ack  ackCommand //the response that satisfies the command
}{
	{`SendIngesterAPIVersion`, func(ew *EntryWriter) error { return ew.SendIngesterAPIVersion() }, ackCommand{cmd: CONFIRM_API_VER_MAGIC}},
	{`IdentifyIngester`, func(ew *EntryWriter) error { return ew.IdentifyIngester(`test`, `v0`, `uuid`) }, ackCommand{cmd: CONFIRM_ID_MAGIC}},
	{`IngestOK`, func(ew *EntryWriter) error {
		ok, err := ew.IngestOK()
		if err == nil && !ok {
			err = errors.New("ingest not ok")
		}
		return err
	}, ackCommand{cmd: CONFIRM_INGEST_OK_MAGIC, val: 1}},
	{`SendIngesterState`, func(ew *EntryWriter) error { return ew.SendIngesterState(IngesterState{Name: `test`}) }, ackCommand{cmd: CONFIRM_INGESTER_STATE_MAGIC}},
	{`WriteDittoBlock`, func(ew *EntryWriter) error { return ew.WriteDittoBlock([]entry.Entry{*makeEntry()}) }, ackCommand{cmd: CONFIRM_DITTO_BLOCK_MAGIC}},
}

// keepaliveOnlyWriter hands back an EntryWriter whose peer drains everything it is
// sent and keepalives at the real indexer cadence but never answers.  The
// peer side of the pipe is returned so a test can inject an answer.  The fake
// indexer is torn down by a cleanup so every goroutine leaves the bubble.
func keepaliveOnlyWriter(t *testing.T, ctx context.Context) (*EntryWriter, net.Conn) {
	t.Helper()
	cli, srv := net.Pipe()
	ew, err := NewEntryWriterEx(EntryReaderWriterConfig{
		Conn:                  cli,
		OutstandingEntryCount: 64,
		BufferSize:            minBufferSize,
		Timeout:               time.Second,
		CTX:                   ctx,
	})
	if err != nil {
		t.Fatal(err)
	}
	//pretend we negotiated a modern indexer so none of the commands short circuit
	ew.serverVersion = VERSION

	done := make(chan struct{})
	go keepaliveUntil(srv, done)
	t.Cleanup(func() {
		close(done)
		cli.Close()
		srv.Close()
	})
	return ew, srv
}

// writeAck encodes a single indexer response onto w.
func writeAck(w io.Writer, ac ackCommand) error {
	buff := make([]byte, 16)
	n, _, err := ac.encode(buff)
	if err != nil {
		return err
	}
	_, err = w.Write(buff[:n])
	return err
}

// runBounded runs op and reports how long it took, failing the test if it has
// not returned by the limit.  A hung command is reported rather than waited
// on so a regression shows up as a failure instead of a deadlocked bubble.
func runBounded(t *testing.T, limit time.Duration, op func() error) (error, time.Duration) {
	t.Helper()
	start := time.Now()
	errCh := make(chan error, 1)
	go func() { errCh <- op() }()
	select {
	case err := <-errCh:
		return err, time.Since(start)
	case <-time.After(limit):
		t.Fatalf("command did not return within %v, it is hung", limit)
	}
	return nil, 0
}

// TestCommandTimeoutWithKeepalives is the defect from the issue: an indexer
// that keeps sending PONG_MAGIC but never answers a command must cause the
// command to fail within commandResponseTimeout rather than hang.
func TestCommandTimeoutWithKeepalives(t *testing.T) {
	for _, tc := range setupCommands {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ew, _ := keepaliveOnlyWriter(t, context.Background())

				err, dur := runBounded(t, commandResponseTimeout+commandTimeoutSlack, func() error { return tc.op(ew) })
				if err == nil {
					t.Fatalf("%s returned success from an indexer that never answered", tc.name)
				}
				if !errors.Is(err, ErrTimeout) {
					t.Fatalf("%s returned %v, want a timeout", tc.name, err)
				}
				if dur < commandResponseTimeout {
					t.Fatalf("%s gave up after %v, before the %v budget", tc.name, dur, commandResponseTimeout)
				}
			})
		})
	}
}

// TestCommandCancelledByContext covers the muxer shutdown case: cancelling
// the writer's context must get a command out of its read loop promptly even
// though the indexer is still keepaliving.
func TestCommandCancelledByContext(t *testing.T) {
	for _, tc := range setupCommands {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cf := context.WithCancel(context.Background())
				defer cf()
				ew, _ := keepaliveOnlyWriter(t, ctx)

				time.AfterFunc(keepAliveInterval/2, cf)
				//the loop re-checks the context on every keepalive, and in the worst
				//case on the two second read deadline
				err, dur := runBounded(t, commandResponseTimeout, func() error { return tc.op(ew) })
				if err == nil {
					t.Fatalf("%s returned success after its context was cancelled", tc.name)
				}
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("%s returned %v, want %v", tc.name, err, context.Canceled)
				}
				if dur > 2*keepAliveInterval {
					t.Fatalf("%s took %v to notice cancellation", tc.name, dur)
				}
			})
		})
	}
}

// TestCommandSucceedsThroughKeepalives makes sure the budget does not break
// the normal case: an indexer that is slow but does answer, with several
// keepalives arriving first, must still succeed.
func TestCommandSucceedsThroughKeepalives(t *testing.T) {
	// long enough that the two second per read deadline would have fired
	// several times over if keepalives were not holding it off, but inside
	// the budget
	const answerAfter = commandResponseTimeout / 2

	for _, tc := range setupCommands {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ew, srv := keepaliveOnlyWriter(t, context.Background())

				//the stalled peer only keepalives, so we answer from the side by
				//writing the confirmation into the same pipe after a delay
				tmr := time.AfterFunc(answerAfter, func() {
					//a write error here means the test already tore down the pipe,
					//which the command result below will report on its own
					writeAck(srv, tc.ack)
				})
				defer tmr.Stop()
				err, dur := runBounded(t, commandResponseTimeout+commandTimeoutSlack, func() error { return tc.op(ew) })
				if err != nil {
					t.Fatalf("%s failed on a slow but live indexer: %v", tc.name, err)
				}
				if dur < answerAfter {
					t.Fatalf("%s returned after %v, before the indexer answered", tc.name, dur)
				}
			})
		})
	}
}
