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
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	grav "github.com/gravwell/gravwell/v4/client"
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
type ListDataFunction[Any any] func(*grav.Client, *pflag.FlagSet) ([]Any, error)

// AddtlFlagFunction (if not nil) bolts additional flags onto this action for later during the data func.
type AddtlFlagFunction func() pflag.FlagSet

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
func NewListAction[retStruct any](short, long string, defaultColumns []string,
	dataStruct retStruct, dataFn ListDataFunction[retStruct], options Options) action.Pair {
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
	}

	// generate the list action.
	la := newListAction(defaultColumns, dataStruct, dataFn, options)

	return action.NewPair(cmd, &la)
}

func generateRun[retStruct any](dataStruct retStruct, dataFn ListDataFunction[retStruct], defaultColumns []string, options Options) func(c *cobra.Command, _ []string) {
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
		return csv, nil
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
	prettyFunc func(*cobra.Command) (string, error),
) (string, error) {
	// hand off control to pretty
	if format == pretty {
		if prettyFunc == nil {
			return "", errors.New("format is pretty, but prettyFunc is nil")
		}
		return prettyFunc(c)
	}

	// massage the data for weave
	data, err := dataFn(connection.Client, c.Flags())
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

//#region interactive mode (model) implementation

type ListAction[Any any] struct {
	// data cleared by .Reset()
	done        bool
	columns     []string
	showColumns bool          // print columns and exit
	fs          pflag.FlagSet // current flagset, parsed or unparsed
	outFile     *os.File      // file to output results to (or nil)

	// data shielded from .Reset()
	DefaultFormat  outputFormat
	DefaultColumns []string // columns to output if unspecified
	//afsFunc        AddtlFlagFunction // the additional flagset to add to the starter when restoring
	color bool           // inferred from the global "--no-color" flag
	cmd   *cobra.Command // the command associated to this list action

	// individualized for each use of scaffoldlist
	dataStruct       Any
	dataFunc         ListDataFunction[Any]       // function for fetching data for table/json/csv
	prettyFunc       func(*cobra.Command) string // free-form function for pretty-printing some data (in the pretty format)
	addtlFlagSetFunc func() pflag.FlagSet        // function to regenerate the additional flags, as all FlagSet copies are shallow
}

// Constructs a ListAction suitable for interactive use.
// Options are execution in array-order.
func newListAction[Any any](defaultColumns []string, dataStruct Any, dFn ListDataFunction[Any], options Options) ListAction[Any] {
	la := ListAction[Any]{
		done:    false,
		columns: defaultColumns,
		fs:      listStarterFlags(),

		DefaultFormat:  tbl,
		DefaultColumns: defaultColumns,
		color:          true,
		cmd:            nil,

		dataStruct:       dataStruct,
		dataFunc:         dFn,
		prettyFunc:       options.Pretty,
		addtlFlagSetFunc: options.AddtlFlags}

	// bolt the additional flags onto the base flagset
	if la.addtlFlagSetFunc != nil {
		afs := la.addtlFlagSetFunc()
		la.fs.AddFlagSet(&afs)
	}

	return la
}

// Update takes in a msg (some event that occurred, like a window redraw or a key press) and acts on it.
// List only ever needs to update once; it figured out what data is to be displayed, fetches it, and spits it out above the prompt.
func (la *ListAction[T]) Update(msg tea.Msg) tea.Cmd {
	if la.done {
		return nil
	}

	// list only ever acts once; immediately mark it as done
	la.done = true

	// check for --show-columns
	if la.showColumns {
		cols, err := weave.StructFields(la.dataStruct, true)
		if err != nil {
			tea.Println("An error has occurred: ", err)
			return textinput.Blink
		}
		return tea.Println(strings.Join(cols, " "))
	}

	// fetch the list data
	s, err := listOutput(&la.fs, la.columns, la.color, la.dataFunc)
	if err != nil {
		// log and print the error
		clilog.Writer.Error(err.Error())
		return tea.Println("An error has occurred: ", err)
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

	la.fs = listStarterFlags()
	// if we were given additional flags, add them
	if la.addtlFlagSetFunc != nil {
		afs := la.addtlFlagSetFunc()
		la.fs.AddFlagSet(&afs)
	}

	if la.outFile != nil {
		la.outFile.Close()
	}
	la.outFile = nil
	return nil
}

var _ action.Model = &ListAction[any]{}

// SetArgs is called when the action is invoked by the user and Mother *enters* handoff mode.
// Mother parses flags and provides us a handle to check against.
func (la *ListAction[T]) SetArgs(
	inherited *pflag.FlagSet, tokens []string) (invalid string, onStart tea.Cmd, err error) {

	// attach inherited flags to the normal flagset
	la.fs.AddFlagSet(inherited)

	err = la.fs.Parse(tokens)
	if err != nil {
		return err.Error(), nil, nil
	}
	//fs := la.fs

	// parse column handling
	// only need to parse columns if user did not pass in --show-columns
	if la.showColumns, err = la.fs.GetBool("show-columns"); err != nil {
		return "", nil, err
	} else if !la.showColumns {
		// fetch columns if it exists
		if cols, err := la.fs.GetStringSlice("columns"); err != nil {
			return "", nil, err
		} else if len(cols) > 0 {
			la.columns = cols
		} // else: defaults to DefaultColumns
	}

	nc, err := la.fs.GetBool("no-color")
	if err != nil {
		la.color = false
		clilog.Writer.Warnf("Failed to fetch no-color from inherited: %v", err)
	}
	la.color = !nc

	if f, err := initOutFile(&la.fs); err != nil {
		return "", nil, err
	} else {
		la.outFile = f
	}

	return "", nil, nil
}

//#endregion interactive mode (model) implementation
