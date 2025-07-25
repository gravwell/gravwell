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
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
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
		v += "\t" + Cur.SpinnerText.Render(s.notice)
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
		spinner.WithStyle(Cur.Spinner))
}

// Table generates the skeleton of a properly styled table
func Table() *table.Table {
	tbl := table.New().
		Border(Cur.TableSty.BorderType).
		BorderStyle(Cur.TableSty.BorderStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == 0:
				return Cur.TableSty.HeaderCells
			case row%2 == 0:
				return Cur.TableSty.EvenCells
			default:
				return Cur.TableSty.OddCells
			}
		}).BorderRow(true)

	return tbl
}

// NewList creates and returns a new list.Model with customized defaults.
// items must fit the listsupport.Item interface in order to be used with the delegate. However,
// because Go cannot interface arrays, you must pass in your items as []list.Item.
func NewList(items []list.Item, width, height int, singular, plural string) list.Model {
	// update the styles on the default delegate to wrap properly
	dlg := list.NewDefaultDelegate()
	dlg.Styles.SelectedTitle = dlg.Styles.SelectedTitle.Foreground(Cur.PrimaryText.GetForeground())
	dlg.Styles.SelectedDesc = dlg.Styles.SelectedDesc.Foreground(Cur.SecondaryText.GetForeground())

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

// The ListItem interface defines the basic values an item must be able to provide prior to casting to list.ListItem for NewList().
// list will cast the item to this interface when interacting with it.
type ListItem interface {
	Title() string
	Description() string
	FilterValue() string
}
