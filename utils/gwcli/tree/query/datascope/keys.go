package datascope

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

var keys = struct {
	showTabs         key.Binding
	cycleTabs        key.Binding
	reverseCycleTabs key.Binding
}{
	showTabs: key.NewBinding(
		key.WithKeys(tea.KeyCtrlS.String()),
	),
	cycleTabs: key.NewBinding(
		key.WithKeys(tea.KeyTab.String()),
	),
	reverseCycleTabs: key.NewBinding(
		key.WithKeys(tea.KeyShiftTab.String()),
	),
}
