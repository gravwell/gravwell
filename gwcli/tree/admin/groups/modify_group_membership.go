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
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/confirmation"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const heightBuffer = 8

type stage uint

const (
	stgUsers        stage = iota // select users
	stgGroups                    // select groups
	stgConfirmation              // confirm and submit changes
	stgDone                      // job's done
)

type membershipChanges struct {
	users  multiselectlist.Model[int32]
	groups multiselectlist.Model[int32]

	stage stage

	// selects are set when leaving the group stage.
	selectedUIDs []int32
	selectedGIDs []int32

	confirm confirmation.Model

	fs *pflag.FlagSet

	add bool // not add == remove
}

// modGroupUsers generates an action suitable for either adding or removing users from a collection of groups.
// It powers both associate and disassociate.
func modGroupUsers(use, short, long string, aliases []string, add bool) action.Pair {
	pair := action.NewPair(treeutils.GenerateAction(use, short, long,
		aliases, func(c *cobra.Command, args []string) error {
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
			if add {
				clilog.Writer.Info("Autonomously adding users to groups", log.KV("UIDs", uids), log.KV("GIDs", gids))
			} else {
				clilog.Writer.Info("Autonomously removing users from groups", log.KV("UIDs", uids), log.KV("GIDs", gids))
			}

			var successes uint
			for _, gid := range gids {
				if gid > math.MaxInt32 {
					return errors.New("Group IDs must satisfy 0 < gid <=" + strconv.FormatInt(math.MaxInt32, 10))
				}
				for _, uid := range uids {
					if uid > math.MaxInt32 {
						return errors.New("User IDs must satisfy 0 < uid <=" + strconv.FormatInt(math.MaxInt32, 10))
					}
					if add {
						if err := connection.Client.AddUserToGroup(int32(uid), int32(gid)); err != nil {
							fmt.Fprintf(c.ErrOrStderr(), "Failed to add user ID %d to group %d: %v", uid, gid, err)
						} else {
							// the user may have already been a part of this group, but we can't tell so.... Job's done.
							fmt.Fprintf(c.OutOrStdout(), "Added user ID %d to group %d", uid, gid)
							successes += 1
						}
					} else {
						if err := connection.Client.DeleteUserFromGroup(int32(uid), int32(gid)); err != nil {
							fmt.Fprintf(c.ErrOrStderr(), "Failed to remove user ID %d from group %d: %v", uid, gid, err)
						} else {
							// the user may not exist in this group, but we can't tell so.... Job's done.
							fmt.Fprintf(c.OutOrStdout(), "Removed user ID %d from group %d", uid, gid)
							successes += 1
						}
					}

				}
			}

			if successes == 0 {
				return errors.New("All requested group changes failed")
			}
			return nil
		}), newMembershipChangesInteractive(add))

	pair.Action.Flags().AddFlagSet(membershipFlagset(add))
	return pair
}

func newMembershipChangesInteractive(add bool) *membershipChanges {
	m := &membershipChanges{
		confirm: confirmation.Model{},
		add:     add,
	}
	m.Reset()
	m.fs = membershipFlagset(m.add)
	return m
}

func membershipFlagset(add bool) *pflag.FlagSet {
	fs := &pflag.FlagSet{}
	if add {
		fs.UintSlice("uid", nil, "ID of users to insert into each group")
		fs.UintSlice("gid", nil, "ID of groups into which to each user should be inserted")
	} else {
		fs.UintSlice("uid", nil, "ID of users to remove from each group")
		fs.UintSlice("gid", nil, "ID of groups from which users should be subtracted")
	}

	return fs
}

