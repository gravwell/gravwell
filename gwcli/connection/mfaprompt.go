/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package connection

// a tiny tea.Model to prompt for MFA
//
// typically follows a cred prompt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/killer"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// mfaPrompt runs a tiny tea.Model that collects a code OR recovery key
//
// ! Not intended to be run while Mother is running.
func mfaPrompt() (code string, recoveryUsed bool, err error) {
	c := mfaModel{
		codeSelected: true,
	}
	c.codeTI = textinput.New()
	c.codeTI.Prompt = stylesheet.TIPromptPrefix
	c.codeTI.Focus()
	c.recoveryTI = textinput.New()
	c.recoveryTI.Prompt = stylesheet.TIPromptPrefix
	c.recoveryTI.EchoMode = textinput.EchoNone
	c.recoveryTI.Blur()
	m, err := tea.NewProgram(c).Run()
	if err != nil {
		return "", false, err
	}
	// pull input results
	final, ok := m.(mfaModel)
	if !ok {
		clilog.Writer.Criticalf("failed to cast credentials model")
		return "", false, errors.New("failed to cast mfa model")
	} else if final.killed {
		return "", false, errors.New("you must authenticate to use gwcli")
	}

	err = nil
	code = strings.TrimSpace(final.codeTI.Value())
	if code == "" {
		code = strings.TrimSpace(final.recoveryTI.Value())
		recoveryUsed = true
	}

	return
}

type mfaModel struct {
	codeTI       textinput.Model
	recoveryTI   textinput.Model
	codeSelected bool // code or recovery TI focused
	killed       bool
}

func (m mfaModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m mfaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if kill := killer.CheckKillKeys(msg); kill != killer.None {
		m.killed = true
		return m, tea.Quit
	}

	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.Type {
		case tea.KeyTab, tea.KeyShiftTab, tea.KeyUp, tea.KeyDown: // swap
			return m.swap(), textinput.Blink
		case tea.KeyEnter: // submit or swap
			if m.codeSelected {
				return m.swap(), textinput.Blink
			}
			return m, tea.Quit
		}

	}
	var (
		codecmd  tea.Cmd
		recovcmd tea.Cmd
	)
	m.codeTI, codecmd = m.codeTI.Update(msg)
	m.recoveryTI, recovcmd = m.recoveryTI.Update(msg)

	return m, tea.Batch(codecmd, recovcmd)
}

func (m mfaModel) View() string {
	// TODO clean up visuals
	return fmt.Sprintf("%v%v\n%v%v\n\n",
		stylesheet.PromptStyle.Render("TOTP code"), m.codeTI.View(),
		stylesheet.PromptStyle.Render("recovery code"), m.recoveryTI.View())
}

// select the next TI
func (m mfaModel) swap() mfaModel {
	m.codeSelected = !m.codeSelected
	if m.codeSelected {
		m.codeTI.Focus()
		m.recoveryTI.Blur()
	} else {
		m.codeTI.Blur()
		m.recoveryTI.Focus()
	}

	return m
}
