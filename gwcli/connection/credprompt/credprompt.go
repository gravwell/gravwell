/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package credprompt is a tiny package for spinning up a mother-independent TUI to collect username and password.
// Use .Collect().
package credprompt

// a tiny tea.Model to prompt for user name and password
//
// If MFA is required, this model will likely be followed up by the MFA prompt

import (
	"fmt"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/killer"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Collect runs a tiny tea.Model that collects username and password.
// This is a blocking call; it only returns when the user enters a username and password or passes a killkey;
// Collect manages its own killkeys, as it is mother-independent.
//
// ! Not intended to be run while Mother is running.
func Collect(initialUser string) (user, pass string, err error) {
	return collect(initialUser, nil)
}

// internal implementation of collect.
// Allows custom programs (likely programs with mocked input) for testing purposes.
// ! Outside of test packages, leave prog==nil.
func collect(initialUser string, prog *tea.Program) (user, pass string, err error) {
	p := prog
	if p == nil {
		var c tea.Model = New(initialUser)
		p = tea.NewProgram(c)
	}

	m, err := p.Run()
	if err != nil {
		return "", "", err
	}
	// pull input results
	finalCredM, ok := m.(credModel)
	if !ok {
		clilog.Writer.Criticalf("failed to cast credentials model")
		return "", "", uniques.ErrGeneric
	} else if finalCredM.killed {
		return "", "", uniques.ErrMustAuth
	}
	return finalCredM.UserTI.Value(), finalCredM.PassTI.Value(), nil
}

type credModel struct {
	userStartingValue string
	UserTI            textinput.Model
	PassTI            textinput.Model
	userSelected      bool
	killed            bool
	done              bool
}

// New creates a new credprompt, which satisfies the tea.Model interface.
// You probably want Collect(), instead; this is mostly used internally and for testing.
func New(initialUser string) credModel {
	c := credModel{userStartingValue: initialUser, userSelected: true}
	c.UserTI = textinput.New()
	c.UserTI.Prompt = stylesheet.Cur.PromptSty.Symbol()
	c.UserTI.SetValue(c.userStartingValue)
	c.UserTI.Focus()
	c.PassTI = textinput.New()
	c.PassTI.Prompt = stylesheet.Cur.PromptSty.Symbol()
	c.PassTI.EchoMode = textinput.EchoNone
	c.PassTI.Blur()
	return c
}

// Init prepares the cred model for usage, setting up the text inputs sending initial messages.
// Once Init returns, Update and View can be safely used via BubbleTea.
func (c credModel) Init() tea.Cmd {
	return textinput.Blink
}

func (c credModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if c.done { // do not accept more input once killed
		return c, nil
	}
	if kill := killer.CheckKillKeys(msg); kill != killer.None {
		c.killed = true
		c.done = true
		return c, tea.Quit
	}

	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.Type {
		case tea.KeyTab, tea.KeyShiftTab, tea.KeyUp, tea.KeyDown: // swap
			return c.swap(), textinput.Blink
		case tea.KeyEnter: // submit or swap
			if c.userSelected {
				return c.swap(), textinput.Blink
			}
			c.done = true
			return c, tea.Quit
		}

	}
	var (
		usercmd tea.Cmd
		passcmd tea.Cmd
	)
	c.UserTI, usercmd = c.UserTI.Update(msg)
	c.PassTI, passcmd = c.PassTI.Update(msg)

	return c, tea.Batch(usercmd, passcmd)
}

func (c credModel) View() string {
	return fmt.Sprintf("%v%v\n%v%v\n\n",
		stylesheet.Cur.Prompt("username"), c.UserTI.View(),
		stylesheet.Cur.Prompt("password"), c.PassTI.View())
}

// select the next TI
func (c credModel) swap() credModel {
	c.userSelected = !c.userSelected
	if c.userSelected {
		c.UserTI.Focus()
		c.PassTI.Blur()
	} else {
		c.UserTI.Blur()
		c.PassTI.Focus()
	}

	return c
}
