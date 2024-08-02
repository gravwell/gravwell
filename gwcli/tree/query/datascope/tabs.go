/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datascope

/**
 * Contains the generalized data and subroutines for propagating DataScope's tabs.
 * Also contains the implementation of the results and help tabs.
 * Results, Download, and Schedule have been split off into their own files.
 */

import (
	"fmt"
	"gwcli/stylesheet"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

//#region consistent styling

const (
	verticalPlace = 0.7 // vertical offset for lipgloss.Place for tabs to use in their view()
)

var (
	tabDescStyle = func(width int) lipgloss.Style {
		return lipgloss.NewStyle().Width(width).PaddingBottom(1).AlignHorizontal(lipgloss.Center)
	}
	evenEntryStyle = lipgloss.NewStyle()
	oddEntryStyle  = lipgloss.NewStyle().Foreground(stylesheet.SecondaryColor)
)

// Returns a line, right-suffixed with the given percent*100.
// Width should be the target total width of this string.
// If you want to add other items horizontally, make sure to subtract their width from the width
// given to this subroutine.
func scrollPercentLine(width int, rawPercent float64) string {
	scrollPercent := fmt.Sprintf("%3.f%%", rawPercent*100)
	line := lipgloss.NewStyle().
		Foreground(stylesheet.PrimaryColor).
		Render(
			strings.Repeat("─",
				max(0, width-lipgloss.Width(scrollPercent))),
		)

	return fmt.Sprintf("%s%s", line, scrollPercent)
}

// Returns a modified viewport, for consistency between text and table results
func NewViewport() viewport.Model {
	vp := viewport.Model{
		// width/height are set later
		// when received in a windowSize message
	}
	//vp.MouseWheelDelta = 1
	vp.MouseWheelEnabled = false
	vp.HighPerformanceRendering = false
	// set up keybinds directly supported by viewport
	// other keybinds are managed by the results tab()
	vp.KeyMap = viewport.KeyMap{
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", " ", "f"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "b"),
		),
		HalfPageUp: key.NewBinding(
			key.WithKeys("u", "ctrl+u"),
		),
		HalfPageDown: key.NewBinding(
			key.WithKeys("d", "ctrl+d"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
		),
	}
	return vp
}

// Handling of additional keys that the viewport bubble does not natively support.
// Returns true iff a key matched.
func viewportAddtlKeys(msg tea.Msg, vp *viewport.Model) (match bool) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.Type {
		case tea.KeyHome:
			vp.GotoTop()
			return true
		case tea.KeyEnd:
			vp.GotoBottom()
			return true
		}
	}
	return false
}

//#endregion

type tab struct {
	name string

	// update and view must take a DS parameter, rather than being methods of DS, as DS's elm arch
	// passes by value
	updateFunc func(*DataScope, tea.Msg) tea.Cmd
	// see the note on updateFunc
	viewFunc func(*DataScope) string
}

const (
	results uint = iota
	help
	download
	schedule
)

// results the array of tabs with all requisite data built in
func (s *DataScope) generateTabs() []tab {
	t := make([]tab, 4)
	t[results] = tab{
		name:       "results",
		updateFunc: updateResults,
		viewFunc:   viewResults}
	t[help] = tab{
		name:       "help",
		updateFunc: func(*DataScope, tea.Msg) tea.Cmd { return nil },
		viewFunc:   func(*DataScope) string { return compiledHelpString }}
	t[download] = tab{
		name:       "download",
		updateFunc: updateDownload,
		viewFunc:   viewDownload}
	t[schedule] = tab{
		name:       "schedule",
		updateFunc: updateSchedule,
		viewFunc:   viewSchedule}

	return t
}

//#region help tab

const cellWidth int = 25

var compiledHelpString string

