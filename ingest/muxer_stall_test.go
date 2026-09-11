/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// TestMuxerReconnectsWhileIndexerStaysDown reproduces gravwell/issues#2820: an
// indexer that stops draining its ingest socket (e.g. because it is stuck on
// disk I/O) used to wedge the write relay routine forever inside an
// undeadlined socket write. Because that write and the routine's own
// dead-connection health check live in the same select loop (writeRelayRoutine,
// muxer.go:1500), a permanently blocked write meant the health check, and the
// reconnect logic behind it, could never run at all -- not "ran slowly", never
// ran. The connection that was live when the stall hit is the one the
// ingester would be stuck on forever, requiring a manual restart.
//
// The distinguishing signal has to be "did a reconnect happen at all while
// the indexer was still down", not "did everything eventually finish" --
// releasing the indexer during or after a single still-in-flight blocked
// write lets that write complete normally with or without the fix, since
// bytes can finally flow once something starts reading. That would pass
// against unfixed code too and prove nothing. So this test holds the indexer
// down for a fixed window, counts how many connections got accepted purely
// from held-indexer activity, and only releases afterward to let the test
// finish. Before the fix that count never moves past the one original
// connection; after the fix, the timed-out write triggers
// syncAndCloseConnection + getNewConnSet, and Accepted() climbs while the
// indexer is still refusing to drain anything.
func TestMuxerReconnectsWhileIndexerStaysDown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long running stall test")
	}
	const (
		numEntries = 5
		// macOS auto-tunes TCP send/receive buffers up to 4MB
		// (net.inet.tcp.autorcvbufmax); this needs to comfortably exceed that
		// on its own so a single entry is guaranteed to force a real blocked
		// write against the held indexer, regardless of buffer auto-tuning.
		entrySize = 8 * 1024 * 1024
		// long enough to guarantee we are deep into at least one blocked
		// write attempt (and its recycle/reconnect aftermath) when we release
		holdFor = 30 * time.Second
	)

	ti, mxr, tg := newSyncTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	ti.hold()

	startAccepted := ti.Accepted()
	if startAccepted != 1 {
		t.Fatalf("expected exactly 1 connection accepted before any writes, got %d", startAccepted)
	}

	data := make([]byte, entrySize)
	for i := range numEntries {
		ent := &entry.Entry{TS: entry.Now(), Tag: tg, Data: data}
		if err := mxr.WriteEntry(ent); err != nil {
			t.Fatalf("failed to queue entry %d: %v", i, err)
		}
	}

	time.Sleep(holdFor)

	reconnects := ti.Accepted() - startAccepted

	// Release and wait for a full, clean drain before returning. This is not
	// part of the assertion (already decided above) -- it just gives Close()
	// a quiesced pipeline to shut down instead of one fighting a mid-retry
	// connection, which otherwise makes teardown slow enough to trip the test
	// binary's own -timeout on an unrelated code path. (syncTimeout/
	// closeTimeout are deliberately soft, cumulative timeouts -- see the
	// comment on IngestConnection.syncTimeout -- so tearing down against a
	// still-held indexer is slow but not itself a bug.)
	ti.release()
	drainDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(drainDeadline) {
		if len(mxr.eChanOut) == 0 && len(mxr.bChanOut) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if reconnects <= 0 {
		t.Fatalf("no reconnect happened in %v against a stalled indexer (accepted connections stayed at %d): "+
			"the write relay routine is permanently wedged on the original connection -- this is gravwell/issues#2820",
			holdFor, startAccepted)
	}
	t.Logf("write relay routine reconnected %d time(s) in %v while the indexer stayed stalled, "+
		"proving it never wedged on the original blocked write", reconnects, holdFor)
}

// TestMuxerSurvivesSlowButAliveIndexer proves Kris's invariant from
// gravwell/issues#2820 holds at the full production stack (IngestMuxer ->
// writeRelayRoutine -> IngestConnection -> EntryWriter -> real TCP -> a real
// protocol-speaking indexer), using the muxer's real, unmodified default
// FlushTimeout/WriteBlockSize (there is no config path from MuxerConfig down
// to those yet) -- not a shrunk test-only value like throttle_test.go and
// entryWriter_test.go use for speed. An indexer that is merely slow (throttled
// per entry, never held) must never trigger a reconnect: ti.Accepted() should
// stay at 1 for the whole run, even though delivering the full backlog takes
// several seconds -- multiples of a single write's worth of silence, but never
// a single gap long enough to trip the deadline.
func TestMuxerSurvivesSlowButAliveIndexer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long running stall test")
	}
	const (
		numEntries    = 10
		entrySize     = 2 * 1024 * 1024
		perEntryDelay = 400 * time.Millisecond // the indexer pauses this long after each entry, well under the 10s default FlushTimeout
		drainWithin   = 60 * time.Second
	)

	ti, mxr, tg := newSyncTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	ti.throttle(perEntryDelay)

	data := make([]byte, entrySize)
	start := time.Now()
	for i := range numEntries {
		ent := &entry.Entry{TS: entry.Now(), Tag: tg, Data: data}
		if err := mxr.WriteEntry(ent); err != nil {
			t.Fatalf("failed to queue entry %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(drainWithin)
	for time.Now().Before(deadline) {
		if ti.Entries() >= numEntries && len(mxr.eChanOut) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	elapsed := time.Since(start)

	if got := ti.Entries(); got < numEntries {
		t.Fatalf("indexer only received %d/%d entries within %v -- delivery stalled against a merely slow indexer",
			got, numEntries, drainWithin)
	}
	if accepted := ti.Accepted(); accepted != 1 {
		t.Fatalf("indexer accepted %d connections, expected exactly 1 -- a slow but alive indexer "+
			"should never trigger a reconnect. This is the regression Kris flagged: a single deadline "+
			"spanning a whole write can't tell a slow-but-progressing peer from a stalled one", accepted)
	}
	// The indexer sleeps perEntryDelay after each read, so (numEntries-1)
	// full gaps must elapse before the last entry can even be read -- the
	// final entry's own trailing sleep isn't required for ti.Entries() to
	// reach numEntries. This is a hard floor regardless of network speed;
	// comfortably longer than a single write's worth of silence confirms
	// real cross-entry backpressure was survived, not just fast delivery
	// that never touched the deadline.
	minExpected := time.Duration(numEntries-1) * perEntryDelay
	if elapsed < minExpected {
		t.Fatalf("delivery finished in %v, expected at least %v given the indexer's throttle -- "+
			"test may not be exercising real timing", elapsed, minExpected)
	}
	t.Logf("delivered all %d entries in %v against a slow (throttled %v/entry) but always-alive indexer, "+
		"with zero reconnects", numEntries, elapsed.Round(time.Millisecond), perEntryDelay)
}
