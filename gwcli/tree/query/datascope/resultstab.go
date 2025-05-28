/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datascope

/**
 * Subroutines for driving and displaying the results tab.
 */

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/colorizer"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/paginator"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type resultsTab struct {
	vp    viewport.Model
	pager paginator.Model
	data  []string // complete set of data to be paged
	ready bool
}

func initResultsTab(data []string) resultsTab {
	// set up backend paginator
	paginator.DefaultKeyMap = paginator.KeyMap{ // do not use pgup/pgdn
		PrevPage: key.NewBinding(key.WithKeys("left", "h")),
		NextPage: key.NewBinding(key.WithKeys("right", "l")),
	}
	p := paginator.New()
	p.Type = paginator.Dots
	p.PerPage = 25
	p.ActiveDot = lipgloss.NewStyle().Foreground(stylesheet.FocusedColor).Render("•")
	p.InactiveDot = lipgloss.NewStyle().Foreground(stylesheet.UnfocusedColor).Render("•")
	p.SetTotalPages(len(data))

	// set up viewport
	vp := NewViewport()

	r := resultsTab{
		vp:    vp,
		pager: p,
		data:  data,
	}

	return r
}

func updateResults(s *DataScope, msg tea.Msg) tea.Cmd {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	// handle pager modifications first
	prevPage := s.results.pager.Page
	s.results.pager, cmd = s.results.pager.Update(msg)
	cmds = append(cmds, cmd)

	s.setResultsDisplayed()               // pass the new content to the view
	if prevPage != s.results.pager.Page { // if page changed, reset to top of view
		s.results.vp.GotoTop()
	}

	// check for keybinds not directly supported by the viewport
	if viewportAddtlKeys(msg, &s.results.vp) {
		return cmds[0]
	}

	s.results.vp, cmd = s.results.vp.Update(msg)
	cmds = append(cmds, cmd)
	return tea.Sequence(cmds...)
}

// view when 'results' tab is active
func viewResults(s *DataScope) string {
	if !s.results.ready {
		return "\nInitializing..."
	}
	return fmt.Sprintf("%s\n%s", s.results.vp.View(), s.results.renderFooter(s.results.vp.Width))
}

// Determines and sets the the content currently visible in the results viewport.
func (s *DataScope) setResultsDisplayed() {
	start, end := s.results.pager.GetSliceBounds(len(s.results.data))
	data := s.results.data[start:end]

	// apply alternating color scheme
	var bldr strings.Builder
	var trueIndex = start // index of full results, between start and end
	for _, d := range data {
		bldr.WriteString(colorizer.Index(trueIndex+1) + ":")
		if trueIndex%2 == 0 {
			bldr.WriteString(evenEntryStyle.Render(d))
		} else {
			bldr.WriteString(oddEntryStyle.Render(d))
		}
		bldr.WriteRune('\n')
		trueIndex += 1
	}
	s.results.vp.SetContent(wrap(s.results.vp.Width, bldr.String()))
}

var resultShortHelp = stylesheet.GreyedOutStyle.Render(
	fmt.Sprintf("%v page • %v scroll • home: jump top • end: jump bottom\n"+
		"tab: cycle • esc: quit",
		stylesheet.LeftRight, stylesheet.UpDown),
)

// generates a renderFooter with the box+line and help keys
func (rt *resultsTab) renderFooter(width int) string {
	var alignerSty = lipgloss.NewStyle().Width(rt.vp.Width).AlignHorizontal(lipgloss.Center)
	// set up each element
	pageNumber := lipgloss.NewStyle().
		Foreground(stylesheet.FocusedColor).
		Render(strconv.Itoa(rt.pager.Page+1)) + " "
	spl := scrollPercentLine(width-lipgloss.Width(pageNumber), rt.vp.ScrollPercent())

	return lipgloss.JoinVertical(lipgloss.Center,
		pageNumber+spl,
		alignerSty.Render(rt.pager.View()),
		alignerSty.Render(resultShortHelp),
	)
}

// recalculate the dimensions of the results tab, factoring in results-specific margins.
// The clipped height is the height available to the results tab (height - tabs height).
func (rt *resultsTab) recalculateSize(rawWidth, clippedHeight int) {
	rt.vp.Height = clippedHeight - lipgloss.Height(rt.renderFooter(rawWidth))
	rt.vp.Width = rawWidth
	rt.ready = true
}
