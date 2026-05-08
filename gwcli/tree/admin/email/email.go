/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package email provides actions for managing email/SMTP configuration.
package email

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	return treeutils.GenerateNav("email", "manage email configuration",
		"Configure the SMTP settings used to send notifications.",
		nil, nil,
		[]action.Pair{
			show(),
			configure(),
			deleteConfig(),
		})
}

func show() action.Pair {
	return scaffoldlist.NewListAction("display email configuration", "Display the current email/SMTP configuration.",
		types.UserMailConfig{},
		func(fs *pflag.FlagSet) ([]types.UserMailConfig, error) {
			mc, err := connection.Client.MailConfig()
			if err != nil {
				return nil, err
			}
			return []types.UserMailConfig{mc}, nil
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{Use: "show"},
			Pretty: func(DQColumns []string, DQToAlias map[string]string) (string, error) {
				mc, err := connection.Client.MailConfig()
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Server: %s\nPort: %d\nUsername: %s\nUseTLS: %v\nInsecureSkipVerify: %v",
					mc.Server, mc.Port, mc.Username, mc.UseTLS, mc.InsecureSkipVerify), nil
			},
		})
}

func configure() action.Pair {
	return scaffold.NewBasicAction("configure", "configure email settings",
		"Set the SMTP server settings used for sending email notifications.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			server, err := fs.GetString("server")
			if err != nil {
				clilog.LogFlagFailedGet("server", err)
				return "failed to get server flag", nil
			}
			port, err := fs.GetUint16("port")
			if err != nil {
				clilog.LogFlagFailedGet("port", err)
				return "failed to get port flag", nil
			}
			username, err := fs.GetString("username")
			if err != nil {
				clilog.LogFlagFailedGet("username", err)
				return "failed to get username flag", nil
			}
			password, err := fs.GetString("password")
			if err != nil {
				clilog.LogFlagFailedGet("password", err)
				return "failed to get password flag", nil
			}
			useTLS, err := fs.GetBool("tls")
			if err != nil {
				clilog.LogFlagFailedGet("tls", err)
				return "failed to get tls flag", nil
			}
			noVerify, err := fs.GetBool("no-verify")
			if err != nil {
				clilog.LogFlagFailedGet("no-verify", err)
				return "failed to get no-verify flag", nil
			}
			if err := connection.Client.ConfigureMail(username, password, server, port, useTLS, noVerify); err != nil {
				return err.Error(), nil
			}
			return "email configuration updated", nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.String("server", "", "SMTP server address")
					fs.Uint16("port", 0, "SMTP server port")
					fs.String("username", "", "SMTP username")
					fs.String("password", "", "SMTP password")
					fs.Bool("tls", false, "use TLS")
					fs.Bool("no-verify", false, "skip TLS verification")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				server, err := fs.GetString("server")
				if err != nil {
					clilog.LogFlagFailedGet("server", err)
				}
				if server == "" {
					return "--server must be non-empty", nil
				}
				port, err := fs.GetUint16("port")
				if err != nil {
					clilog.LogFlagFailedGet("port", err)
				}
				if port == 0 {
					return "--port must be nonzero", nil
				}
				return "", nil
			},
		})
}

func deleteConfig() action.Pair {
	return scaffold.NewBasicAction("delete", "remove email configuration", "Remove the current email/SMTP configuration.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			if err := connection.Client.DeleteMailConfig(); err != nil {
				return err.Error(), nil
			}
			return "email configuration removed", nil
		},
		scaffold.BasicOptions{})
}
