/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package query

/**
 * This file defines the modifiers view, pre-execution modifications of the query.
 * It is intended to collect and display data auxillary (required or optional) to the query itself.
 * DataScope contains actions that can be taken after the query is completed; this is just for
 * modification or actions that must be completed prior to execution.
 */

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/colorizer"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// modifSelection provides the skeleton for cursoring through options within this view.
// Most options have been relocated so it is rather overengineered currently.
// However, its skeleton has been left in place so adding new options in the future is easy.
// See datascope's download and schedule tabs for examples.
type modifSelection = uint

const (
	lowBound modifSelection = iota
	duration
	background
	highBound
)

// modifView represents the composable view box containing all configurable features of the query
type modifView struct {
	width    uint
	height   uint
	selected uint // tracks which modifier is currently active w/in this view
	// knobs available to user
	durationTI textinput.Model
	background bool

	keys []key.Binding
}

// generate the second view to be composed with the query editor
func initialModifView(height, width uint) modifView {
	mv := modifView{
		width:    width,
		height:   height,
		selected: duration, // default to duration
		keys: []key.Binding{
			key.NewBinding(
				key.WithKeys(stylesheet.UpDown),
				// help is not necessary when there is only one option
				// key.WithHelp(stylesheet.UpDown, "select input"),
			)},
	}

	// build duration ti
	mv.durationTI = stylesheet.NewTI(defaultDuration.String(), false)
	mv.durationTI.Placeholder = "1h00m00s00ms00us00ns"
	mv.durationTI.Validate = func(s string) error {
		// checks that the string is composed of valid characters for duration parsing
		// (0-9 and h,m,s,u,n)
		// ! does not confirm that it is a valid duration!
		validChars := map[rune]interface{}{
			'h': nil, 'm': nil, 's': nil,
			'u': nil, 'n': nil, '.': nil,
		}
		for _, r := range s {
			if unicode.IsDigit(r) {
				continue
			}
			if _, found := validChars[r]; !found {
				return errors.New("only digits or the characters h, m, s, u, and n are allowed")
			}
		}
		return nil
	}
	return mv
}

// Walks through the options in modifSelection and passes keys to the currently selected one.
func (mv *modifView) update(msg tea.Msg) []tea.Cmd { // TODO switch away from an array of Cmds.
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyUp:
			mv.selected -= 1
			if mv.selected <= lowBound {
				mv.selected = highBound - 1
			}
			if mv.selected == duration {
				mv.durationTI.Focus()
			} else {
				mv.durationTI.Blur()
			}
			return []tea.Cmd{textinput.Blink}
		case tea.KeyDown:
			mv.selected += 1
			if mv.selected >= highBound {
				mv.selected = lowBound + 1
			}
			if mv.selected == duration {
				mv.durationTI.Focus()
			} else {
				mv.durationTI.Blur()
			}
			return []tea.Cmd{textinput.Blink}
		case tea.KeySpace, tea.KeyEnter:
			// handle booleans
			switch mv.selected {
			case background:
				mv.background = !mv.background
			}
		}
	}
	var cmds []tea.Cmd = make([]tea.Cmd, 1)
	mv.durationTI, cmds[0] = mv.durationTI.Update(msg)

	return cmds
}

func (mv *modifView) view() string {
	var bldr strings.Builder

	bldr.WriteString(" " + stylesheet.Header1Style.Render("Duration:") + "\n")
	bldr.WriteString(
		fmt.Sprintf("%s%s\n", colorizer.Pip(mv.selected, duration), mv.durationTI.View()),
	)
	bldr.WriteString(
		fmt.Sprintf("%s%s %s\n", colorizer.Pip(mv.selected, background), colorizer.Checkbox(mv.background), stylesheet.Header1Style.Render("Background?")),
	)

	return bldr.String()
}

func (mv *modifView) reset() {
	mv.durationTI.Reset()
	mv.durationTI.Blur()
}
