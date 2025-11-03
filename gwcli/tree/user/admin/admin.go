/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package admin defines a basic action to allow users to manipulate their admin status.
package admin

import (
	"fmt"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewUserAdminAction() action.Pair {
	const (
		use   string = "admin"
		short string = "display or modify your admin status"
		long  string = "If called bare, admin displays whether or not you are an admin (and thus can enter admin mode).\n" +
			"Use -t to toggle your admin status, which will attach admin=true to future queries.\n" +
			"Exercise caution in admin mode, as it gives access to objects belonging to other users and makes it easy to break things.\n" +
			"Admin mode does not persist between sessions."
	)
	return scaffold.NewBasicAction(use, short, long,
		func(_ *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
			isAdministrator, err := connection.Client.IsAdmin()
			if err != nil {
				return "failed to fetch administrator status: " + err.Error(), nil
			}

			// branch on toggle flag
			if t, err := fs.GetBool("toggle"); err != nil {
				clilog.LogFlagFailedGet("toggle", err)
				return uniques.ErrGeneric.Error(), nil
			} else if t {
				return toggle(isAdministrator)
			}

			// display state
			inAdminMode := connection.Client.AdminMode()
			if isAdministrator {
				var not string
				if !inAdminMode {
					not = " not"
				}
				return "You are an administrator.\n" + "You are in" + not + " admin mode.", nil
			}
			var s = "You are not an administrator."
			if inAdminMode {
				s += "\nYet, you are somehow in admin mode.\nYour admin mode flag will be ignored. Please file a bug report."
			}
			return s, nil
		},
		scaffold.BasicOptions{
			AddtlFlagFunc: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.BoolP("toggle", "t", false, "toggle your admin status")
				return fs
			},
			CmdMods: func(c *cobra.Command) {
				// NOTE(rlandau): admin mode is hidden by default, as it is currently only effectual while Mother is running.
				// Mother.Spawn reveals the command
				c.Hidden = true

				c.SetUsageFunc(func(c *cobra.Command) error {
					fmt.Fprint(c.OutOrStdout(), use+" "+ft.Optional("-t"))
					return nil
				})
			},
		})
}

func toggle(isAdministrator bool) (string, tea.Cmd) {
	if !isAdministrator {
		return "Only administrators can toggle admin mode", nil
	}
	if !connection.Client.AdminMode() {
		connection.Client.SetAdminMode()
		return "You are now in admin mode", nil
	}
	connection.Client.ClearAdminMode()
	return "You are no longer in admin mode", nil

}
