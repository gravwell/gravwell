/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package tree defines a basic action that simply displays the command structure of gwcli using the lipgloss tree functionality.
*/
package tree

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/group"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss/tree"
	"github.com/spf13/cobra"
)

func NewTreeAction() action.Pair {
	const (
		use   string = "tree"
		short string = "display all commands as a tree"
		long  string = "Displays a directory-tree showing the full structure of gwcli and all" +
			"available actions."
	)
	return scaffold.NewBasicAction(use, short, long,
		func(c *cobra.Command) (string, tea.Cmd) {
			lgt := walkBranch(c.Root())

			return lgt.String(), nil
		}, scaffold.BasicOptions{})
}

func walkBranch(nav *cobra.Command) *tree.Tree {
	// generate a new tree, stemming from the given nav
	branchRoot := tree.New()

	navSty := stylesheet.Cur.Nav
	actionSty := stylesheet.Cur.Action //.PaddingLeft(1)

	branchRoot.Root(navSty.Render(nav.Name()))
	branchRoot.EnumeratorStyle(stylesheet.Cur.PrimaryText.PaddingLeft(1))

	// add children of this nav to its tree
	for _, child := range nav.Commands() {
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
