/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package ft (flagtext) provides a repository of strings shared by flags used across actions to enforce output/visual consistency.
If a flag is used the same way in multiple actions, place it in this package.
All fields should be considered constant and therefore not be modified at runtime.
*/
package ft

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/spf13/pflag"
)

type flag struct {
	Name      string
	Shorthand rune
	Usage     string
	typ       types.BasicKind
}

// Register installs this flag (with its standard type) in the given flagset.
// Sets the default to the zero value of the type.
// Only supports a subset of types; expand as need be.
//
// It is a helper function to provide consistent usage.
func (f flag) Register(fs *pflag.FlagSet) {
	if f.typ == types.Invalid {
		panic("cannot register a flag with an invalid type")
	}
	// There is probably a better way to do this, but pflag does not expose the pflag.Value implementations it uses for primitive types.
	// Therefore, this will do well enough for a helper function of this low priority.
	switch f.typ {
	case types.Bool:
		fs.BoolP(f.Name, f.P(), false, f.Usage)
	case types.String:
		fs.StringP(f.Name, f.P(), "", f.Usage)
	default:
		panic(fmt.Sprintf("unhandled type: %v", f.typ))
	}
}

// P returns the shorthand of the flag as a string, if any.
// Just a convenience function.
func (f flag) P() string {
	var zero rune
	if f.Shorthand == zero {
		return ""
	}
	return string(f.Shorthand)
}

// NoInteractive (--no-interactive) is a global flag that disables all interactive components of gwcli.
var NoInteractive = flag{
	Name:      "no-interactive",
	Shorthand: 'x',
	Usage: "disallows gwcli from awaiting user input, making it safe to execute in a scripting context.\n" +
		"If more data is required or bad input given, gwcli will fail out instead of entering interactive mode",
	typ: types.Bool,
}

// Dryrun (--dryrun) is a local flag implemented by actions (typically deletes) to describe actions that would have been taken had --dryrun not been set.
var Dryrun = flag{
	Name:  "dryrun",
	Usage: "feigns the request action, instead displaying the effects that would have occurred",
	typ:   types.Bool,
}

// GetAll is a local flag that tells the implementing action to fetch all items, rather than just the current user's items (or something to that effect).
// For example, providing this to macros should fetch all macros on the instance, rather than just your macros.
/*var GetAll = struct {
	Name      string
	Shorthand rune
	Usage     func(adminOnly bool, plural string) string
	Register  func(fs *pflag.FlagSet)
}{
	Name: "all",
	// would include "Ignored if you are not admin" suffixed, except I cannot guarentee all Client
	// library GetAll* functions actually do this rather than failing outright.
	Usage: func(adminOnly bool, plural string) string {
		s := "Lists all " + plural + " on the system."
		if adminOnly {
			s = "ADMIN-ONLY." + s
		}
		return s
	},
	Register: func(fs *pflag.FlagSet) {
		// if it is stupid but it works....
		fs.Bool("all", false, Usag)
	},
}*/

//#region output manipulation

// Output (-o) is a local flag implemented by actions to redirect their results to a file.
// Should be paired with --append; often also paired with --json and --csv.
var Output = flag{
	Name:      "output",
	Shorthand: 'o',
	Usage: "file to write results to.\n" +
		"Truncates file unless --" + Append.Name + " is also given",
	typ: types.String,
}

// Append (--append) is a local flag implemented with --output to indicated that the target file should be appended to instead of truncated.
var Append = flag{
	Name:  "append",
	Usage: "append to the given output file instead of truncating it",
	typ:   types.Bool,
}

// CSV (--csv) is a local flag implemented --output to indicated that results should be in csv format.
var CSV = flag{
	Name: "csv",
	Usage: "display results as CSV.\n" +
		"Mutually exclusive with --json, --table",
	typ: types.Bool,
}

// JSON (--json) is a local flag implemented --output to indicated that results should be in json format.
var JSON = flag{
	Name: "json",
	Usage: "display results as JSON.\n" +
		"Mutually exclusive with --csv, --table",
	typ: types.Bool,
}