// displays the available keys and useful information in a borderless table.
// This function rebuilds the help string, allowing it to only be regenerated when necessary
// (ex: a window size message) rather than every visible cycle.
func recompileHelp(s *DataScope) {
	// TODO split into 'all-tabs' keys and results-specific keys

	// we are hiding all border save for inter-row borders, so drop edge characters
	brdr := lipgloss.NormalBorder()
	brdr.MiddleLeft = ""
	brdr.MiddleRight = ""

	// Note the usage of width within these styles rather than the table's width.
	// Doing the reverse would cause long cells to truncate instead of wrap.
	// This method does *not* prevent truncation if the terminal is too small
	keyColumnStyle := lipgloss.NewStyle().Foreground(stylesheet.AccentColor1).
		MaxWidth(s.usableWidth() / 2).Width(cellWidth)
	valueColumnStyle := lipgloss.NewStyle().MaxWidth(s.usableWidth() / 2).Width(cellWidth)

	joinChar := ","

	tbl := table.New().
		Border(brdr).
		BorderRow(true).BorderColumn(false).
		BorderLeft(false).BorderRight(false).
		BorderTop(false).BorderBottom(false).
		BorderStyle(lipgloss.NewStyle().Foreground(stylesheet.TertiaryColor)).
		StyleFunc(func(row, col int) lipgloss.Style {
			if col == 0 {
				return keyColumnStyle
			}
			return valueColumnStyle
		})
	tbl.Rows(
		[][]string{
			{strings.Join(keys.cycleTabs.Keys(), joinChar), "cycle tables"},
			{strings.Join(keys.reverseCycleTabs.Keys(), joinChar), "reverse cycle tables"},
			{stylesheet.UpDown, "scroll page"},
			{stylesheet.LeftRight, "change page"},
			{strings.Join(keys.showTabs.Keys(), joinChar), "toggle tab visibility"},
			{"esc", "quit"},
		}...)

	// 'place' the table in the center of the *viewport*, horizontally and vertically
	compiledHelpString = lipgloss.Place(s.usableWidth(), s.usableHeight(),
		lipgloss.Center, lipgloss.Center, tbl.String())
}

//#endregion

// #region tab drawing
func tabBorderWithBottom(left, middle, right string) lipgloss.Border {
	border := lipgloss.RoundedBorder()
	border.BottomLeft = left
	border.Bottom = middle
	border.BottomRight = right
	return border
}

var (
	inactiveTabBorder = tabBorderWithBottom("┴", "─", "┴")
	activeTabBorder   = tabBorderWithBottom("┘", " ", "└")
	highlightColor    = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	inactiveTabStyle  = lipgloss.NewStyle().Border(inactiveTabBorder, true).
				BorderForeground(highlightColor).
				Padding(0, 1).AlignHorizontal(lipgloss.Center)
	activeTabStyle = inactiveTabStyle.Border(activeTabBorder, true)
)

func (s *DataScope) renderTabs(width int) string {

	var rendered []string = make([]string, len(s.tabs))

	margin, tabCount := 2, len(s.tabs)

	// width = (tab_width * tab_count) + (margin * tab_count-1)
	// tab_width = (width - margin*(tab_count-1))/tab_count
	tabWidth := (width - (margin*tabCount - 1)) / tabCount
	// iterate and draw each tab, with special styling on the active tab
	for i, t := range s.tabs {
		var style lipgloss.Style
		isFirst, isLast, isActive := i == 0, i == len(s.tabs)-1, i == int(s.activeTab)
		if isActive {
			style = activeTabStyle
		} else {
			style = inactiveTabStyle
		}
		style = style.Width(tabWidth)
		border, _, _, _, _ := style.GetBorder()
		if isFirst && isActive {
			border.BottomLeft = "╵"
		} else if isFirst && !isActive {
			border.BottomLeft = "└"
		} else if isLast && isActive {
			border.BottomRight = "╵"
		} else if isLast && !isActive {
			border.BottomRight = "┘"
		}
		style = style.Border(border)
		if i == int(results) {
			rendered[i] = style.Foreground(stylesheet.AccentColor1).Render(t.name)
		} else {
			rendered[i] = style.Render(t.name)
		}

	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	return lipgloss.NewStyle().AlignHorizontal(lipgloss.Left).Render(row)
}

//#endregion
