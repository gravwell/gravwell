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
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
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
			replace(),
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

// wrapper type for types.CapabilityExplanation{}.
// Used to kill the awkward prefix, hide redundant fields, and perform implicit renames.
type capExp struct {
	ID          types.Capability
	Name        string
	Description string
	Category    types.CapabilityCategory
	AdminOnly   bool
	UserGrant   bool    // True if the capability was explicitly granted to the user
	GroupGrants []int32 // An array of GIDs to which the user belongs that grant the capability.
}

func explanationToLocal(orig types.CapabilityExplanation) capExp {
	wrapped := capExp{
		ID:          orig.Cap,
		Name:        orig.Name,
		Description: orig.Desc,
		Category:    orig.Category,
		AdminOnly:   orig.AdminOnly,
		UserGrant:   orig.UserGrant,
		GroupGrants: make([]int32, len(orig.GroupGrants)),
	}
	for i, g := range orig.GroupGrants {
		wrapped.GroupGrants[i] = g.ID
	}
	return wrapped
}
func myCapabilities() action.Pair {
	return scaffoldlist.NewListAction("list your effective capabilities",
		"Display capabilities currently granted to your user account and what grants them to you.",
		capExp{},
		func(addtlFlags *pflag.FlagSet, params scaffoldlist.DataParameters) ([]capExp, error) {
			var caps []capExp
			if ex, err := connection.Client.CurrentUserCapabilityExplanations(); err != nil {
				return nil, err
			} else { // trim out permissions the user doesn't have
				for _, e := range ex {
					if e.Granted {
						caps = append(caps, explanationToLocal(e))
					}
				}
			}

			return caps, nil
		},
		nil, // implicit in the wrapper type
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "my",
				Aliases: []string{"self", "current", "my-capabilities", "my-caps"},
			},
			DefaultColumns: []string{"Cap", "Name", "UserGrant", "GroupGrants"},
		})
}

type getCaps struct {
	ID string
	types.CapabilityState
}

// TODO this is admin only
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
			ValidateArgs: func(fs *pflag.FlagSet) (_ string, err error) {
				uids, err = fs.GetInt32Slice("uids")
				clilog.GetFlag(err)
				gids, err = fs.GetInt32Slice("gids")
				clilog.GetFlag(err)
				if len(uids) < 1 && len(gids) < 1 {
					return "at least one ID must be specified for --uids or --gids", nil
				}
				return "", nil
			},
		},
	)
}

// Fine-grain toggle over a single user's or group's capabilities.
// TODO this would work better as a multistage (issues#2433)
// TODO this should be a scaffoldselect, but scaffoldselect has operate called against each ID, when we would much prefer to batch submit.
/*func edit() action.Pair {
	var id int32  // id if the entity to fetch
	var user bool // if not user, then group
	return scaffoldselect.NewSelectAction(
		"edit capabilities",
		"Tune the capabilities assigned to a single user or group.\n"+
			"Named capabilities will be toggled, unless --enable or --disable are set, in which case redundant operations will be ignored.",
		"capability name",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			// fetch all caps
			// TODO

			// preselect using the caps the user/group has
			var cs types.CapabilityState
			var err error
			if user {
				cs, err = connection.Client.GetUserCapabilities(id)
			} else {
				cs, err = connection.Client.GetGroupCapabilities(id)
			}
			if err != nil {
				return nil, err
			}
			caps, err := cs.CapabilityList()
			if err != nil {
				typ := "user"
				if !user {
					typ = "group"
				}
				clilog.Writer.Warn("failed to transform capability state into capability list",
					log.KV("ID", id), log.KV("type", typ),
					log.KVErr(err))
				return nil, err
			}
			// TODO

			data := make([]multiselectlist.SelectableItem[string], len(caps))
			for i, cap := range caps {
				data[i] = wrapCapForMSL(cap)
			}
			return data, nil
		},
		func(ID string, addtlFlags *pflag.FlagSet) (success string, _ error) {
			// TODO
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("clear", false, "drop all current permissions before assigning the given set")
					fs.Int32("uid", 0, "IDs of the users to edit.\n"+
						"Mutually exclusive with --gid")
					fs.Int32("gid", 0, "IDs of the groups to edit.\n"+
						"Mutually exclusive with --uid")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				id, err = fs.GetInt32("uid")
				clilog.GetFlag(err)
				gid, err := fs.GetInt32("gid")
				clilog.GetFlag(err)
				if gid != 0 {
					if id != 0 {
						return "--uid and --gid are mutually exclusive", nil
					}
					gid = id
				}
				if id == 0 {
					return "you must specify --uid or --gid", nil
				}
				return "", nil
			},
		})
}*/

