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
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/tree/queries/attach"
	"github.com/gravwell/gravwell/v4/gwcli/tree/queries/scheduled"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	use   string = "queries"
	short string = "manage existing and past queries"
	long  string = "Queries contains utilities for managing auxiliary query actions." +
		"Query creation is handled by the top-level `query` action."
)

var aliases []string = []string{"searches"}

func NewQueriesNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{scheduled.NewScheduledNav()},
		[]action.Pair{past(), attach.NewAttachAction()})
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
		func(fs *pflag.FlagSet) ([]types.SearchHistoryEntry, error) {
			opts := &types.QueryOptions{}
			if count, e := fs.GetInt("count"); e != nil {
				return nil, uniques.ErrGetFlag(pastUse, e)
			} else if count > 0 {
				opts.Limit = count
			}

			resp, err := connection.Client.ListSearchHistory(opts)
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
		scaffoldlist.Options{
			Use: pastUse, AddtlFlags: flags,
			DefaultColumns: []string{
				"ID",
				"UserQuery",
				"EffectiveQuery",
				"Launched",
			},
		})
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Int("count", 0, "the number of past searches to display.\n"+
		"If negative or 0, fetches entire history")
	return addtlFlags
}

//#endregion past queries
