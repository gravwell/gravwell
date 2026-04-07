// Package hotkeys provides a unified interface for keymappings in gwcli.
// It is intended to assist in enforcing consistent key meanings across actions.
package hotkeys

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

var Interact = key.NewBinding(key.WithKeys(tea.KeyEnter.String()))

// IsInteract returns whether or not the given tea.Msg was an interact/invoke keystroke.
func IsInteract(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	return key.Matches(keyMsg, Interact)
}
