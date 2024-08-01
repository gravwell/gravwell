/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package myinfo

import (
	"fmt"
	"gwcli/action"
	"gwcli/clilog"
	"gwcli/connection"
	"gwcli/stylesheet"
	ft "gwcli/stylesheet/flagtext"
	"gwcli/utilities/scaffold"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/gravwell/gravwell/v3/utils/weave"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	use   string = "myinfo"
	short string = "information about the current user and session"
	long  string = "Displays your account's information and capabilities."
)

var aliases []string = []string{}

func NewUserMyInfoAction() action.Pair {

	return scaffold.NewBasicAction(use, short, long, aliases,
		func(_ *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
			if asCSV, err := fs.GetBool(ft.Name.CSV); err != nil {
				s := fmt.Sprintf("Failed to fetch csv flag: %v", err)
				clilog.Writer.Error(s)
				return s, nil
			} else if asCSV {
				return weave.ToCSV([]types.UserDetails{connection.MyInfo}, []string{
					"UID",
					"User",
					"Name",
					"Email",
					"Admin",
					"Locked",
					"TS",
					"DefaultGID",
					"Groups",
					"Hash",
					"Synced",
					"CBAC"}), nil
			}

			sty := stylesheet.Header1Style.Bold(false)
			out := fmt.Sprintf("%v, %v, %v\n%s: %v\n%s: %v\n%s: %v",
				connection.MyInfo.Name,
				connection.MyInfo.User, connection.MyInfo.Email,
				sty.Render("Groups"), connection.MyInfo.Groups,
				sty.Render("Capabilities"), connection.MyInfo.CapabilityList(),
				sty.Render("Admin"), connection.MyInfo.Admin)

			return out, nil
		}, flags)
}

func flags() pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.Bool(ft.Name.CSV, false, "display results as CSV")
	return fs
}
