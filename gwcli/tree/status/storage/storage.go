/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/* Package storage defines a basic action for fetching indexer storage info. */
package storage

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/spf13/pflag"
)

// wrapper for the map returned by GetStorageStats.
type namedStorage struct {
	Disk  string
	Stats types.StorageStats
}

// NewAction generates a list action that returns the storage statistics of all indexers in the Gravwell instance..
func NewAction() action.Pair {
	const (
		use   string = "storage"
		short string = "review storage statistics"
	)
	var long = "Fetch instance-wide storage statistics.\n" +
		"All data is in bytes, unless otherwise marked."

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
