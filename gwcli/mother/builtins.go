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

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/group"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/cobra"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss/tree"
)

var treeSyntax string = "tree " + ft.Optional("path")

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
		"tree":    localTree,
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
		"tree": "Syntax: " + treeSyntax + "\n" +
			"Displays a directory-tree rooted at the given command.\n" +
			"Tree can be passed a path to display a the tree rooted on a remote directory.\n" +
			"Ex: " + stylesheet.Cur.ExampleText.Render("tree .. kits"),
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

func localTree(m *Mother, end *cobra.Command, excess []string) tea.Cmd {
	if end == nil {
		clilog.Writer.Warn("end command is nil, defaulting to Mother's pwd",
			rfc5424.SDParam{Name: "pwd", Value: m.pwd.Name()},
			rfc5424.SDParam{Name: "excess", Value: strings.Join(excess, " ")},
		)
		end = m.pwd
	}
	if len(excess) == 0 {
		// no further processing needed
		return tea.Println(walkBranch(end))
	}

	// we were given a command path.
	// validate it and attempt to root the tree at the end of the path

	wr, err := uniques.Walk(end, strings.Join(excess, " "), builtinKeys)
	if err != nil {
		clilog.Writer.Error("failed to walk excess input for tree command",
			rfc5424.SDParam{Name: "excess", Value: strings.Join(excess, " ")},
			rfc5424.SDParam{Name: "error", Value: err.Error()},
		)
		return tea.Println(walkBranch(end))
	} else if wr.HelpMode {
		return stylesheet.ErrPrintf("%s", treeSyntax)
	} else if wr.Builtin != "" {
		return stylesheet.ErrPrintf("cannot root tree on a builtin command")
	} else if wr.EndCmd == nil {
		return tea.Println(walkBranch(end).String())
	}

	return tea.Println(walkBranch(wr.EndCmd).String())

}

func walkBranch(cmd *cobra.Command) *tree.Tree {
	// generate a new tree, stemming from the end command
	branchRoot := tree.New()
	if cmd == nil {
		return branchRoot
	}

	actionSty := stylesheet.Cur.Action

	branchRoot.Root(stylesheet.ColorCommandName(cmd))
	branchRoot.EnumeratorStyle(stylesheet.Cur.PrimaryText.PaddingLeft(1))

	// add children of this nav to its tree
	for _, child := range cmd.Commands() {
		switch child.GroupID {
		case group.ActionID:
			branchRoot.Child(actionSty.Render(child.Name()))
		case group.NavID:
			branchRoot.Child(walkBranch(child))
		default:
			// this will encompass Cobra's default commands (ex: help and completions)
			// nothing else should fall in here
			branchRoot.Child(actionSty.Render(child.Name()))
		}
	}

	return branchRoot
}
