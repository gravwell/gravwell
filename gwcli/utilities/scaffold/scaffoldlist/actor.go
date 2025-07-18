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
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type ListAction[dataStruct any] struct {
	// data cleared by .Reset()
	done        bool
	columns     []string
	showColumns bool           // print columns and exit
	fs          *pflag.FlagSet // current flagset, parsed or unparsed
	outFile     *os.File       // file to output results to (or nil)

	// data shielded from .Reset()
	DefaultFormat  outputFormat
	DefaultColumns []string // columns to output if unspecified
	color          bool     // inferred from the global "--no-color" flag

	// individualized for each use of scaffoldlist
	options        Options
	availDSColumns []string                     // dot-qual columns on the data struct
	dataFunc       ListDataFunction[dataStruct] // function for fetching data for table/json/csv}
}

// Constructs a ListAction suitable for interactive use.
// Assumes that Options.DefaultColumns is set; no other assumptions are made about the state of the options struct.
func newListAction[dataStruct_t any](c *cobra.Command, DSColumns []string, dFn ListDataFunction[dataStruct_t], options Options) ListAction[dataStruct_t] {
	la := ListAction[dataStruct_t]{
		done:    false,
		columns: options.DefaultColumns,
		fs:      nil, // set in SetArgs

		DefaultFormat:  tbl,
		DefaultColumns: options.DefaultColumns,
		color:          true,

		options:        options,
		availDSColumns: DSColumns,
		dataFunc:       dFn,
	}

	return la
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
		return tea.Println(strings.Join(la.availDSColumns, " "))
	}

	// fetch the list data
	s, err := listOutput(
		la.fs,
		determineFormat(la.fs, la.options.Pretty != nil),
		la.columns,
		la.dataFunc,
		la.options.Pretty,
		la.options.ColumnAliases)
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
		fmt.Fprint(la.outFile, s)
		return tea.Println("Successfully output results to " + la.outFile.Name())
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
	la.columns = la.DefaultColumns
	la.showColumns = false
	la.fs = buildFlagSet(la.options.AddtlFlags, la.options.Pretty != nil)
	if la.outFile != nil {
		la.outFile.Close()
	}
	la.outFile = nil

	return nil
}

var _ action.Model = &ListAction[any]{}

// SetArgs is called when the action is invoked by the user and Mother *enters* handoff mode.
// Mother parses flags and provides us a handle to check against.
func (la *ListAction[T]) SetArgs(inherited *pflag.FlagSet, tokens []string) (
	invalid string, onStart tea.Cmd, err error,
) {
	// attach flags
	la.fs = buildFlagSet(la.options.AddtlFlags, la.options.Pretty != nil)

	err = la.fs.Parse(tokens)
	if err != nil {
		return err.Error(), nil, nil
	}

	// run custom validation
	if la.options.ValidateArgs != nil {
		if invalid, err := la.options.ValidateArgs(la.fs); err != nil {
			return "", nil, err
		} else if invalid != "" {
			return invalid, nil, nil
		}
	}

	// default to... well... the default columns
	la.columns = la.DefaultColumns

	// parse column handling
	// only need to parse columns if user did not pass in --show-columns
	if la.showColumns, err = la.fs.GetBool("show-columns"); err != nil {
		return "", nil, err
	} else if !la.showColumns {
		// fetch columns if it exists
		la.columns, err = getColumns(la.fs, la.DefaultColumns, la.availDSColumns)
		if err != nil {
			return err.Error(), nil, nil
		}
	}
	if all, err := la.fs.GetBool(ft.Name.AllColumns); err != nil {
		return "", nil, err
	} else if all {
		la.columns = la.availDSColumns
	}

	if f, err := initOutFile(la.fs); err != nil {
		return "", nil, err
	} else {
		la.outFile = f
	}

	return "", nil, nil
}
