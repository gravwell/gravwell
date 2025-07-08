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
	fp.AutoHeight = false
	// replace the default keys and help display
	fp.KeyMap = filepicker.KeyMap{
		GoToTop:  key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "first")),
		GoToLast: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "last")),
		Down:     key.NewBinding(key.WithKeys("j", "down", "ctrl+n"), key.WithHelp("j/"+stylesheet.UpSigil, "down")),
		Up:       key.NewBinding(key.WithKeys("k", "up", "ctrl+p"), key.WithHelp("k/"+stylesheet.DownSigil, "up")),
		PageUp:   key.NewBinding(key.WithKeys("K", "pgup"), key.WithHelp("K/pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("J", "pgdown"), key.WithHelp("J/pgdown", "page down")),
		Back:     key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/"+stylesheet.LeftSigil, "parent dir")),
		Open:     key.NewBinding(key.WithKeys("l", "right", "enter"), key.WithHelp("l/"+stylesheet.RightSigil, "open file/dir")),
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
	fp.Cursor = stylesheet.Cur.Pip()

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

// Below is relic code for a filegrabber with path jumping capabilities.
// However, it is not currently in use as it would make more sense to spin off filegrabber as a wholly new bubble, rather than trying to wrap filepicker and work around its rough edges.
/*

var (
	ErrEmptyPath     error = errors.New("jump path cannot be empty")
	ErrNotADirectory error = errors.New("jump path must be a directory")
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

	focused bool // are we currently focused?

	jumpMode bool // are we in jump mode?
	toJumpTI textinput.Model

	// current error, cleared on keyMsg.
	// If you don't want to call errView, the error can be displayed directly with .Error()
	err error

	// extra keybinds bolted onto the keymap in filepicker
	fullHelp  key.Binding
	quit      key.Binding
	nextPane  key.Binding // tab
	priorPane key.Binding // shift tab
	jump      key.Binding // edit path to jump to new dir

	Styles struct {
		PathHeader lipgloss.Style
		Path       lipgloss.Style
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

	h := FileGrabber{
		Model:    fp,
		help:     help.New(),
		jumpMode: false,
		toJumpTI: stylesheet.NewTI("", false),
		fullHelp: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
		quit: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "quit"),
		),
		nextPane:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
		priorPane: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous pane")),
		jump:      key.NewBinding(key.WithDisabled()), //key.NewBinding(key.WithKeys("o", "ctrl+i"), key.WithHelp("o", "jump to path")),
		Styles: struct {
			PathHeader lipgloss.Style
			Path       lipgloss.Style
		}{
			PathHeader: stylesheet.Cur.SecondaryText,
			Path:       lipgloss.NewStyle(),
		},
	}
	if !displayTabPaneSwitch {
		h.nextPane = key.NewBinding(key.WithDisabled())
	}
	if !displayShiftTabPaneSwitch {
		h.priorPane = key.NewBinding(key.WithDisabled())
	}
	return h
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
		{fg.nextPane, fg.priorPane, fg.jump},
		{fg.fullHelp, fg.quit},
	}
}

// Error returns the current error held by filegrabber.
// If you are going to directly display this error (and are not using ViewFull()), may as well use .ViewError().
func (fg FileGrabber) Error() error {
	return fg.err
}

// Focus the filegrabber, enabling user input to navigate the file picker and/or enter a path into toJump.
func (fg FileGrabber) Focus() tea.Cmd {
	fg.focused = true
	if fg.jumpMode {
		return fg.toJumpTI.Focus()
	}

	return nil
}

// Blur disables the filegrabber, causing it to ignore all input.
func (fg FileGrabber) Blur() {
	fg.focused = false
	fg.toJumpTI.Blur()
}

//#region updates

// Update handles ShowHelp key ('?') and passes any other messages to the file picker.
func (fg FileGrabber) Update(msg tea.Msg) (FileGrabber, tea.Cmd) {
	if !fg.focused {
		return fg, nil
	}

	// if this is a key message, clear the error
	if _, ok := msg.(tea.KeyMsg); ok {
		fg.err = nil
	}

	// if we are in jump mode, enter text directly into the path bar
	if fg.jumpMode {
		return fg.jumpUpdate(msg)
	}

	// check for show all key
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if key.Matches(keyMsg, fg.fullHelp) {
			fg.help.ShowAll = !fg.help.ShowAll
			return fg, nil
		}
		// check for bolted on keys
		if key.Matches(keyMsg, fg.jump) {
			// enter jump mode
			fg.jumpMode = true
			return fg, nil
		}
	}

	var cmd tea.Cmd
	fg.Model, cmd = fg.Model.Update(msg)

	return fg, cmd
}

func (fg FileGrabber) jumpUpdate(msg tea.Msg) (FileGrabber, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Type == tea.KeyEnter {
			pth := strings.TrimSpace(fg.toJumpTI.Value())
			// test for path validity
			if pth == "" {
				// set error
				fg.err = ErrEmptyPath
				// exit jump mode
				fg.jumpMode = true
				return fg, nil
			}
			info, err := os.Stat(pth)
			if err != nil {
				fg.err = err
				return fg, textinput.Blink
			} else if !info.IsDir() {
				fg.err = ErrNotADirectory
				return fg, textinput.Blink
			}

			// apply path change
			fg.CurrentDirectory = pth

			// switch off jump mode and re-initialize
			fg.toJumpTI.Blur()
			fg.jumpMode = false
			return fg, fg.Init() // re-calling init is the only way to force a readDir msg
		}
	}

	// pass the message into the holder TI
	var cmd tea.Cmd
	fg.toJumpTI, cmd = fg.toJumpTI.Update(msg)
	return fg, cmd
}

//#endregion updates

//#region views

// ViewPath displays the current path and/or
func (fg FileGrabber) ViewPath() string {
	var (
		headerText string
		path       string
	)
	if fg.jumpMode {
		headerText = "jump to:"
		path = fg.toJumpTI.View()
	} else {
		headerText = "current path:"
		path = fg.CurrentDirectory
	}

	return fmt.Sprintf("%v\n%v", fg.Styles.PathHeader.Render(headerText), fg.Styles.Path.Render(path))
}

// View displays the file picker.
func (fg FileGrabber) View() string {
	return fg.Model.View()
}

// ViewHelp displays the help keys and text associated to the file picker.
func (fg FileGrabber) ViewHelp() string {
	return fg.help.View(fg)
}

// ViewError displays the current error (stylized) or "".
func (fg FileGrabber) ViewError() string {
	if fg.err != nil {
		return stylesheet.Cur.ErrorText.Render(fg.err.Error())
	}
	return ""
}

// ViewFull displays all views of
func (fg FileGrabber) ViewFull() string {
	return "" // TODO
}

//#endregion views


*/
