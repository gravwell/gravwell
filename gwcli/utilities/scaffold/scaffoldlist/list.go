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
			map[string]string{"dot.qualified.field": "alias"}
			scaffoldlist.Options{})
	}
*/
package scaffoldlist

// NOTE(rlandau): if you are modifying scaffoldlist, keep in mind that aliases should be handled at all ingress/egress points.
// For the sake of clarity, we try to work in DQ names only internally.

import (
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/gravwell/gravwell/v4/ingest/log"

	"github.com/gravwell/gravwell/v4/utils/weave"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//#region enumeration

type outputFormat uint

const (
	formatJSON outputFormat = iota
	formatCSV
	formatTable
	formatPretty
)

func (f outputFormat) String() string {
	switch f {
	case formatJSON:
		return "JSON"
	case formatCSV:
		return "CSV"
	case formatTable:
		return "table"
	case formatPretty:
		return "pretty"
	}
	return fmt.Sprintf("unknown format (%d)", f)
}

//#endregion enumeration

const (
	outFilePerm         os.FileMode = 0644
	exportedColumnsOnly bool        = true // only allow users to query for exported fields as columns?
	ShowColumnSep       string      = "; " // separator between column names when printing list of available columns
)

// ListDataFunc is a function that retrieves an array of structs of type dataStruct
type ListDataFunc[dataStruct_t any] func(*pflag.FlagSet) ([]dataStruct_t, error)

// AddtlFlagFunc (if not nil) bolts additional flags onto this action for later during the data func.
type AddtlFlagFunc func() pflag.FlagSet

// A PrettyPrinterFunc defines a free-form function for outputting a pretty string for human consumption.
type PrettyPrinterFunc func(DQColumns []string, DQToAlias map[string]string) (string, error)

// NewListAction creates and returns a cobra.Command suitable for use as a list action,
// complete with common flags and a generic run function operating off the given ListDataFunc.
//
// Args:
//
//   - short and long are short and long descriptions of the action.
//
//   - dataStruct must be the type of struct returned in array by dataFunc. Its values do not matter.
//
//   - dataFunc must be a function that returns an array of dataStruct_t containing the data to be listed.
//     Any data massaging required to get the data into an array of structures should be performed in the data function.
//
//   - columnAliases, if not nil, is a map from dot-qualified field name -> alias the user will see instead.
//     It will be destructively edited, so pass in a clone if you care about the data.
//
//   - options defines other modifiers and are detailed internally.
func NewListAction[dataStruct_t any](short, long string,
	dataStruct dataStruct_t, dataFunc ListDataFunc[dataStruct_t],
	columnAliases map[string]string,
	options Options) action.Pair {
	// check for developer errors
	if reflect.TypeOf(dataStruct).Kind() != reflect.Struct {
		panic("dataStruct must be a struct")
	} else if dataFunc == nil {
		panic("data function cannot be nil")
	} else if short == "" {
		panic("short description cannot be empty")
	} else if long == "" {
		panic("long description cannot be empty")
	}

	// cache the struct fields so we can save some reflection cycles later
	DQ, err := weave.StructFields(dataStruct, exportedColumnsOnly)
	if err != nil {
		clilog.Writer.Error("failed to cache available columns",
			log.KVErr(err),
			rfc5424.SDParam{Name: "dataStruct", Value: fmt.Sprintf("%+v", dataStruct)},
		)
	}

	// map DQs to their aliases,
	// install aliases for CommonFields,
	// install aliases for AutomationCommonFields
	DQToAlias := make(map[string]string, len(DQ))
	AliasToDQ := make(map[string]string)
	for _, dq := range DQ {
		alias, hasAlias := columnAliases[dq]
		if !hasAlias {
			var found bool
			// cloak CommonFields prefix
			if alias, found = strings.CutPrefix(dq, "CommonFields."); !found {
				alias, _ = strings.CutPrefix(dq, "AutomationCommonFields.")
			}
		}

		DQToAlias[dq] = alias
		delete(columnAliases, dq)
		if alias != "" { // create the reverse mapping
			AliasToDQ[alias] = dq
		}
	}

	// any aliases that remain are for unknown DQs
	for dq, alias := range columnAliases {
		clilog.Writer.Warn("unknown DQ column", log.KV("DQ", dq), log.KV("alias", alias), scaffold.IdentifyCaller())
	}

	var defaultColumnsDQ = sortColumns(findDefaultColumns(options, DQToAlias))

	// generate a non-interactive action
	run := generateRun(dataFunc, options, DQToAlias, AliasToDQ)

	// generate usage and example
	actionOptions := treeutils.GenerateActionOptions{}
	{
		formats := []string{"--" + ft.CSV.Name(), "--" + ft.JSON.Name(), "--" + ft.Table.Name()}
		if options.Pretty != nil {
			formats = append(formats, "--pretty")
		}
		actionOptions.Usage = fmt.Sprintf("%v %v", ft.MutuallyExclusive(formats), ft.Optional("--"+ft.SelectColumns.Name()+"=col1,col2,..."))
		actionOptions.Example = "--" + ft.JSON.Name() + " --" + ft.AllColumns.Name()
	}

	cmd := treeutils.GenerateAction("list", short, long, nil, run, actionOptions)
	options.Apply(cmd)

	cmd.Flags().AddFlagSet(buildFlagSet(options.Pretty != nil, aliasColumns(defaultColumnsDQ, DQToAlias)))
	cmd.Flags().SortFlags = false // does not seem to be respected
	cmd.MarkFlagsMutuallyExclusive(ft.CSV.Name(), ft.JSON.Name(), ft.Table.Name())

	// generate the interactive action
	la := newListAction(defaultColumnsDQ, DQToAlias, AliasToDQ, dataFunc, options)

	return action.NewPair(cmd, &la)
}

// findDefaultColumns returns the set of columns to use as defaults,
// based on the state of options.DefaultColumns and options.ExcludeColumnsFromDefault.
func findDefaultColumns(opts Options, DQToAlias map[string]string) []string {
	// set default columns from DefaultColumns or ExcludeColumnsFromDefault
	if opts.DefaultColumns != nil && opts.DefaultColumnsFromExcludeRegex != nil { // both were given
		panic("DefaultColumns and ExcludeColumnsFromDefault are mutually exclusive")
	} else if opts.DefaultColumnsFromExcludeRegex != nil { // use the set of all columns, minus those excluded
		var defaultColumns = make([]string, 0)
		// if a column matches NONE of the exclude regexes, include it as default
		for dq := range DQToAlias {
			var match bool
			for _, rgx := range opts.DefaultColumnsFromExcludeRegex {
				if rgx.MatchString(dq) {
					match = true
					break
				}
			}
			if !match {
				defaultColumns = append(defaultColumns, dq)
			}
		}
		return slices.Clip(defaultColumns)
	} else if opts.DefaultColumns != nil { // validate and use the set of default columns
		var defaultColumns []string = make([]string, 0, len(opts.DefaultColumns))
		for _, col := range opts.DefaultColumns {
			if _, found := DQToAlias[col]; !found { // ensure the column exists
				clilog.Writer.Warn("unknown default column", log.KV("column", col), scaffold.IdentifyCaller())
				continue
			}
			defaultColumns = append(defaultColumns, col)
		}
		return slices.Clip(defaultColumns)
	}
	// nothing was given, use the set of all columns
	return slices.Collect(maps.Keys(DQToAlias))
}

// generateRun builds and returns a function to be run when this action is invoked via Cobra.
func generateRun[dataStruct_t any](
	dataFn ListDataFunc[dataStruct_t], opts Options,
	DQToAlias, AliasToDQ map[string]string) func(c *cobra.Command, _ []string) {
	return func(c *cobra.Command, _ []string) {
		// run custom validation
		if opts.ValidateArgs != nil {
			if invalid, err := opts.ValidateArgs(c.Flags()); err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error())
				return
			} else if invalid != "" {
				fmt.Fprintln(c.OutOrStdout(), invalid)
				return
			}
		}

		// check for --show-columns
		if sc, err := c.Flags().GetBool(ft.ShowColumns.Name()); err != nil {
			fmt.Fprintln(c.ErrOrStderr(), uniques.ErrGetFlag("list", err))
			return
		} else if sc {
			fmt.Fprintln(c.OutOrStdout(), ShowColumns(DQToAlias))
			return
		}

		var (
			noInteractive bool
			outFile       *os.File
			format        outputFormat
			columns       []string
		)
		// gather flags and set up variables required for listOutput
		var err error
		noInteractive, err = c.Flags().GetBool(ft.NoInteractive.Name())
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
			stylesheet.Cur = stylesheet.Plain()
		}
		columns, err = getColumns(c.Flags(), DQToAlias, AliasToDQ)
		if err != nil {
			fmt.Fprintln(c.ErrOrStderr(), err)
			return
		}
		format = determineFormat(c.Flags(), opts.Pretty != nil)

		// execute the actual list and format call
		s, err := listOutput(c.Flags(), format, columns, dataFn, opts.Pretty, DQToAlias)
		if err != nil {
			clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error())
			return
		}

		if s == "" {
			if outFile == nil && !noInteractive {
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
