/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package scaffoldlist provides a template for building list actions.

A list action runs a given function that outputs an arbitrary data structure.
The results are sent to weave and packaged in a way that can be listed for the user.

This provides a consistent interface for actions that list arbitrary data.

List actions have the --output, --append, --json, --table, --CSV, and --show-columns default flags.

Example implementation:

	const (
		use   string = "" // defaults to 'list'
		short string = ""
		long  string = ""
	)

	var (
		defaultColumns []string = []string{"ID", "UID", "Name", "Description"}
	)

	func New[parentpkg]ListAction() action.Pair {
		return scaffoldlist.NewListAction(short, long, defaultColumns,
			types.[X]{}, list, flags)
	}

	func flags() pflag.FlagSet {
		addtlFlags := pflag.FlagSet{}
		addtlFlags.Bool(ft.Name.ListAll, false, ft.Usage.ListAll("[plural]")+
			" Supercedes --group. Ignored if you are not an admin.")
		addtlFlags.Int32("group", 0, "Fetches all [Y] shared with the given group id.")
		return addtlFlags
	}

	func list(c *grav.Client, fs *pflag.FlagSet) ([]types.[X], error) {
		if all, err := fs.GetBool(ft.Name.ListAll); err != nil {
			clilog.LogFlagFailedGet(ft.Name.ListAll, err)
		} else if all {
			return c.GetAll[Y]()
		}
		if gid, err := fs.GetInt32("group"); err != nil {
			clilog.LogFlagFailedGet("group", err)
		} else if gid != 0 {
			return c.GetGroup[Y](gid)
		}

		return c.GetUser[Y]()
	}
*/
package scaffoldlist

import (
	"errors"
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

const outFilePerm os.FileMode = 0644

// ListDataFunction is a function that retrieves an array of structs of type dataStruct
type ListDataFunction[dataStruct_t any] func(*pflag.FlagSet) ([]dataStruct_t, error)

// AddtlFlagFunction (if not nil) bolts additional flags onto this action for later during the data func.
type AddtlFlagFunction func() pflag.FlagSet

// A PrettyPrinterFunc defines a free-form function for outputting a pretty string for human consumption.
type PrettyPrinterFunc func(*cobra.Command) (string, error)

// NewListAction creates and returns a cobra.Command suitable for use as a list action,
// complete with common flags and a generic run function operating off the given dataFunction.
//
// Flags: {--csv|--json|--table} [--columns ...]
//
// If no output module is given, defaults to --table.
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
func NewListAction[dataStruct_t any](short, long string, defaultColumns []string,
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
		use = options.Use
	}
	cmd := treeutils.GenerateAction(use, short, long, []string{}, generateRun(dataStruct, dataFn, defaultColumns, options))

	cmd.Flags().AddFlagSet(buildFlagSet(options.AddtlFlags, options.Pretty != nil))
	cmd.Flags().SortFlags = false // does not seem to be respected
	cmd.MarkFlagsMutuallyExclusive(ft.Name.CSV, ft.Name.JSON, ft.Name.Table)

	// attach example
	if options.Example != "" {
		cmd.Example = options.Example
	} else {
		formats := []string{"--csv", "--json", "--table"}
		if options.Pretty != nil {
			formats = append(formats, "--pretty")
		}
		cmd.Example = fmt.Sprintf("%v %v %v", use, ft.MutuallyExclusive(formats), ft.Optional("--columns=[...]"))
	}

	// generate the list action.
	la := newListAction(defaultColumns, dataStruct, dataFn, options)

	return action.NewPair(cmd, &la)
}

func generateRun[dataStruct_t any](dataStruct dataStruct_t, dataFn ListDataFunction[dataStruct_t], defaultColumns []string, options Options) func(c *cobra.Command, _ []string) {
	return func(c *cobra.Command, _ []string) {
		// check for --show-columns
		if sc, err := c.Flags().GetBool("show-columns"); err != nil {
			fmt.Fprintln(c.ErrOrStderr(), uniques.ErrGetFlag("list", err))
			return
		} else if sc {
			cols, err := weave.StructFields(dataStruct, true)
			if err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), fmt.Sprintf("failed to grok struct fields from %#v", dataStruct))
				return
			}
			fmt.Fprintln(c.OutOrStdout(), strings.Join(cols, " "))
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
				fmt.Fprintln(c.ErrOrStderr(), uniques.ErrGetFlag("list", err))
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

			if columns, err = c.Flags().GetStringSlice("columns"); err != nil {
				// non-fatal; falls back to default columns
				uniques.ErrGetFlag("list", err)
			}
			if len(columns) == 0 {
				columns = defaultColumns
			}
			format = determineFormat(c.Flags(), options.Pretty != nil)
		}

		s, err := listOutput(c, format, columns, dataFn, options.Pretty)
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

