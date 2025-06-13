/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package stylesheet

/**
 * Prebuilt, commonly-used models for stylistic consistency.
 */

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// NewTI creates a textinput with common attributes.
func NewTI(defVal string, optional bool) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Width = 20
	ti.Blur()
	ti.SetValue(defVal)
	ti.KeyMap.WordForward.SetKeys("ctrl+right", "alt+right", "alt+f")
	ti.KeyMap.WordBackward.SetKeys("ctrl+left", "alt+left", "alt+b")
	if optional {
		ti.Placeholder = "(optional)"
	}
	return ti
}

//#region For Cobra Usage

type spnr struct {
	notice string // additional text displayed alongside the spinner
	spnr   spinner.Model
}

func (s spnr) Init() tea.Cmd {
	return s.spnr.Tick
}

func (s spnr) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var toRet tea.Cmd
	s.spnr, toRet = s.spnr.Update(msg)
	return s, toRet
}

func (s spnr) View() string {
	v := s.spnr.View()
	if s.notice != "" {
		v += "\t" + Sheet.PromptText.Render(s.notice)
	}
	return v
}

// CobraSpinner creates a new BubbleTea program with just a spinner.
// Intended for use in non-script mode Cobra to show processes are occurring.
// Start the spinner with
//
//	go p.Run()
//
// When you are done waiting, call p.Quit() from a different (or the main) goroutine.
func CobraSpinner(notice string) (p *tea.Program) {
	return tea.NewProgram(spnr{notice: notice,
		spnr: NewSpinner()},
		tea.WithoutSignalHandler(),
		tea.WithInput(nil)) // we do not want the spinner to capture sigints when it is run on its own
}

//#endregion For Cobra Usage

// NewSpinner provides a consistent spinner interface.
// Intended for integration with an existing Model (eg. from interactive mode).
// Add a spinner.Model to your action struct and instantiate it with this.
func NewSpinner() spinner.Model {
	return spinner.New(
		spinner.WithSpinner(spinner.Moon),
		spinner.WithStyle(Sheet.Spinner))
}

// Table generates the skeleton of a properly styled table
func Table() *table.Table {
	tbl := table.New().
		Border(Sheet.Table.BorderType).
		BorderStyle(Sheet.Table.BorderStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == 0:
				return Sheet.Table.HeaderCells
			case row%2 == 0:
				return Sheet.Table.EvenCells
			default:
				return Sheet.Table.OddCells
			}
		}).BorderRow(true)

	return tbl
}
