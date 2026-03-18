/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package alerts

import (
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// This file provides the custom create action for alerts.

func create() action.Pair {
	cmd := treeutils.GenerateAction("create", "create a new alert",
		"Create a new alert by defining the dispatchers that trigger it and the consumers that act when the alert is fired",
		nil,
		func(c *cobra.Command, s []string) {
			availDispatchers, availConsumers, inv, err := prerequisites()
			if err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error()+"\n")
				return
			} else if inv != "" {
				fmt.Fprintln(c.OutOrStdout(), inv)
				return
			}
			flagVals, inv := readFlags(c.Flags())
			if inv != "" {
				fmt.Fprintln(c.OutOrStdout(), inv)
				return
			}

			// because no flags are required in interactive mode, we have to handle all flag validation here
			if len(flagVals.dispatcherIDs) < 1 {
				fmt.Fprintln(c.ErrOrStderr(), "--dispatchers is required in non-interactive mode")
				return
			}
			// validate that all given IDs are known and transmute the IDs
			var dispatchers = make([]types.AlertDispatcher, len(flagVals.dispatcherIDs))
			for i, ID := range flagVals.dispatcherIDs {
				_, found := availDispatchers[ID]
				if !found {
					fmt.Fprintln(c.ErrOrStderr(), ID, "is not a known scheduled search")
					return
				}
				dispatchers[i] = types.AlertDispatcher{
					ID:   strconv.FormatInt(int64(ID), 10),
					Type: types.ALERTDISPATCHERTYPE_SCHEDULEDSEARCH,
				}
			}
			var consumers = make([]types.AlertConsumer, len(flagVals.consumerIDs))
			for i, ID := range flagVals.consumerIDs {
				if _, found := availConsumers[ID]; !found {
					fmt.Fprintln(c.ErrOrStderr(), ID, "is not a known flow")
					return
				}
				consumers[i] = types.AlertConsumer{
					ID:   strconv.FormatInt(int64(ID), 10),
					Type: types.ALERTCONSUMERTYPE_FLOW,
				}
			}

			var ad = types.AlertDefinition{
				Name:        flagVals.name,
				Description: flagVals.description,
				TargetTag:   flagVals.tag,
				UID:         connection.CurrentUser().ID,

				Consumers:          consumers,
				Dispatchers:        dispatchers,
				Disabled:           !flagVals.enabled,
				MaxEvents:          flagVals.maxEvents,
				SaveSearchDuration: flagVals.retain,
			}
			res, err := connection.Client.NewAlert(ad)
			if err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), "failed to create alert: "+err.Error()+"\n")
				return
			}
			fmt.Fprintf(c.OutOrStdout(), "successfully created alert (ID: %s)\n", res.ThingUUID.String())
		},
		treeutils.GenerateActionOptions{
			Usage: fmt.Sprint("--name=", ft.Mandatory("name"),
				" --tag=", ft.Mandatory("tag"),
				" --dispatchers=", ft.Mandatory("ScheduledSearchID1,ID2,ID3,..."),
				" --consumers=", ft.Mandatory("FlowID1,ID2,ID3,...")),
		})

	// attach mandatory flags
	cmd.Flags().StringSlice("dispatchers", nil, "Comma-separated list of IDs of scheduled searches to use as dispatchers.\n"+
		"Use `queries scheduled list` to view all available scheduled queries")
	cmd.Flags().StringSlice("consumers", nil, "Comma-separated list of IDs of flows to use as consumers.\n"+
		"Use `flows list` to view all available flows")
	ft.Name.Register(cmd.Flags(), "alert")
	cmd.Flags().String("tag", "", "The tag to which alerts of this type will be ingested")
	// attach optional flags
	ft.Description.Register(cmd.Flags(), "alert")
	cmd.Flags().Bool("enable", false, "Enable the new alert immediately")
	cmd.Flags().Int("max-events", 16, "Maximum number of events to process for a single alert.\n"+
		"See https://docs.gravwell.io/alerts/alerts.html#max-events")
	cmd.Flags().Int32("retain", 0,
		"Time (in seconds) to retain any search that dispatches this alert.\n"+
			"These searches will be saved as Persistent Searches and retained for the specified duration.\n"+
			"After that time, these Persistent Searches will be automatically deleted.")

	return action.NewPair(cmd, newCreateModel())
}

