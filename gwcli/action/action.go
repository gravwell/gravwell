/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
The action package tests and maintains the action map, which bolts subroutines onto Actions (leaves)
in the cobra command tree so Mother can call them interactively. The action map is not utilized when
gwcli is run from a Cobra context/non-interactively.

Each Action's Model is implemented and *instantiated* in its own package
(ex: tree/tools/macros/create) and added to the map as part of the tree's assembly.

When that cobra.Command is invoked interactively, Mother uses the action map to find the bolted-on
subroutines to supplant her own Update and View subroutines until the action is `Done()`. Reset() is
used to clear the done status and any other no-longer-relevant data so the action can be invoked
again cleanly. This is required because actions are only ever instantiated once, each, at start up.

Utilize the boilerplate actions in utilities/scaffold where possible.
*/
package action

import (
	"errors"
	"github.com/gravwell/gravwell/v3/gwcli/group"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	ErrNotAnAction = errors.New("given command is not an action")
)

// See CONTRIBUTING.md for more information on each of these subroutines.
type Model interface {
	Update(msg tea.Msg) tea.Cmd // action processing
	View() string               // action displaying
	Done() bool                 // should Mother reassert control?
	Reset() error               // clean up action post-run
	SetArgs(*pflag.FlagSet, []string) (invalid string, onStart tea.Cmd, err error)
}

// Duple used to construct the Action Map.
// Associates the Action command with its bolted-on Model subroutines to facillitate interactivity.
type Pair struct {
	Action *cobra.Command // the base model
	Model  Model          // our bolted-on interactivity
}

//#region action map

// Our singleton variable, accessed via Public subrotines below.
// Maps key(command) -> Model.
var actions = map[string]Model{}

// GetModel returns the Model subroutines associated to the given Action.
func GetModel(c *cobra.Command) (m Model, err error) {
	if !Is(c) {
		return nil, ErrNotAnAction
	}
	return actions[key(c)], nil
}

// AddModel adds a new action and its subroutines to the action map
func AddModel(c *cobra.Command, m Model) {
	actions[key(c)] = m
}

// The internal "hashing' logic to reliably generate a string key associated
// to a command.
func key(c *cobra.Command) string {
	var parentName string = "~"
	if c.Parent() != nil {
		parentName = c.Parent().Name()
	}
	return parentName + "/" + c.Name()
}

//#endregion

// Given a cobra.Command, returns whether it is an Action (and thus can supplant
// Mother's Elm cycle) or a Nav.
func Is(cmd *cobra.Command) bool {
	if cmd == nil { // sanity check
		panic("cmd cannot be nil!")
	}
	// does not `return cmd.GroupID == treeutils.ActionID` to facilitate sanity check
	switch cmd.GroupID {
	case group.ActionID:
		return true
	case group.NavID:
		return false
	default: // sanity check
		panic("cmd '" + cmd.Name() + "' is neither a nav nor an action!")
	}
}
