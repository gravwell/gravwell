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
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
)

// the set of primary keybinds.
// You probably don't want to use these directly unless you are sending keys.
// You probably aren't sending keys unless you are writing tests.
const (
	Invoke     = tea.KeyEnter // move onto the next stage/submit current data
	Select     = tea.KeySpace // select/toggle an item
	CursorDown = tea.KeyShiftDown
	CursorUp   = tea.KeyShiftUp
	Complete   = tea.KeyTab
)

// A Model is the standard set of keybindings with help prepared.
// Actions should include a Model in their data and append its View to the bottom of their views.
type Model struct {
	CursorUp   key.Binding
	CursorDown key.Binding
	Invoke     key.Binding
	Select     key.Binding
	Complete   key.Binding

	help help.Model
}

func (km Model) ShortHelp() []key.Binding {
	return []key.Binding{km.CursorUp, km.CursorDown, km.Invoke}
}

func (km Model) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{km.CursorUp},
		{km.CursorDown},
		{km.Invoke},
		{km.Select},
		{km.Complete},
	}
}

func NewModel() Model {
	s := Model{ // all keybindings start enabled
		CursorUp: key.NewBinding(
			key.WithKeys(CursorUp.String()),
			key.WithHelp(stylesheet.UpSigil, "up"),
		),
		CursorDown: key.NewBinding(
			key.WithKeys(CursorDown.String()),
			key.WithHelp(stylesheet.DownSigil, "down"),
		),
		Invoke: key.NewBinding(
			key.WithKeys(Invoke.String()),
			key.WithHelp(stylesheet.EnterSigil, "invoke"),
		),
		Select: key.NewBinding(
			key.WithKeys(Select.String()),
			key.WithHelp("space", "select"),
		),
		Complete: key.NewBinding(
			key.WithKeys(Complete.String()),
			key.WithHelp(stylesheet.TabSigil, "complete"),
		),

		help: help.New(),
	}

	return s
}

var defaultHotkeys = NewModel()
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
func DefaultView() string {
	return defaultHotkeys.View()
}

// ApplyToList greedily applies hotkey bindings to the given keymap.
func ApplyToList(km *list.KeyMap) {
	if km == nil { // nothing to be done
		return
	}
	km.CursorDown = defaultHotkeys.CursorDown
	km.CursorUp = defaultHotkeys.CursorUp
}

// IsSelect returns whether or not the given tea.Msg is a select/minor-invoke keystroke.
func IsSelect(msg tea.Msg) bool {
	return match(msg, defaultHotkeys.Select)
}

// IsInvoke returns whether or not the given tea.Msg is an invocation/submission keystroke.
func IsInvoke(msg tea.Msg) bool {
	return match(msg, defaultHotkeys.Invoke)
}

// IsCursorUp returns whether or not the given tea.Msg indicates moving the cursor up.
func IsCursorUp(msg tea.Msg) bool {
	return match(msg, defaultHotkeys.CursorUp)
}

// IsCursorDown returns whether or not the given tea.Msg indicates moving the cursor down.
func IsCursorDown(msg tea.Msg) bool {
	return match(msg, defaultHotkeys.CursorDown)
}

// helper function to check if the given msg is a keymsg and that key is bound.
func match(msg tea.Msg, b key.Binding) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}

	return key.Matches(keyMsg, b)
}

// IsSubmit tests if the given key message is a select OR an invoke and should be used for checking button presses.
func IsSubmit(msg tea.Msg) bool {
	return IsInvoke(msg) || IsSelect(msg)
}
