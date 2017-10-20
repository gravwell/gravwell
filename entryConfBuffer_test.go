/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"github.com/gravwell/ingest/entry"
	"testing"
)

const (
	DEFAULT_MAX_UNCONFIRMED int = 128
)

func TestECBInit(t *testing.T) {
	_, err := NewEntryConfirmationBuffer(DEFAULT_MAX_UNCONFIRMED)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOrderedPushPop(t *testing.T) {
	var ent *entry.Entry
	entcb, err := NewEntryConfirmationBuffer(DEFAULT_MAX_UNCONFIRMED)
	if err != nil {
		t.Fatal(err)
	}
	for i := EntrySendID(0); i < EntrySendID(8); i++ {
		err = entcb.Add(&EntryConfirmation{i, ent})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := EntrySendID(0); i < EntrySendID(8); i++ {
		err = entcb.Confirm(i)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestUnorderedPushPop(t *testing.T) {
	var ent *entry.Entry
	entcb, err := NewEntryConfirmationBuffer(DEFAULT_MAX_UNCONFIRMED)
	if err != nil {
		t.Fatal(err)
	}
	for i := EntrySendID(1); i <= EntrySendID(8); i++ {
		err = entcb.Add(&EntryConfirmation{i, ent})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := EntrySendID(8); i > EntrySendID(0); i-- {
		err = entcb.Confirm(i)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestMixedUnorderedPushPop(t *testing.T) {
	var ent *entry.Entry
	entcb, err := NewEntryConfirmationBuffer(DEFAULT_MAX_UNCONFIRMED)
	if err != nil {
		t.Fatal(err)
	}
	for i := EntrySendID(1); i <= EntrySendID(8); i++ {
		err = entcb.Add(&EntryConfirmation{i, ent})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := EntrySendID(4); i > EntrySendID(2); i-- {
		err = entcb.Confirm(i)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := EntrySendID(9); i <= EntrySendID(16); i++ {
		err = entcb.Add(&EntryConfirmation{i, ent})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := EntrySendID(1); i <= EntrySendID(2); i++ {
		err = entcb.Confirm(i)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := EntrySendID(5); i <= EntrySendID(16); i++ {
		err = entcb.Confirm(i)
		if err != nil {
			t.Fatal(err)
		}
	}
}
