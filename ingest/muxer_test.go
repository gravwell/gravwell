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
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/entry"
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

// muxerForTags is a muxer with just enough filled in to negotiate a tag.
func muxerForTags(tagMap map[string]entry.EntryTag) *IngestMuxer {
	im := &IngestMuxer{
		mtx:            &sync.RWMutex{},
		tagMap:         tagMap,
		tagTranslators: []*tagTrans{},
		igst:           []*IngestConnection{},
	}
	for name, tg := range tagMap {
		im.tags = append(im.tags, name)
		im.tc.add(tg)
	}
	return im
}

// TestNegotiateTagIDsStayDense covers the rule newTagTrans depends on: tag IDs run from
// zero to the number of tags with no gaps.
//
// The empty map was the broken case.  NegotiateTag picks one past the highest ID in use
// and its running maximum starts at zero, so an empty map looked like one whose highest ID
// was zero: the first tag got ID 1 and nothing held ID 0.  newTagTrans then indexed [1]
// into a table of length 1 and panicked.
func TestNegotiateTagIDsStayDense(t *testing.T) {
	for _, tc := range []struct {
		name string
		have map[string]entry.EntryTag
		want entry.EntryTag
	}{
		{`empty, the first tag ever negotiated`, map[string]entry.EntryTag{}, 0},
		{`one tag already at zero`, map[string]entry.EntryTag{`a`: 0}, 1},
		{`two dense tags`, map[string]entry.EntryTag{`a`: 0, `b`: 1}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// the map is handed over, not copied, so take the count before it grows
			was := len(tc.have)
			im := muxerForTags(tc.have)
			tg, err := im.NegotiateTag(`newtag`)
			if err != nil {
				t.Fatalf("failed to negotiate: %v", err)
			}
			if tg != tc.want {
				t.Errorf("negotiated ID %d, want %d", tg, tc.want)
			}
			if tg != im.tagMap[`newtag`] {
				t.Errorf("returned ID %d but stored %d", tg, im.tagMap[`newtag`])
			}
			// the invariant newTagTrans relies on
			assertDenseTagIDs(t, im.tagMap)

			// negotiating the same name again is a lookup, not a second ID
			again, err := im.NegotiateTag(`newtag`)
			if err != nil {
				t.Fatalf("failed to re-negotiate: %v", err)
			}
			if again != tg {
				t.Errorf("re-negotiating gave %d, want the original %d", again, tg)
			}
			if len(im.tagMap) != was+1 {
				t.Errorf("the map holds %d tags, want %d", len(im.tagMap), was+1)
			}
		})
	}
}

// assertDenseTagIDs checks the IDs in a tag map are exactly 0..len-1, which is the
// contract between NegotiateTag and newTagTrans.
func assertDenseTagIDs(t *testing.T, tagMap map[string]entry.EntryTag) {
	t.Helper()
	seen := make(map[entry.EntryTag]string, len(tagMap))
	for name, tg := range tagMap {
		if int(tg) >= len(tagMap) {
			t.Errorf("tag %q holds ID %d, which is past the end of a %d entry table",
				name, tg, len(tagMap))
		}
		if other, dup := seen[tg]; dup {
			t.Errorf("tags %q and %q share ID %d", name, other, tg)
		}
		seen[tg] = name
	}
}

// TestNegotiateTagRefusesAtTheCeiling covers the wrap the count check misses: cached IDs
// are not dense, so the highest can sit at the ceiling while the map is small.
func TestNegotiateTagRefusesAtTheCeiling(t *testing.T) {
	im := muxerForTags(map[string]entry.EntryTag{`ceiling`: entry.MaxTagId})
	if _, err := im.NegotiateTag(`newtag`); !errors.Is(err, ErrTooManyTags) {
		t.Errorf("negotiating past the highest ID gave %v, want ErrTooManyTags", err)
	}
	// a refusal must not leave the name behind in the tag list with no ID of its own
	if _, ok := im.tagMap[`newtag`]; ok {
		t.Error("a refused tag was still added to the map")
	}
	for _, name := range im.tags {
		if name == `newtag` {
			t.Error("a refused tag was left in the tag list, so the list and the map disagree")
		}
	}
}

