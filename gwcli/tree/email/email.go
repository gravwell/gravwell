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
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/tree/email/send"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/validate"
	"github.com/gravwell/gravwell/v4/ingest/log"
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
			send.NewPair(),
		})
}

func show() action.Pair {
	return scaffoldlist.NewListAction("display email configuration", "Display the current email/SMTP configuration.",
		types.UserMailConfig{},
		func(_ *pflag.FlagSet, _ scaffoldlist.DataParameters) ([]types.UserMailConfig, error) {
			mc, err := connection.Client.MailConfig()
			return []types.UserMailConfig{mc}, err
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{Use: "show"},
			Pretty: func(_ *pflag.FlagSet, DQColumns []string, DQToAlias map[string]string, _ scaffoldlist.DataParameters) (string, error) {
				mc, err := connection.Client.MailConfig()
				if err != nil {
					return "", err
				}
				if mc.Server == "" {
					return "you do not have a mail server configured", nil
				}
				return fmt.Sprintf("Server: %s\nPort: %d\nUsername: %s\nUseTLS: %v\nInsecureSkipVerify: %v",
					mc.Server, mc.Port, mc.Username, mc.UseTLS, mc.InsecureSkipVerify), nil
			},
			QueryOptionsFlags: scaffold.QOOmit{
				Everything: true,
			},
		})
}

const cacheStale = 3 * time.Second

// Used to hit the backend only once to cache fields.
// Set by the first field called during SetArgs
var (
	curEmailCfg     types.UserMailConfig
	curEmailCfgTime time.Time
	curEmailMu      sync.Mutex
)

// fetches the current user's mail configuration from the backend iff it is stale.
func getCurEmailCfg() types.UserMailConfig {
	curEmailMu.Lock()
	defer curEmailMu.Unlock()
	if time.Since(curEmailCfgTime) > cacheStale { // re-cache
		curEmailCfgTime = time.Now()
		var err error
		curEmailCfg, err = connection.Client.MailConfig()
		if err != nil {
			clilog.Writer.Warn("failed to cache mail config", log.KVErr(err))
		}
	}
	return curEmailCfg
}

func configure() action.Pair {
	return scaffoldcreate.NewCreateAction("configuration",
		map[string]scaffoldcreate.Field{
			"server": {
				Title:    "Server",
				Required: true,
				Flag: scaffoldcreate.FlagConfig{
					Name:  "email-server",
					Usage: "the host connection string to reach the mail server",
				},
				Order: 200,
				Provider: &scaffoldcreate.TextProvider{
					CustomSetArgs: func(m textinput.Model) textinput.Model {
						m.SetValue(getCurEmailCfg().Server)
						return m
					},
				},
			},
			"user": {
				Title:    "Username",
				Required: true,
				Flag: scaffoldcreate.FlagConfig{
					Name:  "email-username",
					Usage: "the username to authenticate with the email server as",
				},
				Order: 180,
				Provider: &scaffoldcreate.TextProvider{
					CustomSetArgs: func(m textinput.Model) textinput.Model {
						m.SetValue(getCurEmailCfg().Username)
						return m
					},
				},
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
					CustomSetArgs: func(m textinput.Model) textinput.Model {
						cur := getCurEmailCfg()
						if cur.Server != "" {
							m.SetValue(strconv.FormatInt(int64(cur.Port), 10))
						}
						return m
					},
				},
			},
			"tls": {
				Title: "Use TLS?",
				Flag: scaffoldcreate.FlagConfig{
					Name:  "tls",
					Usage: "Enable TLS encryption for this connection?",
				},
				Order: 120,
				Provider: &scaffoldcreate.BoolProvider{CustomSetArgs: func() bool {
					return getCurEmailCfg().UseTLS
				}},
			},
			"verifyCerts": {
				Title: "Verify TLS Certs?",
				Flag: scaffoldcreate.FlagConfig{
					Name:  "verify-certificate",
					Usage: "Verify TLS certificates for this connection?",
				},
				Order: 100,
				Provider: &scaffoldcreate.BoolProvider{CustomSetArgs: func() bool {
					cur := getCurEmailCfg()
					// only bother to set if a configuration exists at all
					return cur.Server != "" && !cur.InsecureSkipVerify
				}},
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

			// to prevent clobbering the password, do not make an update if everything is the same and password is empty
			if cur, err := connection.Client.MailConfig(); err != nil {
				return nil, "", fmt.Errorf("failed to check current mail configuration: %w", err)
			} else if cur.Username == fields["user"].Provider.Get() &&
				cur.Password == "" &&
				cur.Server == fields["server"].Provider.Get() &&
				cur.Port == int(port) &&
				cur.UseTLS == tls && cur.InsecureSkipVerify == !verifyCerts {
				clilog.Writer.Info("no changes made")
				return nil, "", nil
			}

			clilog.Writer.Info("updating email configuration...")
			return nil, "", connection.Client.ConfigureMail(
				fields["user"].Provider.Get(),
				fields["pass"].Provider.Get(),
				fields["server"].Provider.Get(),
				port,
				tls,
				!verifyCerts,
			)
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "configure",
				Short:   "configure email settings",
				Long:    "Set the SMTP server settings used for sending email notifications.",
				Aliases: []string{"add", "create", "update"},
			},
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
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Aliases: []string{"uninstall", "remove"},
			},
		})
}
