/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package query

/**
 * This file defines the editor view, which contains the query editor users can enter their search
 * string into.
 */

import (
	"fmt"

	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// editorView represents the composable view box containing the query editor and any errors therein
type editorView struct {
	ta  textarea.Model
	err string
}

func initialEditorView(height, width uint) editorView {
	ev := editorView{}

	// configure text area
	ev.ta = textarea.New()
	ev.ta.ShowLineNumbers = true
	ev.ta.Prompt = stylesheet.TAPromptPrefix
	ev.ta.SetWidth(int(width))
	ev.ta.SetHeight(int(height))
	ev.ta.KeyMap.WordForward.SetKeys("ctrl+right", "alt+right", "alt+f")
	ev.ta.KeyMap.WordBackward.SetKeys("ctrl+left", "alt+left", "alt+b")
	ev.ta.Focus()

	return ev
}

// Passes messages into the editor view's text area.
// Returns submit if focused and the submit keybind was contained in the message.
// If submit is returned, caller can attempt to submit the query.
func (ev *editorView) update(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(tea.KeyMsg); ok { // clear error on keymsg
		ev.err = ""
	}
	var t tea.Cmd
	ev.ta, t = ev.ta.Update(msg)
	return t
}

func (ev *editorView) view() string {
	return fmt.Sprintf("%s\n%s\n%s",
		stylesheet.Cur.PrimaryText.Render("Query:"),
		ev.ta.View(),
		stylesheet.Cur.ErrorText.Width(stylesheet.TIWidth).Render(ev.err)) // add a width style for wrapping
}
