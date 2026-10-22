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

A basic action is the simplest action: it does its thing and returns a collection of "Results" to be
printed to the terminal (plus any tea.Cmds to be run by Mother (in interactive mode)).
If the action does only one thing, it is fine to return a single Result.
However, you should always return at least one.

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
		return scaffold.NewBasicAction(use, short, long, aliases, func(*cobra.Command) ([]scaffold.Result, tea.Cmd) {
			data := connection.Client.GetSomeData()
			str := formatData(data)
			return []scaffold.Result{{Output:str, Success: true}}, nil, nil
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
// Results will be printed in order. An error message will be appended if at least 1 returned !success.
type ActFunc func(fs *pflag.FlagSet) (_ []Result, addtlCmds tea.Cmd)

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
	} else if act == nil {
		panic("act func cannot be nil")
	}

	cmd := treeutils.GenerateAction(
		use,
		short,
		long,
		func(c *cobra.Command, _ []string) error {
			if options.ValidateArgs != nil {
				if inv, err := options.ValidateArgs(c.Flags()); err != nil {
					return err
				} else if inv != "" {
					return fmt.Errorf("invalid arguments: %s", inv)
				}
			}
			results, _ := act(c.Flags())
			if len(results) == 0 {
				return nil // nothing else to be done
			}
			// print the results in order, tracking failures
			var errorCount uint
			for _, r := range results {
				if !r.Success {
					errorCount += 1
					fmt.Fprintln(c.ErrOrStderr(), r.Output)
					continue
				}
				fmt.Fprintln(c.OutOrStdout(), r.Output)
			}
			return ResultsError(errorCount, uint(len(results)))
		},
		treeutils.GenerateActionOptions{}, // actions are applied below
	)
	ba := basicAction{options: options, fn: act}

	options.Apply(cmd)

	// operate on the given options, if any

	// if flags were given, add them to the command in case we are run non-interactively.
	if options.AddtlFlags != nil {
		ba.fs.AddFlagSet(options.AddtlFlags())
	}
	if options.Usage != "" {
		cmd.SetUsageFunc(func(c *cobra.Command) error {
			_, err := fmt.Fprint(c.OutOrStdout(), options.Usage)
			return err
		})
	}
	if options.Example != "" {
		cmd.Example = options.Example
	}

	return action.NewPair(cmd, &ba)
}

//#region interactive mode (model) implementation

type basicAction struct {
	// data cleared by .Reset()
	done bool          // true after a single cycle
	fs   pflag.FlagSet // the current state of the flagset

	// individualized for each implementation of scaffoldbasic
	options BasicOptions // modifiers for the list action
	fn      ActFunc      // the function performing the basic action
}

var _ action.Model = &basicAction{}

func (ba *basicAction) Update(msg tea.Msg) tea.Cmd {
	ba.done = true
	results, cmd := ba.fn(&ba.fs)
	// sequence the results, then the error message, then the cmd (if not nil)
	if len(results) == 0 {
		return cmd
	}
	// print the results in order, tracking failures
	printCmds := TeaPrintlnResults(results)
	if cmd != nil {
		return tea.Sequence(printCmds, cmd)
	}
	return printCmds
}

func (*basicAction) View() string {
	return ""
}

func (ba *basicAction) Done() bool {
	return ba.done
}

func (ba *basicAction) Reset() error {
	ba.done = false
	ba.fs = pflag.FlagSet{} // kill flag set, as there are no native flags to worry about
	// reattach extra flags
	if ba.options.AddtlFlags != nil {
		ba.fs.AddFlagSet(ba.options.AddtlFlags())
	}
	return nil
}

func (ba *basicAction) SetArgs(_ *pflag.FlagSet, tokens []string, _, _ int) (
	invalid string, onStart tea.Cmd, err error) {
	if err := ba.fs.Parse(tokens); err != nil {
		return "", nil, err
	}
	// validate
	if ba.options.ValidateArgs != nil {
		if inv, err := ba.options.ValidateArgs(&ba.fs); err != nil {
			return "", nil, err
		} else if inv != "" {
			return inv, nil, nil
		}
	}

	return "", nil, nil
}

//#endregion interactive mode (model) implementation
