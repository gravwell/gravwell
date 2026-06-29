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
			delete(),
			clone(),
		})
}

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

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("dashboard",
		func(dryrun bool, id string, _ *pflag.FlagSet) error {
			if dryrun {
				_, err := connection.Client.GetDashboard(id)
				return err
			}
			return connection.Client.DeleteDashboard(id)
		},
		func(params scaffolddelete.DataParameters) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListDashboards(params.QueryOpts)
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(lr.Results), nil
		},
		scaffolddelete.Options{QueryOptionsFlags: scaffold.QOInclude{Everything: true}})
}

func clone() action.Pair {
	return scaffoldselect.NewSelectAction("clone dashboards", "create a copy of one or many dashboards.",
		"dashboard",
		func(_ *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			dlr, err := connection.Client.ListDashboards(nil)
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(dlr.Results), nil
		},
		func(IDs []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(IDs))
			for i, id := range IDs {
				cur, err := connection.Client.GetDashboard(id)
				if err != nil {
					results[i] = scaffold.Result{
						Output:  fmt.Sprintf("failed to clone dashboard %s: %v", id, err),
						Success: false,
					}
					continue
				}
				new, err := connection.Client.CreateDashboard(cur)
				if err != nil {
					results[i] = scaffold.Result{
						Output:  fmt.Sprintf("failed to clone dashboard %s: %v", id, err),
						Success: false,
					}
					continue
				}
				results[i] = scaffold.Result{
					Output:  "cloned dashboard " + cur.Name + " into dashboard " + new.Name,
					Success: true,
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{Use: "clone"},
		})
}
