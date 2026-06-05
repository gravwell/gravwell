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
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
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
			listAction(),
			deleteAction(),
			cloneAction(),
		})
}

//#region list

func listAction() action.Pair {
	return scaffoldlist.NewListAction("list dashboards", "list dashboards available to you and the system",
		types.Dashboard{}, func(_ *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.Dashboard, error) {
			r, err := connection.Client.ListDashboards(params.QueryOpts)
			return r.Results, err
		},
		nil,
		scaffoldlist.Options{DefaultColumns: []string{
			"CommonFields.ID",
			"CommonFields.Name",
			"CommonFields.Description",
		}})
}

//#region delete

func deleteAction() action.Pair {
	return scaffolddelete.NewDeleteAction("dashboard", "dashboards",
		del, fch, scaffolddelete.Options{})
}

func del(dryrun bool, id string) error {
	if dryrun {
		_, err := connection.Client.GetDashboard(id)
		return err
	}
	return connection.Client.DeleteDashboard(id)
}

func fch() ([]multiselectlist.SelectableItem[string], error) {
	lr, err := connection.Client.ListDashboards(&types.QueryOptions{Filters: []types.Filter{{Key: "OwnerID", Operation: "=", Values: []any{connection.CurrentUser().ID}}}})
	if err != nil {
		return nil, err
	}
	var items = make([]multiselectlist.SelectableItem[string], len(lr.Results))
	for i, d := range lr.Results {
		items[i] = &listitem.Generic{
			Selected_:  false,
			ID_:        d.ID,
			Name:       d.Name,
			SecondLine: d.Description,
		}
	}

	return items, nil
}

func cloneAction() action.Pair {
	return scaffoldselect.NewSelectAction("clone dashboards", "create a copy of one or many dashboards.",
		"dashboard",
		func(_ *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			dlr, err := connection.Client.ListDashboards(nil)
			if err != nil {
				return nil, err
			}
			items := make([]multiselectlist.SelectableItem[string], len(dlr.Results))
			for i, dash := range dlr.Results {
				items[i] = &listitem.Generic{
					Selected_:  false,
					ID_:        dash.ID,
					Name:       dash.Name,
					SecondLine: dash.Description,
				}
			}
			return items, nil
		},
		func(ID string, _ *pflag.FlagSet) (success string, _ error) {
			cur, err := connection.Client.GetDashboard(ID)
			if err != nil {
				return "", err
			}
			new, err := connection.Client.CreateDashboard(cur)
			if err != nil {
				return "", err
			}
			return "cloned dashboard " + cur.Name + " into dashboard " + new.Name, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{Use: "clone"},
		})
}
