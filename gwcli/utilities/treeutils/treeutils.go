/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Treeutils provides functions for creating the cobra command tree.
// It has been extracted into its own package to avoid import cycles.
package treeutils

import (
	"gwcli/action"
	"gwcli/clilog"
	"gwcli/group"
	"gwcli/mother"
	"strings"

	"github.com/spf13/cobra"
)

// Creates and returns a Nav (tree node) that can now be assigned subcommands
func GenerateNav(use, short, long string, aliases []string,
	navCmds []*cobra.Command, actionCmds []action.Pair) *cobra.Command {
	cmd := &cobra.Command{
		Use:     strings.ToLower(use),
		Short:   strings.ToLower(short),
		Long:    long,
		Aliases: aliases,
		GroupID: group.NavID,
		Run:     NavRun,
	}

	// associate groups available to this (and all) navs
	group.AddNavGroup(cmd)
	group.AddActionGroup(cmd)

	// associate subcommands
	for _, sub := range navCmds {
		cmd.AddCommand(sub)
	}
	for _, sub := range actionCmds {
		cmd.AddCommand(sub.Action)
		// now that the commands have a parent, add their models to map
		action.AddModel(sub.Action, sub.Model)
	}

	return cmd
}

// Creates and returns an Action (tree leaf) that can be called directly non-interactively or via
// associated methods (actions.Pair) interactively
func GenerateAction(cmd *cobra.Command, act action.Model) action.Pair {
	return action.Pair{Action: cmd, Model: act}
}

// Returns a boilerplate action command that can be fed into GenerateAction.
func NewActionCommand(use, short, long string, aliases []string,
	runFunc func(*cobra.Command, []string)) *cobra.Command {
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    long,
		Aliases: aliases,
		GroupID: group.ActionID,
		Run:     runFunc,
	}

	cmd.SilenceUsage = true

	return cmd
}

// NavRun is the Run function for all Navs (nodes).
// It checks for the --script flag and initializes Mother with the command as her pwd if script is
// unset.
var NavRun = func(cmd *cobra.Command, args []string) {
	script, err := cmd.Flags().GetBool("script")
	if err != nil {
		panic(err)
	}
	if script {
		cmd.Help()
		return
	}
	// invoke mother
	if err := mother.Spawn(cmd.Root(), cmd, []string{}); err != nil {
		clilog.Tee(clilog.CRITICAL, cmd.ErrOrStderr(),
			"failed to spawn a mother instance: "+err.Error())
	}
}
