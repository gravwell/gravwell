/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package filegrabber implements the filegrabber type, an upgraded filepicker bubble.
package filegrabber

import (
	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
)

// FileGrabber is a wrapper around the FilePicker bubble that bolts on additional functionality.
//
// Specifically, filegrabber:
// 1) fits the help interface,
// 2) enables path jumping,
// 3) and pre-builds each view separately (help, picker, path).
type FileGrabber struct {
	filepicker.Model
	help help.Model

	jumpMode bool // are we in jump mode?

	// extra keybinds bolted onto the keymap in filepicker
	fullHelp  key.Binding
	quit      key.Binding
	nextPane  key.Binding // tab
	priorPane key.Binding // shift tab
	jump      key.Binding // edit path to jump to new dir
}

// ShortHelp returns keybindings to be shown in the mini help view. It's part
// of the key.Map interface.
func (fg FileGrabber) ShortHelp() []key.Binding {
	return []key.Binding{fg.fullHelp, fg.quit, fg.KeyMap.Select, fg.nextPane}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// key.Map interface.
func (fg FileGrabber) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{fg.KeyMap.Up, fg.KeyMap.Down, fg.KeyMap.Back, fg.KeyMap.Open},
		{fg.KeyMap.GoToTop, fg.KeyMap.GoToLast, fg.KeyMap.PageUp, fg.KeyMap.PageDown},
		{fg.nextPane, fg.priorPane},
		{fg.fullHelp, fg.quit},
	}
}

// New returns a new FileGrabber instance.
//
// If displayTabPaneSwitch, then help will also display "tab" to switch to the next composed views.
// If displayShiftTabPaneSwitch, then help will also display "shift tab" to switch to the prior composed view.
func New(displayTabPaneSwitch, displayShiftTabPaneSwitch bool) FileGrabber {
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
		Down:     key.NewBinding(key.WithKeys("j", "down", "ctrl+n"), key.WithHelp("j/"+stylesheet.UpSigil, "down")),
		Up:       key.NewBinding(key.WithKeys("k", "up", "ctrl+p"), key.WithHelp("k/"+stylesheet.DownSigil, "up")),
		PageUp:   key.NewBinding(key.WithKeys("K", "pgup"), key.WithHelp("K/pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("J", "pgdown"), key.WithHelp("J/pgdown", "page down")),
		Back:     key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/"+stylesheet.LeftSigil, "back")),
		Open:     key.NewBinding(key.WithKeys("l", "right", "enter"), key.WithHelp("l/"+stylesheet.RightSigil, "open")),
		Select:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	}

	fp.Styles = filepicker.Styles{
		DisabledCursor:   stylesheet.Cur.DisabledText,
		Cursor:           stylesheet.Cur.PrimaryText,
		Symlink:          stylesheet.Cur.SecondaryText.Italic(true),
		Directory:        stylesheet.Cur.SecondaryText,
		File:             lipgloss.NewStyle(),
		DisabledFile:     stylesheet.Cur.DisabledText,
		DisabledSelected: stylesheet.Cur.DisabledText,
		Permission:       stylesheet.Cur.PrimaryText.Faint(true),
		Selected:         stylesheet.Cur.ExampleText.Bold(true),
		FileSize:         stylesheet.Cur.PrimaryText.Faint(true).Width(fileSizeWidth).Align(lipgloss.Right),
		EmptyDirectory:   stylesheet.Cur.DisabledText.PaddingLeft(paddingLeft).SetString("Bummer. No Files Found."),
	}

	h := FileGrabber{fp,
		help.New(),
		false,
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
		key.NewBinding(key.WithKeys("o", "ctrl+i"), key.WithHelp("o", "jump to path")),
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
func (fg FileGrabber) Update(msg tea.Msg) (FileGrabber, tea.Cmd) {
	// check for show all key
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if key.Matches(keyMsg, fg.fullHelp) {
			fg.help.ShowAll = !fg.help.ShowAll
			return fg, nil
		}
		// check for bolted on keys
		if key.Matches(keyMsg, fg.jump) {
			// enter jump mode
		}
	}

	var cmd tea.Cmd
	fg.Model, cmd = fg.Model.Update(msg)

	return fg, cmd
}

// View displays the file picker.
func (fg FileGrabber) View() string {
	return fg.Model.View()
}

// ViewHelp displays the help keys and text associated to the file picker.
func (fg FileGrabber) ViewHelp() string {
	return fg.help.View(fg)
}
