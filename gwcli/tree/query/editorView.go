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

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// editorView represents the composable view box containing the query editor and any errors therein
type editorView struct {
	ta   textarea.Model
	err  string
	keys []key.Binding
}

func initialEdiorView(height, width uint) editorView {
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
	// set up the help keys
	ev.keys = []key.Binding{ // 0: submit
		key.NewBinding(
			key.WithKeys("alt+enter"),
			key.WithHelp("alt+enter", "submit query"),
		)}

	return ev
}

// Passes messages into the editor view's text area.
// Catches "alt+enter", returning submit if caught, alerting the caller to go ahead and submit the query inside of the TA.
func (ev *editorView) update(msg tea.Msg) (cmd tea.Cmd, submit bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		ev.err = ""
		switch {
		case key.Matches(msg, ev.keys[0]): // submit
			if ev.ta.Value() == "" {
				// superfluous request
				ev.err = "empty request"
				// falls through to standard update
			} else {
				return nil, true
			}
		}
	}
	var t tea.Cmd
	ev.ta, t = ev.ta.Update(msg)
	return t, false
}

func (ev *editorView) view() string {
	return fmt.Sprintf("%s\n%s\n%s",
		stylesheet.Header1Style.Render("Query:"),
		ev.ta.View(),
		stylesheet.ErrStyle.Width(stylesheet.TIWidth).Render(ev.err)) // add a width style for wrapping
}
