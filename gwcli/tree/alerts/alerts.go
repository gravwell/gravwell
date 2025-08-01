/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package alerts provides actions for interacting with your alerts.
package alerts

import (
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewAlertsNav() *cobra.Command {
	const (
		use   string = "alerts"
		short string = "manage alerts"
		long  string = "Alerts allow you to tie sources of intelligence (such as periodic scheduled searches) to actions (such as a flow that files a ticket)." +
			" This can make it much simpler to take automatic action when something of interest occurs."
	)
	return treeutils.GenerateNav(use, short, long, []string{"alert"}, []*cobra.Command{},
		[]action.Pair{list()})
}

func list() action.Pair {
	const (
		short string = "list your alerts"
		long  string = "lists alerts associated to your user. If admin mode is active, returns all alerts for all users."
	)

	return scaffoldlist.NewListAction(short, long, types.AlertDefinition{},
		func(fs *pflag.FlagSet) ([]types.AlertDefinition, error) {
			return connection.Client.GetAlerts()
		},
		scaffoldlist.Options{
			DefaultColumns: []string{"Name", "Description", "Disabled", "Consumers", "Dispatchers", "GUID", "Global", "Labels", "TargetTag", "ThingUUID", "UID"},
		})
}
