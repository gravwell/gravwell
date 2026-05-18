package users

import (
	"errors"
	"fmt"
	"slices"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// This file implements the interactive toggle-admin action.

func toggleAdmin() action.Pair {
	cmd := treeutils.GenerateAction("toggle-admin", "toggle a user's admin status",
		"Toggle admin status for a user. Optionally use --grant or --revoke to set explicitly.",
		nil,
		func(c *cobra.Command, args []string) error {
			uid, err := c.Flags().GetInt32("uid")
			if err != nil {
				clilog.GetFlag(err)
			}

			if uid == 0 {
				// no uid specified; try interactive mode
				ni, err := c.Flags().GetBool(ft.NoInteractive.Name())
				if err != nil {
					clilog.GetFlag(err)
					ni = true
				}
				if !ni {
					return mother.Spawn(c.Root(), c, args)
				}
				return errors.New("--uid must be set and nonzero")
			}

			// non-interactive path
			uwcbac, err := connection.Client.GetUser(uid)
			if err != nil {
				return err
			}
			user := uwcbac.User
			user.Admin = !user.Admin

			if grant, err := c.Flags().GetBool("grant"); err != nil {
				clilog.GetFlag(err)
			} else if grant {
				user.Admin = true
			}
			if revoke, err := c.Flags().GetBool("revoke"); err != nil {
				clilog.GetFlag(err)
			} else if revoke {
				user.Admin = false
			}
			if err := connection.Client.UpdateUser(user); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "user '%s' admin status set to %v\n", user.Username, user.Admin)
			return nil
		},
	)
	fs := toggleAdminFlagSet()
	cmd.Flags().AddFlagSet(fs)

	return action.NewPair(cmd, &toggleAdminModel{})
}

func toggleAdminFlagSet() *pflag.FlagSet {
	fs := &pflag.FlagSet{}
	fs.Int32("uid", 0, "ID of the user")
	fs.Bool("grant", false, "explicitly grant admin status")
	fs.Bool("revoke", false, "explicitly revoke admin status")
	return fs
}

//#region interactive

// toggleAdminModel presents a multi-select list of users and toggles admin status on each selected user.
type toggleAdminModel struct {
	m list.Model
}

func (c *toggleAdminModel) Init() tea.Cmd {
	return nil
}

func (c *toggleAdminModel) Update(msg tea.Msg) (cmd tea.Cmd) {
	c.m, cmd = c.m.Update(msg)
	if c.m.Done() {
		selected := c.m.GetSelectedItems()
		if len(selected) == 0 {
			c.m.Undone()
			return c.m.NewStatusMessage("select at least 1 user")
		}
		var cmds []tea.Cmd
		for _, li := range selected {
			uwcbac, err := connection.Client.GetUser(li.ID())
			if err != nil {
				clilog.Writer.Error(fmt.Sprintf("failed to get user %d: %v", li.ID(), err))
				cmds = append(cmds, tea.Printf("failed to get user %d: %v", li.ID(), err))
				continue
			}
			user := uwcbac.User
			user.Admin = !user.Admin
			if err := connection.Client.UpdateUser(user); err != nil {
				clilog.Writer.Error(fmt.Sprintf("failed to update user %d: %v", li.ID(), err))
				cmds = append(cmds, tea.Printf("failed to update user '%s': %v", user.Username, err))
				continue
			}
			cmds = append(cmds, tea.Printf("user '%s' admin status set to %v", user.Username, user.Admin))
		}
		cmd = tea.Sequence(cmds...)
	}
	return cmd
}

func (c *toggleAdminModel) View() string {
	return c.m.View()
}

func (c *toggleAdminModel) Done() bool {
	return c.m.Done()
}

func (c *toggleAdminModel) Reset() error {
	c.m = multiselectlist.Model[int32]{}
	return nil
}

func (c *toggleAdminModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	users, err := connection.Client.ListUsers(nil)
	if err != nil {
		clilog.Writer.Error("failed to get the list of users", log.KV("error", err))
		return "", nil, fmt.Errorf("failed to get the list of users")
	}
	var itms = make([]multiselectlist.SelectableItem[int32], 0, len(users.Results))
	for _, user := range users.Results {
		itms = append(itms, listitem.NewUserItem(user, false))
	}
	itms = slices.Clip(itms)
	if len(itms) == 0 {
		return "there are no users", nil, nil
	}
	c.m = multiselectlist.New(itms, width, height, multiselectlist.Options{})
	c.m.StatusMessageLifetime = stylesheet.StatusMessageLifetime
	c.m.StatusMessageOnSelect = true
	c.m.Title = "Select users to toggle admin status"
	return "", nil, nil
}

//#endregion interactive
