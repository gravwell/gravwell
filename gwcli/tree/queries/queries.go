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
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/tree/queries/attach"
	"github.com/gravwell/gravwell/v4/gwcli/tree/queries/saved"
	"github.com/gravwell/gravwell/v4/gwcli/tree/queries/scheduled"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
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
		[]string{"queries"},
		[]*cobra.Command{scheduled.NewScheduledNav(), saved.NewSavedNav()},
		[]action.Pair{
			past(),
			attach.NewAttachAction(),
			info(),
			listAction(),
			stop(),
			importAction(),
			save(),
		})
}

// #region past queries

func past() action.Pair {
	const (
		pastUse string = "past"
		short   string = "display search history"
		long    string = "display past searches made by your user"
	)

	return scaffoldlist.NewListAction(
		short, long,
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
			CommonOptions: scaffold.CommonOptions{Use: pastUse},
			DefaultColumns: []string{
				"CommonFields.ID",
				"EffectiveQuery",
				"Launched",
			},
		})
}

// if details, uses ListSearchDetails to return ALL data relevant to a search.
func fetchActiveSearchesForMSL(details bool) ([]multiselectlist.SelectableItem[string], error) {
	if details {
		lsd, err := connection.Client.ListSearchDetails()
		if err != nil {
			return nil, err
		}
		items := make([]multiselectlist.SelectableItem[string], len(lsd))
		for i, s := range lsd {
			var secondLine strings.Builder
			if s.Error != "" {
				secondLine.WriteString("error: ")
				secondLine.WriteString(stylesheet.Cur.ErrorText.Render(s.Error))
			} else {
				secondLine.WriteString(fmt.Sprintf("duration: %s | item count: %v", s.Duration, s.ItemCount))
			}
			items[i] = &listitem.Generic{
				ID_:        s.ID,
				Name:       s.UserQuery,
				SecondLine: secondLine.String(),
			}
		}
		return items, nil
	}
	searches, err := connection.Client.ListSearchStatuses()
	if err != nil {
		return nil, err
	}
	data := make([]multiselectlist.SelectableItem[string], len(searches))
	for i, s := range searches {
		var secondLine strings.Builder
		secondLine.WriteString(s.State.String())
		secondLine.WriteString(" ")
		if s.Error != "" {
			secondLine.WriteString(stylesheet.Cur.ErrorText.Render(s.Error))
		} else {
			secondLine.WriteString(fmt.Sprintf("(started: %v) progress: %v", s.LaunchInfo.Started, s.State.Progress))
		}
		data[i] = &listitem.Generic{
			ID_:        s.ID,
			Name:       s.UserQuery,
			SecondLine: secondLine.String(),
		}
	}
	return data, nil
}

func info() action.Pair {
	var SIDs []types.SearchInfo // clobbered and set in validate
	return scaffoldlist.NewListAction("request info for a given query", "Request information about an active query",
		types.SearchInfo{}, func(addtlFlags *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.SearchInfo, error) {
			return SIDs, nil
		},
		nil, // TODO aliases, make sure fields match to list's alias
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "info",
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() > 1 {
					return phrases.AtLeast1ArgRequired("query IDs"), nil
				}
				SIDs = []types.SearchInfo{}
				for _, ID := range fs.Args() {
					si, err := connection.Client.SearchInfo(ID)
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
		types.SearchCtrlStatus{},
		func(addtlFlags *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.SearchCtrlStatus, error) {
			return connection.Client.ListSearchStatuses()
		},
		nil, // TODO aliases
		scaffoldlist.Options{})
}

func stop() action.Pair {
	return scaffoldselect.NewSelectAction("stop an active query", "Stop a running query", "search ID",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			return fetchActiveSearchesForMSL(false)
		},
		func(ID string, _ *pflag.FlagSet) (success string, _ error) {
			if err := connection.Client.StopSearch(ID); err != nil {
				if phrases.IsNotFoundErr(err) {
					return "", errors.New("there are no running searches with the ID: " + ID)
				}
				return "", err
			}
			return "stopped search " + ID, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "stop",
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
			return 0, "", connection.Client.ImportSearch(f, 0)

		},
		scaffoldcreate.Options{CommonOptions: scaffold.CommonOptions{Use: "import"}})
}

func save() action.Pair {
	return scaffoldselect.NewSelectAction("save a search", "", //TODO
		"search ID",
		func(_ *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			return fetchActiveSearchesForMSL(false)
		},
		func(ID string, addtlFlags *pflag.FlagSet) (success string, _ error) {
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
				ssp.SearchLaunchInfo.Expires = expire
			}

			if err := connection.Client.SaveSearch(ID, ssp); err != nil {
				return "", err
			}
			return "saved search " + ID, nil
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
			},
		},
	)
}
