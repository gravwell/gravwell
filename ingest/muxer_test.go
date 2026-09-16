/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"encoding/json"
	"fmt"
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

// TestNegotiateTagIsImmediate checks that a tag negotiated on a live muxer
// actually exists on the indexer when NegotiateTag returns.  Ingesters and
// preprocessors (the routers in particular) negotiate their tags at startup and
// may not push an entry on one of them for hours, so a tag that is not
// established until data flows is not searchable in the meantime.
func TestNegotiateTagIsImmediate(t *testing.T) {
	ti, mxr := newTagTestMuxer(t)
	defer ti.Close()
	defer mxr.Close()

	tg, err := mxr.NegotiateTag(`new-tag`)
	if err != nil {
		t.Fatal(err)
	}
	if !ti.hasTag(`new-tag`) {
		t.Fatalf("indexer did not get the tag negotiation, it knows about %v", ti.tagNames())
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
			t.Fatalf("indexer did not get tag %q, it knows about %v", name, ti.tagNames())
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
			t.Fatalf("indexer did not get tag %q, it knows about %v", name, ti.tagNames())
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
