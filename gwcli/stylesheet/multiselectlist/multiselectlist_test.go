//go:build ci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package multiselectlist_test

import (
	"slices"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
)

type testItem struct {
	title       string
	description string
}

func (ti testItem) Title() string {
	return ti.title
}

func (ti testItem) Description() string {
	return ti.description
}

func (ti testItem) FilterValue() string {
	return ti.title
}

func TestPreSelection(t *testing.T) {
	t.Run("pre-select items 1 and 3", func(t *testing.T) {
		var items = []multiselectlist.SelectableItem[int]{
			&multiselectlist.DefaultSelectableItem[int]{Title_: "0", Description_: "desc0", ID_: 0},
			&multiselectlist.DefaultSelectableItem[int]{Title_: "1", Description_: "desc1", Selected_: true, ID_: 1},
			&multiselectlist.DefaultSelectableItem[int]{Title_: "2", Description_: "desc2", ID_: 2},
			&multiselectlist.DefaultSelectableItem[int]{Title_: "3", Description_: "desc3", Selected_: true, ID_: 3},
		}

		msl := multiselectlist.New(items, 80, 50,
			multiselectlist.Options{})
		want := []multiselectlist.SelectableItem[int]{
			&multiselectlist.DefaultSelectableItem[int]{Title_: "1", Description_: "desc1", Selected_: true, ID_: 1},
			&multiselectlist.DefaultSelectableItem[int]{Title_: "3", Description_: "desc3", Selected_: true, ID_: 3},
		}

		selected := msl.GetSelectedItems()
		if !slices.EqualFunc(selected, want, func(a, b multiselectlist.SelectableItem[int]) bool {
			return a.ID() == b.ID()
		}) {
			t.Fatal("incorrect selected items", testsupport.ExpectedActual(want, selected))
		}
	})
	t.Run("no pre-selections", func(t *testing.T) {
		var items = []multiselectlist.SelectableItem[int]{
			&multiselectlist.DefaultSelectableItem[int]{Title_: "0", Description_: "desc0", ID_: 0},
			&multiselectlist.DefaultSelectableItem[int]{Title_: "1", Description_: "desc1", ID_: 1},
			&multiselectlist.DefaultSelectableItem[int]{Title_: "2", Description_: "desc2", ID_: 2},
			&multiselectlist.DefaultSelectableItem[int]{Title_: "3", Description_: "desc3", ID_: 3},
		}

		msl := multiselectlist.New(items, 80, 50, multiselectlist.Options{})
		want := []multiselectlist.SelectableItem[int]{}

		selected := msl.GetSelectedItems()
		if !slices.EqualFunc(selected, want, func(a, b multiselectlist.SelectableItem[int]) bool {
			return a.ID() == b.ID()
		}) {
			t.Fatal("incorrect selected items", testsupport.ExpectedActual(want, selected))
		}
	})
}

func TestToggleAndGetCurrentItems(t *testing.T) {
	t.Run("pre-select items 1 and 3", func(t *testing.T) {
		// pre-select items 1 and 3, then toggle item zero to selected manually
		var items = []multiselectlist.SelectableItem[int]{
			&multiselectlist.DefaultSelectableItem[int]{Title_: "0", Description_: "desc0", ID_: 0},
			&multiselectlist.DefaultSelectableItem[int]{Title_: "1", Description_: "desc1", Selected_: true, ID_: 1},
			&multiselectlist.DefaultSelectableItem[int]{Title_: "2", Description_: "desc2", ID_: 2},
			&multiselectlist.DefaultSelectableItem[int]{Title_: "3", Description_: "desc3", Selected_: true, ID_: 3},
		}
		msl := multiselectlist.New(items, 80, 50,
			multiselectlist.Options{})
		if msl.Cursor() != 0 {
			t.Error("cursor is not index 0 at start! Cursor: ", msl.Cursor())
		}
		msl.ToggleCurrentItem()

		want := []multiselectlist.SelectableItem[int]{
			&multiselectlist.DefaultSelectableItem[int]{Title_: "0", Description_: "desc0", ID_: 0},
			&multiselectlist.DefaultSelectableItem[int]{Title_: "1", Description_: "desc1", Selected_: true, ID_: 1},
			&multiselectlist.DefaultSelectableItem[int]{Title_: "3", Description_: "desc3", Selected_: true, ID_: 3},
		}

		selected := msl.GetSelectedItems()
		if !slices.EqualFunc(selected, want, func(a, b multiselectlist.SelectableItem[int]) bool {
			return a.ID() == b.ID()
		}) {
			t.Fatal("incorrect selected items", testsupport.ExpectedActual(want, selected))
		}
	})
}

