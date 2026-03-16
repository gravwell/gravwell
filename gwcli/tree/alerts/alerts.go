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
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
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
			list(),
			toggle(),
			delete(),
		})
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
			DefaultColumns: []string{
				"Name",
				"Description",
				"Disabled",
				"Consumers",
				"Dispatchers",
				"GUID",
				"Global",
				"Labels",
				"TargetTag",
				"ThingUUID",
				"UID",
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
// Tests that, if the flag was set, it is a valid uint.
func validateListID(flagName string, fs *pflag.FlagSet) (id string, invalid string) {
	s, err := fs.GetString(flagName)
	if err != nil {
		clilog.LogFlagFailedGet(flagName, err)
	} else if s != "" {
		if _, err := uuid.Parse(s); err == nil {
			return "", "--" + flagName + " expects a numeric id, not a UUID"
		} else if _, err := strconv.ParseUint(s, 10, 64); err != nil {
			return "", "--" + flagName + " must be a valid number > 0"
		}
	}
	return s, ""
}

// Used to enable/disable an alert
func toggle() action.Pair {
	return scaffold.NewBasicAction("toggle", "enable or disable an alert",
		"Toggle the state of an alert. You may provide --enable or --disable to ensure the alert is in the respective state.",
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			// find the alert in question
			id := fs.Arg(0)
			uid, err := uuid.Parse(id)
			if err != nil {
				return err.Error(), nil
			}
			alert, err := connection.Client.GetAlert(uid)
			if err != nil {
				return err.Error(), nil
			}
			alert.Disabled = !alert.Disabled // toggle

			// check for explicit on or off
			if enable, err := fs.GetBool("enable"); err != nil {
				clilog.LogFlagFailedGet("enable", err)
				return "an error occurred", nil
			} else if enable {
				alert.Disabled = false
			}
			if disable, err := fs.GetBool("disable"); err != nil {
				clilog.LogFlagFailedGet("disable", err)
				return "an error occurred", nil
			} else if disable {
				alert.Disabled = true
			}
			_, err = connection.Client.UpdateAlert(alert)
			if err != nil {
				return err.Error(), nil
			}
			state := "enabled"
			if alert.Disabled {
				state = "disabled"
			}

			return fmt.Sprintf("alert '%s' (ID: %s) %s", alert.Name, uid.String(), state), nil
		},
		scaffold.BasicOptions{
			AddtlFlagFunc: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.Bool("enable", false, "enable the alert. Does nothing if the alert is already enabled. Mutually exclusive with --disable")
				fs.Bool("disable", false, "disable the alert. Does nothing if the alert is already disabled. Mutually exclusive with --enable")
				return fs
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.Changed("enable") && fs.Changed("disable") {
					return "--enable and --disable are mutually exclusive", nil
				}
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("alert ID"), nil
				}
				return "", nil
			},
		},
	)
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("alert", "alerts",
		func(dryrun bool, id uuid.UUID) error {
			if dryrun {
				_, err := connection.Client.GetAlert(id)
				return err
			}
			return connection.Client.DeleteAlert(id)
		},
		func() ([]scaffolddelete.Item[uuid.UUID], error) {
			alerts, err := connection.Client.GetAlerts()
			if err != nil {
				return nil, err
			}
			// sort on name
			slices.SortStableFunc(alerts,
				func(a, b types.AlertDefinition) int {
					return strings.Compare(a.Name, b.Name)
				})
			var items = make([]scaffolddelete.Item[uuid.UUID], len(alerts))
			for i, a := range alerts {
				items[i] = scaffolddelete.NewItem(a.Name, a.Description, a.ThingUUID)
			}
			return items, nil
		})
}
