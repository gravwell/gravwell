/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package admin defines a simple action to tell the user whether or not they are logged in as an admin.
package admin

import (
	"fmt"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func NewUserAdminAction() action.Pair {
	const (
		use   string = "admin"
		short string = "prints your admin status"
		long  string = "Displays whether or not your current user has admin permissions."
	)
	return scaffold.NewBasicAction(use, short, long,
		func(*cobra.Command) (string, tea.Cmd) {
			var not string
			// todo what is the difference re: MyAdminStatus?
			if !connection.Client.AdminMode() {
				not = " not"
			}
			return fmt.Sprintf("You are%v in admin mode", not), nil
		}, scaffold.BasicOptions{})
}
