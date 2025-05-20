/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package ft (flagtext) provides a repository of strings shared by flags across gwcli to enforce output consistency.
While all are constant and *should not be modified at runtime*, it is organized as a struct for clearer access.

Struct parity between Name and Usage is not guaranteed; some usages may vary too much to warrant
sharing a base string.
*/
package ft

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Name struct contains common flag names used across a variety of actions.
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
	Table  string
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
	Table:  "table",
}

// Usage contains shared, common flag usage description used across a variety of actions.
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
	Table  string
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
	CSV:    "display results as CSV.\nMutually exclusive with --json, --table.",
	JSON:   "display results as JSON.\nMutually exclusive with --csv, --table.",
	Table: "display results in a fancy table.\nMutually exclusive with --json, --csv.\n" +
		"Default if no format flags are given.",
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
		case '/', '\'', '"', '|', ' ':
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

// Optional wraps and returns the given elements in curly braces to indicate that they are mutually exclusive with one another.
func MutuallyExclusive(texts []string) string {
	return "{" + strings.Join(texts, "|") + "}"
}
