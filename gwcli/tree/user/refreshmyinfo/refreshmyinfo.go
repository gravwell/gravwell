/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Re-fetches the cached user info (MyInfo) associated to the connection
*/
package refreshmyinfo

import (
	"github.com/gravwell/gravwell/v3/gwcli/action"
	"github.com/gravwell/gravwell/v3/gwcli/connection"
	"github.com/gravwell/gravwell/v3/gwcli/utilities/scaffold"

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
			mi, err := connection.Client.MyInfo()
			if err != nil {
				return "Failed to refresh user info: " + err.Error(), nil
			} else {
				connection.MyInfo = mi
			}

			return "User info refreshed.", nil
		}, nil)
}
