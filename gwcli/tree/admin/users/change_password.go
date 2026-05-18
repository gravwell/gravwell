package users

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// This file implements the interactive change-password action for admin users.

type cpStage uint

const (
	cpStgSelectUser cpStage = iota // select a user
	cpStgPassword                  // enter new password
	cpStgDone                      // completed
)

func changePassword() action.Pair {
	cmd := treeutils.GenerateAction("change-password", "change a user's password",
		"Change a user's password without requiring their current password. "+
			"Non-interactive mode can take the password in clear as --new-password. "+
			"If you prefer to keep the password out of your history, consider using --new-passfile",
		nil,
		func(c *cobra.Command, args []string) error {
			uid, err := c.Flags().GetInt32("uid")
			if err != nil {
				clilog.GetFlag(err)
			}
			password, err := c.Flags().GetString("password")
			if err != nil {
				clilog.GetFlag(err)
			}

			// if both flags are provided, run non-interactively
			if uid != 0 && password != "" {
				if err := connection.Client.AdminChangePass(uid, password); err != nil {
					return err
				}
				fmt.Fprintf(c.OutOrStdout(), "successfully changed password for user %d\n", uid)
				return nil
			}

			// otherwise, boot interactive mode
			ni, err := c.Flags().GetBool(ft.NoInteractive.Name())
			if err != nil {
				clilog.GetFlag(err)
				ni = true
			}
			if !ni {
				return mother.Spawn(c.Root(), c, args)
			}
			if uid == 0 {
				return errors.New("--uid must be set and nonzero")
			}
			return errors.New("--password must be non-empty")
		},
	)

	cmd.Flags().AddFlagSet(cpFlags())

	return action.NewPair(cmd, &changePasswordModel{})
}

func cpFlags() *pflag.FlagSet {
	fs := &pflag.FlagSet{}
	ft.UID.Register(fs)
	fs.String("new-password", "", "the new password to assign the user. Mutually exclusive with --new-passfile.")
	fs.String("new-passfile", "", "reads the users new password from the given path. Mutually exclusive with --new-password.")
	return fs
}

// getPasswordFromFlags attempts to fetch the new password from --new-password and --new-passfile.
// Returns an error if both are set are passfile was set and failed to read from.
// Return "", nil if neither is set.
func getPasswordFromFlags(fs *pflag.FlagSet) (password string, err error) {
	pass, err := fs.GetString("new-password")
	if err != nil {
		clilog.GetFlag(err)
	}
	pf, err := fs.GetString("new-passfile")
	if err != nil {
		clilog.GetFlag(err)
	}
	// we don't set defaults here, so any value in either means changed
	if pass != "" && pf != "" {
		return "", errors.New("--new-password and --new-passfile are mutually exclusive")
	}
	if pass != "" {
		return pass, nil
	}

	if pf != "" {
		b, err := os.ReadFile(pf)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}

	return "", nil
}

//#region interactive

type changePasswordModel struct {
	users      list.Model
	passwordTI textinput.Model
	stage      cpStage

	selectedUser types.User
}

func (m *changePasswordModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	// attach and check flags
	fs := cpFlags()
	if err := fs.Parse(tokens); err != nil {
		return "", nil, err
	}

	// if a password and UID were provided, we can operate without interactive mode.
	uid, err := fs.GetInt32(ft.UID.Name())
	if err != nil {
		clilog.GetFlag(err)
	}
	pass, err := getPasswordFromFlags(fs)
	if err != nil {
		return err.Error(), nil, nil
	}
	if uid != 0 && pass != "" {
		if err := connection.Client.AdminChangePass(m.selectedUser.ID, pass); err != nil {
			return "", nil, err
		}
		m.stage = cpStgDone

		return "", tea.Printf("successfully changed password for user ID: %d", uid), nil
	}

	m.passwordTI.SetValue(pass)

	// fetch all users
	users, err := connection.Client.ListUsers(nil)
	if err != nil {
		clilog.Writer.Error("failed to get the list of users", log.KV("error", err))
		return "", nil, fmt.Errorf("failed to get the list of users")
	}
	var itms = make([]list.Item, 0, len(users.Results))
	for _, user := range users.Results {
		if user.ID == connection.CurrentUser().ID {
			continue
		}
		itms = append(itms, listitem.NewUserItem(user, false))
	}
	itms = slices.Clip(itms)
	if len(itms) == 0 { // TODO ensure we have self change-password set
		return "there are no other users. If you want to change your own password, use " + stylesheet.Cur.Action.Render("~ self change-password"), nil, nil
	}

	m.users = stylesheet.NewList(itms, width, height, "user", "users")

	// set up password text input
	m.passwordTI = stylesheet.NewTI("", false)
	m.passwordTI.EchoMode = textinput.EchoPassword
	m.passwordTI.Placeholder = "enter new password"
	m.passwordTI.Width = 40
	m.passwordTI.Blur()

	return "", nil, nil
}

func (m *changePasswordModel) Update(msg tea.Msg) (cmd tea.Cmd) {
	switch m.stage {
	case cpStgSelectUser:
		// check for an invoke, otherwise, just pass to update
		if hotkeys.Match(msg, hotkeys.Invoke) {
			i := m.users.SelectedItem()
			if i == nil {
				clilog.Writer.Error("selected item is nil!",
					log.KV("global index", m.users.GlobalIndex()),
					log.KV("index", m.users.Index()),
					log.KV("items", m.users.Items()),
				)
				return tea.Println(clilog.ErrInternal{}.Error())
			}
			liu, ok := i.(*listitem.User)
			if !ok {
				m.stage = cpStgDone
				return tea.Println(clilog.TypeAssert(i, &listitem.User{}))
			}
			m.selectedUser = liu.U
			m.stage = cpStgPassword
			m.passwordTI.Focus()
			return textinput.Blink
		}

		m.users, cmd = m.users.Update(msg)
		return cmd
	case cpStgPassword:
		// handle enter to submit
		if hotkeys.Match(msg, hotkeys.Invoke) {
			password := m.passwordTI.Value()
			if password == "" {
				// TODO update the submit button with the error
				return nil // ignore empty submissions
			}
			if err := connection.Client.AdminChangePass(m.selectedUser.ID, password); err != nil {
				clilog.Writer.Error("failed to change password", log.KV("uid", m.selectedUser.ID), log.KVErr(err))
				m.stage = cpStgDone
				return tea.Printf("failed to change password for user '%d': %v", m.selectedUser.ID, err)
			}
			m.stage = cpStgDone
			return tea.Printf("successfully changed password for user '%s' (ID: %d)", m.selectedUser.Username, m.selectedUser.ID)
		}
		// handle esc to cancel
		if hotkeys.Match(msg, hotkeys.SoftQuit) {
			m.stage = cpStgDone
			return tea.Println("cancelled")
		}
		m.passwordTI, cmd = m.passwordTI.Update(msg)
	}
	return cmd
}

func (m *changePasswordModel) View() string {
	switch m.stage {
	case cpStgSelectUser:
		return m.users.View()
	case cpStgPassword:
		return fmt.Sprintf("New password for '%s':\n%s\n\n  %s",
			m.selectedUser.Username,
			m.passwordTI.View(),
			stylesheet.Cur.DisabledText.Render("↲ submit • esc cancel"))
	}
	return ""
}

func (m *changePasswordModel) Done() bool {
	return m.stage == cpStgDone
}

func (m *changePasswordModel) Reset() error {
	m.users = list.Model{}
	m.passwordTI = textinput.Model{}
	m.stage = cpStgSelectUser
	m.selectedUser = types.User{}
	return nil
}

//#endregion interactive
