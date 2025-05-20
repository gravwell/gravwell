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
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/tree/queries/history"
	"github.com/gravwell/gravwell/v4/gwcli/tree/queries/scheduled"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
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
		[]action.Pair{history.NewQueriesHistoryListAction()})
}
