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

// The purpose of this file and package is to provide consistent registering and accessing of flags.
// The internal structure is secondary.
//
// ft.<flag>.Name() is the standardized mechanism for retrieving flags.
// ft.<flag>.Register(fs) is the standardized mechanism for installing this flag in the given flagset.
// If a flag needs to modify its parameters (custom usage, set a default value), Name(), Usage(), and Shorthand() are available for manual installation.

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/spf13/pflag"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// a flag is the minimum required for accessing and registering a flag for use in other actions.
type flag interface {
	// Used for accessing the flag
	Name() string
	Shorthand() string
	// Used for the initial install of the flag in the given flagset.
	Register(fs *pflag.FlagSet)
}

var _ flag = simple{}
var _ flag = stringSliceRegister{}

//#region simple flag

// a simple is the basic/standard implementation of the flag interface
type simple struct {
	name      string
	shorthand rune
	usage     string
	typ       types.BasicKind
}

// Name returns the name of the flag, with no dashes (--<name>).
func (s simple) Name() string {
	return s.name
}

// Shorthand returns the single rune character for accessing this flag.
func (s simple) Shorthand() string {
	var zero rune
	if s.shorthand == zero {
		return ""
	}
	return string(s.shorthand)
}

// Usage returns the flag description.
func (s simple) Usage() string {
	return s.usage
}

// Register installs this flag (with its standard type) in the given flagset.
// Sets the default to the zero value of the type.
// Only supports a subset of types; expand as need be.
//
// It is a helper function to provide consistent usage.
func (s simple) Register(fs *pflag.FlagSet) {
	if s.typ == types.Invalid {
		panic("cannot register a flag with an invalid type")
	}
	// There is probably a better way to do this, but pflag does not expose the pflag.Value implementations it uses for primitive types.
	// Therefore, this will do well enough for a helper function of this low priority.
	switch s.typ {
	case types.Bool:
		fs.BoolP(s.name, s.Shorthand(), false, s.usage)
	case types.String:
		fs.StringP(s.name, s.Shorthand(), "", s.usage)
	default:
		panic(fmt.Sprintf("unhandled type: %v", s.typ))
	}
}

//#endregion simple flag

//#region string slice flag

// stringSliceRegister is a special flag handler for flags that take a string slice (ex: --<name> a,b,c,d).
type stringSliceRegister struct {
	name      string
	shorthand rune
	usage     string
}

func (s stringSliceRegister) Name() string {
	return s.name
}

func (s stringSliceRegister) Shorthand() string {
	var zero rune
	if s.shorthand == zero {
		return ""
	}
	return string(s.shorthand)
}

// Register installs this flag (with its standard type) in the given flagset.
// Sets the default to the zero value of the type.
// Only supports a subset of types; expand as need be.
//
// It is a helper function to provide consistent usage.
func (s stringSliceRegister) Register(fs *pflag.FlagSet) {
	fs.StringSliceP(s.name, s.Shorthand(), nil, s.usage)
}

//#endregion string slice flag

//#region singular

type singular struct {
	name      string
	shorthand rune
	// the body of the usage message, prior to the singular form being appended.
	// Expected to be whitespace-trimmed.
	usagePrefix string
}

func (s singular) Name() string {
	return s.name
}

func (s singular) Shorthand() string {
	var zero rune
	if s.shorthand == zero {
		return ""
	}
	return string(s.shorthand)
}

func (s singular) Usage(singular string) string {
	return s.usagePrefix + " " + singular
}

// Register installs this flag as a string in the given flagset.
// It is a helper function to provide consistent usage.
func (s singular) Register(fs *pflag.FlagSet, singular string) {
	fs.StringP(s.Name(), s.Shorthand(), "", s.Usage(singular))
}

// NoInteractive (--no-interactive) is a global flag that disables all interactive components of gwcli.
var NoInteractive = simple{
	name:      "no-interactive",
	shorthand: 'x',
	usage: "disallows gwcli from awaiting user input, making it safe to execute in a scripting context.\n" +
		"If more data is required or bad input given, gwcli will fail out instead of entering interactive mode",
	typ: types.Bool,
}

// Dryrun (--dryrun) is a local flag implemented by actions (typically deletes) to describe actions that would have been taken had --dryrun not been set.
var Dryrun = simple{
	name:  "dryrun",
	usage: "feigns the request action, instead displaying the effects that would have occurred",
	typ:   types.Bool,
}

//#region output manipulation

