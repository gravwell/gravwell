/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package kits provides actions for interacting with kits. *jazz hands*
package kits

import (
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	const (
		use   string = "kits"
		short string = "view kits associated to this instance"
		long  string = "Kits bundle up of related items (dashboards, queries, scheduled searches," +
			" autoextractors) for easy installation."
	)
	var aliases = []string{"kit"}
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{},
		[]action.Pair{newKitsListAction()})
}

//#region list

func newKitsListAction() action.Pair {
	const short string = "list installed and staged kits"

	return scaffoldlist.NewListAction(
		short, "Lists kits available to your user",
		types.IdKitState{}, func(fs *pflag.FlagSet, param scaffoldlist.DataParameters) ([]types.IdKitState, error) {
			if param.QueryOpts.AdminMode {
				return connection.Client.AdminListKits()
			}

			return connection.Client.ListKits()
		},
		nil,
		scaffoldlist.Options{
			DefaultColumns: []string{
				"UUID",
				"KitState.Name",
				"KitState.Description",
				"KitState.Version",
			},
			Omit: scaffold.OmitFlags{IncludeDeleted: true, Limit: true},
		})
}

//#endregion list
