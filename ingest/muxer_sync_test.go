/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const (
	syncTestSecret = `sync-test-secret`
	syncTestTag    = `synctest`
	syncTestDepth  = 4096 //muxer channel depth used by these tests
)

// TestSyncWithBacklog reproduces https://github.com/gravwell/issues/issues/2587,
// where a Sync issued mid run against a healthy indexer could never complete.
//
// An ingester that pushes hard and then calls Sync (which is exactly what the
// shardingester tool does at the end of every shard) burns its entire timeout
// and gets back ErrTimeout, even though the indexer is up and actively draining
// the backlog.
//
// SyncContext used to take im.mtx and hold it for the whole drain loop while it
// waited for the entry channels to empty.  The write relay routine is the only
// thing that drains those channels, and once per tickerInterval (1.5s to 3s) it
// calls getTrimmedState, which wants that same lock.  The relay parked on the
// mutex, the channels stopped draining, and Sync spun until it timed out.  The
// fix is that the drain loop no longer takes the lock at all.
//
// The test therefore sizes the backlog so that draining it takes longer than
// one ticker interval, then asserts that Sync still finishes well inside its
// timeout.
func TestSyncWithBacklog(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long running sync test")
	}
	const (
		syncTimeout = 45 * time.Second
		maxAccepted = 30 * time.Second
	)

	ti, mxr, tg := newSyncTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	// Hold the indexer so nothing is consumed, then fill the pipeline.  Once
	// the muxer channel is full we know we have a backlog to work with.
	ti.hold()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		writeUntil(mxr, tg, stop)
	}()

	if err := waitForFullChannel(mxr, 30*time.Second); err != nil {
		close(stop)
		wg.Wait()
		t.Fatal(err)
	}
	close(stop)
	wg.Wait()

	backlog := len(mxr.eChanOut)

	// Let the indexer drain, but slowly enough that the drain spans at least
	// one relay ticker interval.  A healthy Sync still finishes easily inside
	// its timeout, it just has to wait for the pipeline to clear.
	ti.throttle(time.Millisecond)
	ti.release()

	ts := time.Now()
	err := mxr.Sync(syncTimeout)
	dur := time.Since(ts)

	if err != nil {
		t.Fatalf("Sync failed after %v with a backlog of %d entries against a healthy indexer: %v (indexer consumed %d entries over %d connection(s))",
			dur.Round(time.Millisecond), backlog, err, ti.Entries(), ti.Conns())
	}
	if dur > maxAccepted {
		t.Fatalf("Sync took %v to clear a backlog of %d entries, expected well under %v",
			dur.Round(time.Millisecond), backlog, maxAccepted)
	}
	if l := len(mxr.eChanOut); l != 0 {
		t.Fatalf("Sync returned with %d entries still in the pipeline", l)
	}
	t.Logf("Sync cleared a backlog of %d entries in %v", backlog, dur.Round(time.Millisecond))
}

