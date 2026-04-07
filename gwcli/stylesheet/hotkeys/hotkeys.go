// Package hotkeys provides a unified interface for keymappings in gwcli.
// It is intended to assist in enforcing consistent key meanings across actions.
package hotkeys

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// the set of primary keybinds.
// You probably don't want to use these directly unless you are sending keys.
// You probably aren't sending keys unless you are writing tests.
const (
	Interact   = tea.KeyEnter
	CursorDown = tea.KeyShiftDown
	CursorUp   = tea.KeyShiftUp
)

var (
	interact   = key.NewBinding(key.WithKeys(Interact.String(), tea.KeySpace.String()))
	cursorUp   = key.NewBinding(key.WithKeys(CursorUp.String(), tea.KeyShiftTab.String()))
	cursorDown = key.NewBinding(key.WithKeys(CursorDown.String(), tea.KeyTab.String()))
)

// IsInteract returns whether or not the given tea.Msg was an interact/invoke/submit keystroke.
func IsInteract(msg tea.Msg) bool {
	return match(msg, interact)
}

// IsCursorUp returns whether or not the given tea.Msg indicated moving the cursor up.
func IsCursorUp(msg tea.Msg) bool {
	return match(msg, cursorUp)
}

// IsCursorDown returns whether or not the given tea.Msg indicated moving the cursor down.
func IsCursorDown(msg tea.Msg) bool {
	return match(msg, cursorDown)
}

// helper function to check if the given msg is a keymsg and that key is bound.
func match(msg tea.Msg, b key.Binding) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}

	return key.Matches(keyMsg, b)
}
