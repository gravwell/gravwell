/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package busywait provides a unified spinner to display while waiting for async operations.
Do not use in a script context.

# When Mother is not running (invocation via a Cobra.Run func):

Call CobraNew() to get a program, p.Run() to allow the program to take over the terminal (after
spinning up the reaper goroutine), and p.Quit() from the reaper when done waiting.

# When Mother is running:

The spinner can also use be used by Mother to ensure consistency in appearance.
Use NewSpinner if Mother is active.

# Example

	spnrP := busywait.CobraNew()
	go func() {
		SomeLongAsyncOperation()
		spnrP.Quit()
	}()
	if _, err := spnrP.Run(); err != nil {
			return err
	}
*/
package busywait

import (
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

//#region For Cobra Usage

type spnr struct {
	spnr spinner.Model
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
	return s.spnr.View()
}

// CobraNew creates a new BubbleTea program with just a spinner.
// Intended for use in non-script mode Cobra to show processes are occurring.
//
// When you are done waiting, call p.Quit() from another goroutine.
func CobraNew() (p *tea.Program) {
	return tea.NewProgram(spnr{
		spnr: NewSpinner()})
}

//#endregion For Cobra Usage

// NewSpinner provides a consistent spinner interface.
// Intended for integration with an existing Model (eg. from interactive mode).
// Add a spinner.Model to your action struct and instantiate it with this.
func NewSpinner() spinner.Model {
	return spinner.New(
		spinner.WithSpinner(spinner.Moon),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(stylesheet.PrimaryColor)))
}
