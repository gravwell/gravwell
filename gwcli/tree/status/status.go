/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package status

import (
	"gwcli/action"
	"gwcli/tree/status/indexers"
	"gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
)

const (
	use   string = "status"
	short string = "view system statuses"
	long  string = "Review the status and indicators of your system."
)

var aliases []string = []string{}

func NewStatusNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{
			indexers.NewIndexersNav(),
		},
		[]action.Pair{})
}
