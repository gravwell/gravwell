/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// rawStr builds a valid JSON string value of n filler bytes for use as a
// Configuration/Metadata block.
func rawStr(n int) json.RawMessage {
	return json.RawMessage(`"` + strings.Repeat("C", n) + `"`)
}

// makeChildren builds n child states, each carrying a Name of nameBytes and,
// when cfgBytes > 0, a Configuration block of cfgBytes.
func makeChildren(n, nameBytes, cfgBytes int) map[string]IngesterState {
	m := make(map[string]IngesterState, n)
	name := strings.Repeat("A", nameBytes)
	var cfg json.RawMessage
	if cfgBytes > 0 {
		cfg = rawStr(cfgBytes)
	}
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("child-%04d", i)
		m[key] = IngesterState{
			UUID:          key,
			Name:          name,
			Configuration: cfg,
		}
	}
	return m
}

// muxerForState returns a bare muxer wired up with just enough state to exercise
// getTrimmedState. No logger is set; im.Error is a no-op when lgr is nil.
func muxerForState(s IngesterState) *IngestMuxer {
	return &IngestMuxer{
		mtx:           &sync.RWMutex{},
		start:         time.Now(),
		ingesterState: s,
	}
}

func assertFits(t *testing.T, s IngesterState) {
	t.Helper()
	sz, err := s.EncodedSize()
	if err != nil {
		t.Fatalf("EncodedSize failed: %v", err)
	}
	if sz > maxIngestStateSize {
		t.Fatalf("state does not fit after trimming: %d > %d", sz, maxIngestStateSize)
	}
}

// TestGetTrimmedStateStages walks each stage of the progressive size reduction
// in getTrimmedState.  Each subtest is built so that the state only drops below
// maxIngestStateSize at the specific stage under test, proving every earlier
// stage ran but was insufficient and that the target stage is what made it fit.
func TestGetTrimmedStateStages(t *testing.T) {
	const MB = int(maxIngestStateSize)

	run := func(s IngesterState) (IngesterState, bool, error) {
		return muxerForState(s).getTrimmedState(time.Time{}, 0)
	}

	// stage 0: already fits, nothing is trimmed
	t.Run("fits-no-trim", func(t *testing.T) {
		out, push, err := run(IngesterState{
			Name:          "small",
			Configuration: rawStr(1024),
			Metadata:      rawStr(1024),
			Children:      makeChildren(2, 512, 512),
		})
		if err != nil || !push {
			t.Fatalf("unexpected err=%v push=%v", err, push)
		}
		if out.Configuration == nil || out.Metadata == nil {
			t.Fatal("stage0: reporting blocks should be retained when the state already fits")
		}
		if len(out.Children) != 2 {
			t.Fatalf("stage0: children should be untouched, got %d", len(out.Children))
		}
		assertFits(t, out)
	})

	// stage 1: child configuration/metadata blocks are what push us over
	t.Run("stage1-child-configs", func(t *testing.T) {
		out, push, err := run(IngesterState{
			Configuration: rawStr(1024), // small own config that must survive
			Children:      makeChildren(4, 256, 400*1024),
		})
		if err != nil || !push {
			t.Fatalf("unexpected err=%v push=%v", err, push)
		}
		if len(out.Children) != 4 {
			t.Fatalf("stage1: children should be retained, got %d", len(out.Children))
		}
		for k, c := range out.Children {
			if c.Configuration != nil {
				t.Fatalf("stage1: child %s config was not trimmed", k)
			}
		}
		if out.Configuration == nil {
			t.Fatal("stage1: own configuration should still be present")
		}
		assertFits(t, out)
	})

	// stage 2: our own configuration/metadata blocks dominate
	t.Run("stage2-own-config", func(t *testing.T) {
		out, push, err := run(IngesterState{
			Configuration: rawStr(2 * MB),
			Metadata:      rawStr(1024),
			Children:      makeChildren(2, 256, 1024),
		})
		if err != nil || !push {
			t.Fatalf("unexpected err=%v push=%v", err, push)
		}
		if out.Configuration != nil || out.Metadata != nil {
			t.Fatal("stage2: own configuration/metadata should be dropped")
		}
		if len(out.Children) != 2 {
			t.Fatalf("stage2: children should be retained, got %d", len(out.Children))
		}
		assertFits(t, out)
	})

	// stage 3: trimChildren(64) - too many children, sized so 64 fit
	t.Run("stage3-trim-children-64", func(t *testing.T) {
		out, push, err := run(IngesterState{
			Children: makeChildren(100, 12*1024, 0),
		})
		if err != nil || !push {
			t.Fatalf("unexpected err=%v push=%v", err, push)
		}
		if len(out.Children) != 64 {
			t.Fatalf("stage3: expected 64 children after trimChildren(64), got %d", len(out.Children))
		}
		assertFits(t, out)
	})

	// stage 4: trimChildren(8) - 64 children still too big, 8 fit
	t.Run("stage4-trim-children-8", func(t *testing.T) {
		out, push, err := run(IngesterState{
			Children: makeChildren(100, 30*1024, 0),
		})
		if err != nil || !push {
			t.Fatalf("unexpected err=%v push=%v", err, push)
		}
		if len(out.Children) != 8 {
			t.Fatalf("stage4: expected 8 children after trimChildren(8), got %d", len(out.Children))
		}
		assertFits(t, out)
	})

	// stage 5: even 7 children are too big, drop them entirely
	t.Run("stage5-drop-children", func(t *testing.T) {
		out, push, err := run(IngesterState{
			Children: makeChildren(100, 200*1024, 0),
		})
		if err != nil || !push {
			t.Fatalf("unexpected err=%v push=%v", err, push)
		}
		if len(out.Children) != 0 {
			t.Fatalf("stage5: expected all children dropped, got %d", len(out.Children))
		}
		assertFits(t, out)
	})

	// stage 6: nothing trimmable can shrink it (Name is never trimmed) -> error
	t.Run("stage6-unshrinkable-errors", func(t *testing.T) {
		_, push, err := run(IngesterState{
			Name: strings.Repeat("A", 2*MB),
		})
		if !push {
			t.Fatal("stage6: expected shouldPush true")
		}
		if err == nil {
			t.Fatal("stage6: expected an error when the state cannot be shrunk below the limit")
		}
	})
}

