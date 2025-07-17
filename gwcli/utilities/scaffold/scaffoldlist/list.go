/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package scaffoldlist provides a template for building list actions.

A list action is any action that fetches and prints data, typically in a tabular manner.
This provides a consistent interface and the versatility of multiple formats for actions that list arbitrary data.

List actions have the --output, --append, --json, --table, --CSV, --columns, and --show-columns default flags.
If a pretty printer function is defined, --pretty is also available.

Implementations will probably look a lot like:

	type someData struct {
		Name             string // IP address or "webserver", typically
		A				 int
		B                []string
	}

	func listAction() action.Pair {
		const (
			short string = "list all data about X"
			long  string = "List data about X but this has more words."
		)

		return scaffoldlist.NewListAction(short, long, someData{},
			func(fs *pflag.FlagSet) ([]someData, error) {
				sd := []someData{}

				if stuff, err := fetchData(); err != nil {
					return nil, err
				} else {
					sd = stuff.transmute()
				}

				return d, nil
			},
			scaffoldlist.Options{})
	}
*/
package scaffoldlist

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/gravwell/gravwell/v4/utils/weave"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//#region enumeration

type outputFormat uint

const (
	json outputFormat = iota
	csv
	tbl
	pretty
)

func (f outputFormat) String() string {
	switch f {
	case json:
		return "JSON"
	case csv:
		return "CSV"
	case tbl:
		return "table"
	case pretty:
		return "pretty"
	}
	return fmt.Sprintf("unknown format (%d)", f)
}

//#endregion enumeration

const (
	outFilePerm         os.FileMode = 0644
	exportedColumnsOnly bool        = true // only allow users to query for exported fields as columns?
)

// ListDataFunction is a function that retrieves an array of structs of type dataStruct
type ListDataFunction[dataStruct_t any] func(*pflag.FlagSet) ([]dataStruct_t, error)

// AddtlFlagFunction (if not nil) bolts additional flags onto this action for later during the data func.
type AddtlFlagFunction func() pflag.FlagSet

// A PrettyPrinterFunc defines a free-form function for outputting a pretty string for human consumption.
type PrettyPrinterFunc func(*pflag.FlagSet) (string, error)

// NewListAction creates and returns a cobra.Command suitable for use as a list action,
// complete with common flags and a generic run function operating off the given dataFunction.
//
// If no output module is given, defaults to --table (unless a PrettyFunc is given, in which case it defaults to --pretty).
//
// ! `dataFn` should be a static wrapper function for a method that returns an array of structures
// containing the data to be listed.
//
// ! `dataStruct` must be the type of struct returned in array by dataFunc.
// Its values do not matter.
//
// ! If use is not specified, it will default to "list".
//
// Any data massaging required to get the data into an array of structures should be performed in
// the data function. Non-list-standard flags (ex: those passed to addtlFlags, if not nil) should
// also be handled in the data function.
// See tree/kits/list's ListKits() as an example.
//
// Go's Generics are a godsend.
func NewListAction[dataStruct_t any](short, long string,
	dataStruct dataStruct_t, dataFn ListDataFunction[dataStruct_t], options Options) action.Pair {
	// check for developer errors
	if reflect.TypeOf(dataStruct).Kind() != reflect.Struct {
		panic("dataStruct must be a struct")
	} else if dataFn == nil {
		panic("data function cannot be nil")
	} else if short == "" {
		panic("short description cannot be empty")
	} else if long == "" {
		panic("long description cannot be empty")
	}

	// generate the command
	var use = "list"
	if options.Use != "" {
		// validate use and override default
		for i := 0; i < len(options.Use); i++ { // check each rune for non-alphanumerics
			if options.Use[i] >= 48 && options.Use[i] <= 57 { // 0-9 in ASCII
				continue
			} else if options.Use[i] >= 65 && options.Use[i] <= 122 { //A-z in ASCII
				continue
			}
			panic("non-alphanumeric character found: " + string(options.Use[i]))
		}

		use = options.Use
	}

	// cache the struct fields so we do not need to reflect through them again later.
	availDSColumns, err := weave.StructFields(dataStruct, exportedColumnsOnly)
	if err != nil {
		panic(fmt.Sprintf("failed to cache available columns: %v", err))
	}

	// if default columns was not set in options, set it to all columns
	if options.DefaultColumns == nil {
		options.DefaultColumns = availDSColumns
	} else if err := validateColumns(options.DefaultColumns, availDSColumns); err != nil { // otherwise, validate the given defaults
		panic(err)
	}

	cmd := treeutils.GenerateAction(use, short, long, options.Aliases, generateRun(dataFn, options, availDSColumns))

	cmd.Flags().AddFlagSet(buildFlagSet(options.AddtlFlags, options.Pretty != nil))
	cmd.Flags().SortFlags = false // does not seem to be respected
	cmd.MarkFlagsMutuallyExclusive(ft.Name.CSV, ft.Name.JSON, ft.Name.Table)

	// attach example
	formats := []string{"--csv", "--json", "--table"}
	if options.Pretty != nil {
		formats = append(formats, "--pretty")
	}
	cmd.Example = fmt.Sprintf("%v %v %v", use, ft.MutuallyExclusive(formats), ft.Optional("--columns=col1,col2,..."))

	// apply command modifiers
	if options.CmdMods != nil {
		options.CmdMods(cmd)
	}

	// generate the list action.
	la := newListAction(cmd, availDSColumns, dataFn, options)

	return action.NewPair(cmd, &la)
}

