/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package refreshmyinfo defines a basic action to re-fetch the user info (MyInfo) associated to the connection, updating the local state.
*/
package refreshmyinfo

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	use   string = "refresh"
	short string = "forcefully ensure your user info is up to date."
	long  string = "Refresh re-caches your user info, pulling any remote changes." +
		"Only useful if your account has had remote changes since the beginning of this session."
)

var aliases []string = []string{}

func NewUserRefreshMyInfoAction() action.Pair {
	return scaffold.NewBasicAction(use, short, long, aliases,
		func(*cobra.Command, *pflag.FlagSet) (string, tea.Cmd) {
			if err := connection.RefreshCurrentUser(); err != nil {
				return "Failed to refresh user info: " + err.Error(), nil
			}

			return "User info refreshed.", nil
		}, nil)
}
