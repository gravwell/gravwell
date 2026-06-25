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
	"slices"
	"strconv"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
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
			edit(),
			set(),
		})
}

func listCapabilities() action.Pair {
	return scaffoldlist.NewListAction(
		"list capabilities",
		"List every capability by its canonical name and description",
		types.CapabilityDesc{},
		func(_ *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.CapabilityDesc, error) {
			caps, err := connection.Client.CapabilityList()
			if err != nil {
				return nil, err
			}
			// sort by cat
			slices.SortStableFunc(caps, func(a, b types.CapabilityDesc) int {
				return strings.Compare(string(a.Category), string(b.Category))
			})
			return caps, nil
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
			DefaultColumns: []string{"Category", "Name", "Desc"},
		})
}

func listTemplates() action.Pair {
	return scaffoldlist.NewListAction(
		"list capability templates",
		"List every capability grouping (template)",
		types.CapabilityTemplate{},
		func(_ *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.CapabilityTemplate, error) {
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
			DefaultColumns: []string{"Name", "Desc", "Caps"},
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

			// sort by cat
			slices.SortStableFunc(caps, func(a, b capExp) int {
				return strings.Compare(string(a.Category), string(b.Category))
			})

			return caps, nil
		},
		nil, // implicit in the wrapper type
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "my",
				Aliases: []string{"self", "current", "my-capabilities", "my-caps"},
			},
			DefaultColumns: []string{"Category", "Name", "UserGrant", "GroupGrants"},
		})
}

