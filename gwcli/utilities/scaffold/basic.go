/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package scaffold contains packages for generating new actions from skeletons.
See scaffoldlist, scaffolddelete, etc for more information.
The bare scaffold package comes with a skeleton for basic actions.

A basic action is the simplest action: it does its thing and returns a string to be printed to the terminal (plus any tea.Cmds to be run by Mother).
Give it the function you want performed when the action is invoked and have it return whatever string value you want printed to the screen, if at all.
Prefer printing via returning a string, rather than returning a tea.Printf cmd.

If this action is for retrieving data, consider making it a scaffoldlist instead.
Scaffoldlist comes with csv/json/table formatting and file redirection out of the box.

Basic actions have no default flags and will not handle flags unless a flagFunc is given.

Implementations will probably look a lot like:

	var (
		use     string   = ""
		short   string   = ""
		long    string   = ""
		aliases []string = []string{}
	)

	func FooAction() action.Pair {
		return scaffold.NewBasicAction(use, short, long, aliases, func(*cobra.Command) (string, tea.Cmd) {
			data := connection.Client.GetSomeData()
			str := formatData(data)
			return str, nil
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

// ActFunc is the driver code for a basic action.
// It is called whenever this action is invoked and runs exactly once per invocation.
//
// ! Do not use the flags inside of cmd. They are unused and their state is undefined.
// Use fs instead.
type ActFunc func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd)

// NewBasicAction creates a new Basic action fully featured for Cobra and Mother usage.
// The given act func will be executed when the action is triggered and its result printed to the
// screen.
//
// NOTE: The tea.Cmd returned by act will be thrown away if run in a Cobra context.
func NewBasicAction(use, short, long string,
	act ActFunc,
	options BasicOptions) action.Pair {
	// validate options
	if use == "" {
		panic("use cannot be empty")
	} else if short == "" {
		panic("short cannot be empty")
	} else if act == nil {
		panic("act func cannot be nil")
	} else if options.AddtlFlagFunc == nil && options.ValidateArgs != nil {
		panic("are you certain you meant to pass a Validate function but no additional flags?")
	}

	cmd := treeutils.GenerateAction(
		use,
		short,
		long,
		options.Aliases,
		func(c *cobra.Command, _ []string) {
			if options.ValidateArgs != nil {
				if inv, err := options.ValidateArgs(c.Flags()); err != nil {
					fmt.Fprintf(c.ErrOrStderr(), "%v", err)
					return
				} else if inv != "" {
					fmt.Fprintf(c.ErrOrStderr(), "invalid arguments: %v", inv)
					return
				}
			}
			s, _ := act(c, c.Flags())
			fmt.Fprintf(c.OutOrStdout(), "%v\n", s)
		})
	ba := basicAction{cmd: cmd, options: options, fn: act}

	// operate on the given options, if any

	// if flags were given, add them to the command in case we are run non-interactively.
	if options.AddtlFlagFunc != nil {
		f := options.AddtlFlagFunc()
		cmd.Flags().AddFlagSet(&f)

		// added to the interactive model in .SetArgs()
	}

	if options.CmdMods != nil {
		options.CmdMods(cmd)
	}

	return action.NewPair(cmd, &ba)
}

//#region interactive mode (model) implementation

type basicAction struct {
	// data cleared by .Reset()
	done bool          // true after a single cycle
	fs   pflag.FlagSet // the current state of the flagset

	// individualized for each implementation of scaffoldbasic
	cmd     *cobra.Command // the command associated to this basic action
	options BasicOptions   // modifiers for the list action
	fn      ActFunc        // the function performing the basic action
}

var _ action.Model = &basicAction{}

func (ba *basicAction) Update(msg tea.Msg) tea.Cmd {
	ba.done = true
	s, cmd := ba.fn(ba.cmd, &ba.fs)
	if cmd != nil { // no point in sequencing with nil
		return tea.Sequence(tea.Println(s), cmd)
	}
	return tea.Println(s)
}

func (*basicAction) View() string {
	return ""
}

func (ba *basicAction) Done() bool {
	return ba.done
}

func (ba *basicAction) Reset() error {
	ba.done = false
	ba.fs = pflag.FlagSet{} // reset to additionals in .SetArgs()
	return nil
}

func (ba *basicAction) SetArgs(fs *pflag.FlagSet, tokens []string, width, height int) (
	invalid string, onStart tea.Cmd, err error) {
	// validate arguments using the set method
	if ba.cmd.Args != nil {
		if err := ba.cmd.Args(ba.cmd, tokens); err != nil {
			return err.Error(), nil, nil
		}
	}

	// set up flags
	if ba.options.AddtlFlagFunc != nil {
		ba.fs = ba.options.AddtlFlagFunc()
		if err := ba.fs.Parse(tokens); err != nil {
			return "", nil, err
		}
		if ba.options.ValidateArgs != nil {
			if inv, err := ba.options.ValidateArgs(&ba.fs); err != nil {
				return "", nil, err
			} else if inv != "" {
				return inv, nil, nil
			}
		}
	}

	return "", nil, nil
}

//#endregion interactive mode (model) implementation
