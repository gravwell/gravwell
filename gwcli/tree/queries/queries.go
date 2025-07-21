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
	long  string = "Queries contians utilities for managing auxillary query actions." +
		"Query creation is handled by the top-level `query` action."
)

var aliases []string = []string{"searches"}

func NewQueriesNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{scheduled.NewScheduledNav()},
		[]action.Pair{past(), attach.NewAttachAction()})
}

//#region past queries

const (
	pastUse string = "past"
)

func past() action.Pair {
	const (
		short = "display search history"
		long  = "display past searches made by your user"
	)
	var defaultColumns = []string{"UID", "GID", "EffectiveQuery"}

	return scaffoldlist.NewListAction(short, long,
		types.SearchLog{}, list,
		scaffoldlist.Options{
			Use: pastUse, AddtlFlags: flags, DefaultColumns: defaultColumns,
		})
}

func flags() pflag.FlagSet {
	const defaultCount = 30

	addtlFlags := pflag.FlagSet{}
	addtlFlags.Int("count", defaultCount, "the number of past searches to display.\n"+
		"If negative, fecthes entire history.")
	return addtlFlags
}

func list(fs *pflag.FlagSet) ([]types.SearchLog, error) {
	var (
		toRet []types.SearchLog
		err   error
	)

	if count, e := fs.GetInt("count"); e != nil {
		uniques.ErrGetFlag(pastUse, err)
	} else if count > 0 {
		toRet, err = connection.Client.GetSearchHistoryRange(0, count)
	} else {
		toRet, err = connection.Client.GetSearchHistory()
	}

	// check for explicit no records error
	if err != nil && strings.Contains(err.Error(), "No record") {
		return []types.SearchLog{}, nil
	}
	return toRet, err
}

//#endregion past queries
