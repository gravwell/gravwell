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

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/killer"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// mfaPrompt runs a tiny tea.Model that collects username and password.
//
// ! Not intended to be run while Mother is running.
func mfaPrompt(initialUser, initialPass string) (code string, err error) {
	c := credModel{
		userSelected: true,
	}
	c.UserTI = textinput.New()
	c.UserTI.Prompt = stylesheet.TIPromptPrefix
	c.UserTI.SetValue(initialUser)
	c.UserTI.Focus()
	c.PassTI = textinput.New()
	c.PassTI.Prompt = stylesheet.TIPromptPrefix
	c.PassTI.EchoMode = textinput.EchoNone
	c.PassTI.SetValue(initialPass)
	c.PassTI.Blur()
	m, err := tea.NewProgram(c).Run()
	if err != nil {
		return "", err
	}
	// pull input results
	final, ok := m.(mfaModel)
	if !ok {
		clilog.Writer.Criticalf("failed to cast credentials model")
		return "", errors.New("failed to cast mfa model")
	} else if final.killed {
		return "", errors.New("you must authenticate to use gwcli")
	}
	return final.codeTI.Value(), nil
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