// TODO this would work better as a multistage (issues#2433)
func replace() action.Pair {
	return scaffoldcreate.NewCreateAction("capabilities",
		map[string]scaffoldcreate.Field{
			"users": {
				Title:    "Users",
				Required: false,
				Flag: scaffoldcreate.FlagConfig{
					Name:  "uids",
					Usage: "IDs of the users who's capabilities will be replaced",
				},
				Order: 200,
				Provider: scaffoldcreate.NewMSLProvider(nil, scaffoldcreate.MSLOptions{
					ListOptions: multiselectlist.Options{},
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListUsers(nil)
						if err != nil {
							clilog.Writer.Error("failed to get user list", log.KVErr(err))
						} else if len(lr.Results) < 1 {
							return nil
						}
						data := make([]multiselectlist.SelectableItem[string], len(lr.Results))
						var sb strings.Builder
						for i, u := range lr.Results {
							sb.Reset()
							if u.Admin {
								sb.WriteString("(admin) ")
							}
							fmt.Fprintf(&sb, "%s (%s)", u.Name, u.Email)
							data[i] = &listitem.Generic{
								ID_:        strconv.FormatInt(int64(u.ID), 10),
								Name:       fmt.Sprintf("(%d) %s", u.ID, u.Username),
								SecondLine: sb.String(),
							}
						}
						return data
					},
				}),
			},
			"groups": {
				Title:    "Groups",
				Required: false,
				Flag: scaffoldcreate.FlagConfig{
					Name:  "gids",
					Usage: "IDs of the groups who's capabilities will be replaced",
				},
				Order: 180,
				Provider: scaffoldcreate.NewMSLProvider(nil, scaffoldcreate.MSLOptions{
					ListOptions: multiselectlist.Options{},
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListGroups(nil)
						if err != nil {
							clilog.Writer.Error("failed to get user list", log.KVErr(err))
						} else if len(lr.Results) < 1 {
							return nil
						}
						data := make([]multiselectlist.SelectableItem[string], len(lr.Results))
						for i, g := range lr.Results {
							data[i] = &listitem.Generic{
								ID_:        strconv.FormatInt(int64(g.ID), 10),
								Name:       fmt.Sprintf("(%d) %s", g.ID, g.Name),
								SecondLine: g.Description,
							}
						}
						return data
					},
				}),
			},
			"caps": {
				Title:    "Capabilities",
				Required: false,
				Flag: scaffoldcreate.FlagConfig{
					Name:  "capabilities",
					Usage: "Canonical names of the capabilities to set",
				},
				Order: 160,
				Provider: scaffoldcreate.NewMSLProvider(nil, scaffoldcreate.MSLOptions{
					ListOptions: multiselectlist.Options{},
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						allCaps, err := connection.Client.CapabilityList()
						if err != nil {
							clilog.Writer.Error("failed to get user list", log.KVErr(err))
						} else if len(allCaps) < 1 {
							return nil
						}
						data := make([]multiselectlist.SelectableItem[string], len(allCaps))
						for i, cap := range allCaps {
							data[i] = &multiselectlist.DefaultSelectableItem[string]{
								ID_:          strconv.FormatInt(int64(cap.Cap), 10),
								Title_:       cap.Name,
								Description_: cap.Desc,
							}
						}
						return data
					},
				}),
			},
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			// get set of entities to update
			var uids []int32
			if s := fields["users"].Provider.Get(); true {
				for sUID := range strings.SplitSeq(s, scaffoldcreate.MSLProviderSeparator) {
					id, err := strconv.ParseInt(sUID, 10, 32)
					if err != nil {
						clilog.Writer.Error("failed to parse user ID from MSLProvider Get() into int32", log.KV("string", sUID), log.KVErr(err))
						return 0, "", clilog.ErrInternal{}
					}
					uids = append(uids, int32(id))
				}
			}
			var gids []int32
			if s := fields["groups"].Provider.Get(); true {
				for sGID := range strings.SplitSeq(s, scaffoldcreate.MSLProviderSeparator) {
					id, err := strconv.ParseInt(sGID, 10, 32)
					if err != nil {
						clilog.Writer.Error("failed to parse group ID from MSLProvider Get() into int32", log.KV("string", sGID), log.KVErr(err))
						return 0, "", clilog.ErrInternal{}
					}
					gids = append(gids, int32(id))
				}
			}

			if len(uids)+len(gids) < 1 { // nonsense request
				return 0, "you must select at least one user/group to update", nil
			}

			// construct set of new caps
			var newCaps types.CapabilityState
			for cap := range strings.SplitSeq(fields["caps"].Provider.Get(), scaffoldcreate.MSLProviderSeparator) {
				newCaps.Grants = append(newCaps.Grants, cap)
			}
			// execute
			for _, uid := range uids {
				if err := connection.Client.SetUserCapabilities(uid, newCaps); err != nil {
					return 0, "", err
				}
			}
			for _, gid := range gids {
				if err := connection.Client.SetGroupCapabilities(gid, newCaps); err != nil {
					return 0, "", err
				}
			}
			return 0, "", nil // TODO what will this print?
		},
		scaffoldcreate.Options{
			Short: "replace capabilities of users and groups",
			Long:  "Supplant the capabilities of each selected user and group by providing a new set of capabilities.",
		})
}
