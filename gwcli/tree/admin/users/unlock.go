package users

import (
	"fmt"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/pflag"
)

func unlockAction() action.Pair {
	return scaffoldselect.NewSelectAction("unlock user accounts", "Unlock one or several user accounts.", "account", "accounts",
		func() ([]multiselectlist.SelectableItem[int32], error) {
			ulr, err := connection.Client.ListUsers(nil)
			if err != nil {
				return nil, err
			}
			items := make([]multiselectlist.SelectableItem[int32], 0, len(ulr.Results))
			for _, user := range ulr.Results {
				if user.ID == connection.CurrentUser().ID || !user.Locked {
					continue
				}
				items = append(items, listitem.NewUserItem(user, false))
			}
			items = slices.Clip(items)
			return items, nil
		},
		func(ID int32) (success string, _ error) {
			if err := connection.Client.UnlockUserAccount(ID); err != nil {
				return "", fmt.Errorf("failed to unlock user account %d: %v", ID, err)
			}
			return fmt.Sprintf("User %v unlocked", ID), nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "unlock",
			},
			NoItemsError: "There are no locked accounts you can unlock.",
		})
}

//#region interactive

// unlockModel is basically just a multiselect that calls UnlockUserAccount on each item selected.
type unlockModel struct {
	m multiselectlist.Model[int32]
}

// Init is unused. It just exists so we can feed unlockModel into teatest.
func (c *unlockModel) Init() tea.Cmd {
	return nil
}

func (c *unlockModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	// unlock has no local flags

	// stuff all locked users into the list.
	users, err := connection.Client.ListUsers(nil)
	if err != nil {
		clilog.Writer.Error("failed to get the list of users", log.KV("error", err))
		return "", nil, fmt.Errorf("failed to get the list of users")
	}
	var itms = make([]multiselectlist.SelectableItem[int32], 0, len(users.Results))
	for _, user := range users.Results {
		if user.Locked {
			itms = append(itms, listitem.NewUserItem(user, false))
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

func (c *unlockModel) Update(msg tea.Msg) (cmd tea.Cmd) {
	c.m, cmd = c.m.Update(msg)
	if c.m.Done() { // process unlocks
		var cmds []tea.Cmd
		for _, li := range c.m.GetSelectedItems() {
			if err := connection.Client.UnlockUserAccount(int32(li.ID())); err != nil {
				clilog.Writer.Error(fmt.Sprintf("failed to unlock user account %d: %v", li.ID(), err))
				return
			}
			cmds = append(cmds, tea.Printf("User %v unlocked", li.ID()))
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
	c.m = multiselectlist.Model[int32]{}
	return nil
}
