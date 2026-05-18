/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package groups introduces actions to managing groups.
//
// Only available to admins.
package groups

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"

	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/validate"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	const (
		use   string = "groups"
		short string = "manage groups"
		long  string = "View and edit groups"
	)

	return treeutils.GenerateNav(use, short, long, []string{"group"},
		nil,
		[]action.Pair{
			listGroups(),
			create(),
			delete(),
			edit(),
			listUsers(),
			// add users to groups
			modGroupUsers("associate", "add users to groups",
				"Associate any number of user to each specified group. Users already in a given group will be ignored.",
				[]string{"add-users", "add-user"}, true),
			// remove users from groups
			modGroupUsers("disassociate", "remove users from groups",
				"Disassociate any number of user from each specified group. Users already absent from a given groups will be ignored.",
				[]string{"rm-user", "remove-user", "rm-users", "remove-users"}, false),
		})
}

// lists all groups the current user is able to see
func listGroups() action.Pair {
	return scaffoldlist.NewListAction("list groups", "Retrieves a list of groups available on the system",
		types.Group{},
		func(fs *pflag.FlagSet) ([]types.Group, error) {
			resp, err := connection.Client.ListGroups(nil)
			return resp.Results, err
		},
		nil,
		scaffoldlist.Options{})
}

func create() action.Pair {
	return scaffoldcreate.NewCreateAction("group",
		map[string]scaffoldcreate.Field{
			"name": scaffoldcreate.FieldName("group"),
			"desc": scaffoldcreate.FieldDescription("group"),
		},

		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			result, err := connection.Client.CreateGroup(types.Group{
				Name:        fields["name"].Provider.Get(),
				Description: fields["desc"].Provider.Get(),
			})
			return result.Name, "", err
		}, scaffoldcreate.Options{})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("group", "groups",
		func(dryrun bool, id int32) error {
			if dryrun {
				_, err := connection.Client.GetGroup(id)
				return err
			}
			return connection.Client.DeleteGroup(id)
		},
		func() ([]scaffolddelete.Item[int32], error) {
			resp, err := connection.Client.ListGroups(nil)
			if err != nil {
				return nil, err
			}
			var items = make([]scaffolddelete.Item[int32], len(resp.Results))
			for i, g := range resp.Results {
				items[i] = scaffolddelete.NewItem(g.Name, g.Description, g.ID)
			}
			return items, nil
		})
}

func edit() action.Pair {
	cfg := scaffoldedit.Config{
		"name":        scaffoldedit.FieldName("group"),
		"description": scaffoldedit.FieldDescription("group"),
		"search priority": &scaffoldedit.Field{
			Title: "Search Priority",
			Usage: "Set the search priority of the group",
			Order: 80,
			CustomTIFuncInit: func() textinput.Model {
				ti := stylesheet.NewTI("0", true)
				ti.Validate = func(s string) error {
					if err := validate.Numeric(s); err != nil {
						return fmt.Errorf("Search Priority: %w", err)
					}
					return nil
				}
				return ti
			},
		},
	}
	funcs := scaffoldedit.SubroutineSet[int32, types.Group]{
		SelectSub: func(id int32) (types.Group, error) {
			gcbac, err := connection.Client.GetGroup(id)
			if err != nil {
				return types.Group{}, err
			}
			return gcbac.Group, nil
		},
		FetchSub: func() ([]types.Group, error) {
			resp, err := connection.Client.ListGroups(nil)
			if err != nil {
				return nil, err
			}
			return resp.Results, nil
		},
		GetFieldSub: func(item types.Group, fieldKey string) (string, error) {
			switch fieldKey {
			case "name":
				return item.Name, nil
			case "description":
				return item.Description, nil
			case "search priority":
				return strconv.FormatInt(int64(item.SearchPriority), 10), nil
			}
			return "", fmt.Errorf("unknown field key: %v", fieldKey)
		},
		SetFieldSub: func(item *types.Group, fieldKey, val string) (string, error) {
			switch fieldKey {
			case "name":
				item.Name = val
			case "description":
				item.Description = val
			case "search priority":
				if err := validate.Numeric(val); err != nil {
					return "", err
				}
				sp, err := strconv.ParseInt(val, 10, 32)
				if err != nil {
					return "", err
				}

				item.SearchPriority = int(sp)
			default:
				return "", fmt.Errorf("unknown field key: %v", fieldKey)
			}
			return "", nil
		},
		GetTitleSub:       func(item types.Group) string { return item.Name },
		GetDescriptionSub: func(item types.Group) string { return item.Description },
		UpdateSub: func(data *types.Group) (string, error) {
			return data.Name, connection.Client.UpdateGroup(*data)
		},
	}
	return scaffoldedit.NewEditAction("group", "groups", cfg, funcs)
}

var listUsersGID int32

// list the users in a group
func listUsers() action.Pair {
	return scaffoldlist.NewListAction("list users in a group", "Display the users that are members of a given group.",
		types.User{},
		func(fs *pflag.FlagSet) ([]types.User, error) {
			return connection.Client.GetGroupUsers(listUsersGID)
		},
		nil,
		scaffoldlist.Options{
			CommonOptions:  scaffold.CommonOptions{Use: "users"},
			DefaultColumns: []string{"ID", "Username", "Name", "Email", "Admin"},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				listUsersGID = 0 // ensure it is wiped
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("group ID"), nil
				}
				gid, err := strconv.ParseInt(fs.Arg(0), 10, 32)
				if err != nil {
					return fs.Arg(0) + " is not a valid group ID", nil
				}
				listUsersGID = int32(gid)
				return "", nil
			},
		})
}