func (m *membershipChanges) SetArgs(parentFS *pflag.FlagSet, tokens []string, width, height int) (
	invalid string, onStart tea.Cmd, err error) {
	// build each list from the set of users and groups
	glr, err := connection.Client.ListGroups(nil)
	if err != nil {
		return "", nil, err
	} else if len(glr.Results) < 1 {
		return "", nil, errors.New("No groups available. Please create one before attempting to change its users.")
	}
	ulr, err := connection.Client.ListUsers(nil)
	if err != nil {
		return "", nil, err
	} else if len(ulr.Results) < 1 {
		return "", nil, errors.New("I don't know how you managed to get here with zero users, but you have no users to modify the group membership of.")
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

			var desc string = "(no groups)"
			if gids := strings.TrimSpace(grpIDssb.String()); gids != "" {
				desc = fmt.Sprintf("Groups: %v", gids)
			}

			userItems[i] = &multiselectlist.DefaultSelectableItem[int32]{
				Title_:       fmt.Sprintf("(%d) %s", user.ID, user.Username),
				Description_: desc,
				Selected_:    false,
				ID_:          user.ID,
			}
		}
	}
	m.users = multiselectlist.New(userItems, width, max(0, height-heightBuffer), multiselectlist.Options{})
	m.users.SetShowStatusBar(true) // TODO set status message styling
	m.users.StatusMessageLifetime = stylesheet.StatusMessageLifetime
	m.users.Title = "Users"

	// build the group list
	groupItems := make([]multiselectlist.SelectableItem[int32], len(glr.Results))
	{
		for i, grp := range glr.Results {
			desc := "(no users)"
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
	m.groups = multiselectlist.New(groupItems, width, max(0, height-heightBuffer), multiselectlist.Options{})
	m.groups.SetShowStatusBar(true) // TODO set status message styling
	m.groups.StatusMessageLifetime = stylesheet.StatusMessageLifetime
	m.groups.Title = "Groups"

	m.confirm.Init([]string{"user selection", "group selection"}, uint(width), uint(height))
	return "", nil, nil
}

func (m *membershipChanges) Update(msg tea.Msg) tea.Cmd {
	// if this is a window size message, make sure it is passed to every stage
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		wsm.Height = max(0, wsm.Height-heightBuffer)
		var cmds = make([]tea.Cmd, 3)
		m.users, cmds[0] = m.users.Update(wsm)
		m.groups, cmds[1] = m.groups.Update(wsm)
		m.confirm, cmds[2], _, _, _ = m.confirm.Update(wsm)
		return tea.Batch(cmds...)
	}

	var cmd tea.Cmd
	switch m.stage {
	case stgUsers:
		// display the users in the list
		m.users, cmd = m.users.Update(msg)
		if m.users.Done() {
			m.users.Undone() // in case we come back

			if len(m.users.GetSelectedItems()) < 1 {
				return m.users.NewStatusMessage("you must select at least 1 user")
			}
			m.stage = stgGroups
		}
	case stgGroups:
		m.groups, cmd = m.groups.Update(msg)
		if m.groups.Done() {
			m.groups.Undone() // in case we come back

			if len(m.groups.GetSelectedItems()) < 1 {
				return m.groups.NewStatusMessage("you must select at least 1 group")
			}
			m.stage = stgConfirmation
		}

		// fetch selections
		var sbUIDs strings.Builder
		selected := m.users.GetSelectedItems()
		m.selectedUIDs = make([]int32, len(selected))
		for i, itm := range selected {
			m.selectedUIDs[i] = itm.ID()
			sbUIDs.WriteString(strconv.FormatInt(int64(itm.ID()), 10) + " ")
		}
		var sbGIDs strings.Builder
		selected = m.groups.GetSelectedItems()
		m.selectedGIDs = make([]int32, len(selected))
		for i, itm := range selected {
			m.selectedGIDs[i] = itm.ID()
			sbGIDs.WriteString(strconv.FormatInt(int64(itm.ID()), 10) + " ")
		}

		if m.add {
			m.confirm.HeaderLines = []string{
				"Adding " + strconv.FormatInt(int64(len(m.selectedUIDs)), 10) + " users",
				"[" + strings.TrimSpace(sbUIDs.String()) + "]",
				"to",
				strconv.FormatInt(int64(len(m.selectedGIDs)), 10) + " groups",
				"[" + strings.TrimSpace(sbGIDs.String()) + "]"}
		} else {
			m.confirm.HeaderLines = []string{
				"Removing " + strconv.FormatInt(int64(len(m.selectedUIDs)), 10) + " users",
				"[" + strings.TrimSpace(sbUIDs.String()) + "]",
				"from",
				strconv.FormatInt(int64(len(m.selectedGIDs)), 10) + " groups",
				"[" + strings.TrimSpace(sbGIDs.String()) + "]"}
		}

	case stgConfirmation:
		var (
			done   bool
			submit bool
			choice uint
		)
		m.confirm, cmd, done, submit, choice = m.confirm.Update(msg)
		if !done {
			return cmd
		}
		if submit { // for each group, attempt to add each user
			var resultCmds []tea.Cmd
			var successes uint
			for _, gid := range m.selectedGIDs {
				for _, uid := range m.selectedUIDs {
					if m.add {
						if err := connection.Client.AddUserToGroup(int32(uid), int32(gid)); err != nil {
							resultCmds = append(resultCmds, tea.Printf("Failed to add user ID %d to group %d: %v", uid, gid, err))
						} else {
							// the user may have already been a part of this group, but we can't tell so.... Job's done.
							resultCmds = append(resultCmds, tea.Printf("Added user ID %d to group %d", uid, gid))
							successes += 1
						}
					} else {
						if err := connection.Client.DeleteUserFromGroup(int32(uid), int32(gid)); err != nil {
							resultCmds = append(resultCmds, tea.Printf("Failed to remove user ID %d from group %d: %v", uid, gid, err))
						} else {
							resultCmds = append(resultCmds, tea.Printf("Removed user ID %d from group %d", uid, gid))
							successes += 1
						}
					}

				}
			}
			if successes == 0 {
				resultCmds = append(resultCmds, tea.Println("All requested group changes failed"))
			} else {
				m.stage = stgDone
			}
			return tea.Batch(cmd, tea.Sequence(resultCmds...))
		}
		// return to the selected stage
		m.stage = stage(choice)
	default:
		clilog.Writer.Error("unknown stage", log.KV("stage", m.stage))
		m.stage = stgDone
	}
	return cmd
}

func (m *membershipChanges) View() string {
	switch m.stage {
	case stgUsers:
		return m.users.View()
	case stgGroups:
		return m.groups.View()
	case stgConfirmation:
		return m.confirm.View()
	}
	clilog.Writer.Error("unknown stage", log.KV("stage", m.stage))
	return clilog.ErrInternal{}.Error()
}

func (m *membershipChanges) Reset() error {
	m.users = multiselectlist.Model[int32]{}
	m.groups = multiselectlist.Model[int32]{}

	m.stage = 0

	m.selectedUIDs = nil
	m.selectedGIDs = nil

	m.confirm = confirmation.Model{}

	// don't touch add!

	return nil
}

func (m *membershipChanges) Done() bool {
	return m.stage == stgDone
}
