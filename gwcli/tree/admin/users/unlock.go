package admin_users

import (
	"fmt"
	"slices"
	"strconv"

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

// This file implements user account unlocking

func unlockAction() action.Pair {
	cmd := treeutils.GenerateAction("unlock", "unlock a user account",
		"Unlocks a locked user account.",
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

			// at least one ID was specified, attempt to unlock each account
			var uids = make([]int32, c.Flags().NArg())
			for i, s := range c.Flags().Args() {
				uid, err := strconv.ParseInt(s, 10, 32)
				if err != nil {
					clilog.Tee(clilog.INFO, c.ErrOrStderr(), "\""+c.Flags().Arg(0)+"\" is not a valid integer; no accounts were unlocked")
					return
				}
				uids[i] = int32(uid)
			}
			for _, uid := range uids {
				if err := connection.Client.UnlockUserAccount(int32(uid)); err != nil {
					clilog.Tee(clilog.INFO, c.ErrOrStderr(), fmt.Sprintf("failed to unlock user account %d: %v", uid, err))
					return
				}
				fmt.Fprintf(c.OutOrStdout(), "User %v unlocked", uid)
			}
		}, treeutils.GenerateActionOptions{
			Usage:   fmt.Sprintf("%s %s ...", ft.Mandatory("UID1"), ft.Optional("UID2")),
			Example: "7",
		})

	return action.NewPair(cmd, &unlockModel{})
}

//#region interactive

// unlockModel is basically just a multiselect that calls UnlockUserAccount on each item selected.
type unlockModel struct {
	m    multiselectlist.Model
	self bool
}

// Init is unused. It just exists so we can feed unlockModel into teatest.
func (c *unlockModel) Init() tea.Cmd {
	return nil
}

func (c *unlockModel) Update(msg tea.Msg) (cmd tea.Cmd) {
	c.m, cmd = c.m.Update(msg)
	if c.m.Done() { // process unlocks
		var cmds []tea.Cmd
		for _, li := range c.m.GetSelectedItems() {
			// cast so we can fetch the UID
			itm, ok := li.(item)
			if !ok {
				clilog.Writer.Errorf("failed to cast item from DefaultItem. Bare item: %v", li)
				continue
			}
			if err := connection.Client.UnlockUserAccount(int32(itm.id)); err != nil {
				clilog.Writer.Error(fmt.Sprintf("failed to unlock user account %d: %v", itm.id, err))
				return
			}
			cmds = append(cmds, tea.Printf("User %v unlocked", itm.id))
		}
		cmd = tea.Sequence(cmds...)
	}
	return cmd
}

func (c *unlockModel) View() string {
	return c.m.View()
}

func (c *unlockModel) Done() bool {
	return c.m.Done()
}

func (c *unlockModel) Reset() error {
	c.m = multiselectlist.Model{}
	return nil
}

func (c *unlockModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	// unlock has no local flags

	// stuff all locked users into the list.
	users, err := connection.Client.GetAllUsers()
	if err != nil {
		clilog.Writer.Error("failed to get the list of users", log.KV("error", err))
		return "", nil, fmt.Errorf("failed to get the list of users")
	}
	var itms = make([]list.DefaultItem, 0, len(users))
	for _, user := range users {
		if user.Locked {
			itms = append(itms, item{
				id:       user.ID,
				username: user.Username,
				name:     user.Name,
				email:    user.Email,
				admin:    user.Admin,
			})
		}

	}
	itms = slices.Clip(itms)
	if len(itms) == 0 {
		return "There are no locked users", nil, nil
	}
	c.m = multiselectlist.New(itms, width, height, multiselectlist.Options{})
	c.m.StatusMessageLifetime = stylesheet.StatusMessageLifetime
	c.m.StatusMessageOnSelect = true
	return "", nil, nil
}