// generateRun builds and returns a function to be run when this action is invoked via Cobra.
func generateRun[dataStruct_t any](
	dataFn ListDataFunction[dataStruct_t],
	options Options,
	availDataStructColumns []string) func(c *cobra.Command, _ []string) {
	return func(c *cobra.Command, _ []string) {
		// run custom validation
		if options.ValidateArgs != nil {
			if invalid, err := options.ValidateArgs(c.Flags()); err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error())
				return
			} else if invalid != "" {
				fmt.Fprintln(c.OutOrStdout(), invalid)
				return
			}
		}

		// check for --show-columns
		if sc, err := c.Flags().GetBool("show-columns"); err != nil {
			fmt.Fprintln(c.ErrOrStderr(), uniques.ErrGetFlag("list", err))
			return
		} else if sc {
			fmt.Fprintln(c.OutOrStdout(), strings.Join(availDataStructColumns, " "))
			return
		}

		var (
			script  bool // TODO should script imply no-color at a global level?
			outFile *os.File
			format  outputFormat
			columns []string
		)
		{ // gather flags and set up variables required for listOutput
			var err error
			script, err = c.Flags().GetBool(ft.Name.Script)
			if err != nil {
				fmt.Fprintln(c.ErrOrStderr(), uniques.ErrGetFlag(c.Use, err))
				return
			}
			outFile, err = initOutFile(c.Flags())
			if err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error())
				return
			} else if outFile != nil {
				defer outFile.Close()
				// ensure color is disabled.
				stylesheet.Cur = stylesheet.NoColor()
			}

			columns, err = getColumns(c.Flags(), options.DefaultColumns, availDataStructColumns)
			if err != nil {
				fmt.Fprintln(c.ErrOrStderr(), err)
				return
			}
			format = determineFormat(c.Flags(), options.Pretty != nil)
		}

		s, err := listOutput(c.Flags(), format, columns, dataFn, options.Pretty)
		if err != nil {
			clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error())
			return
		}

		if s == "" {
			if outFile == nil && !script {
				fmt.Fprintln(c.OutOrStdout(), "no data found")
			}
			return
		}

		if outFile != nil {
			fmt.Fprintln(outFile, s)
		} else {
			fmt.Fprintln(c.OutOrStdout(), s)
		}
	}
}
