/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"testing"

	"github.com/gravwell/gravwell/v4/ingest/entry"
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
