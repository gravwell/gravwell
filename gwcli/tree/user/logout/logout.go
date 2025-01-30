/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// A simple logout action that logs out the current user and ends the program
package logout

import (
	"github.com/gravwell/gravwell/v3/gwcli/action"
	"github.com/gravwell/gravwell/v3/gwcli/connection"
	"github.com/gravwell/gravwell/v3/gwcli/utilities/scaffold"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	use   string = "logout"
	short string = "logout and end the session"
	long  string = "Ends your current session and invalids your login token, forcing the next" +
		" login to request credentials."
)

var aliases []string = []string{}

func NewUserLogoutAction() action.Pair {
	return scaffold.NewBasicAction(use, short, long, aliases,
		func(*cobra.Command, *pflag.FlagSet) (string, tea.Cmd) {
			connection.Client.Logout()
			connection.End()

			return "Successfully logged out", tea.Quit
		}, nil)
}