// buildFlagSet constructs and returns a flagset composed of the default list flags, additional flags defined for this action, and --pretty if a prettyFunc was defined.
func buildFlagSet(afs AddtlFlagFunction, prettyDefined bool) *pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.Bool(ft.Name.CSV, false, ft.Usage.CSV)
	fs.Bool(ft.Name.JSON, false, ft.Usage.JSON)
	fs.Bool(ft.Name.Table, true, ft.Usage.Table) // default
	fs.StringSlice("columns", []string{},
		"comma-seperated list of columns to include in the results."+
			"Use --show-columns to see the full list of columns.")
	fs.Bool("show-columns", false, "display the list of fully qualified column names and die.")
	fs.StringP(ft.Name.Output, "o", "", ft.Usage.Output)
	fs.Bool(ft.Name.Append, false, ft.Usage.Append)
	// if prettyFunc was defined, bolt on pretty
	if prettyDefined {
		fs.Bool("pretty", false, "display results as prettified text.\n"+
			"Takes precedence over other format flags.")
	}
	// if additional flags are warranted, add them
	if afs != nil {
		a := afs()
		fs.AddFlagSet(&a)
	}

	return &fs

}

// Opens a file, per the given --output and --append flags in the flagset, and returns its handle.
// Returns nil if the flags do not call for a file.
func initOutFile(fs *pflag.FlagSet) (*os.File, error) {
	if !fs.Parsed() {
		return nil, nil
	}
	outPath, err := fs.GetString(ft.Name.Output)
	if err != nil {
		return nil, err
	} else if strings.TrimSpace(outPath) == "" {
		return nil, nil
	}
	var flags = os.O_CREATE | os.O_WRONLY
	if append, err := fs.GetBool(ft.Name.Append); err != nil {
		return nil, err
	} else if append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	return os.OpenFile(outPath, flags, outFilePerm)
}

// Given a **parsed** flagset, determines and returns output format.
// Logs errors, allowing execution to continue towards default.
// If an error was returned, the outputFormat is undefined.
func determineFormat(fs *pflag.FlagSet, prettyDefined bool) outputFormat {
	if !fs.Parsed() {
		clilog.Writer.Warnf("flags must be parsed prior to determining format")
		return tbl
	}
	var format = tbl   // default to tbl
	if prettyDefined { // if defined, default to pretty and check for explicit flag
		format = pretty
		if format_pretty, err := fs.GetBool("pretty"); err != nil {
			clilog.Writer.Criticalf("failed to fetch --pretty despite believing prettyFunc to be defined: %v", err)
		} else if format_pretty {
			// manually declared, use it
			return pretty
		}
	}
	// check for CSV
	if format_csv, err := fs.GetBool(ft.Name.CSV); err != nil {
		uniques.ErrGetFlag("list", err)
		// non-fatal
	} else if format_csv {
		return csv
	}

	// check for JSON
	if format_json, err := fs.GetBool(ft.Name.JSON); err != nil {
		uniques.ErrGetFlag("list", err)
	} else if format_json {
		format = json
	}

	// if we made it this far, return the default
	return format
}

// Driver function to call the provided data func and format its output via weave.
//
// ! pretty format should not be given here
func listOutput[retStruct any](
	c *cobra.Command,
	format outputFormat,
	columns []string,
	dataFn ListDataFunction[retStruct],
	prettyFunc PrettyPrinterFunc,
) (string, error) {
	// hand off control to pretty
	if format == pretty {
		if prettyFunc == nil {
			return "", errors.New("format is pretty, but prettyFunc is nil")
		}
		return prettyFunc(c)
	}

	// massage the data for weave
	data, err := dataFn(c.Flags())
	if err != nil {
		return "", err
	} else if len(data) < 1 {
		return "", nil
	}

	// hand off control
	clilog.Writer.Debugf("List: format %s | row count: %d", format, len(data))
	toRet, err := "", nil
	switch format {
	case csv:
		toRet = weave.ToCSV(data, columns)
	case json:
		toRet, err = weave.ToJSON(data, columns)
	case tbl:
		// TODO check if this is still necessary
		//if color {
		toRet = weave.ToTable(data, columns, stylesheet.Table)
		/*} else {
			toRet = weave.ToTable(data, columns, func() *table.Table {
				tbl := table.New()
				tbl.Border(lipgloss.ASCIIBorder())
				return tbl
			}) // omit table styling
		}*/
	default:
		toRet = ""
		err = fmt.Errorf("unknown output format (%d)", format)
	}
	return toRet, err
}
