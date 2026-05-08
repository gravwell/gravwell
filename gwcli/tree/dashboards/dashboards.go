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

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

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

	return scaffoldlist.NewListAction(short, long,
		types.Dashboard{}, list,
		scaffoldlist.Options{AddtlFlags: flags, DefaultColumns: []string{"ID", "Name", "Description"}})
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	ft.GetAll.Register(&addtlFlags, true, "dashboards")

	return addtlFlags
}

func list(fs *pflag.FlagSet) ([]types.Dashboard, error) {
	if all, err := fs.GetBool(ft.GetAll.Name()); err != nil {
		uniques.ErrGetFlag("dashboards list", err)
	} else if all {
		return connection.Client.GetAllDashboards()
	}
	return connection.Client.GetUserDashboards(connection.CurrentUser().UID)
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
	ud, err := connection.Client.GetUserDashboards(connection.CurrentUser().UID)
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
