/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package connection

// a tiny tea.Model to prompt for login credentials in interactive mode

import (
	"fmt"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/killer"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

/*type mode uint8

const (
	u_p mode = iota
	mfa mode = iota
)*/

// Run a tiny tea.Model that collects username and password.
// Not intended to be run while Mother is running.
func CredPrompt(user, pass string) (tea.Model, error) {
	c := credModel{
		//mode:         u_p,
		userSelected: true,
	}
	c.UserTI = textinput.New()
	c.UserTI.Prompt = stylesheet.TIPromptPrefix
	c.UserTI.SetValue(user)
	c.UserTI.Focus()
	c.PassTI = textinput.New()
	c.PassTI.Prompt = stylesheet.TIPromptPrefix
	c.PassTI.EchoMode = textinput.EchoNone
	c.PassTI.SetValue(pass)
	c.PassTI.Blur()
	return tea.NewProgram(c).Run()
}

type credModel struct {
	//mode mode

	UserTI       textinput.Model
	PassTI       textinput.Model
	userSelected bool
	killed       bool
}

func (c credModel) Init() tea.Cmd {
	return textinput.Blink
}

func (c credModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	//switch c.mode {
	//case u_p:
	if kill := killer.CheckKillKeys(msg); kill != killer.None {
		c.killed = true
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
			//c.mode = mfa
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
	// case mfa:
	// }
}

func (c credModel) View() string {
	//switch c.mode {
	//case u_p:
	return fmt.Sprintf("%v%v\n%v%v\n\n",
		stylesheet.PromptStyle.Render("username"), c.UserTI.View(),
		stylesheet.PromptStyle.Render("password"), c.PassTI.View())
	// case mfa:
	// }
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
