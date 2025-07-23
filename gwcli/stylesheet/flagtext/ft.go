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
	"strings"
)

type flag struct {
	Name      string
	Shorthand rune
	Usage     string
}

// P returns the shorthand of the flag as a string, if any.
// Just a convenience function.
func (f flag) P() string {
	return string(f.Shorthand)
}

// NoInteractive (--no-interactive) is a global flag that disables all interactive components of gwcli.
var NoInteractive = flag{
	Name:      "no-interactive",
	Shorthand: 'x',
	Usage: "disallows gwcli from awaiting user input, making it safe to execute in a scripting context.\n" +
		"If more data is required or bad input given, gwcli will fail out instead of entering interactive mode"}

// Dryrun (--dryrun) is a local flag implemented by actions (typically deletes) to describe actions that would have been taken had --dryrun not been set.
var Dryrun = flag{
	Name:  "dryrun",
	Usage: "feigns the request action, instead displaying the effects that would have occurred",
}

//#region output manipulation

// Output (-o) is a local flag implemented by actions to redirect their results to a file.
// Should be paired with --append; often also paired with --json and --csv.
var Output = flag{
	Name:      "output",
	Shorthand: 'o',
	Usage: "file to write results to.\n" +
		"Truncates file unless --" + Append.Name + " is also given",
}

// Append (--append) is a local flag implemented with --output to indicated that the target file should be appended to instead of truncated.
var Append = flag{
	Name:  "append",
	Usage: "append to the given output file instead of truncating it",
}

// CSV (--csv) is a local flag implemented --output to indicated that results should be in csv format.
var CSV = flag{
	Name: "csv",
	Usage: "display results as CSV.\n" +
		"Mutually exclusive with --json, --table",
}

// JSON (--json) is a local flag implemented --output to indicated that results should be in json format.
var JSON = flag{
	Name: "json",
	Usage: "display results as JSON.\n" +
		"Mutually exclusive with --csv, --table",
}

// Table (--table) is a local flag implemented --output to indicated that results should be outputted as a fancy table.
var Table = flag{
	Name: "table",
	Usage: "display results in a fancy table.\nMutually exclusive with --json, --csv.\n" +
		"Default if no format flags are given",
}

//#endregion output manipulation

// Name struct contains common flag names used across a variety of actions.
var Name = struct {
	Name      string
	Desc      string
	ID        string
	Query     string
	Frequency string
	Expansion string // macro expansions
	ListAll   string

	// column selection

	AllColumns    string // return data from all available columns
	SelectColumns string // return data from specified columns
}{
	Name:      "name",
	Desc:      "description",
	ID:        "id",
	Query:     "query",
	Frequency: "frequency",
	Expansion: "expansion",
	ListAll:   "all",
	// column selection

	AllColumns:    "all-columns",
	SelectColumns: "columns",
}

// Usage contains shared, common flag usage description used across a variety of actions.
// The compiler should inline all of these functions so they are overhead-less.
var Usage = struct {
	Name      func(singular string) string
	Desc      func(singular string) string
	Frequency string
	Expansion string // macro expansions
	// would include "Ignored if you are not admin" suffixed, except I cannot guarentee all Client
	// library GetAll* functions actually do this rather than failing outright.
	ListAll func(plural string) string

	// column selection

	AllColumns    string // return data from all available columns
	SelectColumns string // return data from specified columns
}{
	Name: func(singular string) string {
		return "name of the " + singular
	},
	Desc: func(singular string) string {
		return "flavour description of the " + singular
	},
	Frequency: "cron-style execution frequency",
	Expansion: "value for the macro to expand to", // macro expansions
	ListAll: func(plural string) string {
		return "ADMIN-ONLY. Lists all " + plural + " on the system."
	},

	// column selection

	AllColumns: "displays data from all columns, ignoring the default column set.\n" +
		"Overrides --" + Name.AllColumns,
	SelectColumns: "comma-separated list of columns to include in the results." +
		"Use --show-columns to see the full list of columns.",
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
