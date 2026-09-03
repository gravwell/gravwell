/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package queries provides a nav that contains utilities related to interacting with existing or former queries.
All query creation is done at the top-level query action.
*/
package queries

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/annotations"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/tree/queries/attach"
	"github.com/gravwell/gravwell/v4/gwcli/tree/queries/saved"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	return treeutils.GenerateNav("searches", "manage existing and past searches",
		"Manage active, past, saved, and scheduled queries.\n"+
			"You can issue new searches using the top-level "+stylesheet.Cur.Action.Render("query")+" action.",
		[]*cobra.Command{saved.NewSavedNav()},
		[]action.Pair{
			past(),
			attach.NewAttachAction(),
			info(),
			listAction(),
			stop(),
			importAction(),
			save(),
			background(),
			delete(),
			setAccess(),
		},
		treeutils.NodeOptions{CommandAliases: []string{"queries"}},
	)
}

// #region past queries

func past() action.Pair {
	return scaffoldlist.NewListAction(
		"display search history", "display past searches made by your user",
		types.SearchHistoryEntry{},
		func(fs *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.SearchHistoryEntry, error) {
			resp, err := connection.Client.ListSearchHistory(params.QueryOpts)
			if err != nil {
				// check for explicit no records error
				if strings.Contains(err.Error(), "No record") {
					clilog.Writer.Debugf("no records error: %v", err)
					return nil, nil
				}
				return nil, err
			}
			return resp.Results, nil
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "past",
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.SearchHistory},
					XPermissions: []types.Capability{types.SearchHistory},
				},
			},
			DefaultColumns: []string{
				"CommonFields.ID",
				"EffectiveQuery",
				"Launched",
			},
			QueryOptionsFlags: scaffold.QOInclude{AllData: true, Limit: true},
		})
}

// if details, uses ListSearchDetails to return ALL data relevant to a search.
//
// Requires types.Search, types.AttachSearch (as ListSearches requires both).
//
// TODO install omit
func fetchActiveSearchesForMSL(details bool) ([]multiselectlist.SelectableItem[string], error) {
	lsd, err := connection.Client.ListSearches(nil)
	if err != nil {
		return nil, err
	}
	items := make([]multiselectlist.SelectableItem[string], len(lsd.Results))
	for i, s := range lsd.Results {
		var secondLine strings.Builder
		if s.Error != "" {
			secondLine.WriteString("error: ")
			secondLine.WriteString(stylesheet.Cur.ErrorText.Render(s.Error))
		} else {
			fmt.Fprintf(&secondLine, "duration: %s | item count: %v", s.Duration, s.ItemCount)
		}
		items[i] = &listitem.Generic{
			ID_:        s.ID,
			Name:       s.UserQuery,
			SecondLine: secondLine.String(),
		}
	}
	return items, nil
}

func info() action.Pair {
	var SIDs []types.SearchInfo // clobbered and set in validate
	return scaffoldlist.NewListAction("request info for a given query", "Request information about an active query",
		types.SearchInfo{}, func(addtlFlags *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.SearchInfo, error) {
			return SIDs, nil
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "info",
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.Search, types.AttachSearch},
					XPermissions: []types.Capability{types.Search, types.AttachSearch},
				},
			},
			DefaultColumns: []string{"ID", "UID"},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() < 1 {
					return phrases.AtLeast1ArgRequired("query IDs"), nil
				}
				SIDs = []types.SearchInfo{}
				for _, ID := range fs.Args() {
					si, err := connection.Client.GetSearch(ID)
					if phrases.IsNotFoundErr(err) {
						return phrases.ErrUnknownSID(ID).Error(), nil
					} else if err != nil {
						return "", err
					}
					SIDs = append(SIDs, si)
				}
				return "", nil
			},
		})
}

func listAction() action.Pair {
	return scaffoldlist.NewListAction("list active queries", "List all current queries.",
		types.SearchInfo{},
		func(addtlFlags *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.SearchInfo, error) {
			if params.QueryOpts.AdminMode {
				resp, err := connection.Client.ListAllSearches(nil)
				return resp.Results, err
			}
			resp, err := connection.Client.ListSearches(nil)
			return resp.Results, err
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.Search, types.AttachSearch},
					XPermissions: []types.Capability{types.Search, types.AttachSearch},
				},
			},
			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Owner.Username",
				"UserQuery",
				"State.Status",
				"Webserver",
				"AttachedClients",
			},
			QueryOptionsFlags: scaffold.QOOmit{
				AllData:        false,
				IncludeDeleted: true,
				Limit:          true,
			},
		})
}

