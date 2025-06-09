/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package mfaprompt provide a tiny, mother-independent TUI to collect a TOTP or recovery code.
// Use .Collect().
package mfaprompt

// a tiny tea.Model to prompt for MFA
//
// typically follows a cred prompt

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/killer"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Collect runs a tiny tea.Model that returns a code OR recovery key.
//
// ! Not intended to be run while Mother is running.
func Collect() (code string, at types.AuthType, err error) {
	return collect(nil)
}

// internal implementation of collect.
// Allows custom programs (likely programs with mocked input) for testing purposes.
// ! Outside of test packages, leave prog==nil.
func collect(prog *tea.Program) (code string, at types.AuthType, err error) {
	p := prog
	if p == nil {
		p = tea.NewProgram(New())
	}

	m, err := p.Run()
	if err != nil {
		return "", types.AUTH_TYPE_NONE, err
	}
	// pull input results
	final, ok := m.(mfaModel)
	if !ok {
		clilog.Writer.Criticalf("failed to cast credentials model")
		return "", types.AUTH_TYPE_NONE, uniques.ErrGeneric
	} else if final.killed {
		return "", types.AUTH_TYPE_NONE, uniques.ErrMustAuth
	}

	err = nil
	code = strings.TrimSpace(final.codeTI.Value())
	at = types.AUTH_TYPE_TOTP
	if code == "" {
		code = strings.TrimSpace(final.recoveryTI.Value())
		at = types.AUTH_TYPE_RECOVERY
	}

	return
}

type mfaModel struct {
	codeTI       textinput.Model
	recoveryTI   textinput.Model
	codeSelected bool // code or recovery TI focused
	killed       bool
	done         bool
}

func New() mfaModel {
	c := mfaModel{codeSelected: true}
	c.codeTI = textinput.New()
	c.codeTI.Prompt = stylesheet.TIPromptPrefix
	c.codeTI.Validate = func(s string) error {
		for _, r := range s {
			if !unicode.IsDigit(r) {
				return errors.New("TOTP code can only be digits")
			}
		}
		return nil
	}
	c.codeTI.Width = 6
	c.codeTI.CharLimit = 6
	c.codeTI.Placeholder = "123456"
	c.codeTI.Focus()

	c.recoveryTI = textinput.New()
	c.recoveryTI.Prompt = stylesheet.TIPromptPrefix
	c.recoveryTI.Blur()

	return c
}

func (m mfaModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m mfaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.done { // do not accept more input once killed
		return m, nil
	}
	if kill := killer.CheckKillKeys(msg); kill != killer.None {
		m.killed = true
		m.done = true
		return m, tea.Quit
	}

	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.Type {
		case tea.KeyTab, tea.KeyShiftTab, tea.KeyUp, tea.KeyDown: // swap
			return m.swap(), textinput.Blink
		case tea.KeyEnter: // submit
			m.done = true
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
	return fmt.Sprintf("%v%v\n"+
		"If you don't have access to your authenticator, you can enter a recovery code below:\n"+
		"%v%v\n"+
		"Once a recovery code has been used, it cannot be used again!\n",
		stylesheet.IndexStyle.Render("TOTP"), m.codeTI.View(),
		stylesheet.ExampleStyle.Render("recovery"), m.recoveryTI.View())
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
