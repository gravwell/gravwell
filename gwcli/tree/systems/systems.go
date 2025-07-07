/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/* Package systemshealth defines a nav for actions related to the status of the backend. */
package systemshealth

import (
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/tree/systems/indexers"
	"github.com/gravwell/gravwell/v4/gwcli/tree/systems/ingesters"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	use   string = "systems"
	short string = "systems and health of the instance"
	long  string = "Review the state and health of your system."
)

var aliases []string = []string{"health", "status"}

func NewSystemsNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{
			indexers.NewIndexersNav(),
			ingesters.NewIngestersNav(),
		},
		[]action.Pair{
			newStorageAction(),
			newHardwareAction(),
		})
}

//#region storage
// a basic action for fetching indexer storage info.

// wrapper for the map returned by GetStorageStats.
type namedStorage struct {
	Disk  string
	Stats types.StorageStats
}

// Generates a list action that returns the storage statistics of all indexers in the Gravwell instance.
func newStorageAction() action.Pair {
	const (
		use   string = "storage"
		short string = "review storage statistics"
		long  string = "Fetch instance-wide storage statistics.\n" +
			"All data is in bytes, unless otherwise marked."
	)

	return scaffoldlist.NewListAction(short, long, namedStorage{},
		func(fs *pflag.FlagSet) ([]namedStorage, error) {
			ss, err := connection.Client.GetStorageStats()
			if err != nil {
				return []namedStorage{}, err
			}
			var wrap = make([]namedStorage, len(ss))
			var i = 0
			for disk, stats := range ss {
				wrap[i] = namedStorage{Disk: disk, Stats: stats}
				i += 1
			}

			return wrap, nil
		}, scaffoldlist.Options{Use: use})
}

//#endregion storage
