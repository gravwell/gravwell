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
