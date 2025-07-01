/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package history provides an action to list search history.
package history

import (
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/gravwell/gravwell/v4/client/types"
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
	return scaffoldlist.NewListAction(short, long,
		types.SearchLog{}, list,
		scaffoldlist.Options{Use: use, AddtlFlags: flags})
}

func flags() pflag.FlagSet {
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
		uniques.ErrGetFlag(use, err)
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
