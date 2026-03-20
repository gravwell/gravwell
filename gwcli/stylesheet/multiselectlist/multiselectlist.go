// Package multiselectlist wraps the list bubble to enable selecting multiple items at once.
//
// TODO Generalize this package.
//
// - Do not rely on the default delegate and default delegate item.
//
// - Enable overriding of enter and space as the interaction keys.
//
// - Properly display keys, rather than just attaching Enter and Space below View.
//
// - Do not import our stylesheet; enable setting of the checkbox function/style.
//
// - Use a different method for pre-selection; the current method assumes titles are unique and that is a bad assumption.
package multiselectlist

import (
	"fmt"
	"slices"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
)

// Model bolts additional functionality onto the list bubble such that multiple items can be selected.
type Model struct {
	list.Model

	done bool

	// if set, Update will batch an extra tea.Cmd with a status message stating that the item was selected.
	StatusMessageOnSelect bool

	selectedViewFunc func(set bool) string
}

type Options struct {
	// if not nil, items with matching titles will be preselected.
	Preselected map[uint]bool
	// sets the prefix to show whether or not an item is selected.
	// Uses DefaultSelectedViewFunc if nil.
	SelectedViewFunc func(set bool) string
}

// DefaultSelectedViewFunc sets the prefix if Options.SelectedViewFunc is not set.
func DefaultSelectedViewFunc(set bool) string {
	if set {
		return "[✓]"
	}
	return "[ ]"
}

// New returns a Multi-Select enabled list with the default delegate used by list.
func New(items []list.DefaultItem, width, height int, opts Options) Model {
	// make sure the pre-selection map isn't nil
	if opts.Preselected == nil {
		opts.Preselected = make(map[uint]bool)
	}

	// wrap each item in our select-enabled item type
	wrapped := make([]list.Item, len(items))
	for i, item := range items {
		wrapped[i] = selectableItem{item, opts.Preselected[uint(i)]}
	}
	msl := Model{
		Model:            list.New(wrapped, list.NewDefaultDelegate(), width, height),
		selectedViewFunc: DefaultSelectedViewFunc,
	}
	if opts.SelectedViewFunc != nil {
		msl.selectedViewFunc = DefaultSelectedViewFunc
	}

	return msl
}

func (msl Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		msl.Model.SetHeight(msg.Height)
		msl.Model.SetWidth(msg.Width)
		return msl, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeySpace:
			cmd := msl.ToggleCurrentItem()
			return msl, cmd
		case tea.KeyEnter:
			msl.done = true
			return msl, nil
		}
	}
	var cmd tea.Cmd
	msl.Model, cmd = msl.Model.Update(msg)
	return msl, cmd
}

func (msl Model) View() string {
	return msl.Model.View() + "\n  " + stylesheet.Cur.DisabledText.Render("space select • ↲ continue")
}

// ToggleCurrentItem does as it says on the tin.
// If no item is selected (aka the list is empty or your cursor is off in wonderland), this is a no-op.
//
// NOTE(rlandau): This function can panic, but if it does, something has gone truly, horrifically wrong.
func (msl *Model) ToggleCurrentItem() tea.Cmd {
	baseItem := msl.Model.SelectedItem()
	if baseItem == nil {
		return nil
	}
	li, ok := baseItem.(selectableItem)
	if !ok {
		panicFailedAssert(msl.Model.SelectedItem())
	}
	li.selected = !li.selected
	// reinsert the item
	cmd := msl.Model.SetItem(msl.Model.GlobalIndex(), li)

	if msl.StatusMessageOnSelect {
		var statusMsg string
		if li.selected {
			statusMsg = "selected"
		} else {
			statusMsg = "deselected"
		}
		statusMsg += " dispatcher " + li.Title()
		cmd = tea.Batch(cmd, msl.Model.NewStatusMessage(statusMsg))
	}

	return cmd

}

// Done returns true once the user hits enter.
// It should be checked after each msl.Update()
func (msl *Model) Done() bool {
	return msl.done
}

// GetSelectedItems returns the list of selected items.
//
// Operates in O(n) time where n = len(msl.Items()).
func (msl *Model) GetSelectedItems() []list.DefaultItem {
	items := msl.Model.Items()
	sel := make([]list.DefaultItem, 0, len(items))
	for _, item := range items {
		selectable, ok := item.(selectableItem)
		if !ok {
			panicFailedAssert(item)
		}
		if selectable.selected {
			sel = append(sel, selectable.DefaultItem)
		}

	}
	return slices.Clip(sel)
}

//#region selectable item

// selectableItem wraps a given item type, prefixing select functionality
type selectableItem struct {
	list.DefaultItem
	selected bool
}

// FilterValue sets the string to include/disclude this item on when a user filters.
func (i selectableItem) FilterValue() string {
	return i.DefaultItem.FilterValue()
}

func (i selectableItem) Title() string {
	return stylesheet.Checkbox(i.selected) + " " + i.DefaultItem.Title()
}

func (i selectableItem) Description() string {
	return i.DefaultItem.Description()
}

//#endregion selectable item

// assertTarget should be "selectableItem" or "list.DefaultItem"
// pray this is never called.
func panicFailedAssert(baseItem list.Item) {
	panic(fmt.Sprintf("failed to cast item to selectableItem. Base item: %#v", baseItem))
}
