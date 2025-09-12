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
	borderWidth int = 48
	fieldWidth  int = 14
)

var sectionHeader = func(str string) string { return stylesheet.Cur.TertiaryText.Bold(true).Render(str) }

func NewUserMyInfoAction() action.Pair {
	const (
		use   string = "myinfo"
		short string = "information about the current user and session"
		long  string = "Displays your account's information and capabilities."
	)
	return scaffold.NewBasicAction(use, short, long,
		func(_ *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
			// check for refresh
			if refresh, err := fs.GetBool("refresh"); err != nil {
				clilog.LogFlagFailedGet("refresh", err)
			} else if refresh {
				if err := connection.RefreshCurrentUser(); err != nil {
					clilog.Writer.Warn("failed to refresh local user's information")
				}
			}

			// get our information
			inf := connection.CurrentUser()

			// output as CSV
			if asCSV, err := fs.GetBool(ft.CSV.Name()); err != nil {
				clilog.LogFlagFailedGet(ft.CSV.Name(), err)
			} else if asCSV {
				return weave.ToCSV(
					[]types.User{inf},
					[]string{
						"ID",
						"Username",
						"Name",
						"Email",
						"Admin",
						"Locked",
						"Groups",
					},
					weave.CSVOptions{}), nil
			}

			// output as segmented table

			// compose the body
			body := fmt.Sprintf("%v\n"+
				"%v\n"+
				"%s%v\n"+
				"%s%v\n"+
				"%s%v\n"+
				"%s%v\n"+
				"%s%v",
				inf.Name,
				inf.Email,
				stylesheet.Cur.Field("UserID", fieldWidth), inf.ID,
				stylesheet.Cur.Field("MFA Enabled?", fieldWidth), inf.MFA.MFAEnabled(),
				stylesheet.Cur.Field("Groups", fieldWidth), inf.Groups,
				stylesheet.Cur.Field("Capabilities", fieldWidth), inf.CapabilityList(),
				stylesheet.Cur.Field("Admin", fieldWidth), inf.Admin)
			res, err := stylesheet.SegmentedBorder(stylesheet.Cur.ComposableSty.ComplimentaryBorder.BorderForeground(stylesheet.Cur.PrimaryText.GetForeground()),
				borderWidth,
				struct {
					StylizedTitle string
					Contents      string
				}{sectionHeader(" " + inf.Username + " "), body})
			if err != nil {
				clilog.Writer.Warnf("failed to generate segmented border: %v", err)
			}
			return res, nil
		}, scaffold.BasicOptions{AddtlFlagFunc: flags})
}

func flags() pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.Bool(ft.CSV.Name(), false, "display results as CSV")
	fs.BoolP("refresh", "r", false, "refresh the local user cache prior to display")
	return fs
}
