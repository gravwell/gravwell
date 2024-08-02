/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package dashboards

import (
	"gwcli/action"
	"gwcli/tree/dashboards/delete"
	"gwcli/tree/dashboards/list"
	"gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
)

const (
	use   string = "dashboards"
	short string = "manage your dashboards"
	long  string = "Manage and view your available web dashboards." +
		"Dashboards are not usable from the CLI, but can be altered."
)

var aliases []string = []string{"dashboard", "dash"}

func NewDashboardNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{},
		[]action.Pair{
			list.NewDashboardsListAction(),
			delete.NewDashboardDeleteAction(),
		})
}
