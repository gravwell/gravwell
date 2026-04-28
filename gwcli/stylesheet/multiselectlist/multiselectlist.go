/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package multiselectlist wraps the list bubble to enable selecting multiple items at once.
//
// TODO Generalize this package.
//
// - Allow users to pass in a delegate.
//
// - Enable overriding of enter and space as the interaction keys.
//
// - Properly display keys, rather than just attaching Enter and Space below View.
//
// - Do not import our stylesheet.
package multiselectlist

import (
	"fmt"
	"maps"
	"slices"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
)

// Model bolts additional functionality onto the list bubble such that multiple items can be selected.
type Model[ID_t comparable] struct {
	list.Model

	done bool

	// if set, Update will batch an extra tea.Cmd with a status message stating that the item was selected.
	StatusMessageOnSelect bool
}

// New returns a Multi-Select enabled list with the default delegate used by list.
func New[ID_t comparable](items []SelectableItem[ID_t], width, height int, opts Options) Model[ID_t] {
	dd := NewDefaultDelegate[ID_t](opts.ShowSelectStateFunc)
	dd.ShowDescription = !opts.HideDescription

	msl := Model[ID_t]{
		Model: list.New(wrapItems(items), dd, width, height),
	}

	hotkeys.ApplyToList(&msl.KeyMap)
	msl.KeyMap.AcceptWhileFiltering = key.NewBinding(key.WithKeys(tea.KeyEnter.String()))

	return msl
}

func wrapItems[ID_t comparable](bareItems []SelectableItem[ID_t]) []list.Item {
	// wrap each item in our select-enabled item type
	wrapped := make([]list.Item, len(bareItems))
	for i, item := range bareItems {
		wrapped[i] = item
	}
	return wrapped
}

func (msl Model[ID_t]) Update(msg tea.Msg) (Model[ID_t], tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		msl.Model.SetHeight(msg.Height)
		msl.Model.SetWidth(msg.Width)
		return msl, nil
	}

	// handle special inputs
	switch {
	case hotkeys.IsSelect(msg):
		cmd := msl.ToggleCurrentItem()
		return msl, cmd
	case hotkeys.IsInvoke(msg) && !msl.FilteringEnabled():
		msl.done = true
		return msl, nil
	}

	var cmd tea.Cmd
	msl.Model, cmd = msl.Model.Update(msg)
	return msl, cmd
}

func (msl Model[ID_t]) View() string {
	return msl.Model.View() + "\n  " + stylesheet.Cur.DisabledText.Render("space select • ↲ continue")
}

// CursorItem returns the item at the current cursor.
func (msl *Model[ID_t]) CursorItem() SelectableItem[ID_t] {
	return msl.Item(msl.Index(), false)
}

// Item fetches the item at the given index.
//
// If global, index ignores current filter state.
//
// Returns nil if not found.
func (msl *Model[ID_t]) Item(index int, global bool) SelectableItem[ID_t] {
	if index < 0 {
		return nil
	}

	var items []list.Item
	if global {
		items = msl.Items()
	} else {
		items = msl.VisibleItems()
	}

	if len(items) == 0 || len(items) <= index {
		return nil
	}

	sel, ok := items[index].(SelectableItem[ID_t])
	if !ok {
		panicFailedAssert(items[index])
	}
	return sel
}

// ToggleCurrentItem does as it says on the tin.
// If no item is selected (aka the list is empty or your cursor is off in wonderland), this is a no-op.
//
// NOTE(rlandau): This function can panic, but if it does, something has gone truly, horrifically wrong.
func (msl *Model[ID_t]) ToggleCurrentItem() tea.Cmd {
	item := msl.CursorItem()
	if item == nil {
		return nil
	}
	item.SetSelected(!item.Selected())
	// reinsert the item
	cmd := msl.Model.SetItem(msl.Model.GlobalIndex(), item)

	if msl.StatusMessageOnSelect {
		var statusMsg string
		if item.Selected() {
			statusMsg = "selected"
		} else {
			statusMsg = "deselected"
		}
		statusMsg += " " + item.Title()
		cmd = tea.Batch(cmd, msl.Model.NewStatusMessage(statusMsg))
	}

	return cmd

}

// Done returns true once the user hits enter.
// It should be checked after each msl.Update()
func (msl *Model[ID_t]) Done() bool {
	return msl.done
}

// Undone unsets the done flag without resetting the whole model.
func (msl *Model[ID_t]) Undone() {
	msl.done = false
}

// GetSelectedItems returns the list of selected items.
//
// Operates in O(n) time where n = len(msl.Items()).
func (msl *Model[ID_t]) GetSelectedItems() []SelectableItem[ID_t] {
	items := msl.Model.Items()
	// cast items back to Selectable
	sel := make([]SelectableItem[ID_t], 0, len(items))
	for _, item := range items {
		selectable, ok := item.(SelectableItem[ID_t])
		if !ok {
			panicFailedAssert(item)
		}
		if selectable.Selected() {
			sel = append(sel, selectable)
		}

	}
	return slices.Clip(sel)
}

// SelectItems selects each item with a .ID that matches one of them items in toSelect.
// Returns the first ID with no matches.
//
// Operates in roughly O(toSelect * len(msl)) time.
func (msl *Model[ID_t]) SelectItems(toSelect []ID_t) (cmd tea.Cmd, notFound []ID_t) {

	// duplicate toSelect and remove an item whenever it is found
	var nf = map[ID_t]bool{}
	for _, id := range toSelect {
		nf[id] = true
	}
	var cmds []tea.Cmd
	itms := msl.Items()
	for _, id := range toSelect {
		for i, itm := range itms {
			selectable, ok := itm.(SelectableItem[ID_t])
			if !ok {
				panicFailedAssert(itm)
			}
			if selectable.ID() == id {
				selectable.SetSelected(true)
				// reinsert the item
				cmds = append(cmds, msl.Model.SetItem(i, selectable))
				delete(nf, id)
			}
		}
	}
	return tea.Batch(cmds...), slices.Collect(maps.Keys(nf))
}

// SetItems replaces the items in the list with the given items.
func (msl *Model[ID_t]) SetItems(items []SelectableItem[ID_t]) tea.Cmd {
	wrapped := wrapItems(items)
	return msl.Model.SetItems(wrapped)
}

// pray this is never called.
func panicFailedAssert(baseItem list.Item) {
	panic(fmt.Sprintf("failed to cast item to SelectableItem. Base item: %#v", baseItem))
}