type getCaps struct { // TODO should we be using this for list as well?
	ID     string // uid/gid
	Grants []string
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
					ID:     uidString,
					Grants: u.Grants,
				})
			}
			for _, gid := range gids {
				gidString := "gid" + strconv.FormatInt(int64(gid), 10)
				g, err := connection.Client.GetGroupCapabilities(gid)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", gidString, err)
				}
				items = append(items, getCaps{
					ID:     gidString,
					Grants: g.Grants,
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
func edit() action.Pair {
	var ( // everything here is destroyed and then re-set in validate
		id            int32  // id if the entity to fetch
		user          bool   // if not user, then group
		grant, revoke bool   // only do this, do not toggle
		noun          string // "user" or "group"
	)
	return scaffoldselect.NewSelectAction(
		"edit capabilities",
		"Tune the capabilities assigned to a single user or group.\n"+
			"If neither --grant nor --revoke are specified, the given set will supplant the set of capabilities assigned to the user or group.\n"+
			"Use "+stylesheet.Cur.Action.Render("cbac replace")+" if you wish to replace the capabilities of multiple groups/users at once.",
		"capability name",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			// NOTE(rlandau): grant/revoke do not affect cap collection for interactive mode; they only alter how the final set is constructed.

			// fetch all caps
			caps, err := connection.Client.CapabilityList()
			if err != nil {
				return nil, err
			}
			// sort by cat
			slices.SortStableFunc(caps, func(a, b types.CapabilityDesc) int {
				return strings.Compare(string(a.Category), string(b.Category))
			})

			// preselect using the caps the user/group has
			var cs types.CapabilityState
			if user {
				cs, err = connection.Client.GetUserCapabilities(id)
			} else {
				cs, err = connection.Client.GetGroupCapabilities(id)
			}
			if err != nil {
				return nil, err
			}
			// transform cs into a map for faster lookups
			preselections := map[string]bool{}
			for _, grant := range cs.Grants {
				preselections[grant] = true
			}

			data := make([]multiselectlist.SelectableItem[string], len(caps))
			for i, cap := range caps {
				data[i] = &multiselectlist.DefaultSelectableItem[string]{
					ID_:          cap.Name,
					Title_:       cap.Name,
					Description_: cap.Desc,
					Selected_:    preselections[cap.Name],
				}
			}
			return data, nil
		},
		func(CanonicalCapNames []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, err error) {
			// IDs now contains the full set of selected capabilities the user should have.
			// We need to compare it against their current set and toggle, only grant, or only revoke based on the given flags.
			// As we validate bare arguments, we can guarantee that the list of cap names contains only valid caps.

			switch {
			case grant: // add selected caps not in the entity's current set
				results = make([]scaffold.Result, 0, len(CanonicalCapNames))
				var (
					caps types.CapabilityState
				)
				if user {
					caps, err = connection.Client.GetUserCapabilities(id)
				} else {
					caps, err = connection.Client.GetGroupCapabilities(id)
				}
				if err != nil {
					return nil, fmt.Errorf("failed to refetch current %s capabilities: %w", noun, err)
				}
				// insert
				slices.Sort(caps.Grants)
				clilog.Writer.Debugf("%s ID %d current caps: %v", noun, id, caps)
				for _, cap := range CanonicalCapNames {
					idx, found := slices.BinarySearch(caps.Grants, cap)
					if !found {
						caps.Grants = slices.Insert(caps.Grants, idx, cap)
						results = append(results, scaffold.Result{
							Success: true,
							Output:  fmt.Sprintf("added capability '%s' to %s %d", cap, noun, id),
						})
					} else {
						clilog.Writer.Debug("redundant cap specified", log.KV("name", cap))
					}
				}

				if user {
					err = connection.Client.SetUserCapabilities(id, caps)
				} else {
					err = connection.Client.SetGroupCapabilities(id, caps)
				}
				if err != nil {
					return nil, fmt.Errorf("failed to install updated set of caps into %s: %w", noun, err)
				}

				return slices.Clip(results), err
			case revoke:
				results = make([]scaffold.Result, 0, len(CanonicalCapNames))
				var (
					caps types.CapabilityState
				)
				if user {
					caps, err = connection.Client.GetUserCapabilities(id)
				} else {
					caps, err = connection.Client.GetGroupCapabilities(id)
				}
				if err != nil {
					return nil, fmt.Errorf("failed to refetch current %s capabilities: %w", noun, err)
				}
				// remove caps
				slices.Sort(caps.Grants)
				clilog.Writer.Debugf("%s ID %d current caps: %v", noun, id, caps)
				for _, cap := range CanonicalCapNames {
					idx, found := slices.BinarySearch(caps.Grants, cap)
					if !found {
						clilog.Writer.Debug("cap specified for deletion not found in target's cap set", log.KV("name", cap))
						continue
					}
					caps.Grants = slices.Delete(caps.Grants, idx, idx+1)
					results = append(results, scaffold.Result{
						Success: true,
						Output:  fmt.Sprintf("removed capability '%s' from %s %d", cap, noun, id),
					})
				}

				if user {
					err = connection.Client.SetUserCapabilities(id, caps)
				} else {
					err = connection.Client.SetGroupCapabilities(id, caps)
				}
				if err != nil {
					return nil, fmt.Errorf("failed to install updated set of caps into %s: %w", noun, err)
				}

				return slices.Clip(results), err
			default: // supplant cap set with the selections
				cs := types.CapabilityState{Grants: CanonicalCapNames}
				if user {
					err = connection.Client.SetUserCapabilities(id, cs)
				} else {
					err = connection.Client.SetGroupCapabilities(id, cs)
				}
				if err != nil {
					return nil, fmt.Errorf("failed to install updated set of caps into %s: %w", noun, err)
				}
				return []scaffold.Result{
					{Success: true, Output: fmt.Sprintf("replaced %s ID %d's capability set with %v", noun, id, CanonicalCapNames)},
				}, nil
			}
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "edit",
				Usage: "edit " +
					ft.MutuallyExclusive([]string{"--uid", "--gid"}) +
					ft.Optional(ft.MutuallyExclusive([]string{"--grant", "--revoke"})),
				Example: "edit --uid=5",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Int32("uid", 0, "ID of the user to edit.\n"+
						"Mutually exclusive with --gid")
					fs.Int32("gid", 0, "ID of the group to edit.\n"+
						"Mutually exclusive with --uid")
					fs.Bool("grant", false, "Only grant caps; no caps will be removed through this call")
					fs.Bool("revoke", false, "Only revoke caps; no caps will be added through this call")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				// ensure all prior data is destroyed
				id = 0
				user, grant, revoke = false, false, false
				noun = ""

				// fetch specifications for this run
				uid, err := fs.GetInt32("uid")
				clilog.GetFlag(err)
				if uid != 0 {
					noun = "user"
					id = uid
					user = true
				}
				gid, err := fs.GetInt32("gid")
				clilog.GetFlag(err)
				if gid != 0 {
					if id != 0 {
						return "--uid and --gid are mutually exclusive", nil
					}
					noun = "group"
					id = gid
				}
				if id == 0 {
					return "you must specify --uid or --gid", nil
				}
				grant, err = fs.GetBool("grant")
				clilog.GetFlag(err)
				revoke, err = fs.GetBool("revoke")
				clilog.GetFlag(err)
				if grant && revoke {
					return "--grant and --revoke are mutually exclusive", nil
				}

				// ensure that pre-selected cap names are valid
				bare := fs.Args()
				if len(bare) > 0 {
					caps, err := connection.Client.CapabilityList()
					if err != nil {
						return "", err
					}
					for _, arg := range bare {
						if !slices.ContainsFunc(caps, func(c types.CapabilityDesc) bool {
							return c.Name == arg
						}) {
							return arg + " is not a valid, canonical capability name", nil
						}
					}
				}

				return "", nil
			},
		})
}

