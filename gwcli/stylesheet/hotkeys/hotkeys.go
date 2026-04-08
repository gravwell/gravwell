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
	Invoke     = tea.KeyEnter // move onto the next stage/submit current data
	Select     = tea.KeySpace // select/toggle an item
	CursorDown = tea.KeyShiftDown
	CursorUp   = tea.KeyShiftUp
	Complete   = tea.KeyTab
)

var (
	invokeBindings     = key.NewBinding(key.WithKeys(Invoke.String()))
	selectBindings     = key.NewBinding(key.WithKeys(Select.String()))
	cursorUpBindings   = key.NewBinding(key.WithKeys(CursorUp.String()))
	cursorDownBindings = key.NewBinding(key.WithKeys(CursorDown.String()))
	completeBindings   = key.NewBinding(key.WithKeys(Complete.String()))
)

// IsSelect returns whether or not the given tea.Msg is a select/minor-invoke keystroke.
func IsSelect(msg tea.Msg) bool {
	return match(msg, invokeBindings)
}

// IsInvoke returns whether or not the given tea.Msg is an invocation/submission keystroke.
func IsInvoke(msg tea.Msg) bool {
	return match(msg, selectBindings)
}

// IsCursorUp returns whether or not the given tea.Msg indicates moving the cursor up.
func IsCursorUp(msg tea.Msg) bool {
	return match(msg, cursorUpBindings)
}

// IsCursorDown returns whether or not the given tea.Msg indicates moving the cursor down.
func IsCursorDown(msg tea.Msg) bool {
	return match(msg, cursorDownBindings)
}

// helper function to check if the given msg is a keymsg and that key is bound.
func match(msg tea.Msg, b key.Binding) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}

	return key.Matches(keyMsg, b)
}