// Output (-o) is a local flag implemented by actions to redirect their results to a file.
// Should be paired with --append; often also paired with --json and --csv.
var Output = simple{
	name:      "output",
	shorthand: 'o',
	usage: "file to write results to.\n" +
		"Truncates file unless --" + Append.name + " is also given",
	typ: types.String,
}

// Append (--append) is a local flag implemented with --output to indicated that the target file should be appended to instead of truncated.
var Append = simple{
	name:  "append",
	usage: "append to the given output file instead of truncating it",
	typ:   types.Bool,
}

// CSV (--csv) is a local flag implemented --output to indicated that results should be in csv format.
var CSV = simple{
	name: "csv",
	usage: "display results as CSV.\n" +
		"Mutually exclusive with --json, --table",
	typ: types.Bool,
}

// JSON (--json) is a local flag implemented --output to indicated that results should be in json format.
var JSON = simple{
	name: "json",
	usage: "display results as JSON.\n" +
		"Mutually exclusive with --csv, --table",
	typ: types.Bool,
}

// Table (--table) is a local flag implemented --output to indicated that results should be outputted as a fancy table.
var Table = simple{
	name: "table",
	usage: "display results in a fancy table.\nMutually exclusive with --json, --csv.\n" +
		"Default if no format flags are given",
	typ: types.Bool,
}

//#endregion output manipulation

//#region scaffoldlist/columns

// ShowColumns (--show-columns) is a local flag used by scaffold list to display all known columns.
// Unlikely to be used outside of actions that implement scaffold list.
var ShowColumns = simple{
	name:  "show-columns",
	usage: "display the list of fully qualified column names and exit",
	typ:   types.Bool,
}

// SelectColumns (--columns) is a local flag used by scaffold list to select which columns to display, overriding the default.
// Unlikely to be used outside of actions that implement scaffold list.
var SelectColumns = stringSliceRegister{
	name: "columns",
	usage: "comma-separated list of columns to include in the results\n." +
		"Use --" + ShowColumns.name + " to see the full list of columns",
}

// AllColumns (--all-columns) is a local flag used by scaffold list to force the action to display data from all available columns.
// Unlikely to be used outside of actions that implement scaffold list.
var AllColumns = simple{
	name: "all-columns",
	usage: "displays data from all columns, ignoring the default column set.\n" +
		"Overrides --" + SelectColumns.name,
	typ: types.Bool,
}

//#endregion scaffoldlist/columns

// need custom handling for GetAll.
// Does not fit the flag interface, but it has similar enough usage so what's it matter?
type getAllFlag struct {
}

// GetAll is a local flag that tells the implementing action to fetch all items, rather than just the current user's items (or something to that effect).
// For example, providing this to macros should fetch all macros on the instance, rather than just your macros.
var GetAll = getAllFlag{}

func (gaf getAllFlag) Name() string { return "all" }

// Register installs this flag in the given flagset.
//
// requiresAdmin prefixes "ADMIN ONLY" to the usage.
//
// plural is the plural form of the thing being fetched.
//
// usageSuffixLines an optional set of ordered sentences to be attached (separated by newlines) to the usage of this flag.
// Each line will be titled cased and have a period appended.
func (gaf getAllFlag) Register(fs *pflag.FlagSet, requiresAdmin bool, plural string, usageSuffixLines ...string) {
	usage := "Lists all " + plural + " on the system."
	if requiresAdmin {
		usage = "ADMIN ONLY." + usage
	}
	// append each extra line
	if usageSuffixLines != nil {
		usage += "\n"
		var (
			sb  strings.Builder
			ttl = cases.Title(language.English)
		)
		for _, line := range usageSuffixLines {
			l := ttl.String(line)
			if !strings.HasSuffix(l, ".") {
				l += "."
			}
			sb.WriteString(l)
		}
	}

	fs.Bool("all", false, strings.TrimSuffix(strings.TrimSpace(usage), "."))
}

// Frequency is a local flag for defining a cron-style interval in which something occurs.
var Frequency = simple{
	name:      "frequency",
	shorthand: 'f',
	usage:     "cron-style scheduling for scheduled execution",
}

// Description is a local flag for providing the description of an item.
var Description = singular{
	name:        "description",
	shorthand:   'd',
	usagePrefix: "flavour description of the",
}

// Name is a local flag for providing or specifying the name of an item.
var Name = singular{
	name:        "name",
	shorthand:   'n',
	usagePrefix: "name of the",
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
