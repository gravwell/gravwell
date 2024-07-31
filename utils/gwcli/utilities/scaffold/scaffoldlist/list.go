/*
A list action runs a given function that outputs an arbitrary data structure.
The results are sent to weave and packaged in a way that can be listed for the user.

This provides a consistent interface for actions that list arbitrary data.

List actions have the --output, --append, --json, --table, --CSV, and --show-columns default flags.

Example implementation:

	const (
		short string = ""
		long  string = ""
	)

	var (
		aliases        []string = []string{}
		defaultColumns []string = []string{"ID", "UID", "Name", "Description"}
	)

	func New[parentpkg]ListAction() action.Pair {
		return scaffoldlist.NewListAction(short, long, aliases, defaultColumns,
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
	"fmt"
	"gwcli/action"
	"gwcli/clilog"
	"gwcli/connection"
	"gwcli/stylesheet"
	ft "gwcli/stylesheet/flagtext"
	"gwcli/utilities/treeutils"
	"os"
	"reflect"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	grav "github.com/gravwell/gravwell/v3/client"
	"github.com/gravwell/gravwell/v3/utils/weave"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//#region enumeration

type outputFormat uint

const (
	json outputFormat = iota
	csv
	table
	unknown
)

func (f outputFormat) String() string {
	switch f {
	case json:
		return "JSON"
	case csv:
		return "CSV"
	case table:
		return "table"
	}
	return fmt.Sprintf("unknown format (%d)", f)
}

//#endregion enumeration

const outFilePerm = 0644

// Function that retrieves an array of structs of type dataStruct
type dataFunction[Any any] func(*grav.Client, *pflag.FlagSet) ([]Any, error)
type addtlFlagFunction func() pflag.FlagSet

// NewListAction creates and returns a cobra.Command suitable for use as a list
// action, complete with common flags and a generic run function operating off
// the given dataFunction.
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
// Any data massaging required to get the data into an array of structures should be performed in
// the data function. Non-list-standard flags (ex: those passed to addtlFlags, if not nil) should
// also be handled in the data function.
// See tree/kits/list's ListKits() as an example.
//
// Go's Generics are a godsend.
func NewListAction[Any any](short, long string, defaultColumns []string,
	dataStruct Any, dataFn dataFunction[Any], addtlFlagsFunc addtlFlagFunction) action.Pair {
	// assert developer provided a usable data struct
	if reflect.TypeOf(dataStruct).Kind() != reflect.Struct {
		panic("dataStruct must be a struct") // developer error
	}

	// the function to run if called from the shell/non-interactively
	runFunc := func(cmd *cobra.Command, _ []string) {
		// check for --show-columns
		if sc, err := cmd.Flags().GetBool("show-columns"); err != nil {
			clilog.LogFlagFailedGet("show-columns", err)
			fmt.Fprintln(cmd.OutOrStdout(), "An error has occurred.")
			return
		} else if sc {
			cols, err := weave.StructFields(dataStruct, true)
			if err != nil {
				clilog.LogFlagFailedGet("show-columns", err)
				fmt.Fprintln(cmd.OutOrStdout(), "An error has occurred.")
				return
			}
			fmt.Fprintln(cmd.OutOrStdout(), strings.Join(cols, " "))
			return
		}

		// fetch columns
		var (
			columns []string
			err     error
		)
		if columns, err = cmd.Flags().GetStringSlice("columns"); err != nil {
			clilog.LogFlagFailedGet("columns", err)
			// will fall back to default columns
		} else if len(columns) == 0 {
			columns = defaultColumns
		}

		// check for --no-color
		noColor, err := cmd.Flags().GetBool("no-color")
		if err != nil {
			clilog.LogFlagFailedGet("no-color", err)
		}

		// check for output file
		outFile, err := initOutFile(cmd.Flags())
		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
			return
		} else if outFile != nil {
			defer outFile.Close()
		}

		s, err := listOutput(cmd.Flags(), columns, !noColor, dataFn)
		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
			return
		}

		// if we received no data, do nothing if outfile, note the result to stdout otherwise
		if s == "" {
			if outFile == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "no data found")
			}
			return
		}

		if outFile != nil {
			fmt.Fprintln(outFile, s)
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), s)
		}

	}

	// generate the command
	cmd := treeutils.NewActionCommand("list", short, long, []string{}, runFunc)

	// attach normal list flags and, if applicable, additional flags
	startFS := listStarterFlags()
	cmd.Flags().AddFlagSet(&startFS)
	var addtlFlags pflag.FlagSet
	if addtlFlagsFunc != nil {
		addtlFlags = addtlFlagsFunc()
		cmd.Flags().AddFlagSet(&addtlFlags)
	}

	cmd.Flags().SortFlags = false // does not seem to be respected
	cmd.MarkFlagsMutuallyExclusive("csv", "json", "table")

	// spin up a list action for interactive use
	la := newListAction(defaultColumns, dataStruct, dataFn, addtlFlagsFunc)

	return treeutils.GenerateAction(cmd, &la)
}

// define the basic flags shared by all list actions
func listStarterFlags() pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.Bool(ft.Name.CSV, false, ft.Usage.CSV)
	fs.Bool(ft.Name.JSON, false, ft.Usage.JSON)
	fs.Bool("table", true, "display results in a human-readable table") // default
	fs.StringSlice("columns", []string{},
		"comma-seperated list of columns to include in the results."+
			"Use --show-columns to see the full list of columns.")
	fs.Bool("show-columns", false, "display the list of fully qualified column names and die.")
	fs.StringP(ft.Name.Output, "o", "", ft.Usage.Output)
	fs.Bool(ft.Name.Append, false, ft.Usage.Append)
	return fs
}

// Opens a file, per the given --output and --append flags in the flagset, and returns its handle.
// Returns nil if the flags do not call for a file.
func initOutFile(fs *pflag.FlagSet) (*os.File, error) {
	outPath, err := fs.GetString(ft.Name.Output)
	if err != nil {
		return nil, err
	} else if strings.TrimSpace(outPath) == "" {
		return nil, nil
	}
	var flags int = os.O_CREATE | os.O_WRONLY
	if append, err := fs.GetBool(ft.Name.Append); err != nil {
		return nil, err
	} else if append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	return os.OpenFile(outPath, flags, outFilePerm)
}

// Given a **parsed** flagset, determines and returns output format
func determineFormat(fs *pflag.FlagSet) outputFormat {
	if !fs.Parsed() {
		return unknown
	}
	var format outputFormat
	if format_csv, err := fs.GetBool(ft.Name.CSV); err != nil {
		clilog.LogFlagFailedGet(ft.Name.CSV, err)
	} else if format_csv {
		format = csv
	} else {
		if format_json, err := fs.GetBool(ft.Name.JSON); err != nil {
			clilog.LogFlagFailedGet(ft.Name.JSON, err)
		} else if format_json {
			format = json
		} else {
			format = table
		}
	}
	return format
}

// Driver function to call the provided data func and format its output via weave
func listOutput[Any any](fs *pflag.FlagSet, columns []string, color bool,
	dataFn dataFunction[Any]) (string, error) {

	data, err := dataFn(connection.Client, fs)
	if err != nil {
		return "", err
	} else if len(data) < 1 {
		return "", nil
	}

	// NOTE format flags are marked mutually exclusive on creation
	//		we do not need to check for exclusivity here
	var format outputFormat = determineFormat(fs)
	clilog.Writer.Debugf("List: format %s | row count: %d", format, len(data))
	toRet, err := "", nil
	switch format {
	case csv:
		toRet = weave.ToCSV(data, columns)
	case json:
		toRet, err = weave.ToJSON(data, columns)
	case table:
		if color {
			toRet = weave.ToTable(data, columns, stylesheet.Table)
		} else {
			toRet = weave.ToTable(data, columns) // omit table styling

		}
	default:
		toRet = ""
		err = fmt.Errorf(fmt.Sprintf("unknown output format (%d)", format))
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
	DefaultColumns []string          // columns to output if unspecified
	afsFunc        addtlFlagFunction // the additional flagset to add to the starter when restoring
	color          bool              // inferred from the global "--no-color" flag

	// individualized for each user of list_generic
	dataStruct Any
	dataFunc   dataFunction[Any]
}

// Constructs a ListAction suitable for interactive use
func newListAction[Any any](defaultColumns []string, dataStruct Any, dFn dataFunction[Any],
	addtlFlags addtlFlagFunction) ListAction[Any] {

	fs := listStarterFlags()
	if addtlFlags != nil {
		afs := addtlFlags()
		fs.AddFlagSet(&afs)
	}

	la := ListAction[Any]{
		columns:        defaultColumns,
		fs:             fs,
		DefaultFormat:  table,
		DefaultColumns: defaultColumns,
		afsFunc:        addtlFlags,
		dataStruct:     dataStruct,
		dataFunc:       dFn}

	return la
}

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

	// if we received no data, do nothing if outfile, note the result otherwise
	if s == "" {
		if la.outFile != nil {
			return textinput.Blink
		}
		return tea.Println("no data found")
	}

	// output the results to a file, if given
	if la.outFile != nil {
		fmt.Fprint(la.outFile, s)
		return textinput.Blink
	}

	return tea.Println(s)
}

func (la *ListAction[T]) View() string {
	return ""
}

// Called once per cycle to test if Mother should reassert control
func (la *ListAction[T]) Done() bool {
	return la.done
}

// Called when the action is unseated by Mother on exiting handoff mode
func (la *ListAction[T]) Reset() error {
	la.done = false
	la.columns = la.DefaultColumns
	la.showColumns = false

	la.fs = listStarterFlags()
	// if a function providing additional flags was given, add them
	if la.afsFunc != nil {
		afs := la.afsFunc()
		la.fs.AddFlagSet(&afs)
	}

	if la.outFile != nil {
		la.outFile.Close()
	}
	la.outFile = nil
	return nil
}

var _ action.Model = &ListAction[any]{}

// Called when the action is invoked by the user and Mother *enters* handoff mode
// Mother parses flags and provides us a handle to check against
func (la *ListAction[T]) SetArgs(
	inherited *pflag.FlagSet, tokens []string) (invalid string, onStart tea.Cmd, err error) {

	err = la.fs.Parse(tokens)
	if err != nil {
		return err.Error(), nil, nil
	}
	fs := la.fs

	// parse column handling
	// only need to parse columns if user did not pass in --show-columns
	if la.showColumns, err = fs.GetBool("show-columns"); err != nil {
		return "", nil, err
	} else if !la.showColumns {
		// fetch columns if it exists
		if cols, err := fs.GetStringSlice("columns"); err != nil {
			return "", nil, err
		} else if len(cols) > 0 {
			la.columns = cols
		} // else: defaults to DefaultColumns
	}

	nc, err := inherited.GetBool("no-color")
	if err != nil {
		la.color = false
		clilog.Writer.Warnf("Failed to fetch no-color from inherited: %v", err)
	}
	la.color = !nc

	if f, err := initOutFile(&fs); err != nil {
		return "", nil, err
	} else {
		la.outFile = f
	}

	return "", nil, nil
}

//#endregion interactive mode (model) implementation
