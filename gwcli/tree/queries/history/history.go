/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package history

import (
	"gwcli/action"
	"gwcli/clilog"
	"gwcli/utilities/scaffold/scaffoldlist"
	"strings"

	grav "github.com/gravwell/gravwell/v3/client"

	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/spf13/pflag"
)

const (
	use   string = "history"
	short string = "display search history"
	long  string = "display past searches made by your user"
)

var (
	defaultColumns []string = []string{"UID", "GID", "EffectiveQuery"}
)

const defaultCount = 30

func NewQueriesHistoryListAction() action.Pair {
	return scaffoldlist.NewListAction(use, short, long, defaultColumns,
		types.SearchLog{}, list, flags)
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Int("count", defaultCount, "the number of past searches to display.\n"+
		"If negative, fecthes entire history.")
	return addtlFlags
}

func list(c *grav.Client, fs *pflag.FlagSet) ([]types.SearchLog, error) {
	var (
		toRet []types.SearchLog
		err   error
	)

	if count, e := fs.GetInt("count"); e != nil {
		clilog.LogFlagFailedGet("count", err)
	} else if count > 0 {
		toRet, err = c.GetSearchHistoryRange(0, count)
	} else {
		toRet, err = c.GetSearchHistory()
	}

	// check for explicit no records error
	if err != nil && strings.Contains(err.Error(), "No record") {
		return []types.SearchLog{}, nil
	}
	return toRet, err
}
