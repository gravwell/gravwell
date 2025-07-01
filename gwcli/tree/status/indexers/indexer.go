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
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
			newStatsListAction(),
			newInspectBasicAction(),
		})
}

//#region stats

// wrapper for the SysStats map
type namedStats struct {
	Indexer string
	Stats   types.HostSysStats
}

func newStatsListAction() action.Pair {
	const (
		use   string = "stats"
		short string = "review the statistics of each indexer"
		long  string = "Review the statistics of each indexer"
	)

	return scaffoldlist.NewListAction(
		short, long,
		namedStats{}, listStats, scaffoldlist.Options{
			Use:    use,
			Pretty: nil, // TODO
		})
}

func listStats(fs *pflag.FlagSet) ([]namedStats, error) {
	var ns []namedStats

	stats, err := connection.Client.GetSystemStats()
	if err != nil {
		return []namedStats{}, err
	}
	ns = make([]namedStats, len(stats))

	// wrap the results in namedStats
	var i = 0
	for k, v := range stats {
		ns[i] = namedStats{Indexer: k, Stats: *v.Stats}
		i += 1
	}

	return ns, nil
}

//#endregion stats
