/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// A simple action to tell the user whether or not they are logged in as an admin.
package admin

import (
	"fmt"
	"github.com/gravwell/gravwell/v3/gwcli/action"
	"github.com/gravwell/gravwell/v3/gwcli/connection"
	"github.com/gravwell/gravwell/v3/gwcli/utilities/scaffold"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	use   string = "admin"
	short string = "prints your admin status"
	long  string = "Displays whether or not your current user has admin permissions."
)

var aliases []string = []string{}

func NewUserAdminAction() action.Pair {
	p := scaffold.NewBasicAction(use, short, long, aliases,
		func(*cobra.Command, *pflag.FlagSet) (string, tea.Cmd) {
			var not string
			// todo what is the difference re: MyAdminStatus?
			if !connection.Client.AdminMode() {
				not = " not"
			}
			return fmt.Sprintf("You are%v in admin mode", not), nil
		}, nil)
	return p
}
