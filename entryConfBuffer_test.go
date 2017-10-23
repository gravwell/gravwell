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
	_, err := newEntryConfirmationBuffer(DEFAULT_MAX_UNCONFIRMED)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOrderedPushPop(t *testing.T) {
	var ent *entry.Entry
	entcb, err := newEntryConfirmationBuffer(DEFAULT_MAX_UNCONFIRMED)
	if err != nil {
		t.Fatal(err)
	}
	for i := entrySendID(0); i < entrySendID(8); i++ {
		err = entcb.Add(&entryConfirmation{i, ent})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := entrySendID(0); i < entrySendID(8); i++ {
		err = entcb.Confirm(i)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestUnorderedPushPop(t *testing.T) {
	var ent *entry.Entry
	entcb, err := newEntryConfirmationBuffer(DEFAULT_MAX_UNCONFIRMED)
	if err != nil {
		t.Fatal(err)
	}
	for i := entrySendID(1); i <= entrySendID(8); i++ {
		err = entcb.Add(&entryConfirmation{i, ent})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := entrySendID(8); i > entrySendID(0); i-- {
		err = entcb.Confirm(i)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestMixedUnorderedPushPop(t *testing.T) {
	var ent *entry.Entry
	entcb, err := newEntryConfirmationBuffer(DEFAULT_MAX_UNCONFIRMED)
	if err != nil {
		t.Fatal(err)
	}
	for i := entrySendID(1); i <= entrySendID(8); i++ {
		err = entcb.Add(&entryConfirmation{i, ent})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := entrySendID(4); i > entrySendID(2); i-- {
		err = entcb.Confirm(i)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := entrySendID(9); i <= entrySendID(16); i++ {
		err = entcb.Add(&entryConfirmation{i, ent})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := entrySendID(1); i <= entrySendID(2); i++ {
		err = entcb.Confirm(i)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := entrySendID(5); i <= entrySendID(16); i++ {
		err = entcb.Confirm(i)
		if err != nil {
			t.Fatal(err)
		}
	}
}
