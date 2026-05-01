/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldlist

// This file defines interactive usage of the scaffolded action.
// The defined action satisfies the action.Model interface.

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/pflag"
)

type ListAction[dataStruct any] struct {
	// data cleared by .Reset()
	done        bool
	columns     []string       // the set of columns request by the user on *this* invocation
	showColumns bool           // print columns and exit
	fs          *pflag.FlagSet // current flagset, parsed or unparsed
	outFile     *os.File       // file to output results to (or nil)

	// individualized for each use of scaffoldlist
	defaultColumnsDQ []string                 // columns to output if --all and --columns=<> are unspecified
	dqToAlias        map[string]string        // DQ column names -> alias (alias will be "" if a column does not have an alias)
	aliasToDQ        map[string]string        // inverse of dqToAlias
	options          Options                  // modifiers for the list action
	dataFunc         ListDataFunc[dataStruct] // function for fetching data for table/json/csv}
}

// Constructs a ListAction suitable for interactive use.
// Assumes that Options.DefaultColumns is set; no other assumptions are made about the state of the options struct.
func newListAction[dataStruct_t any](
	defaultColumnsDQ []string,
	DQToAlias, AliasToDQ map[string]string,
	dataFunc ListDataFunc[dataStruct_t],
	options Options) ListAction[dataStruct_t] {
	la := ListAction[dataStruct_t]{
		done: false,
		fs:   nil, // set in SetArgs

		dqToAlias: DQToAlias,
		aliasToDQ: AliasToDQ,

		options: options,

		dataFunc: dataFunc,
	}

	return la
}

// SetArgs is called when the action is invoked by the user and Mother *enters* handoff mode.
// Mother parses flags and provides us a handle to check against.
func (la *ListAction[T]) SetArgs(fs *pflag.FlagSet, tokens []string, width, height int) (
	invalid string, onStart tea.Cmd, err error) {
	// refresh flags
	la.fs = buildFlagSet(la.options.Pretty != nil, aliasColumns(la.defaultColumnsDQ, la.dqToAlias))
	if la.options.AddtlFlags != nil {
		la.fs.AddFlagSet(la.options.AddtlFlags())
	}
	err = la.fs.Parse(tokens)
	if err != nil {
		return err.Error(), nil, nil
	}

	// check for --show-columns
	if la.showColumns, err = la.fs.GetBool(ft.ShowColumns.Name()); err != nil {
		return "", nil, err
	} else if la.showColumns { // all done
		return "", nil, nil
	}

	// run custom validation
	if la.options.ValidateArgs != nil {
		if invalid, err := la.options.ValidateArgs(la.fs); err != nil {
			return "", nil, err
		} else if invalid != "" {
			return invalid, nil, nil
		}
	}

	if la.columns, err = getColumns(la.fs, la.dqToAlias, la.aliasToDQ); err != nil {
		// treat these errors as invalids
		return err.Error(), nil, nil
	}

	if f, err := initOutFile(la.fs); err != nil {
		return "", nil, err
	} else {
		la.outFile = f
	}

	return "", nil, nil
}

// Update takes in a msg (some event that occurred, like a window redraw or a key press) and acts on it.
// List only ever needs to update once; it figures out what data is to be displayed, fetches it, and spits it out above the prompt.
func (la *ListAction[T]) Update(msg tea.Msg) tea.Cmd {
	if la.done {
		return nil
	}

	// list only ever acts once; immediately mark it as done
	la.done = true

	// check for --show-columns
	if la.showColumns {
		return tea.Println(ShowColumns(la.dqToAlias))
	}

	// fetch the list data
	s, err := listOutput(
		la.fs,
		determineFormat(la.fs, la.options.Pretty != nil),
		la.columns,
		la.dataFunc,
		la.options.Pretty,
		la.dqToAlias)
	if err != nil {
		// log and print the error
		clilog.Writer.Error(err.Error())
		return tea.Println(uniques.ErrGeneric.Error())
	}

	// if we received no data, note that (unless we are printing to a file, then do nothing)
	if s == "" {
		if la.outFile != nil {
			return textinput.Blink
		}
		return tea.Println("no data found")
	}

	// output the results to a file, if given
	if la.outFile != nil {
		n, err := fmt.Fprint(la.outFile, s)
		if err != nil {
			str := fmt.Sprint("failed to write results to file: ", err)
			clilog.Writer.Warn(str)
			return tea.Println(str)
		}
		return tea.Println(phrases.SuccessfullyWroteToFile(n, la.outFile.Name()))
	}

	return tea.Println(s)
}

// View is called after each update cycle to redraw dynamic content,
// but is not used by list actions as they output all of their data rather than dynamically viewing it.
func (la *ListAction[T]) View() string {
	return ""
}

// Done is called once per cycle to test if Mother should reassert control
func (la *ListAction[T]) Done() bool {
	return la.done
}

// Reset is called when the action is unseated by Mother on exiting handoff mode
func (la *ListAction[T]) Reset() error {
	la.done = false
	la.columns = la.defaultColumnsDQ
	la.showColumns = false
	if la.outFile != nil {
		la.outFile.Close()
	}
	la.outFile = nil

	// flags are refreshed in SetArgs to guarantee they are built even on first run

	return nil
}

var _ action.Model = &ListAction[any]{}
