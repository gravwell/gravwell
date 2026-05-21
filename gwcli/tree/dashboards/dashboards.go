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
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
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
			listAction(),
			deleteAction(),
			cloneAction(),
		})
}

//#region list

func listAction() action.Pair {
	return scaffoldlist.NewListAction("list dashboards", "list dashboards available to you and the system",
		types.Dashboard{}, list,
		nil,
		scaffoldlist.Options{CommonOptions: scaffold.CommonOptions{AddtlFlags: flags}, DefaultColumns: []string{
			"ID",
			"Name",
			"Description",
		}})
}

func flags() *pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	ft.GetAll.Register(&addtlFlags, true, "dashboards")

	return &addtlFlags
}

func list(fs *pflag.FlagSet) ([]types.Dashboard, error) {
	if all, err := fs.GetBool(ft.GetAll.Name()); err != nil {
		clilog.GetFlag(err)
	} else if all {
		return connection.Client.GetAllDashboards()
	}
	return connection.Client.GetUserDashboards(connection.CurrentUser().ID)
}

//#region delete

func deleteAction() action.Pair {
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
	ud, err := connection.Client.GetUserDashboards(connection.CurrentUser().ID)
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

func cloneAction() action.Pair {
	return scaffoldselect.NewSelectAction("clone dashboards", "create a copy of one or many dashboards.",
		"dashboard", "dashboards",
		func() ([]multiselectlist.SelectableItem[string], error) {
			dashboards, err := connection.Client.GetAllDashboards()
			if err != nil {
				return nil, err
			}
			items := make([]multiselectlist.SelectableItem[string], len(dashboards))
			for i, dash := range dashboards {

			}
		},
		func(ID string) error {},
		func(s string) (cast string, inv string) { return s, "" },
		scaffoldselect.Options{})

	return scaffold.NewBasicAction("clone", "clone a dashboard", "Create a copy of a dashboard by its ID.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			origID, err := strconv.ParseUint(fs.Arg(0), 10, 64)
			if err != nil {
				return fs.Arg(0) + " is not a valid dashboard ID", nil
			}
			newID, err := connection.Client.CloneDashboard(origID)
			if err != nil {
				return err.Error(), nil
			}
			return fmt.Sprintf("created clone with ID %d", newID), nil
		},
		scaffold.BasicOptions{
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("dashboard ID"), nil
				}
				if _, err := strconv.ParseUint(fs.Arg(0), 10, 64); err != nil {
					return fs.Arg(0) + " is not a valid dashboard ID", nil
				}
				return "", nil
			},
		})
}
