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
	"strings"
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

// bogusEntryID is an entry ID the writer never issued, so a confirmation for
// it is filler the indexer should not be sending.
const bogusEntryID = 0xBAD

// setupCommands are the exchanges that wait on an indexer response, every
// caller of readCommandsUntil and readCommandsUntilAny.  Ping is the one
// caller left out on purpose: it waits for
// PONG_MAGIC, so a keepalive is a valid answer and it cannot be stalled.
//
// ack is the response that satisfies the command, bad is one the command's
// loop has no handler for and must reject, budget is how long the loop is
// allowed to wait before giving up.
var setupCommands = []struct {
	name   string
	op     func(ew *EntryWriter) error
	ack    ackCommand
	bad    ackCommand
	budget time.Duration
}{
	{
		name:   `SendIngesterAPIVersion`,
		op:     func(ew *EntryWriter) error { return ew.SendIngesterAPIVersion() },
		ack:    ackCommand{cmd: CONFIRM_API_VER_MAGIC},
		bad:    ackCommand{cmd: CONFIRM_TAG_MAGIC},
		budget: commandResponseTimeout,
	},
	{
		name:   `IdentifyIngester`,
		op:     func(ew *EntryWriter) error { return ew.IdentifyIngester(`test`, `v0`, `uuid`) },
		ack:    ackCommand{cmd: CONFIRM_ID_MAGIC},
		bad:    ackCommand{cmd: CONFIRM_TAG_MAGIC},
		budget: commandResponseTimeout,
	},
	{
		name: `IngestOK`,
		op: func(ew *EntryWriter) error {
			ok, err := ew.IngestOK()
			if err == nil && !ok {
				err = errors.New("ingest not ok")
			}
			return err
		},
		ack:    ackCommand{cmd: CONFIRM_INGEST_OK_MAGIC, val: 1},
		bad:    ackCommand{cmd: CONFIRM_TAG_MAGIC},
		budget: commandResponseTimeout,
	},
	{
		name:   `SendIngesterState`,
		op:     func(ew *EntryWriter) error { return ew.SendIngesterState(IngesterState{Name: `test`}) },
		ack:    ackCommand{cmd: CONFIRM_INGESTER_STATE_MAGIC},
		bad:    ackCommand{cmd: CONFIRM_TAG_MAGIC},
		budget: commandResponseTimeout,
	},
	{
		name:   `WriteDittoBlock`,
		op:     func(ew *EntryWriter) error { return ew.WriteDittoBlock([]entry.Entry{*makeEntry()}) },
		ack:    ackCommand{cmd: CONFIRM_DITTO_BLOCK_MAGIC},
		bad:    ackCommand{cmd: CONFIRM_TAG_MAGIC},
		budget: commandResponseTimeout,
	},
	{
		name: `NegotiateTag`,
		op: func(ew *EntryWriter) error {
			_, err := ew.NegotiateTag(`newtag`)
			return err
		},
		ack:    ackCommand{cmd: CONFIRM_TAG_MAGIC, val: 7},
		bad:    ackCommand{cmd: CONFIRM_ID_MAGIC},
		budget: commandResponseTimeout,
	},
}

// chatteringWriter hands back an EntryWriter whose peer drains everything it
// is sent and writes ac every interval, forever, but never answers.  With a
// PONG_MAGIC that is a wedged indexer whose ack routine is still alive, with
// a bogus CONFIRM_ENTRY_MAGIC it is one spraying acks for entries it was never
// sent.  The peer side of the pipe is returned so a test can inject an
// answer, the fake indexer is torn down by a cleanup so every goroutine
// leaves the bubble.
func chatteringWriter(t *testing.T, ctx context.Context, every time.Duration, ac ackCommand) (*EntryWriter, net.Conn) {
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
	go chatterUntil(srv, done, every, ac)
	t.Cleanup(func() {
		close(done)
		cli.Close()
		srv.Close()
	})
	return ew, srv
}

// keepaliveOnlyWriter is chatteringWriter with the real indexer keepalive.
func keepaliveOnlyWriter(t *testing.T, ctx context.Context) (*EntryWriter, net.Conn) {
	t.Helper()
	return chatteringWriter(t, ctx, keepAliveInterval, ackCommand{cmd: PONG_MAGIC})
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
func runBounded(t *testing.T, limit time.Duration, op func() error) (time.Duration, error) {
	t.Helper()
	start := time.Now()
	errCh := make(chan error, 1)
	go func() { errCh <- op() }()
	select {
	case err := <-errCh:
		return time.Since(start), err
	case <-time.After(limit):
		t.Fatalf("command did not return within %v, it is hung", limit)
	}
	return 0, nil
}

// TestCommandTimeoutWithKeepalives is the defect from the issue: an indexer
// that keeps sending PONG_MAGIC but never answers a command must cause the
// command to fail within its budget rather than hang.
func TestCommandTimeoutWithKeepalives(t *testing.T) {
	for _, tc := range setupCommands {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ew, _ := keepaliveOnlyWriter(t, context.Background())
				assertTimesOut(t, tc.name, tc.budget, func() error { return tc.op(ew) })
			})
		})
	}
}

