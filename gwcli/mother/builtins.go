/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mother

/*
 Builtins are special, meta actions users can invoke from Mother's prompt, no matter their pwd.
*/

import (
	"maps"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/spf13/cobra"

	tea "github.com/charmbracelet/bubbletea"
)

// invocation string -> function to be invoked
var builtins map[string](func(m *Mother, endCmd *cobra.Command, excessTokens []string) tea.Cmd)

// a cache of maps.Keys(builtins) so we don't have to call it multiple times.
var builtinKeys []string

// invocation string -> help string displayed when `help <builtin>` is called
var builtinHelp map[string]string

// initialize the maps used for builtin actions
func initBuiltins() {
	builtins = map[string](func(*Mother, *cobra.Command, []string) tea.Cmd){
		"help":    contextHelp,
		"ls":      contextHelp,
		"history": listHistory,
		"pwd":     pwd,
		"quit":    quit,
		"exit":    quit,
		"clear":   clear,
	}

	builtinKeys = slices.Collect(maps.Keys(builtins))

	builtinHelp = map[string]string{
		"help": "Display context-sensitive help. Equivalent to pressing F1.\n" +
			"Calling " + stylesheet.Cur.ExampleText.Render("help") + " bare provides currently available navigations.\n" +
			"Help can also be passed a path to display help on remote directories or actions.\n" +
			"Ex: " +
			stylesheet.Cur.ExampleText.Render("help ~ kits list") +
			", " +
			stylesheet.Cur.ExampleText.Render("help query"),
		"history": "List previous commands. Navigate history via " + stylesheet.UpDownSigils,
		"pwd":     "Current working directory (path)",
		"quit":    "Kill the application",
		"exit":    "Kill the application",
		"clear":   "clear the screen",
	}
}

// Built-in, interactive help invocation.
// Uses tkns to consume a builtin.
// If tkns[0] is a valid builtin, help will be displayed for it instead of endCmd.
func contextHelp(m *Mother, endCmd *cobra.Command, tkns []string) tea.Cmd {
	// check for builtin
	if len(tkns) > 0 && strings.TrimSpace(tkns[0]) != "" {
		bi := tkns[0]
		helpStr, found := builtinHelp[bi]
		if !found {
			return tea.Println("no help defined for builtin '" + bi + "'")
		}
		return tea.Println(helpStr)
	}
	// return help for the command
	return TeaCmdContextHelp(endCmd)
}

// Returns a print tea.Cmd to display records from oldest (top) to newest (bottom).
func listHistory(m *Mother, _ *cobra.Command, _ []string) tea.Cmd {
	toPrint := strings.Builder{}
	rs := m.history.getAllRecords()

	// print the oldest record first, so newest record is directly over prompt
	for i := len(rs) - 1; i > 0; i-- {
		toPrint.WriteString(rs[i] + "\n")
	}

	// chomp last newline
	return tea.Println(strings.TrimSpace(toPrint.String()))
}

// Returns the current working directory.
// Basically redundant in the current prompt style, but facilitates alternative prompt formats or prompt path truncation.
func pwd(m *Mother, _ *cobra.Command, _ []string) tea.Cmd {
	return tea.Println(m.pwd.UseLine())
}

func quit(m *Mother, _ *cobra.Command, _ []string) tea.Cmd {
	m.exiting = true
	return tea.Sequence(tea.Println("Bye"), tea.Quit)
}

// Clears the screen, returning Mother's prompt to the top left.
func clear(m *Mother, _ *cobra.Command, _ []string) tea.Cmd {
	return tea.ClearScreen
}
