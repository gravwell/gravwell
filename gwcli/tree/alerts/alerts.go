/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package alerts provides actions for interacting with your alerts.
package alerts

import (
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	alertscreate "github.com/gravwell/gravwell/v4/gwcli/tree/alerts/create"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
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
		[]action.Pair{
			alertsList(),
			toggleEnabled(),
			delete(),
			alertscreate.Action(),
		})
}

// set and unset by list's ValidateArgs
var (
	listConsumerID   string
	listDispatcherID string
)

func alertsList() action.Pair {
	const (
		short string = "list your alerts"
		long  string = "lists alerts associated to your user. If admin mode is active, returns all alerts for all users."
	)

	return scaffoldlist.NewListAction(short, long, types.Alert{},
		func(fs *pflag.FlagSet) ([]types.Alert, error) {
			if listConsumerID != "" {
				resp, err := connection.Client.ListAlerts(&types.QueryOptions{
					Filters: []types.Filter{
						{
							Key:       "Consumers.ID",
							Operation: "=",
							Values:    []any{listConsumerID},
						},
					},
				})
				return resp.Results, err

			} else if listDispatcherID != "" {
				resp, err := connection.Client.ListAlerts(&types.QueryOptions{
					Filters: []types.Filter{
						{
							Key:       "Dispatchers.ID",
							Operation: "=",
							Values:    []any{listDispatcherID},
						},
					},
				})
				return resp.Results, err
			}

			resp, err := connection.Client.ListAlerts(nil)
			return resp.Results, err
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.String("consumer", "", "Filter to alerts that refer to this consumer. Should be the ID of the a flow. Used to answer: which alerts will launch this specific flow")
					fs.String("dispatcher", "", "Filter to alerts that refer to this dispatcher. Should be the ID of the a scheduled search. Used to answer: which alerts will be invoked by this specific scheduled search")
					return fs
				},
			},
			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Name",
				"CommonFields.Description",
				"Disabled",
				"Consumers",
				"Dispatchers",
				"TargetTag",
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, _ error) {
				if listConsumerID, invalid = validateListID("consumer", fs); invalid != "" {
					return invalid, nil
				}
				if listDispatcherID, invalid = validateListID("dispatcher", fs); invalid != "" {
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
func validateListID(flagName string, fs *pflag.FlagSet) (id string, invalid string) {
	s, err := fs.GetString(flagName)
	if err != nil {
		clilog.GetFlag(err)
	}
	return s, ""
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("alert", "alerts",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetAlert(id)
				return err
			}
			return connection.Client.DeleteAlert(id)
		},
		func() ([]scaffolddelete.Item[string], error) {
			alerts, err := connection.Client.ListAlerts(nil)
			if err != nil {
				return nil, err
			}
			// sort on name
			slices.SortStableFunc(alerts.Results,
				func(a, b types.Alert) int {
					return strings.Compare(a.Name, b.Name)
				})
			var items = make([]scaffolddelete.Item[string], len(alerts.Results))
			for i, a := range alerts.Results {
				items[i] = scaffolddelete.NewItem(a.Name, a.Description, a.ID)
			}
			return items, nil
		})
}