// TestCommandTimeoutWithBogusConfirms is the same defect with different
// filler: confirmations for entries that were never sent are ignored, but
// they must not count as progress either, or an indexer spraying them keeps
// the exchange alive exactly the way keepalives did.
func TestCommandTimeoutWithBogusConfirms(t *testing.T) {
	for _, tc := range setupCommands {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ew, _ := chatteringWriter(t, context.Background(), 500*time.Millisecond,
					ackCommand{cmd: CONFIRM_ENTRY_MAGIC, val: bogusEntryID})
				assertTimesOut(t, tc.name, tc.budget, func() error { return tc.op(ew) })
			})
		})
	}
}

// assertTimesOut runs op against a peer that never answers and checks it
// gives up with a timeout, no sooner than its budget.
func assertTimesOut(t *testing.T, name string, budget time.Duration, op func() error) {
	t.Helper()
	dur, err := runBounded(t, budget+commandTimeoutSlack, op)
	if err == nil {
		t.Fatalf("%s returned success from an indexer that never answered", name)
	}
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("%s returned %v, want a timeout", name, err)
	}
	if dur < budget {
		t.Fatalf("%s gave up after %v, before the %v budget", name, dur, budget)
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
				dur, err := runBounded(t, tc.budget, func() error { return tc.op(ew) })
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

// TestCommandFailsFastOnUnexpectedResponse pins the fail fast arm: a response
// the loop has no handler for means the stream is out of step, and that has
// to surface immediately with the real cause, not as a timeout after the
// whole budget has been burned.
func TestCommandFailsFastOnUnexpectedResponse(t *testing.T) {
	for _, tc := range setupCommands {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ew, srv := keepaliveOnlyWriter(t, context.Background())

				badAfter := keepAliveInterval / 2
				tmr := time.AfterFunc(badAfter, func() { writeAck(srv, tc.bad) })
				defer tmr.Stop()
				dur, err := runBounded(t, tc.budget+commandTimeoutSlack, func() error { return tc.op(ew) })
				if err == nil {
					t.Fatalf("%s accepted a %v it has no business accepting", tc.name, tc.bad.cmd)
				}
				if errors.Is(err, ErrTimeout) {
					t.Fatalf("%s reported a timeout for an out of step stream, hiding the cause: %v", tc.name, err)
				}
				if !strings.Contains(err.Error(), tc.bad.cmd.String()) {
					t.Fatalf("%s rejected the stream without naming the %v that caused it: %v", tc.name, tc.bad.cmd, err)
				}
				if dur > badAfter+keepAliveInterval {
					t.Fatalf("%s took %v to reject an unexpected %v, want immediate", tc.name, dur, tc.bad.cmd)
				}
			})
		})
	}
}

// TestCommandSucceedsThroughKeepalives makes sure the budget does not break
// the normal case: an indexer that is slow but does answer, with several
// keepalives arriving first, must still succeed.
func TestCommandSucceedsThroughKeepalives(t *testing.T) {
	for _, tc := range setupCommands {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// long enough that the two second per read deadline would have
				// fired several times over if keepalives were not holding it
				// off, but inside the budget
				answerAfter := tc.budget / 2
				assertSucceedsAfter(t, tc.name, tc.budget, answerAfter, func(srv net.Conn) {
					time.AfterFunc(answerAfter, func() { writeAck(srv, tc.ack) })
				}, tc.op)
			})
		})
	}
}

// TestCommandIgnoresStrayEntryConfirm covers the errEntryNotFound path: a
// confirmation for an entry the writer does not know about is dropped, it
// must neither fail the command nor stop the real answer from being seen.
func TestCommandIgnoresStrayEntryConfirm(t *testing.T) {
	for _, tc := range setupCommands {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				answerAfter := 2 * keepAliveInterval
				assertSucceedsAfter(t, tc.name, tc.budget, answerAfter, func(srv net.Conn) {
					time.AfterFunc(keepAliveInterval, func() {
						writeAck(srv, ackCommand{cmd: CONFIRM_ENTRY_MAGIC, val: bogusEntryID})
					})
					time.AfterFunc(answerAfter, func() { writeAck(srv, tc.ack) })
				}, tc.op)
			})
		})
	}
}

// assertSucceedsAfter runs op against a keepaliving peer, lets inject
// schedule whatever the peer should send, and checks op succeeds and did not
// return before the answer was due.  A write error inside the injected
// timers means the test already tore down the pipe, which op's own result
// reports.
func assertSucceedsAfter(t *testing.T, name string, budget, answerAfter time.Duration, inject func(srv net.Conn), op func(ew *EntryWriter) error) {
	t.Helper()
	ew, srv := keepaliveOnlyWriter(t, context.Background())
	inject(srv)
	dur, err := runBounded(t, budget+commandTimeoutSlack, func() error { return op(ew) })
	if err != nil {
		t.Fatalf("%s failed on a slow but live indexer: %v", name, err)
	}
	if dur < answerAfter {
		t.Fatalf("%s returned after %v, before the indexer answered", name, dur)
	}
}
