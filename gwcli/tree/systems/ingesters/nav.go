/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package ingesters contains actions for fetching information about the state of the ingesters.
package ingesters

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
)

func NewIngestersNav() *cobra.Command {
	const (
		use   string = "ingesters"
		short string = "review the state of ingesters"
		long  string = "Review information for all ingesters or get detailed information about a specific ingester."
	)

	return treeutils.GenerateNav(use, short, long, []string{},
		[]*cobra.Command{},
		[]action.Pair{
			list(),
			get(),
		})
}
