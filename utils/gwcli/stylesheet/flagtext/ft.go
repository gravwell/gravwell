/*
ft (flagtext) provides a repository of strings shared by flags across gwcli.
While all are constant and *should not be modified at runtime*, it is organized as a struct for
clearer access.

Struct parity between Name and Usage is not guarenteed; some usages may vary too much to warrant
sharing a base string.
*/
package ft

import "github.com/charmbracelet/lipgloss"

// Common flag names used across a variety of actions
var Name = struct {
	Dryrun    string
	Name      string
	Desc      string
	ID        string
	Query     string
	Frequency string
	Expansion string // macro expansions
	ListAll   string
	Script    string

	// output manipulation

	Output string // file output
	Append string // append to file output
	CSV    string
	JSON   string
}{
	Dryrun:    "dryrun",
	Name:      "name",
	Desc:      "description",
	ID:        "id",
	Query:     "query",
	Frequency: "frequency",
	Expansion: "expansion",
	ListAll:   "all",
	Script:    "script",

	// output manipulation

	Output: "output",
	Append: "append",
	CSV:    "csv",
	JSON:   "json",
}

// Common flag usage description used across a variety of actions
// The compiler should inline all of these functions so they are overhead-less.
var Usage = struct {
	Dryrun    string
	Name      func(singular string) string
	Desc      func(singular string) string
	Frequency string
	Expansion string // macro expansions
	// would include "Ignored if you are not admin" suffixed, except I cannot guarentee all Client
	// library GetAll* functions actually do this rather than failing outright.
	ListAll func(plural string) string

	// output manipulation

	Output string // file output
	Append string // append to file output
	CSV    string
	JSON   string
}{
	Dryrun: "feigns, describing actions that " +
		lipgloss.NewStyle().Italic(true).Render("would") +
		" have been taken",
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

	// output manipulation

	Output: "file to write results to.\nTruncates file unless --append is also given.",
	Append: "append to the given output file instead of truncating it.",
	CSV:    "display results as CSV.\nMutually exclusive with --json.",
	JSON:   "display results as JSON.\nMutually exclusive with --csv.",
}
