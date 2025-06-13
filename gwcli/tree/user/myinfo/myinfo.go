/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package myinfo defines a simple action to fetch information about the current user.
package myinfo

import (
	"fmt"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/utils/weave"
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
				return weave.ToCSV([]types.UserDetails{connection.CurrentUser()}, []string{
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

			inf := connection.CurrentUser()

			sty := stylesheet.Sheet.PrimaryText.Bold(false)
			out := fmt.Sprintf("%v, %v, %v\n%s: %v\n%s: %v\n%s: %v",
				inf.Name,
				inf.User, inf.Email,
				sty.Render("Groups"), inf.Groups,
				sty.Render("Capabilities"), inf.CapabilityList(),
				sty.Render("Admin"), inf.Admin)

			return out, nil
		}, flags)
}

func flags() pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.Bool(ft.Name.CSV, false, "display results as CSV")
	return fs
}