// TestNewTagTransRejectsSparseIDs covers the guard.  An ID at or past the end of the
// table is a broken tag map and has to be reported; as a bare > it panicked instead.
func TestNewTagTransRejectsSparseIDs(t *testing.T) {
	// The guard fires before the connection is used, so a nil one is fine.  That is also
	// why this recovers: if the guard stops rejecting, the code reaches igst.GetTag and
	// dies on that nil, and a bare panic would take the whole package run down.
	reject := func(name string, tagMap map[string]entry.EntryTag) {
		t.Helper()
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("%s: newTagTrans panicked instead of rejecting a broken tag map: %v", name, r)
			}
		}()
		if _, err := muxerForTags(tagMap).newTagTrans(nil); !errors.Is(err, ErrTagMapInvalid) {
			t.Errorf("%s: got %v, want ErrTagMapInvalid", name, err)
		}
	}

	// an ID exactly equal to the table length, which is the case a bare > let through
	reject(`an ID one past the end`, map[string]entry.EntryTag{`sparse`: 1})
	// an ID well past it, the shape a restored tag cache can produce
	reject(`an ID far past the end`, map[string]entry.EntryTag{`sparse`: 41})
	// and an empty map is refused rather than producing an empty translator
	reject(`an empty tag map`, map[string]entry.EntryTag{})
}

// TestNegotiateTagOnEmptyMuxerGoesHot is the failure end to end, over a real connection.
//
// It is what a hosted ingester with dynamic configuration does on a fresh install: start
// with no tags because no plugin is configured, then negotiate the first one when a
// webserver deploys a runner.  Before the fix that tag got ID 1 with one entry in the map,
// and the next connection built a table of length 1 and assigned to index 1, panicking
// inside connRoutine.
//
// A panic in another goroutine cannot be recovered, so a regression here kills the run
// rather than failing this test.  The bug was a crash, not a bad error.
func TestNegotiateTagOnEmptyMuxerGoesHot(t *testing.T) {
	const secret = `tagidsecret`
	ti, err := newTestIndexer(secret)
	if err != nil {
		t.Fatal(err)
	}
	defer ti.Close()

	// no tags: the muxer that an ingester with no plugins configured builds
	mxr, err := NewUniformMuxer(UniformMuxerConfig{
		Destinations:    []string{ti.Destination()},
		Auth:            secret,
		CacheDepth:      64,
		IngesterName:    `tagidtest`,
		IngesterVersion: `test`,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mxr.Close()
	if len(mxr.tagMap) != 0 {
		t.Fatalf("a muxer built with no tags holds %d of them, this test is not testing what it thinks", len(mxr.tagMap))
	}
	if err = mxr.Start(); err != nil {
		t.Fatal(err)
	}

	// the first tag arrives while the muxer is already running, as it does for a
	// dynamically deployed runner
	tg, err := mxr.NegotiateTag(`dynamic-tag`)
	if err != nil {
		t.Fatalf("failed to negotiate the first tag: %v", err)
	}
	if tg != 0 {
		t.Errorf("the first tag negotiated took ID %d, want 0: ID %d leaves a hole at zero and "+
			"newTagTrans indexes its table by ID", tg, tg)
	}

	// and it has to connect, which is where the crash landed
	if err = mxr.WaitForHot(15 * time.Second); err != nil {
		t.Fatalf("the muxer never went hot after negotiating its first tag: %v", err)
	}

	// the tag has to be usable, not just non-fatal: a table that is the right size but
	// wrong would send entries under somebody else's tag
	if err = mxr.WriteEntry(&entry.Entry{
		TS:   entry.Now(),
		Tag:  tg,
		Data: []byte(`tag id regression`),
	}); err != nil {
		t.Fatalf("failed to write with the negotiated tag: %v", err)
	}
	if err = mxr.Sync(10 * time.Second); err != nil {
		t.Fatalf("failed to sync: %v", err)
	}
	if n := ti.Entries(); n != 1 {
		t.Errorf("the indexer received %d entries, want 1", n)
	}
	for _, err := range ti.Errors() {
		t.Errorf("the indexer reported an error: %v", err)
	}
}
