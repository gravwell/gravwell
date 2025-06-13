/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package stylesheet managing the visual effects of gwcli.
// Most styling is via lipgloss and encompasses colors, alignment, borders, etc.
//
// The stylesheet package should also be used for maintaining consistent visuals.
// This is accomplished via the provided pre-built elements and the Sheet variable for pre-set styles.
//
// I don't know much about color theory or picking good palettes,
// so homegrown palettes are based on Gravwell's colors and expanded via https://coolors.co/.
package stylesheet

// miscellaneous styles

import "github.com/charmbracelet/lipgloss"

type sheet struct {
	Nav    lipgloss.Style // style of nav/directory items while traversing the tree
	Action lipgloss.Style // style of actions/invokables while traversing the tree

	// for building multi-pane views
	Composable struct {
		FocusedBorder       lipgloss.Style // stylized border for wrapping elements currently in focus
		UnfocusedBorder     lipgloss.Style // stylized border for wrapping elements that could be in focus, but are currently blurred
		ComplimentaryBorder lipgloss.Style // stylized border for wrapping complimentary elements that do not toggle focus
		ModifierText        lipgloss.Style // modifier field names, typically grouped and wrapped by (Un)FocusedBorder
	}

	// for building tables
	Table struct {
		HeaderCells lipgloss.Style
		EvenCells   lipgloss.Style
		OddCells    lipgloss.Style
		BorderType  lipgloss.Border
		BorderStyle lipgloss.Style
	}

	ErrText      lipgloss.Style // text that displays an error
	ExampleText  lipgloss.Style // text that display an example
	DisabledText lipgloss.Style // text that is currently disabled

	// TODO convert prompt into func(string) string
	PromptText lipgloss.Style // text that prefixes an input box, but is not a modifier (primarily used for Mother's prompt)

	PrimaryText   lipgloss.Style // catchall for important/focal text that does not fit into a different category
	SecondaryText lipgloss.Style // catchall for text that does not fit into a different category and is not primary

	Spinner lipgloss.Style
}

// Stylesheet currently in-use by gwcli.
// This is what other packages should reference when stylizing their elements.
var Sheet sheet

func init() {
	// set the current stylesheet
	Sheet = softPink()
}

func softPink() sheet {
	return sheet{
		Nav:    lipgloss.NewStyle().Foreground(roseTaupe),
		Action: lipgloss.NewStyle().Foreground(melon),

		Composable: struct {
			FocusedBorder       lipgloss.Style
			UnfocusedBorder     lipgloss.Style
			ComplimentaryBorder lipgloss.Style
			ModifierText        lipgloss.Style
		}{
			FocusedBorder: lipgloss.NewStyle().
				Align(lipgloss.Left, lipgloss.Center).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(amethyst),
			UnfocusedBorder: lipgloss.NewStyle().
				Align(lipgloss.Left, lipgloss.Center).
				BorderStyle(lipgloss.HiddenBorder()),
			ComplimentaryBorder: lipgloss.NewStyle().
				Align(lipgloss.Left, lipgloss.Center).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(mistyRose),
			ModifierText: lipgloss.NewStyle().Foreground(melon),
		},

		Table: struct {
			HeaderCells lipgloss.Style
			EvenCells   lipgloss.Style
			OddCells    lipgloss.Style
			BorderType  lipgloss.Border
			BorderStyle lipgloss.Style
		}{
			HeaderCells: lipgloss.NewStyle().
				Foreground(amethyst).
				AlignHorizontal(lipgloss.Center).
				AlignVertical(lipgloss.Center).Bold(true),
			EvenCells:   lipgloss.NewStyle().Padding(0, 1).Width(30).Foreground(melon),
			OddCells:    lipgloss.NewStyle().Padding(0, 1).Width(30).Foreground(mistyRose),
			BorderType:  lipgloss.NormalBorder(),
			BorderStyle: lipgloss.NewStyle().Foreground(amethyst),
		},

		ErrText:      lipgloss.NewStyle().Foreground(bloodRed),
		ExampleText:  lipgloss.NewStyle().Foreground(satinSheenGold),
		DisabledText: lipgloss.NewStyle().Faint(true),

		PromptText: lipgloss.NewStyle().Foreground(amethyst),

		PrimaryText:   lipgloss.NewStyle().Foreground(amethyst),
		SecondaryText: lipgloss.NewStyle().Foreground(melon),

		Spinner: lipgloss.NewStyle().Foreground(amethyst),
	}
}
