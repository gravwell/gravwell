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
	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
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

// FilePickerWH (With Help) is a wrapper around the FilePicker bubble that bolts on help and applies a consistent set of keybinds.
// If/when the filepicker bubble properly interfaces with the help interface, this can probably be removed (or at least heavily stripped back).
type FilePickerWH struct {
	filepicker.Model
	help help.Model
	// extra keybinds bolted onto the keymap in filepicker
	fullHelp  key.Binding
	quit      key.Binding
	nextPane  key.Binding // tab
	priorPane key.Binding // shift tab
}

// ShortHelp returns keybindings to be shown in the mini help view. It's part
// of the key.Map interface.
func (fph FilePickerWH) ShortHelp() []key.Binding {
	return []key.Binding{fph.fullHelp, fph.quit, fph.KeyMap.Select, fph.nextPane}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// key.Map interface.
func (fph FilePickerWH) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{fph.KeyMap.Up, fph.KeyMap.Down, fph.KeyMap.Back, fph.KeyMap.Open},
		{fph.KeyMap.GoToTop, fph.KeyMap.GoToLast, fph.KeyMap.PageUp, fph.KeyMap.PageDown},
		{fph.nextPane, fph.priorPane},
		{fph.fullHelp, fph.quit},
	}
}

// NewFilePickerWH returns a new FilePickerWithHelp struct, which wraps the filepicker bubble.
// This version enforces consistent keys and UI and bolts on the subroutines required for filepicker to take advantage of the help bubble.
//
// If displayTabPaneSwitch, then help will also display "tab" to switch to the next composed views.
// If displayShiftTabPaneSwitch, then help will also display "shift tab" to switch to the prior composed view.
func NewFilePickerWH(displayTabPaneSwitch, displayShiftTabPaneSwitch bool) FilePickerWH {
	const (
		marginBottom  = 5
		fileSizeWidth = 7
		paddingLeft   = 2
	)
	fp := filepicker.New()
	// replace the default keys and help display
	fp.KeyMap = filepicker.KeyMap{
		GoToTop:  key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "first")),
		GoToLast: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "last")),
		Down:     key.NewBinding(key.WithKeys("j", "down", "ctrl+n"), key.WithHelp("j/"+UpSigil, "down")),
		Up:       key.NewBinding(key.WithKeys("k", "up", "ctrl+p"), key.WithHelp("k/"+DownSigil, "up")),
		PageUp:   key.NewBinding(key.WithKeys("K", "pgup"), key.WithHelp("K/pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("J", "pgdown"), key.WithHelp("J/pgdown", "page down")),
		Back:     key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/"+LeftSigil, "back")),
		Open:     key.NewBinding(key.WithKeys("l", "right", "enter"), key.WithHelp("l/"+RightSigil, "open")),
		Select:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	}

	fp.Styles = filepicker.Styles{
		DisabledCursor:   Sheet.DisabledText,
		Cursor:           Sheet.PrimaryText,
		Symlink:          Sheet.SecondaryText.Italic(true),
		Directory:        Sheet.SecondaryText,
		File:             lipgloss.NewStyle(),
		DisabledFile:     Sheet.DisabledText,
		DisabledSelected: Sheet.DisabledText,
		Permission:       Sheet.PrimaryText.Faint(true),
		Selected:         Sheet.ExampleText.Bold(true),
		FileSize:         Sheet.PrimaryText.Faint(true).Width(fileSizeWidth).Align(lipgloss.Right),
		EmptyDirectory:   Sheet.DisabledText.PaddingLeft(paddingLeft).SetString("Bummer. No Files Found."),
	}

	h := FilePickerWH{fp,
		help.New(),
		key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
		key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "quit"),
		),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
		key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous pane")),
	}
	if !displayTabPaneSwitch {
		h.nextPane = key.NewBinding(key.WithDisabled())
	}
	if !displayShiftTabPaneSwitch {
		h.priorPane = key.NewBinding(key.WithDisabled())
	}
	return h
}

// Update handles ShowHelp key ('?') and passes any other messages to the file picker.
func (fph FilePickerWH) Update(msg tea.Msg) (FilePickerWH, tea.Cmd) {
	// check for show all key
	if keyMsg, ok := msg.(tea.KeyMsg); ok && key.Matches(keyMsg, fph.fullHelp) {
		fph.help.ShowAll = !fph.help.ShowAll
		return fph, nil
	}
	var cmd tea.Cmd
	fph.Model, cmd = fph.Model.Update(msg)

	return fph, cmd
}

// View displays the file picker.
func (fph FilePickerWH) View() string {
	return fph.Model.View()
}

// ViewHelp displays the help keys and text associated to the file picker.
func (fph FilePickerWH) ViewHelp() string {
	return fph.help.View(fph)
}
