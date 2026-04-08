package admin_users

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// This file implements user account locking

func lockAction() action.Pair {
	cmd := treeutils.GenerateAction("lock", "lock a user account",
		"Locks a user account.\n"+
			"The user will be unable to log in until unlocked, and all existing sessions will be terminated.",
		nil,
		func(c *cobra.Command, args []string) {
			if c.Flags().NArg() == 0 { // none specified; boot mother or fail out
				ni, err := c.Flags().GetBool(ft.NoInteractive.Name())
				if err != nil {
					clilog.LogFlagFailedGet(ft.NoInteractive.Name(), err)
					ni = true // better we assume no-interactive
				}
				if !ni {
					if err := mother.Spawn(c.Root(), c, args); err != nil {
						clilog.Tee(clilog.CRITICAL, c.ErrOrStderr(),
							"failed to spawn a mother instance: "+err.Error()+"\n")
					}
					return
				}
				fmt.Fprintln(c.ErrOrStderr(), phrases.AtLeast1ArgRequired("user IDs"))
				return
			}

			self, err := c.Flags().GetBool("include-self")
			if err != nil {
				clilog.LogFlagFailedGet("include-self", err)
			}

			// at least one ID was specified, attempt to lock each account
			var uids = make([]int32, 0)
			for i, s := range c.Flags().Args() {
				uid, err := strconv.ParseInt(s, 10, 32)
				if err != nil {
					clilog.Tee(clilog.INFO, c.ErrOrStderr(), "\""+c.Flags().Arg(i)+"\" is not a valid integer; no accounts were locked")
					return
				}
				// if the user attempts to lock their own account and did not specify --self, skip
				if !self && uid == int64(connection.CurrentUser().ID) {
					fmt.Fprintf(c.ErrOrStderr(), "refusing to lock the account of the caller.\n"+
						"If you actually intended to lock your current account, rerun with --self\n")
					continue
				}
				uids = append(uids, int32(uid))
			}
			for _, uid := range uids {
				if err := connection.Client.LockUserAccount(int32(uid)); err != nil {
					clilog.Tee(clilog.INFO, c.ErrOrStderr(), fmt.Sprintf("failed to lock user account %d: %v", uid, err))
					return
				}
				fmt.Fprintf(c.OutOrStdout(), "User %v locked", uid)
			}
		}, treeutils.GenerateActionOptions{
			Usage:   fmt.Sprintf("%s %s ...", ft.Mandatory("UID1"), ft.Optional("UID2")),
			Example: "7",
		})
	cmd.Flags().AddFlagSet(createFlagSet())

	return action.NewPair(cmd, &lockModel{})
}

// createFlagSet returns a fresh flagset of the flags used by this create action.
// It should be used to ensure flags are equivalent interactive and non-interactive use.
func createFlagSet() *pflag.FlagSet {
	fs := &pflag.FlagSet{}
	fs.Bool("include-self", false, "If set, you will be allowed to lock your user. Be careful!")
	return fs
}

//#region interactive

// lockModel is basically just a multiselect that calls LockUserAccount on each item selected.
type lockModel struct {
	m    multiselectlist.Model
	self bool
}

// Init is unused. It just exists so we can feed lockModel into teatest.
func (c *lockModel) Init() tea.Cmd {
	return nil
}

func (c *lockModel) Update(msg tea.Msg) (cmd tea.Cmd) {
	c.m, cmd = c.m.Update(msg)
	if c.m.Done() { // process locks
		var cmds []tea.Cmd
		for _, li := range c.m.GetSelectedItems() {
			// cast so we can fetch the UID
			itm, ok := li.(item)
			if !ok {
				clilog.Writer.Errorf("failed to cast item from DefaultItem. Bare item: %v", li)
				continue
			}
			if err := connection.Client.LockUserAccount(int32(itm.id)); err != nil {
				clilog.Writer.Error(fmt.Sprintf("failed to lock user account %d: %v", itm.id, err))
				return
			}
			cmds = append(cmds, tea.Printf("User %v locked", itm.id))
		}
		cmd = tea.Sequence(cmds...)
	}
	return cmd
}

func (c *lockModel) View() string {
	return c.m.View()
}

func (c *lockModel) Done() bool {
	return c.m.Done()
}

func (c *lockModel) Reset() error {
	c.m = multiselectlist.Model{}
	return nil
}

func (c *lockModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	fs := createFlagSet()
	if err := fs.Parse(tokens); err != nil {
		return "", nil, err
	}
	self, err := fs.GetBool("include-self")
	if err != nil {
		clilog.LogFlagFailedGet("include-self", err)
	}

	// stuff all users into the list, except the caller. Probably don't want the caller to be able to lock themselves easily.
	users, err := connection.Client.ListUsers(nil)
	if err != nil {
		clilog.Writer.Error("failed to get the list of users", log.KV("error", err))
		return "", nil, fmt.Errorf("failed to get the list of users")
	}
	var itms = make([]list.DefaultItem, 0, len(users.Results))
	for _, user := range users.Results {
		if !self && user.ID == connection.CurrentUser().ID {
			continue
		}
		itms = append(itms, item{
			id:       user.ID,
			username: user.Username,
			name:     user.Name,
			email:    user.Email,
			admin:    user.Admin,
		})
	}
	itms = slices.Clip(itms)
	if len(itms) == 0 {
		return "There are no other users to lock", nil, nil
	}
	c.m = multiselectlist.New(itms, width, height, multiselectlist.Options{})
	c.m.StatusMessageLifetime = stylesheet.StatusMessageLifetime
	c.m.StatusMessageOnSelect = true
	return "", nil, nil
}

type item struct {
	id       int32
	username string
	name     string
	email    string
	admin    bool
}

// FilterValue sets the string to include/disclude this item on when a user filters.
func (i item) FilterValue() string {
	return i.username + i.name + i.email
}

func (i item) Title() string {
	return i.username
}

func (i item) Description() string {
	var sb strings.Builder

	if i.admin {
		sb.WriteString("(admin) ")
	}
	fmt.Fprintf(&sb, "(ID: %d) %s (%s)", i.id, i.name, i.email)

	return sb.String()
}

//#endregion interactive
