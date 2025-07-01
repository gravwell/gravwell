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
	"fmt"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/utils/weave"

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
		short string = "review storage statistics for all indexers"
	)
	var long = fmt.Sprintf("Fetch storage statistics across all indexers.\n"+
		"Use the %v %v action for more detailed information about a specified indexer.", stylesheet.Cur.Nav.Render("indexer"), stylesheet.Cur.Action.Render("inspect"))
	// default to using all columns
	cols, err := weave.StructFields(namedStorage{}, true)
	if err != nil { // something has gone horribly wrong
		clilog.Writer.Criticalf("failed to divine fields from storage wrapper: %v", err)
		cols = []string{}
	}

	return scaffoldlist.NewListAction(short, long, cols, namedStorage{},
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
