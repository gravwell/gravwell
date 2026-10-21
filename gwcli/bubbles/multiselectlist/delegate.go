/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package multiselectlist

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const ellipsis = "…"

// DefaultShowSelectState sets the prefix if Options.ShowSelectStateViewFunc is not set.
func DefaultShowSelectState(set bool) string {
	if set {
		return "[✓]"
	}
	return "[ ]"
}

// DefaultDelegate is a standard delegate designed to work in lists.
// It's styled by DefaultItemStyles, which can be customized as you like.
// It is based very closely off of list's default delegate.
//
// The description line can be hidden by setting Description to false, which
// renders the list as single-line-items. The spacing between items can be set
// with the SetSpacing method.
//
// Settings ShortHelpFunc and FullHelpFunc is optional. They can be set to
// include items in the list's default short and full help menus.
type DefaultDelegate[ID_t comparable] struct {
	ShowSelectStateFunc func(selected bool) string

	ShowDescription bool
	Styles          list.DefaultItemStyles
	ShortHelpFunc   func() []key.Binding
	FullHelpFunc    func() [][]key.Binding
	height          int
	spacing         int
}

// NewDefaultDelegate creates a new delegate with default styles.
func NewDefaultDelegate[ID_t comparable](sssf func(selected bool) string) DefaultDelegate[ID_t] {
	const defaultHeight = 2
	const defaultSpacing = 1
	dd := DefaultDelegate[ID_t]{
		ShowSelectStateFunc: sssf,

		ShowDescription: true,
		Styles:          list.NewDefaultItemStyles(),
		height:          defaultHeight,
		spacing:         defaultSpacing,
	}

	if dd.ShowSelectStateFunc == nil {
		dd.ShowSelectStateFunc = DefaultShowSelectState
	}
	return dd
}

// SetHeight sets delegate's preferred height.
func (d *DefaultDelegate[ID_t]) SetHeight(i int) {
	d.height = i
}

// Height returns the delegate's preferred height.
// This has effect only if ShowDescription is true, otherwise height is always 1.
func (d DefaultDelegate[ID_t]) Height() int {
	if d.ShowDescription {
		return d.height
	}
	return 1
}

// SetSpacing sets the delegate's spacing.
func (d *DefaultDelegate[ID_t]) SetSpacing(i int) {
	d.spacing = i
}

// Spacing returns the delegate's spacing.
func (d DefaultDelegate[ID_t]) Spacing() int {
	return d.spacing
}

// Update checks whether the delegate's UpdateFunc is set and calls it.
func (d DefaultDelegate[ID_t]) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

// Render prints an item.
func (d DefaultDelegate[ID_t]) Render(w io.Writer, m list.Model, index int, item list.Item) {
	var (
		title, selectedPrefix, desc string
		matchedRunes                []int
		s                           = &d.Styles
	)

	if m.Width() <= 0 {
		// short-circuit
		return
	}

	if i, ok := item.(SelectableItem[ID_t]); ok {
		title = i.Title()
		selectedPrefix = d.ShowSelectStateFunc(i.Selected())
		desc = i.Description()
	} else {
		return
	}

	// Prevent text from exceeding list width
	textwidth := m.Width() - s.NormalTitle.GetPaddingLeft() - s.NormalTitle.GetPaddingRight()
	title = ansi.Truncate(title, textwidth, ellipsis)
	if d.ShowDescription {
		var lines []string
		for i, line := range strings.Split(desc, "\n") {
			if i >= d.height-1 {
				break
			}
			lines = append(lines, ansi.Truncate(line, textwidth, ellipsis))
		}
		desc = strings.Join(lines, "\n")
	}

	// Conditions
	var (
		isSelected  = index == m.Index()
		emptyFilter = m.FilterState() == list.Filtering && m.FilterValue() == ""
		isFiltered  = m.FilterState() == list.Filtering || m.FilterState() == list.FilterApplied
	)

	if isFiltered {
		// Get indices of matched characters
		matchedRunes = m.MatchesForItem(index)
	}

	var titleSty lipgloss.Style

	if emptyFilter {
		titleSty = s.DimmedTitle
		desc = s.DimmedDesc.Render(desc)
	} else if isSelected && m.FilterState() != list.Filtering {
		if isFiltered {
			// Highlight matches
			unmatched := s.SelectedTitle.Inline(true)
			matched := unmatched.Inherit(s.FilterMatch)
			title = lipgloss.StyleRunes(title, matchedRunes, matched, unmatched)
		}
		titleSty = s.SelectedTitle
		desc = s.SelectedDesc.Render(desc)
	} else {
		if isFiltered {
			// Highlight matches
			unmatched := s.NormalTitle.Inline(true)
			matched := unmatched.Inherit(s.FilterMatch)
			title = lipgloss.StyleRunes(title, matchedRunes, matched, unmatched)
		}
		titleSty = s.NormalTitle
		desc = s.NormalDesc.Render(desc)
	}

	titleLine := titleSty.Render(fmt.Sprintf("%s %s", selectedPrefix, title))

	if d.ShowDescription {
		fmt.Fprintf(w, "%s\n%s", titleLine, desc)
		return
	}
	fmt.Fprintf(w, "%s", titleLine)
}

// ShortHelp returns the delegate's short help.
func (d DefaultDelegate[ID_t]) ShortHelp() []key.Binding {
	if d.ShortHelpFunc != nil {
		return d.ShortHelpFunc()
	}
	return nil
}

// FullHelp returns the delegate's full help.
func (d DefaultDelegate[ID_t]) FullHelp() [][]key.Binding {
	if d.FullHelpFunc != nil {
		return d.FullHelpFunc()
	}
	return nil
}

//#endregion
