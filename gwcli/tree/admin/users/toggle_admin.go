package users

import (
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/confirmation"
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

// This file implements the interactive toggle-admin action.

func toggleAdmin() action.Pair {
	cmd := treeutils.GenerateAction("toggle-admin", "toggle a user's admin status",
		"Toggle admin status for a user. Optionally use --grant or --revoke to set explicitly.",
		nil,
		func(c *cobra.Command, args []string) error {
			uid, grant, revoke, err := toggleAdminGetFlags(c.Flags())
			if err != nil {
				return err
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
			uc, err := connection.Client.GetUser(uid)
			if err != nil {
				return err
			}

			success, err := setAdmin(uc.User, grant, revoke)
			if err != nil {
				return err
			}
			fmt.Fprintln(c.OutOrStdout(), success)
			return nil
		},
	)
	fs := toggleAdminFlagSet()
	cmd.Flags().AddFlagSet(fs)

	m := &toggleAdminModel{}
	m.Reset()

	return action.NewPair(cmd, m)
}

// Set admin on the given user according to the states of grant and revoke.
func setAdmin(u types.User, grant, revoke bool) (success string, _ error) {
	u.Admin = !u.Admin
	if grant && revoke { // er, something is probably wrong
		clilog.Writer.Warn("both grant and revoke were set, failing out...")
		return "", clilog.ErrInternal{}
	}
	if grant {
		u.Admin = true
	} else if revoke {
		u.Admin = false
	}
	if err := connection.Client.UpdateUser(u); err != nil {
		return "", err
	}
	return fmt.Sprintf("user '%s' admin status set to %v\n", u.Username, u.Admin), nil
}

// set flags
func toggleAdminFlagSet() *pflag.FlagSet {
	fs := &pflag.FlagSet{}
	ft.UID.Register(fs)
	fs.Bool("grant", false, "explicitly grant admin status")
	fs.Bool("revoke", false, "explicitly revoke admin status")
	return fs
}

// get flags
func toggleAdminGetFlags(fs *pflag.FlagSet) (uid int32, grant, revoke bool, err error) {
	uid, err = fs.GetInt32(ft.UID.Name())
	if err != nil {
		clilog.GetFlag(err)
		return
	}
	grant, err = fs.GetBool("grant")
	if err != nil {
		clilog.GetFlag(err)
		return
	}
	revoke, err = fs.GetBool("revoke")
	if err != nil {
		clilog.GetFlag(err)
		return
	}
	return
}

//#region interactive

// toggleAdminModel presents a multi-select list of users and toggles admin status on each selected user.
type toggleAdminModel struct {
	selecting bool // we are either selection or confirming (or done, but that is handled by the other bool)

	grant, revoke bool

	uList   list.Model
	confirm confirmation.Model

	done bool
}

func (c *toggleAdminModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	fs := toggleAdminFlagSet()

	uid, grant, revoke, err := toggleAdminGetFlags(fs)
	if err != nil {
		return "", nil, err
	}

	if uid != 0 { // if UID was given, we can skip directly to setting
		u, err := connection.Client.GetUser(uid)
		if err != nil {
			if errors.Is(err, client.ErrNotFound) { // TODO I don't think this works; we probably need an Is404Error()
				return "Unknown user ID: " + strconv.FormatInt(int64(uid), 10), nil, nil
			}
			return "", nil, err
		}
		c.done = true
		success, err := setAdmin(u.User, grant, revoke)
		if err != nil {
			return "", nil, err
		}
		return "", tea.Println(success), nil
	}

	users, err := connection.Client.ListUsers(nil)
	if err != nil {
		clilog.Writer.Error("failed to get the list of users", log.KV("error", err))
		return "", nil, fmt.Errorf("failed to get the list of users")
	}
	var itms = make([]list.Item, 0, len(users.Results))
	for _, user := range users.Results {
		itms = append(itms, listitem.NewUserItem(user, false))
	}
	itms = slices.Clip(itms)
	if len(itms) == 0 {
		return "there are no users", nil, nil
	}
	c.uList = stylesheet.NewList(itms, width, height, "user", "users")
	c.confirm.Init([]string{"user selection"}, uint(width), uint(height))
	return "", nil, nil
}

func (c *toggleAdminModel) Update(msg tea.Msg) (cmd tea.Cmd) {
	if c.done {
		return nil // do nothing
	}

	if c.selecting {
		// check for a selection
		if hotkeys.Match(msg, hotkeys.Invoke, hotkeys.Select) {
			c.selecting = false // move to confirming
		} else {
			c.uList, cmd = c.uList.Update(msg)
		}

		return cmd
	}
	// we are in confirmation mode
	var (
		selectionMade, confirmed bool
		ret                      uint
	)
	c.confirm, cmd, selectionMade, confirmed, ret = c.confirm.Update(msg)
	if !selectionMade {
		return cmd
	}

	// are we done or are we returning?
	if !confirmed { // nope, reselect user

		// sanity check:
		// we only supplied a single stage to return to; if we were given something else, something is horribly wrong
		if ret != 0 {
			clilog.Writer.Error("user selected non-0 choice", log.KV("choice", ret), log.KV("confirmation view", c.confirm.View()))
		}

		c.selecting = true
		return nil
	}

	// submit
	u, err := listitem.GetUser(&c.uList)
	if err != nil {
		return tea.Println(err)
	}
	success, err := setAdmin(u, c.grant, c.revoke)
	c.done = true
	if err != nil {
		return tea.Println(err)
	}
	return tea.Println(success)
}

func (c *toggleAdminModel) View() string {
	if c.selecting {
		return c.uList.View()
	}
	return c.confirm.View()
}

func (c *toggleAdminModel) Done() bool {
	return c.done
}

func (c *toggleAdminModel) Reset() error {
	c.selecting = true
	c.grant = false
	c.revoke = false
	c.uList = list.Model{}
	c.confirm = confirmation.Model{}
	c.done = false
	return nil
}
