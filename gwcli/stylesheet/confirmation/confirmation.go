/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package confirmation provides an action.Model that request the user confirm their actions before submission.
package confirmation

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
)

type Model struct {
	choices        []string
	choicesCursor  uint
	submitSelected bool

	// width/height are used to center the buttons
	width  uint
	height uint

	headerLines []string
}

// New generates a new confirmation Model with the given options.
// Passing nil choices is allowed, but somewhat nonsensical.
//
// The View will place two buttons, submit and cancel, in a left column and all given choice in a right columns.
func (m *Model) Init(choices []string, width, height uint, headerTextLines ...string) {
	m.choices = choices
	m.choicesCursor = 0
	m.submitSelected = true

	m.width = width
	m.height = height
	m.headerLines = headerTextLines
}

// Update processing incoming messages and updates the internal structure accordingly.
// It also returns the selection made (iff done is set).
func (m Model) Update(msg tea.Msg) (_ Model, _ tea.Cmd, done, submit bool, choice uint) {
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = uint(wsm.Width)
		m.height = uint(wsm.Height)
	}

	switch {
	case hotkeys.Match(msg, hotkeys.CursorDown):
		m.next()
	case hotkeys.Match(msg, hotkeys.CursorUp):
		m.prev()
	case hotkeys.Match(msg, hotkeys.CursorRight):
		m.right()
	case hotkeys.Match(msg, hotkeys.CursorLeft):
		m.left()
	case hotkeys.ButtonPressed(msg):
		return m, nil, true, m.submitSelected, m.choicesCursor
	}
	return m, nil, false, m.submitSelected, m.choicesCursor

}

// Move cursor left, onto the submit button.
func (m *Model) left() {
	if m.submitSelected {
		return
	}
	m.submitSelected = true
}

func (m *Model) right() {
	m.submitSelected = false
}

func (m *Model) prev() {
	if m.submitSelected { // job's done
		return
	}
	if m.choicesCursor == 0 { // wrap
		m.choicesCursor = uint(len(m.choices) - 1)
		return
	}
	m.choicesCursor -= 1
}

func (m *Model) next() {
	if m.submitSelected { // job's done
		return
	}

	if m.choicesCursor == uint(len(m.choices)-1) { // wrap
		m.choicesCursor = 0
		return
	}
	m.choicesCursor += 1
}

// View style:
//
//			HeaderLine1
//			HeaderLine2
//			...
//			HeaderLineN
//
// 						Return to:
//
// 						[<choice1>]
//
// 						[<choice2>]
// [Submit]		or
//						[<choice3>]
//
//							...
//
//						[<choiceN>]
//
// 		Press esc to cancel
//
//

func (m Model) View() string {
	// generate each button
	var submitBtnPip string = " "
	if m.submitSelected {
		submitBtnPip = stylesheet.Cur.Pip()
	}
	var (
		submitBtn  = lipgloss.JoinHorizontal(lipgloss.Center, submitBtnPip, stylesheet.Button("submit"))
		choiceBtns = make([]string, len(m.choices))
	)

	for i, choice := range m.choices {
		var pip string = " "
		if !m.submitSelected && m.choicesCursor == uint(i) {
			pip = stylesheet.Cur.Pip()
		}
		choiceBtns[i] = lipgloss.JoinHorizontal(lipgloss.Center, pip, stylesheet.Button(choice))
	}

	// compose choice buttons
	right := lipgloss.JoinVertical(lipgloss.Center,
		append([]string{"Return to:"}, choiceBtns...)...,
	)
	// give submit the same width as the right side
	submitBtn = lipgloss.NewStyle().Width(lipgloss.Width(right) + 1).Render(submitBtn)

	// join submit, "or", and choices
	body := lipgloss.JoinHorizontal(lipgloss.Center, submitBtn, "or", right)
	// compose header lines above selections and cancel line below
	return lipgloss.JoinVertical(lipgloss.Center, append(m.headerLines, "", body, "Press "+hotkeys.SoftQuit.Keys()[0]+" to cancel")...)
}
