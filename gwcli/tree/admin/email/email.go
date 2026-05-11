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
	"strconv"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/validate"
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
	return scaffoldcreate.NewCreateAction("configuration",
		map[string]scaffoldcreate.Field{
			"server": {
				Title:    "Server",
				Required: true,
				Flag: scaffoldcreate.FlagConfig{
					Name:  "email-server",
					Usage: "the host connection string to reach the mail server", // TODO
				},
				Order:    200,
				Provider: &scaffoldcreate.TextProvider{},
			},
			"user": {
				Title:    "Username",
				Required: true,
				Flag: scaffoldcreate.FlagConfig{
					Name:  "email-username",
					Usage: "the username to authenticate with the email server as",
				},
				Order:    180,
				Provider: &scaffoldcreate.TextProvider{},
			},
			"pass": scaffoldcreate.FieldPassword(
				false,
				scaffoldcreate.FlagConfig{
					Name:  "email-password",
					Usage: "the password to authenticate with the email server",
				},
				160),
			"port": {
				Title:    "Port",
				Required: true,
				Flag: scaffoldcreate.FlagConfig{
					Name:  "email-port",
					Usage: "the port by which to access the server ",
				},
				DefaultValue: "587",
				Order:        140,
				Provider: &scaffoldcreate.TextProvider{
					CustomInit: func() textinput.Model {
						ti := stylesheet.NewTI("587", false)
						ti.Validate = func(s string) error {
							_, err := validate.PortNumber(s)
							if err != nil {
								return err
							}
							return nil
						}
						return ti
					},
				},
			},
			"tls": {
				Title: "Use TLS?",
				Flag: scaffoldcreate.FlagConfig{
					Name:  "tls",
					Usage: "Enable TLS encryption for this connection?",
				},
				Order:    120,
				Provider: &scaffoldcreate.BoolProvider{},
			},
			"verifyCerts": {
				Title: "Verify TLS Certs?",
				Flag: scaffoldcreate.FlagConfig{
					Name:  "verify-certificate",
					Usage: "Verify TLS certificates for this connection?",
				},
				Order:    100,
				Provider: &scaffoldcreate.BoolProvider{},
			},
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			var port uint16
			if p, err := strconv.ParseUint(fields["port"].Provider.Get(), 10, 16); err != nil {
				return "", err.Error(), nil
			} else {
				port = uint16(p)
			}
			var tls bool
			if b, err := strconv.ParseBool(fields["tls"].Provider.Get()); err != nil {
				return "", err.Error(), nil
			} else {
				tls = b
			}
			var verifyCerts bool
			if b, err := strconv.ParseBool(fields["verifyCerts"].Provider.Get()); err != nil {
				return "", err.Error(), nil
			} else {
				verifyCerts = b
			}

			return 0, "", connection.Client.ConfigureMail(
				fields["user"].Provider.Get(),
				fields["pass"].Provider.Get(),
				fields["server"].Provider.Get(),
				port,
				tls,
				!verifyCerts,
			)
			// TODO where/when does validation occur if we call this non-interactively?
		},
		// TODO prepop fields with default values
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{},
			Short:         "configure email settings",
			Long:          "Set the SMTP server settings used for sending email notifications.",
		})
}

func deleteConfig() action.Pair {
	return scaffold.NewBasicAction("delete", "remove email configuration", "Remove the current email/SMTP configuration for your user.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			if err := connection.Client.DeleteMailConfig(); err != nil {
				return err.Error(), nil
			}
			return "email configuration removed", nil
		},
		scaffold.BasicOptions{})
}