// TestModel runs a few key commands through Update and View to check that interactivity works.
func TestModel(t *testing.T) {
	var items = []multiselectlist.SelectableItem[string]{
		&multiselectlist.DefaultSelectableItem[string]{Title_: "0", Description_: "desc0"},
		&multiselectlist.DefaultSelectableItem[string]{Title_: "1", Description_: "desc1"},
		&multiselectlist.DefaultSelectableItem[string]{Title_: "2", Description_: "desc2", Selected_: true},
		&multiselectlist.DefaultSelectableItem[string]{Title_: "3", Description_: "desc3", Selected_: true},
	}
	// NOTE(rlandau): view wants can probably be replaced by teatest for cleaner interaction,
	// but I haven't had much luck with teatest.
	msl := multiselectlist.New(items, 30, 20,
		multiselectlist.Options{ShowSelectStateFunc: func(selected bool) string {
			if selected {
				return "(-)"
			}
			return "( )"
		}})
	t.Run("initial view", func(t *testing.T) {
		want := testsupport.LinesTrimSpace(`   List                                                   
                                                          
  4 items                                                 
                                                          
│ ( ) 0                                                   
│ desc0                                                   
                                                          
  ( ) 1                                                   
  desc1                                                   
                                                          
  (-) 2                                                   
  desc2                                                   
                                                          
  (-) 3                                                   
  desc3                                                   
                                                          
                                                          
                                                          
                                                          
  ↑ cursor up • ↓ cursor down • \ filter • shift+← clear filter • ↹ accept • ctrl+\ cancel filter • esc quit • ? more
  space select • ↲ continue`)
		if v := testsupport.LinesTrimSpace(msl.View()); v != want {
			t.Fatal("incorrect view", testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
	})
	t.Run("toggle first and last items", func(t *testing.T) {
		msl, _ = msl.Update(testsupport.SendHotkey(hotkeys.Select)) // toggle first
		// Reminder: lists do not natively support wrapping!
		msl.CursorDown()
		msl, _ = msl.Update(testsupport.SendHotkey(hotkeys.CursorDown)) // should have the same result as .CursorDown()
		msl.CursorDown()
		msl.ToggleCurrentItem() // toggle last
		want := testsupport.LinesTrimSpace(`   List                                                   
                                                          
  4 items                                                 
                                                          
  (-) 0                                                   
  desc0                                                   
                                                          
  ( ) 1                                                   
  desc1                                                   
                                                          
  (-) 2                                                   
  desc2                                                   
                                                          
│ ( ) 3                                                   
│ desc3                                                   
                                                          
                                                          
                                                          
                                                          
  ↑ cursor up • ↓ cursor down • \ filter • shift+← clear filter • ↹ accept • ctrl+\ cancel filter • esc quit • ? more
  space select • ↲ continue`)
		if v := testsupport.LinesTrimSpace(msl.View()); v != want {
			t.Fatal("incorrect view", testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
	})
	if numSel := len(msl.GetSelectedItems()); numSel != 2 {
		t.Error("incorrect number of items selected.", testsupport.ExpectedActual(2, numSel))
	}
	t.Run("done", func(t *testing.T) {
		msl, _ = msl.Update(testsupport.SendHotkey(hotkeys.Invoke))
		want := testsupport.LinesTrimSpace(`   List                                                   
                                                          
  4 items                                                 
                                                          
  (-) 0                                                   
  desc0                                                   
                                                          
  ( ) 1                                                   
  desc1                                                   
                                                          
  (-) 2                                                   
  desc2                                                   
                                                          
│ ( ) 3                                                   
│ desc3                                                   
                                                          
                                                          
                                                          
                                                          
  ↑ cursor up • ↓ cursor down • \ filter • shift+← clear filter • ↹ accept • ctrl+\ cancel filter • esc quit • ? more
  space select • ↲ continue`)
		if v := testsupport.LinesTrimSpace(msl.View()); v != want {
			t.Error("incorrect view", testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
		if !msl.Done() {
			t.Error("expected msl to be done after sending Enter.")
		}
	})
}
