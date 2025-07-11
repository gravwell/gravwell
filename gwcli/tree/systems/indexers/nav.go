/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package indexers contains actions for fetching information about the state of the indexers.
package indexers

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
)

func NewIndexersNav() *cobra.Command {
	const (
		use   string = "indexers"
		short string = "view indexer status"
	)

	var long = "Review the health, storage, configuration, and history of indexers. Use " + stylesheet.Cur.Action.Render("list")

	var aliases = []string{"index", "idx", "indexer"}

	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{},
		[]action.Pair{
			get(),
			list(),
			newCalendarAction(),
		})
}
