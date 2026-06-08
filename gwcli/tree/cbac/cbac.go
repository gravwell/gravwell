/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package cbac provides actions for inspecting and managing Capability-Based
// Access Control rules.
package cbac

import (
	"fmt"
	"strconv"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	return treeutils.GenerateNav(
		"cbac", "manage capability-based access control",
		"Inspect and manage CBAC rules that govern which operations users and groups may perform.",
		[]string{"capabilities"},
		[]*cobra.Command{},
		[]action.Pair{
			listCapabilities(),
			listTemplates(),
			myCapabilities(),
			get(),
			edit(),
		})
}

func listCapabilities() action.Pair {
	return scaffoldlist.NewListAction(
		"list capabilities",
		"List every capability by its canonical name and description",
		types.CapabilityDesc{},
		func(_ *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.CapabilityDesc, error) {
			return connection.Client.CapabilityList()
		},
		map[string]string{
			"Cap":  "ID",
			"Desc": "Description",
		},
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "capabilities",
				Aliases: []string{"caps", "list-caps", "list-capabilities"},
			},
			DefaultColumns: []string{"Cap", "Name", "Desc"},
		})
}

func listTemplates() action.Pair {
	return scaffoldlist.NewListAction(
		"list capability templates",
		"List every capability grouping (template)",
		types.CapabilityTemplate{},
		func(_ *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.CapabilityTemplate, error) {
			// TODO we may need additional processing to display the capabilities in each template
			return connection.Client.CapabilityTemplateList()
		},
		map[string]string{
			"Desc": "Description",
		},
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "templates",
				Aliases: []string{"list-templates"},
			},
			DefaultColumns: []string{"Name", "Desc"},
		})
}

func myCapabilities() action.Pair {
	return scaffoldlist.NewListAction("list your effective capabilities",
		"Display capabilities currently granted to your user account",
		types.CapabilityDesc{},
		func(addtlFlags *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.CapabilityDesc, error) {},
		map[string]string{
			"Cap":  "ID",
			"Desc": "Description",
		}, scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "my",
				Aliases: []string{"self", "current", "my-capabilities", "my-caps"},
			},
			DefaultColumns: []string{"Cap", "Name", "Desc"},
		})
}

type getCaps struct {
	ID string
	types.CapabilityState
}

func get() action.Pair {
	var uids, gids []int32
	return scaffoldlist.NewListAction("get user/group capabilities",
		"List capabilities assigned to specific users and/or groups",
		getCaps{},
		func(addtlFlags *pflag.FlagSet, params scaffoldlist.DataParameters) ([]getCaps, error) {
			// compose the fetched data with what caused it to be fetched
			items := []getCaps{}
			for _, uid := range uids {
				uidString := "uid" + strconv.FormatInt(int64(uid), 10)
				u, err := connection.Client.GetUserCapabilities(uid)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", uidString, err)
				}
				items = append(items, getCaps{
					ID:              uidString,
					CapabilityState: u,
				})
			}
			for _, gid := range gids {
				gidString := "gid" + strconv.FormatInt(int64(gid), 10)
				g, err := connection.Client.GetGroupCapabilities(gid)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", gidString, err)
				}
				items = append(items, getCaps{
					ID:              gidString,
					CapabilityState: g,
				})
			}
			return items, nil
		},
		nil, // TODO
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "get",
				Aliases: []string{"users", "groups"},
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Int32Slice("uids", nil, "IDs of the users to include")
					fs.Int32Slice("gids", nil, "IDs of the groups to include")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				uids, err = fs.GetInt32Slice("uids")
				clilog.GetFlag(err)
				gids, err = fs.GetInt32Slice("gids")
				err = nil
				if len(uids) < 1 && len(gids) < 1 {
					return "at least one ID must be specified for --uids or --gids", nil
				}
				return "", nil
			},
		},
	)
}

func edit() action.Pair {
	var uids, gids []int32
	return scaffoldselect.NewSelectAction("edit capabilities", "Change the capabilities assigned to a user or group", "capability name",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {

		},
		func(ID string, addtlFlags *pflag.FlagSet) (success string, _ error) {},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("clear", false, "drop all current permissions before assigning the given set")
					fs.Int32Slice("uids", nil, "IDs of the users to edit")
					fs.Int32Slice("gids", nil, "IDs of the groups to edit")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				uids, err = fs.GetInt32Slice("uids")
				clilog.GetFlag(err)
				gids, err = fs.GetInt32Slice("gids")
				err = nil
				if len(uids) < 1 && len(gids) < 1 {
					return "at least one ID must be specified for --uids or --gids", nil
				}
				return "", nil
			},
		})
}
