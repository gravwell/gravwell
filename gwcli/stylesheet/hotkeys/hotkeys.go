/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package hotkeys provides mechanisms for processing and displaying keys consistently.
// It provides subroutines for checking incoming messages (Is*()) and
// an exportable model for actions that need to integrate the default hotkeys into their own keymaps.
package hotkeys

import (
	"sync"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/sigils"
)

var (
	// move onto the next stage/submit current data
	Invoke = key.NewBinding(
		key.WithKeys(tea.KeyEnter.String()),
		key.WithHelp(sigils.Enter, "invoke"),
	)
	// select/toggle an item
	Select = key.NewBinding(
		key.WithKeys(tea.KeySpace.String()),
		key.WithHelp("space", "select"),
	)
	CursorDown = key.NewBinding(
		key.WithKeys(tea.KeyDown.String(), "j"),
		key.WithHelp(sigils.Down, "cursor down"),
	)
	CursorUp = key.NewBinding(
		key.WithKeys(tea.KeyUp.String(), "k"),
		key.WithHelp(sigils.Up, "cursor up"),
	)
	// complete current partial string
	Complete = key.NewBinding(
		key.WithKeys(tea.KeyTab.String()),
		key.WithHelp(sigils.Tab, "complete"),
	)
	// soft kill children
	SoftQuit = key.NewBinding(
		key.WithKeys(tea.KeyEsc.String()),
		key.WithHelp("esc", "quit"),
	)
)

// list-specific hotkeys
var (
	Filter = key.NewBinding(
		key.WithKeys("\\"),
		key.WithHelp("\\", "clear filter"),
	)
	CancelWhileFiltering = key.NewBinding(
		key.WithKeys(tea.KeyCtrlBackslash.String()),
		key.WithHelp("ctrl+\\", "clear filter"),
	)
	AcceptWhileFiltering = key.NewBinding(
		key.WithKeys(tea.KeyTab.String()),
		key.WithHelp(sigils.Tab, "accept"),
	)
	ClearFilter = key.NewBinding(
		key.WithKeys(tea.KeyShiftLeft.String()),
		key.WithHelp("shift"+sigils.Left, "clear filter"),
	)
)

// ApplyToList greedily applies hotkey bindings to the given keymap.
func ApplyToList(km *list.KeyMap) {
	if km == nil { // nothing to be done
		return
	}
	km.CursorDown = CursorDown
	km.CursorUp = CursorUp
	km.Quit = SoftQuit

	km.Filter = Filter
	km.CancelWhileFiltering = CancelWhileFiltering
	km.AcceptWhileFiltering = AcceptWhileFiltering
	km.ClearFilter = ClearFilter
}

// A Model is the standard set of keybindings with help prepared.
// Actions should include a Model in their data and append its View to the bottom of their views.
type Model struct {
	CursorUp   key.Binding
	CursorDown key.Binding
	Invoke     key.Binding
	Select     key.Binding
	Complete   key.Binding
	SoftAction key.Binding

	help help.Model
}

// ShortHelp shows combined cursor up/down.
func (m Model) ShortHelp() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithHelp(sigils.UpDown, "up/down")), m.Invoke}
}

func (m Model) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{m.CursorUp},
		{m.CursorDown},
		{m.Invoke},
		{m.Select},
		{m.Complete},
		{m.SoftAction},
	}
}

func NewModel() Model {
	s := Model{ // all keybindings start enabled
		CursorUp:   CursorUp,
		CursorDown: CursorDown,
		Invoke:     Invoke,
		Select:     Select,
		Complete:   Complete,
		SoftAction: SoftQuit,

		help: help.New(),
	}

	return s
}

var (
	defaultHotkeys = NewModel()
	// as this is a TUI, we never expect defaultHotkeys to be used in multiple places at once.
	// This is just to shut up -race as we are technically mutating a singleton in DefaultView().
	defaultMu = sync.Mutex{}
)
var _ tea.Model = Model{}

func (Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if wsmsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.help.Width = wsmsg.Width
	}
	return m, nil
}

func (m Model) View() string {
	return m.help.View(m)
}

// DefaultView displays the help view of the standard hotkeys.
// Intended for use in actions that don't need to modify the hotkey map (such as disabling keys or changing help text).
//
// If width > 0, the given width will be factored into this view (and only this view).
func DefaultView(width int) string {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	var t int
	if width > 0 {
		t = defaultHotkeys.help.Width
		defaultHotkeys.help.Width = width
		defer func() { defaultHotkeys.help.Width = t }()
	}
	v := defaultHotkeys.View()

	return v
}

// Match is just a wrapper for matching an incoming message against any number of bindings.
func Match(msg tea.Msg, b ...key.Binding) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}

	return key.Matches(keyMsg, b...)
}