func stop() action.Pair {
	return scaffoldselect.NewSelectAction("stop an active query", "Stop a running query without deleting it entirely.", "search ID",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			return fetchActiveSearchesForMSL(false)
		},
		func(IDs []string, _ *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				if err := connection.Client.StopSearch(ID); err != nil {
					var msg string
					if phrases.IsNotFoundErr(err) {
						msg = "there are no running searches with the ID: " + ID
					} else {
						msg = fmt.Sprintf("failed to stop search %s: %v", ID, err)
					}
					results[i] = scaffold.Result{Success: false, Output: msg}
				} else {
					results[i] = scaffold.Result{Success: true, Output: "stopped search " + ID}
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "stop",
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.Search, types.AttachSearch},
					XPermissions: []types.Capability{types.Search, types.AttachSearch},
				},
			},
			NoItemsError: func(fs *pflag.FlagSet) string { return "There are no running queries (that you can access)" }},
	)

}

func importAction() action.Pair {
	return scaffoldcreate.NewCreateAction("archived search",
		map[string]scaffoldcreate.Field{
			"path": scaffoldcreate.FieldPath("archived search", true),
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			pth := fields["path"].Provider.Get()
			f, err := os.Open(pth)
			if err != nil {
				return 0, "", err
			}
			defer clilog.CloseFile(f)
			return 0, "", connection.Client.ImportSearch(f, 0)

		},
		scaffoldcreate.Options{CommonOptions: scaffold.CommonOptions{
			Use: "import",
			Requirements: annotations.Requirements{
				IPermissions: []types.Capability{types.SaveSearch, types.Search, types.SetSearchGroup},
				XPermissions: []types.Capability{types.SaveSearch, types.Search, types.SetSearchGroup},
			},
		}})
}

func save() action.Pair {
	return scaffoldselect.NewSelectAction("save a search",
		"Request that a search be saved and optionally modify the expiration or add a name and notes.\n"+
			"Saving a search will keep the results around until you explicitly delete them.",
		"search ID",
		func(_ *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			return fetchActiveSearchesForMSL(false)
		},
		func(IDs []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) {

			name, err := addtlFlags.GetString(ft.Name.Name())
			clilog.GetFlag(err)
			notes, err := addtlFlags.GetString("notes")
			clilog.GetFlag(err)

			ssp := types.SaveSearchPatch{
				Name:  name,
				Notes: notes,
			}
			if expire, err := addtlFlags.GetTime("expire"); err != nil {
				clilog.GetFlag(err)
			} else if !expire.IsZero() {
				ssp.Expires = expire
			}
			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				if err := connection.Client.SaveSearch(ID, ssp); err != nil {
					results[i] = scaffold.Result{Success: false, Output: fmt.Sprintf("failed to save search %s: %v", ID, err)}
				} else {
					results[i] = scaffold.Result{Success: true, Output: "saved search " + ID}
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "save",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.String(ft.Name.Name(), "", "name the newly saved search")
					fs.String("notes", "", "attach extra information to the saved search")
					fs.Time("expire", time.Time{}, []string{uniques.SearchTimeFormat}, "override the expiration time of the this search")
					return fs
				},
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.Search, types.AttachSearch, types.SaveSearch},
					XPermissions: []types.Capability{types.SaveSearch},
				},
			},
		},
	)
}

func background() action.Pair {
	return scaffoldselect.NewSelectAction("background a search",
		"Background the specified search such that it can continue running without any connected clients.\n"+
			"Note that backgrounded searches do not persist across webserver restarts;"+
			"to keep results around permanently, use the “Save results” option.\n"+
			"Unsaved background queries are automatically deleted after 7 days. Save your search to preserve results permanently.\n"+
			"\n"+
			"Use `queries attach` to foreground a search.",
		"search ID",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			return fetchActiveSearchesForMSL(false)
		},
		func(IDs []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				if err := connection.Client.BackgroundSearch(ID); err != nil {
					results[i] = scaffold.Result{Success: false, Output: fmt.Sprintf("failed to background search %s: %v", ID, err)}
				} else {
					results[i] = scaffold.Result{Success: true, Output: "backgrounded search " + ID}
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.Search, types.AttachSearch, types.BackgroundSearch},
					XPermissions: []types.Capability{types.BackgroundSearch},
				},
			},
		})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("search ID",
		func(dryrun bool, ID string, _ *pflag.FlagSet) error {
			if dryrun {
				_, err := connection.Client.GetSearch(ID)
				return err
			}
			return connection.Client.DeleteSearch(ID)
		},
		func(_ scaffolddelete.DataParameters) ([]multiselectlist.SelectableItem[string], error) {
			return fetchActiveSearchesForMSL(false)
		},
		scaffolddelete.Options{
			QueryOptionsFlags: scaffold.QOOmit{Everything: true},
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.Search, types.AttachSearch},
					XPermissions: []types.Capability{types.Search},
				},
			},
		})
}

