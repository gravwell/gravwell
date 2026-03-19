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

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
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
			inv, ad := validateFlagValues(availConsumers, availDispatchers, flagVals)
			if inv != "" {
				fmt.Fprintln(c.ErrOrStderr(), inv)
				return
			}

			res, err := connection.Client.NewAlert(ad)
			if err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), "failed to create alert: "+err.Error()+"\n")
				return
			}
			fmt.Fprint(c.OutOrStdout(), phrases.SuccessfullyCreatedItem("alert", res.ThingUUID.String()))
		},
		treeutils.GenerateActionOptions{
			Usage: ft.Mandatory("--name=NAME") + ft.Optional("FLAGS"),
			Example: "--name=myalert" +
				" --tag=investigation" +
				" --dispatchers=39350375-4f73-44c3-bc15-e844009a5fa6,61f75c57-2324-4dae-9b65-73430fd0363f" +
				" --consumers=00ec2858-93ff-44f8-ab87-97c0a0346578" + " --enable",
		})

	cmd.Flags().AddFlagSet(createFlagSet())

	return action.NewPair(cmd, newCreateModel())
}

// prerequisites checks that all required data has been created in advance.
// Specifically, it checks that at least one dispatcher and at least one consumer has been created.
//
// Returns the list of dispatchers and consumers so we don't have to hit the backend again.
func prerequisites() (availDispatchers map[uuid.UUID]types.ScheduledSearch, availConsumers map[uuid.UUID]types.ScheduledSearch, inv string, _ error) {
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
	availDispatchers = make(map[uuid.UUID]types.ScheduledSearch, len(dispatchers))
	for _, dsp := range dispatchers {
		availDispatchers[dsp.GUID] = dsp
	}
	availConsumers = make(map[uuid.UUID]types.ScheduledSearch, len(consumers))
	for _, cns := range consumers {
		availConsumers[cns.GUID] = cns
	}

	return availDispatchers, availConsumers, "", nil
}

// createFlagSet returns a fresh flagset of the flags used by this create action.
// It should be used to ensure flags are equivalent interactive and non-interactive use.
func createFlagSet() *pflag.FlagSet {
	fs := &pflag.FlagSet{}
	// attach mandatory flags
	ft.Name.Register(fs, "alert")
	// attach optional flags
	fs.StringSlice("dispatchers", nil, "Comma-separated list of IDs of scheduled searches to use as dispatchers.\n"+
		"Use `queries scheduled list` to view all available scheduled queries")
	fs.StringSlice("consumers", nil, "Comma-separated list of IDs of flows to use as consumers.\n"+
		"Use `flows list` to view all available flows")
	ft.Description.Register(fs, "alert")
	fs.String("tag", "_alerts", "The tag to which alerts of this type will be ingested")
	fs.Bool("enable", false, "Enable the new alert immediately")
	fs.Int("max-events", 16, "Maximum number of events to process for a single alert.\n"+
		"See https://docs.gravwell.io/alerts/alerts.html#max-events")
	fs.Int32("retain", 0,
		"Time (in seconds) to retain any search that dispatches this alert.\n"+
			"These searches will be saved as Persistent Searches and retained for the specified duration.\n"+
			"After that time, these Persistent Searches will be automatically deleted.")
	return fs
}

// validateFlagValues validates the given state, returning on the first invalidity found.
// If no invalidities are found, returns an alert definition suitable for NewAlert().
func validateFlagValues(availableConsumers, availableDispatchers map[uuid.UUID]types.ScheduledSearch, flagVals alertFlags) (invalid string, alert types.AlertDefinition) {
	// check that mandatory flags have values
	if flagVals.name == "" {
		return "you must provide a name for the new alert", types.AlertDefinition{}
	}

	// validate that all given IDs are known and transmute the IDs
	dispatchers := make([]types.AlertDispatcher, len(flagVals.dispatcherIDs))
	for i, GUID := range flagVals.dispatcherIDs {
		_, found := availableDispatchers[GUID]
		if !found {
			return GUID.String() + " is not a known scheduled search", types.AlertDefinition{}
		}
		dispatchers[i] = types.AlertDispatcher{
			ID:   GUID.String(),
			Type: types.ALERTDISPATCHERTYPE_SCHEDULEDSEARCH,
		}
	}
	consumers := make([]types.AlertConsumer, len(flagVals.consumerGUIDs))
	for i, GUID := range flagVals.consumerGUIDs {
		if _, found := availableConsumers[GUID]; !found {
			return GUID.String() + " is not a known flow", types.AlertDefinition{}
		}
		consumers[i] = types.AlertConsumer{
			ID:   GUID.String(),
			Type: types.ALERTCONSUMERTYPE_FLOW,
		}
	}
	return "", types.AlertDefinition{
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
}

type alertFlags struct {
	name          string
	description   string
	dispatcherIDs []uuid.UUID
	consumerGUIDs []uuid.UUID
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
		vals.dispatcherIDs = make([]uuid.UUID, len(dispatchers))
		for i, dsp := range dispatchers {
			vals.dispatcherIDs[i], err = uuid.Parse(dsp)
			if err != nil {
				return vals, fmt.Sprintf("failed to parse '%s' as a UUID dispatcher GUID", dsp)
			}
		}
	}
	{
		consumers, err := fs.GetStringSlice("consumers")
		if err != nil {
			clilog.LogFlagFailedGet("consumers", err)
		}
		vals.consumerGUIDs = make([]uuid.UUID, len(consumers))
		for i, cns := range consumers {
			vals.consumerGUIDs[i], err = uuid.Parse(cns)
			if err != nil {
				return vals, fmt.Sprintf("failed to parse '%s' as UUID consumer GUID", cns)
			}
		}
	}
	return vals, ""
}
