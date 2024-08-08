/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
This package supplies a standadized format for use with implementations of the list bubble.
By sharing a single definition, we can ensure lists look and function similarly no matter what
action or scaffold is invoking it.
*/
package listsupport

import (
	"github.com/gravwell/gravwell/v3/gwcli/stylesheet"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
)

// Creates and returns a new list.Model with customized defaults.
// items must fit the listsupport.Item interface in order to be used with the delegate. However,
// because Go cannot interface arrays, you must pass in your items as []list.Item.
func NewList(items []list.Item, width, height int, singular, plural string) list.Model {
	// update the styles on the default delegate to wrap properly
	dlg := list.NewDefaultDelegate()
	dlg.Styles.SelectedTitle = dlg.Styles.SelectedTitle.Foreground(stylesheet.PrimaryColor)
	dlg.Styles.SelectedDesc = dlg.Styles.SelectedDesc.Foreground(stylesheet.SecondaryColor)

	l := list.New(items, dlg, width, height)
	// list.DefaultKeyMap, but has the quits removed and conflicting filter keys reassigned.
	l.KeyMap = list.KeyMap{
		// Browsing.
		CursorUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		CursorDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		PrevPage: key.NewBinding(
			key.WithKeys("left", "h", "pgup", "b", "u"),
			key.WithHelp("←/h/pgup", "prev page"),
		),
		NextPage: key.NewBinding(
			key.WithKeys("right", "l", "pgdown", "f", "d"),
			key.WithHelp("→/l/pgdn", "next page"),
		),
		GoToStart: key.NewBinding(
			key.WithKeys("home", "g"),
			key.WithHelp("g/home", "go to start"),
		),
		GoToEnd: key.NewBinding(
			key.WithKeys("end", "G"),
			key.WithHelp("G/end", "go to end"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		ClearFilter: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "clear filter"),
		),

		// Filtering.
		CancelWhileFiltering: key.NewBinding(
			key.WithKeys("alt+/"),
			key.WithHelp("alt+/", "cancel"),
		),
		AcceptWhileFiltering: key.NewBinding(
			key.WithKeys("tab", "shift+tab", "ctrl+k", "up", "ctrl+j", "down"),
			key.WithHelp("tab", "apply filter"),
		),

		// Toggle help.
		ShowFullHelp: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "more"),
		),
		CloseFullHelp: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "close help"),
		),
	}
	l.SetSpinner(spinner.Moon)
	l.SetStatusBarItemName(singular, plural)
	l.SetShowTitle(false)

	return l
}

// Interface that items must fit prior to casting to list.Item for NewList()
type Item interface {
	Title() string
	Description() string
	FilterValue() string
}