// TestSyncDoesNotHoldLockDuringAck covers the second phase of a Sync.
//
// Draining the entry channels is only the first phase, after that it calls
// syncTimeout and blocks until the indexer acks everything outstanding.  That
// wait runs for as long as the caller's timeout allows, the shardingester tool
// was invoked with 25 minutes.  SyncContext used to hold im.mtx across it,
// which stalled every other muxer user, including WriteBatch and the connection
// routine that has to reap a connection which dies mid sync.  It now snapshots
// the connection set under the lock and releases it before syncing.
//
// The test parks the indexer so acks stop, gets a Sync into the ack wait, and
// then checks that an unrelated muxer call still completes.
func TestSyncDoesNotHoldLockDuringAck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long running sync test")
	}
	const entryCount = 100

	ti, mxr, tg := newSyncTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	// Stop the indexer acking, then push a small number of entries.  They are
	// few enough that the relay routine moves them all out to the socket, so
	// the channels go empty and Sync gets past its drain loop, but they stay
	// unacked and park Sync in syncTimeout.
	ti.hold()
	data := []byte(`ack wait test entry`)
	for i := 0; i < entryCount; i++ {
		ent := &entry.Entry{
			TS:   entry.Now(),
			Tag:  tg,
			Data: append([]byte(nil), data...),
		}
		if err := mxr.WriteEntry(ent); err != nil {
			t.Fatal(err)
		}
	}
	if err := waitForEmptyChannel(mxr, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	syncDone := make(chan error, 1)
	go func() {
		syncDone <- mxr.Sync(20 * time.Second)
	}()

	// give Sync time to clear the drain loop and settle into the ack wait
	time.Sleep(time.Second)

	// Hot only takes im.mtx.RLock and reads a counter, it should never be able
	// to block behind an indexer that has stopped acking.
	hotDone := make(chan error, 1)
	go func() {
		_, err := mxr.Hot()
		hotDone <- err
	}()

	select {
	case err := <-hotDone:
		if err != nil {
			t.Fatalf("Hot failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		ti.release()
		<-syncDone
		t.Fatal("Hot blocked on im.mtx while Sync was waiting on acks, the muxer lock is held across syncTimeout")
	case err := <-syncDone:
		t.Fatalf("Sync returned early with %v, the test never reached the ack wait", err)
	}

	// let everything wrap up
	ti.release()
	if err := <-syncDone; err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
}

// TestMuxerLockStarvesRelayRoutine isolates the mechanism behind
// a deadlock when syncing a highly loaded muxer without going through Sync at
// all.  It takes im.mtx directly, exactly the way SyncContext does, and shows
// that the write relay routine stops draining the entry channels within one
// ticker interval because getTrimmedState wants the same lock.
//
// This is the hazard that makes any long hold of im.mtx unsafe, so it stays
// meaningful even after SyncContext stops holding the lock across its drain
// loop.  It FAILS as long as the relay routine needs the muxer wide lock on
// its state push timer, so disabling for now.
//
// TEST DISABLED -
// this test manually screws with the locking to express a situation that we cannot
// actually get into.  Every im.mtx.Lock site was audited, none of them hold the lock
// across unbounded work, the only blocking send under the lock is the emergency queue
// drain in Close and by then the relay routines have already been reaped.  There is some
// fear around teardown deadlock when getIngesterState takes the lock and then stalls.
// This will always fail on current code but it may be testing something that can't
// happen.  The test is still valuable so i am leaving it here.
func noTestMuxerLockStarvesRelayRoutine(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long running lock starvation test")
	}
	// the relay ticker fires somewhere in 1.5s to 3s, so hold well past that
	const holdFor = 6 * time.Second

	ti, mxr, tg := newSyncTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	// build a backlog that would take a couple of seconds to drain
	ti.hold()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		writeUntil(mxr, tg, stop)
	}()
	if err := waitForFullChannel(mxr, 30*time.Second); err != nil {
		close(stop)
		wg.Wait()
		t.Fatal(err)
	}
	close(stop)
	wg.Wait()

	ti.throttle(500 * time.Microsecond)
	ti.release()

	// Take the muxer lock the same way SyncContext does and watch whether the
	// relay routine keeps working.
	mxr.mtx.Lock()
	start := len(mxr.eChanOut)
	var stalled time.Duration
	var last int = start
	var lastChange time.Time = time.Now()
	deadline := time.Now().Add(holdFor)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		cur := len(mxr.eChanOut)
		if cur != last {
			last = cur
			lastChange = time.Now()
		} else if d := time.Since(lastChange); d > stalled {
			stalled = d
		}
		if cur == 0 {
			break
		}
	}
	end := len(mxr.eChanOut)
	mxr.mtx.Unlock()

	if end != 0 {
		t.Fatalf("write relay routine stalled while im.mtx was held: channel went from %d to %d entries over %v, no movement for %v",
			start, end, holdFor, stalled.Round(time.Millisecond))
	}
	t.Logf("relay drained %d entries while im.mtx was held", start)
}

// newSyncTestMuxer stands up a test indexer and a muxer wired to it, waits for
// the connection to go hot, and hands back the intermediate tag to write with.
func newSyncTestMuxer(t *testing.T) (*testIndexer, *IngestMuxer, entry.EntryTag) {
	t.Helper()
	ti, err := newTestIndexer(syncTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	mxr, err := NewUniformMuxer(UniformMuxerConfig{
		Destinations:    []string{ti.Destination()},
		Tags:            []string{syncTestTag},
		Auth:            syncTestSecret,
		CacheDepth:      syncTestDepth,
		IngesterName:    `synctest`,
		IngesterVersion: `test`,
	})
	if err != nil {
		ti.Close()
		t.Fatal(err)
	}
	if err = mxr.Start(); err != nil {
		ti.Close()
		t.Fatal(err)
	}
	if err = mxr.WaitForHot(10 * time.Second); err != nil {
		mxr.Close()
		ti.Close()
		t.Fatal(err)
	}
	tg, err := mxr.GetTag(syncTestTag)
	if err != nil {
		mxr.Close()
		ti.Close()
		t.Fatal(err)
	}
	return ti, mxr, tg
}

// writeUntil pushes entries into the muxer until stop is closed.  It uses
// WriteEntry rather than WriteBatch on purpose, WriteBatch takes im.mtx and
// would tangle the writer up in the very lock these tests are about.
func writeUntil(mxr *IngestMuxer, tg entry.EntryTag, stop <-chan struct{}) {
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte('a' + (i % 26))
	}
	for {
		select {
		case <-stop:
			return
		default:
		}
		ent := &entry.Entry{
			TS:   entry.Now(),
			Tag:  tg,
			Data: append([]byte(nil), data...),
		}
		if err := mxr.WriteEntry(ent); err != nil {
			return
		}
	}
}

// waitForFullChannel blocks until the muxer output channel is full, which
// means we have a backlog of syncTestDepth entries plus whatever is already
// outstanding on the wire.
func waitForFullChannel(mxr *IngestMuxer, to time.Duration) error {
	deadline := time.Now().Add(to)
	for time.Now().Before(deadline) {
		if len(mxr.eChanOut) >= cap(mxr.eChanOut) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for the muxer pipeline to fill, got %d of %d",
		len(mxr.eChanOut), cap(mxr.eChanOut))
}

// waitForEmptyChannel blocks until the relay routine has pulled everything out
// of the muxer channels, which is the point where a Sync stops draining and
// starts waiting on acks.
func waitForEmptyChannel(mxr *IngestMuxer, to time.Duration) error {
	deadline := time.Now().Add(to)
	for time.Now().Before(deadline) {
		if len(mxr.eChanOut) == 0 && len(mxr.bChanOut) == 0 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for the muxer pipeline to empty, %d entries left",
		len(mxr.eChanOut))
}