// TODO: this should be converted to a scaffoldcreate with MSL fields after the scaffoldcreate/edit merge.
func setAccess() action.Pair {
	var (
		readerGroups, writerGroups       []int32
		readerGroupsSet, writerGroupsSet bool
		readerGlobal, writerGlobal       bool
		readerGlobalSet, writerGlobalSet bool
	)

	return scaffoldselect.NewSelectAction("set the read/write access of a search",
		`Modify which groups can read or write a set of searches, and/or whether they are globally readable/writable by any user. Only flags you explicitly pass are changed. Anything you omit is left as-is. Only admins may set --reader-global or --writer-global to true, the server will reject this action otherwise. At least one of --reader-groups, --writer-groups, --reader-global, or --writer-global must be given`, "search ID", func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			return fetchActiveSearchesForMSL(false)
		}, func(ids []string, _ *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(ids))
			for i, id := range ids {
				si, err := connection.Client.GetSearch(id)
				if err != nil {
					results[i] = scaffold.Result{Success: false, Output: fmt.Sprintf("failed to fetch search: %s: %v", id, err)}
					continue
				}

				readers, writers := si.Readers, si.Writers
				if readerGroupsSet {
					readers.GIDs = readerGroups
				}
				if readerGlobalSet {
					readers.Global = readerGlobal
				}
				if writerGroupsSet {
					writers.GIDs = writerGroups
				}
				if writerGlobalSet {
					writers.Global = writerGlobal
				}

				if err := connection.Client.SetAccess(id, si.OwnerID, readers, writers); err != nil {
					results[i] = scaffold.Result{Success: false, Output: fmt.Sprintf("failed to set access for search %s: %v", id, err)}
					continue
				}

				results[i] = scaffold.Result{Success: true, Output: "updated access for search " + id}
			}

			return results, nil
		}, scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "set-access",
				Aliases: []string{"set-groups", "set-group"},
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Int32Slice("reader-groups", nil, "Groups allowed to read the search. Omit to leave unchanged; pass an empty list to clear.")
					fs.Int32Slice("writer-groups", nil, "Groups allowed to write the search. Omit to leave unchanged; pass an empty list to clear.")
					fs.Bool("reader-global", false, "Whether the search is globally readable by any user. Omit to leave unchanged. Requires admin to set true.")
					fs.Bool("writer-global", false, "Whether the search is globally writable by any user. Omit to leave unchanged. Requires admin to set true.")
					return fs
				},
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.Search, types.AttachSearch, types.SetSearchGroup},
					XPermissions: []types.Capability{types.SetSearchGroup},
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if readerGroupsSet = fs.Changed("reader-groups"); readerGroupsSet {
					readerGroups, err = fs.GetInt32Slice("reader-groups")
					clilog.GetFlag(err)
				}
				if writerGroupsSet = fs.Changed("writer-groups"); writerGroupsSet {
					writerGroups, err = fs.GetInt32Slice("writer-groups")
					clilog.GetFlag(err)
				}
				if readerGlobalSet = fs.Changed("reader-global"); readerGlobalSet {
					readerGlobal, err = fs.GetBool("reader-global")
					clilog.GetFlag(err)
				}
				if writerGlobalSet = fs.Changed("writer-global"); writerGlobalSet {
					writerGlobal, err = fs.GetBool("writer-global")
					clilog.GetFlag(err)
				}

				if !readerGroupsSet && !writerGroupsSet && !readerGlobalSet && !writerGlobalSet {
					return "you must specify at least one of --reader-groups, --writer-groups, --reader-global, or --writer-global", nil
				}

				return "", nil
			},
		})
}