const (
	tagTestSecret = `tagTestSecret`
	tagTestTag    = `tagtest`
)

// waitHot blocks until the muxer reports n live connections.  WaitForHot only promises
// one, which is not enough when a test needs every destination staged.
func waitHot(t *testing.T, mxr *IngestMuxer, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		cnt, err := mxr.Hot()
		if err != nil {
			t.Fatal(err)
		}
		if cnt >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d connections came up", cnt, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitIndexerTag waits for a tag to show up on the indexer.  NegotiateTag only waits on
// the connections that were live when it was called, a connection that was still coming
// up gets caught up by its own relay routine a beat later.  The wait is short on
// purpose, the point of all this is that the tag shows up without any data being
// written on it, not eventually.
func waitIndexerTag(t *testing.T, ti *testIndexer, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if ti.hasTag(name) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("indexer never got tag %q, it knows about %v, no data has flowed on it", name, ti.tagNames())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestNegotiateTagIsImmediate checks that a tag negotiated on a live muxer gets
// established on the indexer without anything being written on it.  Ingesters and
// preprocessors (the routers in particular) negotiate their tags at startup and may not
// push an entry on one of them for hours, so a tag that is not established until data
// flows is not searchable in the meantime.
func TestNegotiateTagIsImmediate(t *testing.T) {
	ti, mxr := newTagTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	tg, err := mxr.NegotiateTag(`new-tag`)
	if err != nil {
		t.Fatal(err)
	}
	if !ti.hasTag(`new-tag`) {
		t.Fatalf("indexer did not have the tag when NegotiateTag returned, it knows about %v", ti.tagNames())
	}

	// the entry path had better still work on the new tag
	if err = mxr.WriteEntry(&entry.Entry{TS: entry.Now(), Tag: tg, Data: []byte(`test`)}); err != nil {
		t.Fatal(err)
	}
	if err = mxr.Sync(time.Second); err != nil {
		t.Fatal(err)
	}
}

// TestNegotiateTagRepeat makes sure that negotiating a batch of tags keeps the
// local and remote tag sets lined up, and that renegotiating an existing tag is
// a no-op that hands back the same value.
func TestNegotiateTagRepeat(t *testing.T) {
	ti, mxr := newTagTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	tags := []string{`alpha`, `beta`, `charlie`, `delta`}
	tgs := make([]entry.EntryTag, 0, len(tags))
	for _, name := range tags {
		tg, err := mxr.NegotiateTag(name)
		if err != nil {
			t.Fatal(err)
		}
		if !ti.hasTag(name) {
			t.Fatalf("indexer did not have tag %q when NegotiateTag returned, it knows about %v", name, ti.tagNames())
		}
		tgs = append(tgs, tg)
	}

	//renegotiating should hand back exactly what we already have
	for i, name := range tags {
		tg, err := mxr.NegotiateTag(name)
		if err != nil {
			t.Fatal(err)
		} else if tg != tgs[i] {
			t.Fatalf("tag %q changed on renegotiation, %d != %d", name, tg, tgs[i])
		}
	}

	//every tag should route to its own well on the indexer side
	for i, name := range tags {
		if err := mxr.WriteEntry(&entry.Entry{TS: entry.Now(), Tag: tgs[i], Data: []byte(name)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := mxr.Sync(time.Second); err != nil {
		t.Fatal(err)
	}
}

// TestNegotiateTagWhileWriting exercises the negotiation path while the write
// relay routine is actively translating tags, the two share the translator.
func TestNegotiateTagWhileWriting(t *testing.T) {
	ti, mxr := newTagTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	tg, err := mxr.GetTag(tagTestTag)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		writeUntil(mxr, tg, stop)
	}()

	for i := 0; i < 16; i++ {
		name := `concurrent` + string(rune('a'+i))
		if _, err = mxr.NegotiateTag(name); err != nil {
			close(stop)
			<-done
			t.Fatal(err)
		}
		if !ti.hasTag(name) {
			close(stop)
			<-done
			t.Fatalf("indexer did not have tag %q when NegotiateTag returned, it knows about %v", name, ti.tagNames())
		}
	}
	close(stop)
	<-done
}

func newTagTestMuxer(t *testing.T) (*testIndexer, *IngestMuxer) {
	t.Helper()
	ti, err := newTestIndexer(tagTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	mxr, err := NewUniformMuxer(UniformMuxerConfig{
		Destinations:    []string{ti.Destination()},
		Tags:            []string{tagTestTag},
		Auth:            tagTestSecret,
		IngesterName:    `tagtest`,
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
	return ti, mxr
}

// TestNegotiateTagMidConnect covers a tag negotiated while one destination is still
// coming up.  getConnection answers the tag handshake and builds the translator under
// the muxer lock, then finishes the connection with that lock released, so a
// NegotiateTag in that window sees a nil entry in im.igst and skips the destination.
// Nothing after that would establish the tag short of an entry actually being written
// on it, which is the very thing this is all supposed to avoid.
func TestNegotiateTagMidConnect(t *testing.T) {
	fast, err := newTestIndexer(tagTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	defer fast.Close()
	slow, err := newTestIndexer(tagTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()

	// park the slow indexer right after it has answered the tag handshake
	slow.holdPostAuth()

	mxr, err := NewUniformMuxer(UniformMuxerConfig{
		Destinations:    []string{fast.Destination(), slow.Destination()},
		Tags:            []string{tagTestTag},
		Auth:            tagTestSecret,
		IngesterName:    `tagtest`,
		IngesterVersion: `test`,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mxr.Close()
	if err = mxr.Start(); err != nil {
		t.Fatal(err)
	}
	// WaitForHot comes back as soon as ONE connection is hot, which is the fast one
	if err = mxr.WaitForHot(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	// the slow one has answered the handshake, it just is not installed yet
	if !slow.hasTag(tagTestTag) {
		t.Fatal("the parked indexer never answered the tag handshake, the gate is in the wrong place")
	}

	if _, err = mxr.NegotiateTag(`mid-connect`); err != nil {
		t.Fatal(err)
	}
	if !fast.hasTag(`mid-connect`) {
		t.Fatalf("the hot indexer did not have the tag when NegotiateTag returned, it knows about %v", fast.tagNames())
	}

	// let the parked connection finish, it gets installed with a translator built
	// before the negotiation happened
	slow.releasePostAuth()

	// NOTHING is ever written on this tag, it has to show up purely from the reconcile
	deadline := time.Now().Add(20 * time.Second)
	for {
		if slow.hasTag(`mid-connect`) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the indexer that was still connecting never got the tag, it knows about %v, no data has flowed on it", slow.tagNames())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestNegotiateTagAllFail checks that a tag no live connection can establish comes back
// as an error rather than quietly succeeding.  Before this the caller got a nil and a
// tag that did not exist anywhere.
func TestNegotiateTagAllFail(t *testing.T) {
	ti, mxr := newTagTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	ti.rejectTag(`no-such-tag`)
	tg, err := mxr.NegotiateTag(`no-such-tag`)
	if err == nil {
		t.Fatal("NegotiateTag reported success for a tag the indexer refused")
	}
	// the intermediate tag is still good, the muxer knows it and it rides in on the
	// next handshake, the error is about the indexer not about the tag being unusable
	if tg == 0 {
		t.Fatal("expected a usable intermediate tag alongside the error")
	}
	if !slices.Contains(mxr.KnownTags(), `no-such-tag`) {
		t.Fatalf("muxer dropped the tag on a negotiation failure, it knows %v", mxr.KnownTags())
	}
}

// TestNegotiateTagPartialFailure checks that one bad indexer does not fail the call when
// another one took the tag.  Waiting for every connection would make a single sick
// indexer break tag creation for the whole cluster.
func TestNegotiateTagPartialFailure(t *testing.T) {
	good, err := newTestIndexer(tagTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	defer good.Close()
	bad, err := newTestIndexer(tagTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Close()
	bad.rejectTag(`partial`)

	mxr, err := NewUniformMuxer(UniformMuxerConfig{
		Destinations:    []string{good.Destination(), bad.Destination()},
		Tags:            []string{tagTestTag},
		Auth:            tagTestSecret,
		IngesterName:    `tagtest`,
		IngesterVersion: `test`,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mxr.Close()
	if err = mxr.Start(); err != nil {
		t.Fatal(err)
	}
	if err = mxr.WaitForHot(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	// both connections have to be INSTALLED, not merely past the handshake, otherwise
	// this only proves the single connection case
	waitHot(t, mxr, 2)

	if _, err = mxr.NegotiateTag(`partial`); err != nil {
		t.Fatalf("one indexer took the tag, that should be a success: %v", err)
	}
	if !good.hasTag(`partial`) {
		t.Fatalf("the healthy indexer did not get the tag, it knows about %v", good.tagNames())
	}
}

// TestNegotiateTagNoConnections checks that staging a tag with nothing connected is not
// an error.  Those tags go out in the authentication handshake when a connection comes
// up, so there is nothing to report and nothing to wait for.
func TestNegotiateTagNoConnections(t *testing.T) {
	ti, err := newTestIndexer(tagTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	dst := ti.Destination()
	ti.Close() //nothing is listening any more

	mxr, err := NewUniformMuxer(UniformMuxerConfig{
		Destinations:    []string{dst},
		Tags:            []string{tagTestTag},
		Auth:            tagTestSecret,
		IngesterName:    `tagtest`,
		IngesterVersion: `test`,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mxr.Close()
	if err = mxr.Start(); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, lerr := mxr.NegotiateTag(`offline`)
		done <- lerr
	}()
	select {
	case lerr := <-done:
		if lerr != nil {
			t.Fatalf("staging a tag with no connections should not be an error: %v", lerr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("NegotiateTag blocked waiting on connections that do not exist")
	}
}

// TestNegotiateTagEntriesLandOnTheRightTag is the end to end check on the translator.
// Every other test only asserts that the indexer MINTED a name, which says nothing
// about whether entries written on the intermediate tag arrive carrying the id the
// indexer minted for it.  A translator that is off by one would pass all of those and
// quietly file entries into the wrong well.
func TestNegotiateTagEntriesLandOnTheRightTag(t *testing.T) {
	ti, mxr := newTagTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	// negotiate a spread of tags, then write a different number of entries on each so
	// a shifted mapping cannot accidentally line up
	names := []string{`well-a`, `well-b`, `well-c`, `well-d`}
	counts := map[string]uint64{`well-a`: 1, `well-b`: 2, `well-c`: 3, `well-d`: 5}
	tags := make(map[string]entry.EntryTag, len(names))
	for _, name := range names {
		tg, err := mxr.NegotiateTag(name)
		if err != nil {
			t.Fatal(err)
		}
		tags[name] = tg
	}

	for _, name := range names {
		for i := uint64(0); i < counts[name]; i++ {
			ent := &entry.Entry{TS: entry.Now(), Tag: tags[name], Data: []byte(name)}
			if err := mxr.WriteEntry(ent); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := mxr.Sync(10 * time.Second); err != nil {
		t.Fatal(err)
	}

	for _, name := range names {
		want := counts[name]
		deadline := time.Now().Add(5 * time.Second)
		var got uint64
		for {
			if got = ti.entriesForTag(name); got == want {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("tag %q received %d entries on the indexer, want %d, the translator is mapping to the wrong well", name, got, want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// TestNegotiateTagConcurrentDistinctNames hammers the translator from many goroutines
// at once.  Tags have to land in the queue and in the dense active slice in the same
// order or registerTag rejects them, and this is the test that actually exercises that
// under the race detector.
func TestNegotiateTagConcurrentDistinctNames(t *testing.T) {
	ti, mxr := newTagTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	const n = 24
	var wg sync.WaitGroup
	errs := make(chan error, n)
	tags := make([]entry.EntryTag, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("concurrent-%02d", i)
			tg, err := mxr.NegotiateTag(name)
			if err != nil {
				errs <- fmt.Errorf("%s: %w", name, err)
				return
			}
			tags[i] = tg
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	// every name has to exist on the indexer, and every intermediate tag has to be
	// distinct, a collision means two names share a well
	seen := map[entry.EntryTag]string{}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("concurrent-%02d", i)
		if !ti.hasTag(name) {
			t.Fatalf("indexer never got %q, it knows about %v", name, ti.tagNames())
		}
		if prev, ok := seen[tags[i]]; ok {
			t.Fatalf("%q and %q were both handed intermediate tag %d", prev, name, tags[i])
		}
		seen[tags[i]] = name
	}

	// and the muxer's own view has to agree
	known := mxr.KnownTags()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("concurrent-%02d", i)
		if !slices.Contains(known, name) {
			t.Fatalf("muxer does not know %q, it knows %v", name, known)
		}
	}
}

// TestNegotiateTagRecoversAfterFailure covers the recovery path a failed negotiation
// leans on.  The connection is bounced and the tag goes out in the authentication
// handshake instead, so a tag that could not be renegotiated still ends up established
// without anything ever being written on it.
func TestNegotiateTagRecoversAfterFailure(t *testing.T) {
	ti, mxr := newTagTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	before := ti.Accepted()
	ti.rejectTag(`recovered`)
	if _, err := mxr.NegotiateTag(`recovered`); err == nil {
		t.Fatal("the indexer refused the renegotiation, that should be an error")
	}

	// the relay routine bounces the connection on a negotiation failure
	deadline := time.Now().Add(20 * time.Second)
	for ti.Accepted() <= before {
		if time.Now().After(deadline) {
			t.Fatalf("the connection never bounced after the failed negotiation, accepted stuck at %d", ti.Accepted())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// the reconnect carries the full tag set, so the tag gets established in the
	// handshake, still without a single entry being written on it
	waitIndexerTag(t, ti, `recovered`)
}

// TestNegotiateTagQueuedBehindFailure checks that a tag stuck behind a failing one does
// not have to sit out the whole timeout.  Negotiation stops at the first failure and
// the connection is about to be bounced, so everything queued behind it is never going
// to be attempted and its caller needs to hear about that now.
func TestNegotiateTagQueuedBehindFailure(t *testing.T) {
	ti, mxr := newTagTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	// stop the relay routine from draining so both tags pile up in the queue together
	ti.holdPostAuth()
	ti.rejectTag(`blocker`)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, name := range []string{`blocker`, `behind`} {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			_, errs[i] = mxr.NegotiateTag(name)
		}(i, name)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("a tag queued behind a failing one sat out the whole negotiation timeout")
	}
	ti.releasePostAuth()

	if errs[0] == nil {
		t.Fatal("the rejected tag should have reported a failure")
	}
	// the one behind it may go either way depending on ordering, what matters is that
	// it came back promptly rather than blocking on the timeout
	t.Logf("blocker err: %v", errs[0])
	t.Logf("behind err:  %v", errs[1])
}

// These are the invariants the rest of the tag machinery is built on, tested without a
// socket in sight.  The translator is a dense slice indexed by the intermediate tag, so
// anything that lets it go non dense or out of order corrupts tag routing everywhere.

func newTestTagTrans(n int) *tagTrans {
	tt := &tagTrans{
		active: make([]entry.EntryTag, n),
		notify: make(chan struct{}, 1),
	}
	for i := range tt.active {
		//remote ids deliberately do not match local ones
		tt.active[i] = entry.EntryTag(100 + i)
	}
	return tt
}

func TestTagTransTranslate(t *testing.T) {
	tt := newTestTagTrans(3)

	for i := 0; i < 3; i++ {
		got, ok := tt.translate(entry.EntryTag(i))
		if !ok {
			t.Fatalf("tag %d did not translate", i)
		}
		if want := entry.EntryTag(100 + i); got != want {
			t.Fatalf("tag %d translated to %d, want %d", i, got, want)
		}
	}
	// a tag past the end is not translatable, the caller has to go negotiate it
	if _, ok := tt.translate(entry.EntryTag(3)); ok {
		t.Fatal("a tag the translator has never seen should not translate")
	}
	// gravwell always passes straight through, even on an empty translator
	empty := &tagTrans{notify: make(chan struct{}, 1)}
	if got, ok := empty.translate(entry.GravwellTagId); !ok || got != entry.GravwellTagId {
		t.Fatalf("the gravwell tag must pass through untouched, got %d ok=%v", got, ok)
	}
}

func TestTagTransHasTagBoundaries(t *testing.T) {
	tt := newTestTagTrans(2)
	if !tt.hasTag(0) || !tt.hasTag(1) {
		t.Fatal("negotiated tags should report as present")
	}
	// the boundary is the whole point, len is one past the last valid index
	if tt.hasTag(2) {
		t.Fatal("a tag equal to the active length is out of range, not present")
	}
	if tt.hasTag(3) {
		t.Fatal("a tag past the active length is not present")
	}
	if !tt.hasTag(entry.GravwellTagId) {
		t.Fatal("the gravwell tag is always present")
	}
}

func TestTagTransRegisterTagOrder(t *testing.T) {
	tt := newTestTagTrans(2)

	// out of order registration has to be refused, silently accepting it would put
	// entries in the wrong well
	if err := tt.registerTag(3, 900); err == nil {
		t.Fatal("registering a tag past the end of the translator should fail")
	}
	if err := tt.registerTag(1, 900); err == nil {
		t.Fatal("re-registering an existing tag should fail")
	}
	if err := tt.registerTag(2, 900); err != nil {
		t.Fatalf("registering the next tag in order should work: %v", err)
	}
	if got, ok := tt.translate(2); !ok || got != 900 {
		t.Fatalf("tag 2 translated to %d ok=%v, want 900", got, ok)
	}
}

func TestTagTransNegotiationQueueIsFIFO(t *testing.T) {
	tt := newTestTagTrans(1)
	if tt.negotiationsPending() {
		t.Fatal("a fresh translator has nothing queued")
	}

	for i, name := range []string{`first`, `second`, `third`} {
		if err := tt.registerTagForNegotiation(name, entry.EntryTag(1+i), nil); err != nil {
			t.Fatal(err)
		}
	}
	if !tt.negotiationsPending() {
		t.Fatal("three tags are queued, something is pending")
	}

	// peek does not consume, the entry only leaves once it has been registered
	for i := 0; i < 2; i++ {
		v, ok := tt.peekToNegotiate()
		if !ok {
			t.Fatal("expected a queued tag")
		}
		if v.name != `first` || v.local != 1 {
			t.Fatalf("peek %d gave %q/%d, want first/1", i, v.name, v.local)
		}
	}

	tt.clearToNegotiate(1)
	v, ok := tt.peekToNegotiate()
	if !ok || v.name != `second` {
		t.Fatalf("after clearing one the head should be second, got %q ok=%v", v.name, ok)
	}

	tt.clearToNegotiate(5) //more than is left
	if tt.negotiationsPending() {
		t.Fatal("clearing more than is queued should empty the queue, not wrap")
	}
	if _, ok = tt.peekToNegotiate(); ok {
		t.Fatal("nothing should be left to peek at")
	}
}

func TestTagTransWakeIsNonBlocking(t *testing.T) {
	tt := newTestTagTrans(1)
	// far more wakes than the buffer holds, none of them may block
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			tt.wake()
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("wake blocked, it must never block the muxer lock holder")
	}
	// and exactly one wake up is pending, which is all the relay routine needs
	select {
	case <-tt.notify:
	default:
		t.Fatal("expected a pending wake up")
	}
	select {
	case <-tt.notify:
		t.Fatal("the wake up channel should be drained")
	default:
	}
}

func TestTagTransReverse(t *testing.T) {
	tt := newTestTagTrans(3)
	for i := 0; i < 3; i++ {
		if got := tt.reverse(entry.EntryTag(100 + i)); got != entry.EntryTag(i) {
			t.Fatalf("remote %d reversed to %d, want %d", 100+i, got, i)
		}
	}
	if got := tt.reverse(entry.GravwellTagId); got != entry.GravwellTagId {
		t.Fatalf("the gravwell tag must reverse to itself, got %d", got)
	}
	// an unknown remote tag falls back to the default well rather than exploding
	if got := tt.reverse(9999); got != 0 {
		t.Fatalf("an unknown remote tag should fall back to 0, got %d", got)
	}
}

// TestNegotiationWaiterFirstSuccessWins is the contract NegotiateTag rests on, one
// indexer taking the tag is enough even when others are failing.
func TestNegotiationWaiterFirstSuccessWins(t *testing.T) {
	w := newNegotiationWaiter()
	w.add()
	w.add()
	w.add()

	w.report(errors.New(`one down`))
	select {
	case <-w.done:
		t.Fatal("one failure out of three must not settle the waiter")
	default:
	}

	w.report(nil)
	select {
	case <-w.done:
	default:
		t.Fatal("a success has to settle the waiter immediately")
	}
	if err := w.err(); err != nil {
		t.Fatalf("a settled success must not report an error: %v", err)
	}

	// a straggler reporting after the fact changes nothing
	w.report(errors.New(`too late`))
	if err := w.err(); err != nil {
		t.Fatalf("a late failure must not undo a success: %v", err)
	}
}

func TestNegotiationWaiterAllFail(t *testing.T) {
	w := newNegotiationWaiter()
	w.add()
	w.add()

	w.report(errors.New(`indexer one said no`))
	select {
	case <-w.done:
		t.Fatal("still one connection outstanding")
	default:
	}

	w.report(errors.New(`indexer two said no`))
	select {
	case <-w.done:
	default:
		t.Fatal("the last failure has to settle the waiter")
	}
	err := w.err()
	if err == nil {
		t.Fatal("expected an error when every connection failed")
	}
	// both reasons have to survive, one of them is usually the interesting one
	for _, want := range []string{`indexer one said no`, `indexer two said no`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

func TestNegotiationWaiterSeal(t *testing.T) {
	// nothing to negotiate against is a success, the tag rides in on the handshake
	w := newNegotiationWaiter()
	if !w.seal() {
		t.Fatal("a waiter with no connections should seal")
	}
	if err := w.err(); err != nil {
		t.Fatalf("sealing with nothing outstanding is not a failure: %v", err)
	}

	// a waiter with work outstanding must not be sealed out from under it
	w = newNegotiationWaiter()
	w.add()
	if w.seal() {
		t.Fatal("seal must not settle a waiter that still has a connection working")
	}
	select {
	case <-w.done:
		t.Fatal("seal closed a waiter that was still working")
	default:
	}
	w.report(nil)
	if err := w.err(); err != nil {
		t.Fatal(err)
	}
}

func TestNegotiationWaiterWaitTimeout(t *testing.T) {
	w := newNegotiationWaiter()
	w.add()

	ts := time.Now()
	if err := w.wait(context.Background(), context.Background(), 50*time.Millisecond); err != ErrTimeout {
		t.Fatalf("expected a timeout, got %v", err)
	}
	if el := time.Since(ts); el > 5*time.Second {
		t.Fatalf("the timeout did not bound the wait, took %v", el)
	}
}

func TestNegotiationWaiterWaitContext(t *testing.T) {
	w := newNegotiationWaiter()
	w.add()

	ctx, cf := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cf()
	}()
	if err := w.wait(ctx, context.Background(), 0); err != context.Canceled {
		t.Fatalf("expected the caller context to cancel the wait, got %v", err)
	}

	// a muxer going down must not leave the caller sitting on the timeout either
	w = newNegotiationWaiter()
	w.add()
	muxCtx, muxCf := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		muxCf()
	}()
	if err := w.wait(context.Background(), muxCtx, time.Minute); err != ErrNotRunning {
		t.Fatalf("expected the muxer shutdown to end the wait, got %v", err)
	}
}

// TestNegotiationWaiterConcurrentReports is the race detector's problem, the relay
// routines all report independently.
func TestNegotiationWaiterConcurrentReports(t *testing.T) {
	const n = 32
	w := newNegotiationWaiter()
	for i := 0; i < n; i++ {
		w.add()
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i == n/2 {
				w.report(nil)
				return
			}
			w.report(errors.New(`nope`))
		}(i)
	}
	wg.Wait()
	select {
	case <-w.done:
	default:
		t.Fatal("the waiter never settled")
	}
	if err := w.err(); err != nil {
		t.Fatalf("one report succeeded, the waiter should be a success: %v", err)
	}
}

// TestTagMaskTrackerConcurrent is why tagMaskTracker went atomic, a tag negotiated on a
// running muxer is written while every WriteEntry is reading the same word.
func TestTagMaskTrackerConcurrent(t *testing.T) {
	var tmt tagMaskTracker
	// all of these land in the same 32 bit word, which is the interesting case
	tags := []entry.EntryTag{0, 1, 2, 3, 30, 31}
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, tg := range tags {
				_ = tmt.has(tg)
			}
		}
	}()

	for _, tg := range tags {
		wg.Add(1)
		go func(tg entry.EntryTag) {
			defer wg.Done()
			tmt.add(tg)
		}(tg)
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	// every add has to have survived, a read modify write race would drop some
	for _, tg := range tags {
		if !tmt.has(tg) {
			t.Fatalf("tag %d was lost, concurrent adds to one word clobbered each other", tg)
		}
	}
}

// TestTagTransFailQueued covers the path that keeps a tag stuck behind a failing one
// from sitting out the whole negotiation timeout.  Negotiation stops at the first
// failure and the connection is about to be bounced, so nothing queued behind it will
// ever be attempted on that connection.
func TestTagTransFailQueued(t *testing.T) {
	tt := newTestTagTrans(1)

	first := newNegotiationWaiter()
	first.add()
	second := newNegotiationWaiter()
	second.add()
	third := newNegotiationWaiter()
	third.add()

	if err := tt.registerTagForNegotiation(`one`, 1, first); err != nil {
		t.Fatal(err)
	}
	if err := tt.registerTagForNegotiation(`two`, 2, second); err != nil {
		t.Fatal(err)
	}
	if err := tt.registerTagForNegotiation(`three`, 3, third); err != nil {
		t.Fatal(err)
	}

	boom := errors.New(`indexer refused it`)
	tt.failQueued(boom)

	for name, w := range map[string]*negotiationWaiter{`one`: first, `two`: second, `three`: third} {
		select {
		case <-w.done:
		default:
			t.Fatalf("waiter for %q was left hanging, it would have burned the full timeout", name)
		}
		err := w.err()
		if err == nil {
			t.Fatalf("waiter for %q reported success after the connection gave up", name)
		}
		if !strings.Contains(err.Error(), boom.Error()) {
			t.Fatalf("waiter for %q lost the reason: %v", name, err)
		}
	}

	// a nil waiter is the reconcile path, it must not panic
	tt2 := newTestTagTrans(1)
	if err := tt2.registerTagForNegotiation(`nobody-waiting`, 1, nil); err != nil {
		t.Fatal(err)
	}
	tt2.failQueued(boom)
}
