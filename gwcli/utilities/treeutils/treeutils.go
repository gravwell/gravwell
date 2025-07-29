/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package treeutils provides functions for creating the cobra command tree.
// It has been extracted into its own package to avoid import cycles.
package treeutils

import (
	"fmt"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/group"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"

	"github.com/spf13/cobra"
)

// GenerateNav creates and returns a Nav (tree node) that can now be assigned subcommands (child navs and actions).
// It is responsible for adding each of its Actions to the action map.
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

	cmd.SetUsageFunc(
		func(c *cobra.Command) error {
			if c.HasSubCommands() {
				// select the first few children.
				subCmds := c.Commands()
				// if there are more, suffix an ellipse
				kids := make([]string, min(4, len(subCmds)))
				for i, c := range subCmds {
					if i > 2 {
						kids[3] = "..."
						break
					}
					kids[i] = stylesheet.ColorCommandName(c)
				}

				fmt.Fprintf(c.OutOrStdout(), "%s %s", c.Name(), ft.MutuallyExclusive(kids))

			} else {
				fmt.Fprintf(c.OutOrStdout(), "%s [subcommand]", c.Name())

			}

			return nil
		},
	)

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

type GenerateActionOptions struct {
	// Sets the general form of this command (the usage).
	// Use is already prefixed; no need to include it or a path in the example.
	// Printed in the form: "Usage: <command.Name> <Usage>"
	Usage string
	// Sets the example on the command.
	// Use is already prefixed; no need to include it or a path in the example.
	// Printed in the form: "Example: <command.Name> <Example>"
	Example string
}

// GenerateAction returns a boilerplate action command with all required information for it to be fed into action.NewPair().
// Basically just a form of cobra.Command constructor.
//
// Accepts 0 or 1 GenerateActionOptions; any more are ignored.
//
// ! Does NOT add this action to the action map or add the Action to a parent.
func GenerateAction(use, short, long string, aliases []string,
	runFunc func(*cobra.Command, []string), options ...GenerateActionOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    long,
		Aliases: aliases,
		GroupID: group.ActionID,
		Run:     runFunc,
	}

	// possibly overwritten by options
	cmd.SetUsageFunc(func(c *cobra.Command) error {
		fmt.Fprintf(c.OutOrStdout(), "%s %s", cmd.Name(), ft.Optional("flags"))
		return nil
	})

	// apply options
	if len(options) > 0 {
		if usage := strings.TrimSpace(options[0].Usage); usage != "" {
			cmd.SetUsageFunc(func(c *cobra.Command) error {
				fmt.Fprintf(c.OutOrStdout(), "%s %s", cmd.Name(), options[0].Usage)
				return nil
			})
		}
		if ex := strings.TrimSpace(options[0].Example); ex != "" {
			cmd.Example = cmd.Name() + " " + options[0].Example
		}
	}

	cmd.SilenceUsage = true
	return cmd
}

// NavRun is the Run function for all Navs (nodes).
// It checks for the --no-interactive flag and initializes Mother with the command as her pwd if script is unset.
var NavRun = func(cmd *cobra.Command, args []string) {
	noInteractive, err := cmd.Flags().GetBool(ft.NoInteractive.Name())
	if err != nil {
		panic(err)
	}
	if noInteractive {
		cmd.Help()
		return
	}
	// invoke mother
	if err := mother.Spawn(cmd.Root(), cmd, []string{}); err != nil {
		clilog.Tee(clilog.CRITICAL, cmd.ErrOrStderr(),
			"failed to spawn a mother instance: "+err.Error())
	}
}
