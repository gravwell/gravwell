/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package groups

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type autgStage uint

const (
	users autgStage = iota
	groups
	confirmation
	done
)

type addUsersToGroup struct {
	users  multiselectlist.Model[int32]
	groups multiselectlist.Model[int32]

	stage autgStage

	confirmStage uint // on the confirmation stage, which of the items is selected

	fs *pflag.FlagSet
}

func addUsersToGroups() action.Pair {
	return action.NewPair(treeutils.GenerateAction("associate", "add users to groups",
		"Associate any number of user to all specified groups. Users already in the given group will be ignored.",
		[]string{"add-users", "add-user"}, func(c *cobra.Command, args []string) error {
			x, err := c.Flags().GetBool(ft.NoInteractive.Name())
			if err != nil {
				clilog.GetFlag(err)
			}

			uids, err := c.Flags().GetUintSlice("uid")
			if err != nil {
				clilog.GetFlag(err)
			}
			gids, err := c.Flags().GetUintSlice("gid")
			if err != nil {
				clilog.GetFlag(err)
			}
			if len(uids) < 1 || len(gids) < 1 {
				if x { // if we are in no-interactive, this is fatal
					return errors.New("You must specify at least one group (--gid) and at least one user (--uid)")
				}
				return mother.Spawn(c.Root(), c, args)
			}
			clilog.Writer.Debug("Autonomously adding users to groups", log.KV("UIDs", uids), log.KV("GIDs", gids))

			var successes uint
			for _, gid := range gids {
				if gid > math.MaxInt32 {
					return errors.New("Group IDs must satisfy 0 < gid <=" + strconv.FormatInt(math.MaxInt32, 10))
				}
				for _, uid := range uids {
					if uid > math.MaxInt32 {
						return errors.New("User IDs must satisfy 0 < uid <=" + strconv.FormatInt(math.MaxInt32, 10))
					}
					if err := connection.Client.AddUserToGroup(int32(uid), int32(gid)); err != nil {
						fmt.Fprintf(c.ErrOrStderr(), "Failed to add user ID %d to group %d: %v", err)
					} else {
						// the user may have already been a part of this group, but we can't tell so.... Job's done.
						fmt.Fprintf(c.OutOrStdout(), "Added user ID %d to group %d", uid, gid)
						successes += 0
					}
				}
			}

			if successes == 0 {
				return errors.New("All requested group assignments failed")
			}
			return nil
		}), newAUtG())
}

func newAUtG() *addUsersToGroup {
	m := &addUsersToGroup{}
	m.Reset()
	m.fs = autgFlagset()
	return m
}

func autgFlagset() *pflag.FlagSet {
	fs := &pflag.FlagSet{}
	fs.UintSlice("uid", nil, "ID of users to insert into each group")
	fs.UintSlice("gid", nil, "ID of groups into which to each user should be inserted.")
	return fs
}

func (m *addUsersToGroup) SetArgs(parentFS *pflag.FlagSet, tokens []string, width, height int) (
	invalid string, onStart tea.Cmd, err error) {
	// build each list from the set of users and groups
	glr, err := connection.Client.ListGroups(nil)
	if err != nil {
		return "", nil, err
	} else if len(glr.Results) < 1 {
		return "", nil, errors.New("No groups available. Please create one before attempting to add users to it.")
	}
	ulr, err := connection.Client.ListUsers(nil)
	if err != nil {
		return "", nil, err
	} else if len(ulr.Results) < 1 {
		return "", nil, errors.New("I don't know how you managed to get here with zero users, but you have no users to add to any groups.")
	}

	// build the user list
	userItems := make([]multiselectlist.SelectableItem[int32], len(ulr.Results))
	GIDToUIDs := make(map[int32][]int32) // inverse user -> groups mapping so we don't have to hit the backend again
	{
		var grpIDssb strings.Builder // reused each cycle
		for i, user := range ulr.Results {
			grpIDssb.Reset()
			for _, grp := range user.Groups {
				fmt.Fprint(&grpIDssb, grp.ID, " ")
				GIDToUIDs[grp.ID] = append(GIDToUIDs[grp.ID], user.ID)
			}

			userItems[i] = &multiselectlist.DefaultSelectableItem[int32]{
				Title_:       fmt.Sprintf("(%d) %s", user.ID, user.Username),
				Description_: fmt.Sprintf("Groups: %v", strings.TrimSpace(grpIDssb.String())),
				Selected_:    false,
				ID_:          user.ID,
			}
		}
	}
	m.users = multiselectlist.New(userItems, width, height, multiselectlist.Options{})

	// build the group list
	groupItems := make([]multiselectlist.SelectableItem[int32], len(glr.Results))
	{
		for i, grp := range glr.Results {
			desc := "(empty)"
			uids := GIDToUIDs[grp.ID]
			if len(uids) > 0 {
				desc = fmt.Sprintf("(Member UIDs: %v)", uids)
			}

			groupItems[i] = &multiselectlist.DefaultSelectableItem[int32]{
				Title_:       grp.Name,
				Description_: desc + " " + grp.Description,
				ID_:          grp.ID,
			}
		}
	}
	m.groups = multiselectlist.New(groupItems, width, height, multiselectlist.Options{}) // TODO check if we need to factor in header height
	return "", nil, nil
}

func (m *addUsersToGroup) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.stage {
	case users:
		// display the users in the list
		m.users, cmd = m.users.Update(msg)
		if m.users.Done() {
			m.stage += 1
		}
	case groups:
		m.groups, cmd = m.groups.Update(msg)
		if m.groups.Done() {
			m.stage += 1
		}
	case confirmation:
		// In confirmation mode, we allow the user to select one of: "submit", "return to user selection", "return to group selection"
		// TODO

		// TODO spin out the confirmation page for uniformity
	default:
		clilog.Writer.Error("unknown stage", log.KV("action", "associate"), log.KV("stage", m.stage))
		m.stage = done
	}
	return cmd
}

func (m *addUsersToGroup) View() string {
	// TODO
	return ""
}

func (m *addUsersToGroup) Reset() error {
	m.users = multiselectlist.Model[int32]{}
	m.groups = multiselectlist.Model[int32]{}

	m.stage = 0

	return nil
}

func (m *addUsersToGroup) Done() bool {
	return m.stage == done
}
