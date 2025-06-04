/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package dashboards contains actions for interacting with web gui dashboards
package dashboards

import (
	"fmt"
	"time"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewDashboardNav() *cobra.Command {
	const (
		use   string = "dashboards"
		short string = "manage your dashboards"
		long  string = "Manage and view your available web dashboards." +
			"Dashboards are not usable from the CLI, but can be altered."
	)

	var aliases = []string{"dashboard", "dash"}
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{},
		[]action.Pair{
			newDashboardsListAction(),
			newDashboardDeleteAction(),
		})
}

//#region list

func newDashboardsListAction() action.Pair {
	const (
		short string = "list dashboards"
		long  string = "list dashboards available to you and the system"
	)
	var defaultColumns = []string{"ID", "Name", "Description"}

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

//#endregion list

//#region delete

func newDashboardDeleteAction() action.Pair {
	return scaffolddelete.NewDeleteAction("dashboard", "dashboards",
		del, fch)
}

func del(dryrun bool, id uint64) error {
	if dryrun {
		_, err := connection.Client.GetDashboard(id)
		return err
	}
	return connection.Client.DeleteDashboard(id)
}

func fch() ([]scaffolddelete.Item[uint64], error) {
	ud, err := connection.Client.GetUserDashboards(connection.MyInfo.UID)
	if err != nil {
		return nil, err
	}
	// not too important to sort this one
	var items = make([]scaffolddelete.Item[uint64], len(ud))
	for i, u := range ud {
		items[i] = scaffolddelete.NewItem(u.Name,
			fmt.Sprintf("Updated: %v\n%s",
				ud[i].Updated.Format(time.RFC822), ud[i].Description),
			u.ID)
	}

	return items, nil
}

//#endregion delete
