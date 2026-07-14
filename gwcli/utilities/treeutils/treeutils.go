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
	"errors"
	"fmt"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/group"
	"github.com/gravwell/gravwell/v4/gwcli/internal/annotations"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/utils"

	"github.com/spf13/cobra"
)

type NodeOptions struct {
	// other names this nav can be called under
	CommandAliases []string
	// this command can only be invoked by admins
	AdminOnly bool
}

// ApplyNodeOptions installs a NodeOptions struct into a given command.
//
// ! It is called automatically by GenerateNav and GenerateAction
func ApplyNodeOptions(cmd *cobra.Command, nopts NodeOptions) {
	if cmd == nil {
		clilog.Writer.Warn("cannot apply annotations to a nil command")
		return
	}
	if nopts.AdminOnly {
		annotations.AdminOnly(cmd)
	}
	cmd.Aliases = utils.Deduplicate(append(cmd.Aliases, nopts.CommandAliases...))

}

// GenerateNav creates and returns a Nav (tree node) with every sub-nav and sub-action installed (and the latter registered with the action map).
// If this Nav is marked as AdminOnly, all descendents will be, too.
func GenerateNav(use, short, long string, navCmds []*cobra.Command, actionCmds []action.Pair, opts ...NodeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     strings.ToLower(use),
		Short:   strings.ToLower(short),
		Long:    long,
		GroupID: group.NavID,
		RunE:    NavRun,
	}

	if len(opts) > 0 {
		ApplyNodeOptions(cmd, opts[0])
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
				fmt.Fprintf(c.OutOrStdout(), "%s %s", c.Name(), ft.MutuallyExclusive(kids...))
			} else {
				fmt.Fprintf(c.OutOrStdout(), "%s [subcommand]", c.Name())
			}

			return nil
		},
	)

	// associate groups available to this (and all) navs
	group.AddNavGroup(cmd)
	group.AddActionGroup(cmd)

	ao := annotations.IsAdminOnly(cmd)

	// associate subcommands; if this nav is admin only, everything beneath it should also be admin only
	for _, sub := range navCmds {
		if ao {
			annotations.AdminOnly(sub)
		}
		cmd.AddCommand(sub)
	}
	for _, sub := range actionCmds {
		if ao {
			annotations.AdminOnly(sub.Action)
		}
		cmd.AddCommand(sub.Action)
		// now that the commands have a parent, add their models to map
		action.AddModel(sub.Action, sub.Model)
	}

	return cmd
}

type GenerateActionOptions struct {
	NodeOptions
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
func GenerateAction(use, short, long string,
	runEFunc func(*cobra.Command, []string) error, opts ...GenerateActionOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    long,
		GroupID: group.ActionID,
		RunE:    runEFunc,
	}

	// possibly overwritten by options
	cmd.SetUsageFunc(func(c *cobra.Command) error {
		fmt.Fprintf(c.OutOrStdout(), "%s %s", cmd.Name(), ft.Optional("flags"))
		return nil
	})

	// apply options
	if len(opts) > 0 {
		ApplyNodeOptions(cmd, opts[0].NodeOptions)
		if usage := strings.TrimSpace(opts[0].Usage); usage != "" {
			cmd.SetUsageFunc(func(c *cobra.Command) error {
				fmt.Fprintf(c.OutOrStdout(), "%s %s", cmd.Name(), opts[0].Usage)
				return nil
			})
		}
		if ex := strings.TrimSpace(opts[0].Example); ex != "" {
			cmd.Example = cmd.Name() + " " + opts[0].Example
		}
	}

	cmd.SilenceUsage = true
	return cmd
}

// NavRun is the Run function for all Navs (nodes).
// It checks for the --no-interactive flag and initializes Mother with the command as her pwd if script is unset.
var NavRun = func(cmd *cobra.Command, args []string) error {
	noInteractive, err := cmd.Flags().GetBool(ft.NoInteractive.Name())
	if err != nil {
		return err
	}
	if noInteractive {
		cmd.Help()
		return nil
	}

	if len(args) > 0 { // NavRun was called, but extra tokens were found, so something went wrong
		return errors.New(args[0] + " is not a valid builtin or subcommand")
	}

	// invoke mother
	return mother.Spawn(cmd.Root(), cmd, []string{})
}
