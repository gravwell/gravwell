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
	"strconv"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
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

// set and unset by list's ValidateArgs
var (
	listConsumerID   string
	listDispatcherID string
)

func list() action.Pair {
	const (
		short string = "list your alerts"
		long  string = "lists alerts associated to your user. If admin mode is active, returns all alerts for all users."
	)

	return scaffoldlist.NewListAction(short, long, types.AlertDefinition{},
		func(fs *pflag.FlagSet) ([]types.AlertDefinition, error) {
			if listConsumerID != "" {
				return connection.Client.GetAlertsByConsumer(listConsumerID, types.ALERTCONSUMERTYPE_FLOW) // there is currently only 1 type

			} else if listDispatcherID != "" {
				return connection.Client.GetAlertsByDispatcher(listDispatcherID, types.ALERTDISPATCHERTYPE_SCHEDULEDSEARCH) // there is currently only 1 type
			}

			return connection.Client.GetAlerts()
		},
		scaffoldlist.Options{
			AddtlFlags: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.String("consumer", "", "Filter to alerts that refer to this consumer. Should be the ID of the a flow, not the GUID. Used to answer: which alerts will launch this specific flow")
				fs.String("dispatcher", "", "Filter to alerts that refer to this dispatcher. Should be the ID of the a scheduled search, not the GUID. Used to answer: which alerts will be invoked by this specific scheduled search")

				return fs
			},
			DefaultColumns: []string{"Name", "Description", "Disabled", "Consumers", "Dispatchers", "GUID", "Global", "Labels", "TargetTag", "ThingUUID", "UID"},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if listConsumerID, invalid = validateListID("consumer", fs); err != nil {
					return invalid, nil
				}
				if listDispatcherID, invalid = validateListID("dispatcher", fs); err != nil {
					return invalid, nil
				}

				if listConsumerID != "" && listDispatcherID != "" {
					return "--consumer and --dispatcher are mutually exclusive", nil
				}
				return "", nil
			},
		})
}

// helper function for list's ValidateArgs.
// Tests that, if the flag was set, it is a valid uint.
func validateListID(flagName string, fs *pflag.FlagSet) (id string, invalid string) {
	s, err := fs.GetString(flagName)
	if err != nil {
		clilog.LogFlagFailedGet(flagName, err)
	} else if _, err := uuid.Parse(s); err == nil {
		return "", "--" + flagName + " expects a numeric id, not a UUID"
	} else if _, err := strconv.ParseUint(s, 10, 64); err != nil {
		return "", "--" + flagName + " must be a valid number > 0"
	}
	return s, ""
}
