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
	"github.com/gravwell/gravwell/v3/gwcli/clilog"
	"github.com/gravwell/gravwell/v3/gwcli/stylesheet"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// invocation string -> function to be invoked
var builtins map[string](func(*Mother, []string) tea.Cmd)

// invocation string -> help string displayed when `help <builtin>` is called
var builtinHelp map[string]string

// initialize the maps used for builtin actions
func initBuiltins() {
	builtins = map[string](func(*Mother, []string) tea.Cmd){
		"help":    contextHelp,
		"history": listHistory,
		"quit":    quit,
		"exit":    quit}

	builtinHelp = map[string]string{
		"help": "Display context-sensitive help. Equivalent to pressing F1.\n" +
			"Calling " + stylesheet.ExampleStyle.Render("help") + " bare provides currently available navigations.\n" +
			"Help can also be passed a path to display help on remote directories or actions.\n" +
			"Ex: " +
			stylesheet.ExampleStyle.Render("help ~ kits list") +
			", " +
			stylesheet.ExampleStyle.Render("help query"),
		"history": "List previous commands. Navigate history via " + stylesheet.UpDown,
		"quit":    "Kill the application",
		"exit":    "Kill the application",
	}
}

// Built-in, interactive help invocation
func contextHelp(m *Mother, args []string) tea.Cmd {
	if len(args) == 0 {
		return TeaCmdContextHelp(m.pwd)
	}

	// walk the command tree
	// action or nav, print help about it
	// if invalid/no destination, print error
	wr := walk(m.pwd, args)

	if wr.errString != "" { // erroneous input
		return tea.Println(stylesheet.ErrStyle.Render(wr.errString))
	}
	switch wr.status {
	case foundNav, foundAction:
		return TeaCmdContextHelp(wr.endCommand)
	case foundBuiltin:
		if _, ok := builtins[args[0]]; ok {
			str, found := builtinHelp[args[0]]
			if !found {
				str = "no help defined for '" + args[0] + "'"
			}

			return tea.Printf(str)
		}

	}

	clilog.Writer.Infof("Doing nothing (%#v)", wr)

	return nil
}

// Returns a print tea.Cmd to display records from oldest (top) to newest (bottom).
func listHistory(m *Mother, _ []string) tea.Cmd {
	toPrint := strings.Builder{}
	rs := m.history.getAllRecords()

	// print the oldest record first, so newest record is directly over prompt
	for i := len(rs) - 1; i > 0; i-- {
		toPrint.WriteString(rs[i] + "\n")
	}

	// chomp last newline
	return tea.Println(strings.TrimSpace(toPrint.String()))
}

func quit(*Mother, []string) tea.Cmd {
	return tea.Sequence(tea.Println("Bye"), tea.Quit)
}
