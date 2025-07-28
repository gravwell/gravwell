/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

// The modView file contains an implementation of the modifiers pane, which allows users to punch in a tag, source, and play with a couple toggles.
// It has a 2x2 format.

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
)

// currently selected item in the mod view for pips and focus
type modItem = uint

const (
	lowBound  modItem = iota
	src               // (1,1)
	tag               // (1,2)
	ignoreTS          // (2,1)
	localTime         // (2,2)
	highBound
)

// mod struct represents the state of the modifier/excess details pane
type mod struct {
	// meta
	focused  bool    // is the modifier pane in focus?
	selected modItem // currently selected item in the mod pane

	tagTI     textinput.Model // tag to ingest file under
	srcTI     textinput.Model // user-provided IP address source
	ignoreTS  bool
	localTime bool
}

func NewMod() mod {
	m := mod{
		focused:  false,
		selected: src,

		tagTI: stylesheet.NewTI("", false),
		srcTI: stylesheet.NewTI("", true),
	}
	m.srcTI.Placeholder = ""
	m.srcTI.Focus()
	m.tagTI.Blur()
	return m
}

// Does not handle enter or tab; caller is expected to catch and process these before handing off control.
func (m mod) update(msg tea.Msg) (mod, tea.Cmd) {
	if m.moveCursor(msg) {
		return m, textinput.Blink
	}

	var cmds = []tea.Cmd{nil, nil}
	m.srcTI, cmds[0] = m.srcTI.Update(msg)
	m.tagTI, cmds[1] = m.tagTI.Update(msg)

	return m, tea.Batch(cmds...)
}

// moveCursor checks if the message is an arrow key and changes the selected field accordingly.
// Returns done if the message has been fully handled; if !done, caller should pass the message to other components (aka: the TIs).
func (m *mod) moveCursor(msg tea.Msg) (done bool) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyLeft:
			switch m.selected {
			case tag:
				m.selected = src
				done = true
			case localTime:
				m.selected = ignoreTS
				done = true
			}
		case tea.KeyRight:
			switch m.selected {
			case src:
				m.selected = tag
				done = true
			case ignoreTS:
				m.selected = localTime
				done = true
			}
		case tea.KeyUp:
			switch m.selected {
			case ignoreTS:
				m.selected = src
				done = true
			case localTime:
				m.selected = tag
				done = true
			}
		case tea.KeyDown:
			switch m.selected {
			case src:
				m.selected = ignoreTS
				done = true
			case tag:
				m.selected = localTime
				done = true
			}
		case tea.KeySpace:
			// toggle the selected boolean
			switch m.selected {
			case ignoreTS:
				m.ignoreTS = !m.ignoreTS
				done = true
			case localTime:
				m.localTime = !m.localTime
				done = true
			}
		}
		m.focusSelected()
	}

	return done
}

func (m mod) view(termWidth int) string {
	// always allocate the maximum space so it doesn't jitter when switching focys
	maxFrameSize := max(stylesheet.Cur.ComposableSty.FocusedBorder.GetHorizontalFrameSize(), stylesheet.Cur.ComposableSty.UnfocusedBorder.GetHorizontalFrameSize())
	availWidth := termWidth - maxFrameSize

	src := stylesheet.Pip(m.selected, src) + stylesheet.Cur.Field("source", 0) + m.srcTI.View()
	ts := stylesheet.Pip(m.selected, ignoreTS) + stylesheet.Cur.Field("Ignore Timestamps?", 0) + stylesheet.Checkbox(m.ignoreTS)
	l := lipgloss.JoinVertical(lipgloss.Left, src, ts)

	tag := stylesheet.Pip(m.selected, tag) + stylesheet.Cur.Field("tag", 0) + m.tagTI.View()
	tm := stylesheet.Pip(m.selected, localTime) + stylesheet.Cur.Field("Use Server Local Time?", 0) + stylesheet.Checkbox(m.localTime)
	r := lipgloss.JoinVertical(lipgloss.Left, tag, tm)

	s := lipgloss.NewStyle().AlignHorizontal(lipgloss.Center).Width(availWidth).Render(lipgloss.JoinHorizontal(lipgloss.Center, l, r))

	if !m.focused {
		return stylesheet.Cur.ComposableSty.UnfocusedBorder.
			AlignHorizontal(lipgloss.Center).
			Render(s)
	} else {
		return stylesheet.Cur.ComposableSty.FocusedBorder.
			AlignHorizontal(lipgloss.Center).
			Render(s)
	}
}

// Returns a mod view that has been returned to its initial form and is ready for re-use.
func (m mod) reset() mod {
	m.focused = false
	m.tagTI.Reset()
	m.srcTI.Reset()
	m.ignoreTS = false
	m.localTime = false

	m.selected = src
	m.srcTI.Focus()
	m.tagTI.Blur()

	return m
}

// update the focus/blur settings to field corresponding to the current enumeration of m.selected.
func (m *mod) focusSelected() {
	switch m.selected {
	case src:
		m.srcTI.Focus()
		m.tagTI.Blur()
	case tag:
		m.srcTI.Blur()
		m.tagTI.Focus()
	default:
		m.srcTI.Blur()
		m.tagTI.Blur()
	}
}
