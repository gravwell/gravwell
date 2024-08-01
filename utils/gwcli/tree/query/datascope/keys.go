/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

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