// Table (--table) is a local flag implemented --output to indicated that results should be outputted as a fancy table.
var Table = flag{
	Name: "table",
	Usage: "display results in a fancy table.\nMutually exclusive with --json, --csv.\n" +
		"Default if no format flags are given",
	typ: types.Bool,
}

//#endregion output manipulation

//#region scaffoldlist/columns

// ShowColumns (--show-columns) is a local flag used by scaffold list to display all known columns.
// Unlikely to be used outside of actions that implement scaffold list.
var ShowColumns = flag{
	Name:  "show-columns",
	Usage: "display the list of fully qualified column names and exit",
	typ:   types.Bool,
}

// SelectColumns (--columns) is a local flag used by scaffold list to select which columns to display, overriding the default.
// Unlikely to be used outside of actions that implement scaffold list.
var SelectColumns = struct { // we have to use a custom Add implementation as we need StringSlice.
	Name      string
	Shorthand rune
	Usage     string
	Register  func(fs *pflag.FlagSet)
}{
	Name: "columns",
	Usage: "comma-separated list of columns to include in the results\n." +
		"Use --" + ShowColumns.Name + " to see the full list of columns",
	Register: func(fs *pflag.FlagSet) {
		// if it is stupid but it works....
		fs.StringSlice("columns", nil, "comma-separated list of columns to include in the results\n."+
			"Use --"+ShowColumns.Name+" to see the full list of columns")
	},
}

// AllColumns (--all-columns) is a local flag used by scaffold list to force the action to display data from all available columns.
// Unlikely to be used outside of actions that implement scaffold list.
var AllColumns = flag{
	Name: "all-columns",
	Usage: "displays data from all columns, ignoring the default column set.\n" +
		"Overrides --" + SelectColumns.Name,
	typ: types.Bool,
}

//#endregion scaffoldlist/columns

// Name struct contains common flag names used across a variety of actions.
var Name = struct {
	Name      string
	Desc      string
	ID        string
	Query     string
	Frequency string
	Expansion string // macro expansions
}{
	Name:      "name",
	Desc:      "description",
	ID:        "id",
	Query:     "query",
	Frequency: "frequency",
	Expansion: "expansion",
}

// Usage contains shared, common flag usage description used across a variety of actions.
// The compiler should inline all of these functions so they are overhead-less.
var Usage = struct {
	Name      func(singular string) string
	Desc      func(singular string) string
	Frequency string
	Expansion string // macro expansions
}{
	Name: func(singular string) string {
		return "name of the " + singular
	},
	Desc: func(singular string) string {
		return "flavour description of the " + singular
	},
	Frequency: "cron-style execution frequency",
	Expansion: "value for the macro to expand to", // macro expansions
}

// WarnFlagIgnore returns a string about ignoring ignoredFlag due to causeFlag's existence.
func WarnFlagIgnore(ignoredFlag, causeFlag string) string {
	return fmt.Sprintf("WARN: ignoring flag --%v due to --%v", ignoredFlag, causeFlag)
}

// DeriveFlagName returns a consistent, sanitized string, usable as a flag name.
// Lower-cases the name and maps special characters to '-'.
// Used to ensure consistency.
func DeriveFlagName(title string) string {
	title = strings.ToLower(title)
	title = strings.Map(func(r rune) rune {
		switch r {
		case '.', '\\', '/', '\'', '"', '|', ' ':
			return '-'
		}
		return r
	}, title)
	return title
}

// Mandatory wraps and returns the given text in angle brackets to indicate that it is a required flag or argument.
func Mandatory(text string) string {
	return "<" + text + ">"
}

// Optional wraps and returns the given text in square brackets to indicate that it is an optional flag or argument.
func Optional(text string) string {
	return "[" + text + "]"
}

// MutuallyExclusive wraps and returns the given elements in curly braces to indicate that they are mutually exclusive with one another.
func MutuallyExclusive(texts []string) string {
	return "{" + strings.Join(texts, "|") + "}"
}
