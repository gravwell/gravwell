/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mother

/*
Walk is the beefy boy that enables dynamic path-finding through the tree.
It recusively walks a series of tokens, determining what to do at each step until an acceptable
endpoint is reached (e.g. an executable action, a nav, an error).
It is both used directly for Mother traversal of the command tree as well as determining the
validity of a proposed path.
*/

import (
	"fmt"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

type walkStatus int // how to read the returned walkResult

const (
	invalidCommand walkStatus = iota
	foundNav
	foundAction
	foundBuiltin
	erroring
)

type walkResult struct {
	endCommand *cobra.Command // the relevent command walk completed on
	status     walkStatus     // ending state
	errString  string

	builtinFunc func(*Mother, []string) tea.Cmd // built-in func to invoke

	// contains args for actions
	remainingString string // any tokens remaining for later processing by walk caller
}

/*
Recursively walk the tokens of the exploded user input until we run out or find a valid
destination.

Returns a walkResult struct with:
  - the relevant command (ending Nav destination or action to invoke)
  - the type of the command (action, nav, invalid)
  - a list of commands to pass to Bubble Tea
  - and an error (if one occurred)
*/
func walk(dir *cobra.Command, tokens []string) walkResult {
	if len(tokens) == 0 {
		// only move if the final command was a nav
		return walkResult{
			endCommand: dir,
			status:     foundNav,
		}
	}

	curToken := strings.TrimSpace(tokens[0])
	// if there is no token, just keep walking
	if curToken == "" {
		return walk(dir, tokens[1:])
	}

	if bif, ok := builtins[curToken]; ok { // check for built-in command
		return walkResult{
			endCommand:      nil,
			status:          foundBuiltin,
			builtinFunc:     bif,
			remainingString: strings.Join(tokens[1:], " "),
		}
	}

	if curToken == ".." { // navigate upward
		dir = up(dir)
		return walk(dir, tokens[1:])
	}

	if curToken == "~" || curToken == "/" { // navigate to root
		dir = dir.Root()
		return walk(dir, tokens[1:])
	}
	// test for a local command
	var invocation *cobra.Command = nil
	for _, c := range dir.Commands() {

		if c.Name() == curToken { // check name
			invocation = c
			clilog.Writer.Debugf("Direct match on %s", invocation.Name())
		} else { // check aliases
			for _, alias := range c.Aliases {
				if alias == curToken {
					invocation = c
					clilog.Writer.Debugf("Alias match on %s", invocation.Name())
					break
				}
			}
		}
		if invocation != nil {
			break
		}
	}

	// check if we found a match
	if invocation == nil {
		// user request unhandlable
		return walkResult{
			endCommand:      nil,
			status:          invalidCommand,
			errString:       fmt.Sprintf("unknown command '%s'. Press F1 or type 'help' for relevant commands.", curToken),
			remainingString: strings.Join(tokens[1:], " "),
		}
	}

	// split on action or nav
	if action.Is(invocation) {
		return walkResult{
			endCommand:      invocation,
			status:          foundAction,
			remainingString: strings.Join(tokens[1:], " "),
		}
	} else { // nav
		// navigate given path
		dir = invocation
		return walk(dir, tokens[1:])
	}
}
