/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package scaffold contains packages for generating new actions from skeletons. See scaffoldlist, scaffolddelete, etc for more information.
The bare scaffold package comes with a skeleton for basic actions.

A basic action is the simplest action: it does its thing and returns a string to be printed to the
terminal. Give it the function you want performed when the action is invoked and have it return
whatever string value you want printed to the screen, if at all.

Basic actions have no default flags and will not handle flags unless a flagFunc is given.

Implementations will probably look a lot like:

	var (
		use     string   = ""
		short   string   = ""
		long    string   = ""
		aliases []string = []string{}
	)

	func New[parentpkg][pkg]Action() action.Pair {
		return scaffold.NewBasicAction(use, short, long, aliases, func(*cobra.Command, *pflag.FlagSet) (string, tea.Cmd) {

		}, nil)
	}
*/
package scaffold

import (
	"fmt"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// NewBasicAction creates a new Basic action fully featured for Cobra and Mother usage.
// The given act func will be executed when the action is triggered and its result printed to the
// screen.
//
// NOTE: The tea.Cmd returned by act will be thrown away if run in a Cobra context.
func NewBasicAction(use, short, long string, aliases []string,
	act func(*cobra.Command, *pflag.FlagSet) (string, tea.Cmd), flagFunc func() pflag.FlagSet) action.Pair {

	cmd := treeutils.GenerateAction(
		use,
		short,
		long,
		aliases,
		func(c *cobra.Command, _ []string) {
			s, _ := act(c, c.Flags())
			fmt.Fprintf(c.OutOrStdout(), "%v\n", s)
		})

	if flagFunc != nil {
		f := flagFunc()
		cmd.Flags().AddFlagSet(&f)
	}

	ba := BasicAction{cmd: cmd, fn: act}
	if flagFunc != nil {
		ba.fs = flagFunc()
		ba.fsFunc = flagFunc
	}

	return action.NewPair(cmd, &ba)
}

//#region interactive mode (model) implementation

type BasicAction struct {
	done bool

	fs     pflag.FlagSet        // the current state of the flagset; destroyed on .Reset()
	fsFunc func() pflag.FlagSet // used by .Reset() to restore the base flagset

	cmd *cobra.Command // the command associated to this basic action

	// the function performing the basic action
	fn func(*cobra.Command, *pflag.FlagSet) (string, tea.Cmd)
}

var _ action.Model = &BasicAction{}

func (ba *BasicAction) Update(msg tea.Msg) tea.Cmd {
	ba.done = true
	s, cmd := ba.fn(ba.cmd, &ba.fs)
	return tea.Sequence(tea.Println(s), cmd)
}

func (*BasicAction) View() string {
	return ""
}

func (ba *BasicAction) Done() bool {
	return ba.done
}

func (ba *BasicAction) Reset() error {
	ba.done = false
	if ba.fsFunc != nil {
		ba.fs = ba.fsFunc()
	}
	return nil
}

func (ba *BasicAction) SetArgs(_ *pflag.FlagSet, tokens []string) (_ string, _ tea.Cmd, err error) {
	// if no additional flags could be given, we have nothing more to do
	// (basic actions have no starter flags)
	if ba.fsFunc != nil {
		// we must parse manually each interactive call, as we restore fs from base each invocation
		err = ba.fs.Parse(tokens)
		if err != nil {
			return err.Error(), nil, nil
		}
	}

	return "", nil, nil
}

//#endregion interactive mode (model) implementation
