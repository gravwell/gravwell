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
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewKitsNav() *cobra.Command {
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
	const (
		short string = "list installed and staged kits"
		long  string = "lists kits available to your user" +
			"(or all kits on the system, via the --all flag if you are an admin)"
	)

	return scaffoldlist.NewListAction(
		short, long,
		types.IdKitState{}, func(fs *pflag.FlagSet) ([]types.IdKitState, error) {
			// if --all, use the admin version
			if all, err := fs.GetBool("all"); err != nil {
				uniques.ErrGetFlag("kist list", err)
			} else if all {
				return connection.Client.AdminListKits()
			}

			return connection.Client.ListKits()
		},
		scaffoldlist.Options{AddtlFlags: flags, DefaultColumns: []string{"UUID", "KitState.Name", "KitState.Description", "KitState.Version"}})
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Bool("all", false, "(admin-only) Fetch all kits on the system."+
		"Ignored if you are not an admin.")

	return addtlFlags
}

//#endregion list