// prerequisites checks that all required data has been created in advance.
// Specifically, it checks that at least one dispatcher and at least one consumer has been created.
//
// Returns the list of dispatchers and consumers so we don't have to hit the backend again.
func prerequisites() (availDispatchers map[int32]types.ScheduledSearch, availConsumers map[int32]types.ScheduledSearch, inv string, _ error) {
	dispatchers, err := connection.Client.GetScheduledSearchList()
	if err != nil {
		return nil, nil, "", err
	} else if len(dispatchers) < 1 {
		return nil, nil, "No dispatchers available. Dispatchers may be scheduled searches. Please create one before creating an alert.", nil
	}

	consumers, err := connection.Client.GetFlowList()
	if err != nil {
		return nil, nil, "", err
	} else if len(consumers) < 1 {
		return nil, nil, "No consumers available. Consumers may be flows. Please create one before creating an alert.", nil
	}

	// transmute dispatchers and consumers to maps to improve lookup time
	availDispatchers = make(map[int32]types.ScheduledSearch, len(dispatchers))
	for _, dsp := range dispatchers {
		availDispatchers[dsp.ID] = dsp
	}
	availConsumers = make(map[int32]types.ScheduledSearch, len(consumers))
	for _, cns := range consumers {
		availDispatchers[cns.ID] = cns
	}

	return availDispatchers, availConsumers, "", nil
}

type alertFlags struct {
	name          string
	description   string
	dispatcherIDs []int32
	consumerIDs   []int32
	tag           string
	maxEvents     int
	enabled       bool
	retain        int32
}

// readFlags corrals data from flags.
// The data is only validated in so far as it is type-cast.
// Returns the first error it encounters.
func readFlags(fs *pflag.FlagSet) (vals alertFlags, firstInvalid string) {
	var err error
	if vals.name, err = fs.GetString(ft.Name.Name()); err != nil {
		clilog.LogFlagFailedGet(ft.Name.Name(), err)
	}
	if vals.description, err = fs.GetString(ft.Description.Name()); err != nil {
		clilog.LogFlagFailedGet(ft.Description.Name(), err)
	}
	if vals.tag, err = fs.GetString("tag"); err != nil {
		clilog.LogFlagFailedGet("tag", err)
	}
	if vals.maxEvents, err = fs.GetInt("max-events"); err != nil {
		clilog.LogFlagFailedGet("max-events", err)
	}
	if vals.enabled, err = fs.GetBool("enable"); err != nil {
		clilog.LogFlagFailedGet("enable", err)
	}
	if vals.retain, err = fs.GetInt32("retain"); err != nil {
		clilog.LogFlagFailedGet("retain", err)
	}
	{
		dispatchers, err := fs.GetStringSlice("dispatchers")
		if err != nil {
			clilog.LogFlagFailedGet("dispatchers", err)
		}
		vals.dispatcherIDs = make([]int32, len(dispatchers))
		for i, dsp := range dispatchers {
			id, err := strconv.ParseInt(dsp, 10, 32)
			if err != nil {
				return vals, fmt.Sprintf("failed to parse '%s' as a int32 dispatcher ID", dsp)
			}
			vals.dispatcherIDs[i] = int32(id)
		}
	}
	{
		consumers, err := fs.GetStringSlice("consumers")
		if err != nil {
			clilog.LogFlagFailedGet("consumers", err)
		}
		vals.consumerIDs = make([]int32, len(consumers))
		for i, cns := range consumers {
			id, err := strconv.ParseInt(cns, 10, 32)
			if err != nil {
				return vals, fmt.Sprintf("failed to parse '%s' as a int32 consumer ID", cns)
			}
			vals.consumerIDs[i] = int32(id)
		}
	}
	return vals, ""
}

type createModel struct{}

func newCreateModel() *createModel {
	return &createModel{}
}

// Init is unused. It just exists so we can feed createModel into teatest.
func (c *createModel) Init() tea.Cmd {
	return nil
}

func (c *createModel) Update(msg tea.Msg) tea.Cmd {
	return nil
}

func (c *createModel) View() string {
	return ""
}

func (c *createModel) Done() bool {
	return true
}

func (c *createModel) Reset() error {
	return nil
}

func (c *createModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	return "", stylesheet.ErrPrintf("interactivity not yet implemented"), nil
}
