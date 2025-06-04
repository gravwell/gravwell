/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/* Package indexers defines a nav for status actions related to the indexers. */
package indexers

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/tree/status/indexers/stats"
	"github.com/gravwell/gravwell/v4/gwcli/tree/status/indexers/storage"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
)

const (
	use   string = "indexers"
	short string = "view indexer status"
	long  string = "Review the status, storage, and state of indexers associated to your instance."
)

var aliases []string = []string{"index", "idx", "indexer"}

func NewIndexersNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{},
		[]action.Pair{
			storage.NewIndexerStorageAction(),
			stats.NewStatsListAction(),
		})
}
