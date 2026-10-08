/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package logout defines a simple logout action that logs out the current user and ends the program
package logout

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"
)

func NewAction() action.Pair {
	const (
		use   string = "logout"
		short string = "logout and end the session"
		long  string = "Ends your current session and invalids your login token, forcing the next" +
			" login to request credentials."
	)
	return scaffold.NewBasicAction(use, short, long,
		func(*pflag.FlagSet) (string, tea.Cmd, error) {
			if err := connection.Client.Logout(); err != nil {
				clilog.Writer.Warnf("failed to log out: %v", err)
			}
			connection.End()

			// Make sure we destroy the cached token so a stale/invalidated session can never be
			// re-used on the next login attempt.
			if err := connection.DestroyTokenFile(cfgdir.DefaultTokenPath); err != nil {
				clilog.Writer.Warnf("failed to remove cached token file: %v", err)
			}

			return "Successfully logged out", tea.Quit, nil
		},
		scaffold.BasicOptions{})
}
