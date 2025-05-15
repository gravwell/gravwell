/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package list

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/spf13/pflag"
)

var (
	short          string   = "list dashboards"
	long           string   = "list dashboards available to you and the system"
	defaultColumns []string = []string{"ID", "Name", "Description"}
)

func NewDashboardsListAction() action.Pair {
	return scaffoldlist.NewListAction("", short, long, defaultColumns,
		types.Dashboard{}, list, flags)
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Bool(ft.Name.ListAll, false, ft.Usage.ListAll("dashboards"))

	return addtlFlags
}

func list(c *grav.Client, fs *pflag.FlagSet) ([]types.Dashboard, error) {
	if all, err := fs.GetBool(ft.Name.ListAll); err != nil {
		clilog.LogFlagFailedGet(ft.Name.ListAll, err)
	} else if all {
		return c.GetAllDashboards()
	}
	return c.GetUserDashboards(connection.MyInfo.UID)
}