// TODO this would work better as a multistage (issues#2433)
func set() action.Pair {
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
							clilog.Writer.Error("failed to get group list", log.KVErr(err))
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
					Name: "caps",
					Usage: "Canonical names of the capabilities to set." +
						"Omitting this field will clear all capabilities from specified users and groups!",
				},
				Order: 160,
				Provider: scaffoldcreate.NewMSLProvider(nil, scaffoldcreate.MSLOptions{
					ListOptions: multiselectlist.Options{},
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						allCaps, err := connection.Client.CapabilityList()
						if err != nil {
							clilog.Writer.Error("failed to get capability list", log.KVErr(err))
						} else if len(allCaps) < 1 {
							return nil
						}
						data := make([]multiselectlist.SelectableItem[string], len(allCaps))
						for i, cap := range allCaps {
							data[i] = &multiselectlist.DefaultSelectableItem[string]{
								ID_:          cap.Name,
								Title_:       cap.Name,
								Description_: cap.Desc,
							}
						}
						return data
					},
				}),
			},
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (_ any, invalid string, _ error) {
			// get set of entities to update
			var uids []int32
			users := strings.TrimSpace(fields["users"].Provider.Get())
			if users != "" {
				for sUID := range strings.SplitSeq(users, scaffoldcreate.MSLProviderSeparator) {
					id, err := strconv.ParseInt(sUID, 10, 32)
					if err != nil {
						clilog.Writer.Error("failed to parse user ID from MSLProvider Get() into int32", log.KV("string", sUID), log.KVErr(err))
						return 0, "", clilog.ErrInternal{}
					}
					uids = append(uids, int32(id))
				}
			}

			var gids []int32
			groups := strings.TrimSpace(fields["groups"].Provider.Get())
			if groups != "" {
				for sGID := range strings.SplitSeq(groups, scaffoldcreate.MSLProviderSeparator) {
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
			if caps := strings.TrimSpace(fields["caps"].Provider.Get()); caps != "" {
				for cap := range strings.SplitSeq(caps, scaffoldcreate.MSLProviderSeparator) {
					newCaps.Grants = append(newCaps.Grants, cap)
				}
			}

			// execute
			for _, uid := range uids {
				if err := connection.Client.SetUserCapabilities(uid, newCaps); err != nil {
					return 0, "", fmt.Errorf("uid: %w", err)
				}
			}
			for _, gid := range gids {
				if err := connection.Client.SetGroupCapabilities(gid, newCaps); err != nil {
					return 0, "", fmt.Errorf("gid: %w", err)
				}
			}

			var sb strings.Builder
			// craft success message
			if len(newCaps.Grants) < 1 {
				sb.WriteString("removed all capabilities from")
			} else {
				sb.WriteString("replaced the capabilities of")
			}
			if len(uids) > 0 {
				fmt.Fprintf(&sb, " users %v", uids)
			}
			if len(gids) > 0 {
				if len(uids) > 0 {
					fmt.Fprintf(&sb, " and")
				}
				fmt.Fprintf(&sb, " groups %v", gids)
			}
			if len(newCaps.Grants) > 1 {
				fmt.Fprintf(&sb, " with %v", newCaps.Grants)
			}

			return "Replaced the capabilities of ", "", nil // TODO custom success message
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "set",
				Usage: "set " +
					ft.MutuallyExclusive([]string{"--uids", "--gids"}) +
					ft.Mandatory("--caps"),
				Aliases: []string{"replace"},
			},
			Short:              "set capabilities of users and groups",
			Long:               "Supplant the current capabilities of each selected user and group by providing a new set of capabilities.",
			IDIsSuccessMessage: true,
		})
}
