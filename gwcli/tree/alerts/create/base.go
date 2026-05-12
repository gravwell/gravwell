/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package alertscreate supplies the alerts nav
package alertscreate

import (
	"fmt"

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

func Action() action.Pair {
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

			res, err := connection.Client.CreateAlert(ad)
			if err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), "failed to create alert: "+err.Error()+"\n")
				return
			}
			fmt.Fprint(c.OutOrStdout(), phrases.SuccessfullyCreatedItem("alert", res.ID))
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
func prerequisites() (availDispatchers map[string]types.ScheduledSearch, availConsumers map[string]types.Flow, inv string, _ error) {
	dispatchers, err := connection.Client.ListScheduledSearches(nil)
	if err != nil {
		return nil, nil, "", err
	} else if len(dispatchers.Results) < 1 {
		return nil, nil, "No dispatchers available. Dispatchers may be scheduled searches. Please create one before creating an alert.", nil
	}

	consumers, err := connection.Client.ListFlows(nil)
	if err != nil {
		return nil, nil, "", err
	} else if len(consumers.Results) < 1 {
		return nil, nil, "No consumers available. Consumers may be flows. Please create one before creating an alert.", nil
	}

	// transmute dispatchers and consumers to maps to improve lookup time
	availDispatchers = make(map[string]types.ScheduledSearch, len(dispatchers.Results))
	for _, dsp := range dispatchers.Results {
		availDispatchers[dsp.ID] = dsp
	}
	availConsumers = make(map[string]types.Flow, len(consumers.Results))
	for _, cns := range consumers.Results {
		availConsumers[cns.ID] = cns
	}

	return availDispatchers, availConsumers, "", nil
}

// createFlagSet returns a fresh flagset of the flags used by this create action.
// It should be used to ensure flags are equivalent interactive and non-interactive use.
func createFlagSet() *pflag.FlagSet {
	fs := &pflag.FlagSet{}
	// attach mandatory flags
	ft.Name.Register(fs, "", "alert")
	// attach optional flags
	fs.StringSlice("dispatchers", nil, "Comma-separated list of IDs of scheduled searches to use as dispatchers.\n"+
		"Use `queries scheduled list` to view all available scheduled queries")
	fs.StringSlice("consumers", nil, "Comma-separated list of IDs of flows to use as consumers.\n"+
		"Use `flows list` to view all available flows")
	ft.Description.Register(fs, "", "alert")
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
// If no invalidities are found, it returns a validated types.Alert definition.
func validateFlagValues(availableConsumers map[string]types.Flow, availableDispatchers map[string]types.ScheduledSearch, flagVals alertFlags) (invalid string, alert types.Alert) {
	// check that mandatory flags have values
	if flagVals.name == "" {
		return "you must provide a name for the new alert", types.Alert{}
	}

	// validate that all given IDs are known and transmute the IDs
	dispatchers := make([]types.AlertDispatcher, len(flagVals.dispatcherIDs))
	for i, ID := range flagVals.dispatcherIDs {
		_, found := availableDispatchers[ID]
		if !found {
			return ID + " is not a known scheduled search", types.Alert{}
		}
		dispatchers[i] = types.AlertDispatcher{
			ID:   ID,
			Type: types.ALERTDISPATCHERTYPE_SCHEDULEDSEARCH,
		}
	}
	consumers := make([]types.AlertConsumer, len(flagVals.consumerIDs))
	for i, ID := range flagVals.consumerIDs {
		if _, found := availableConsumers[ID]; !found {
			return ID + " is not a known flow", types.Alert{}
		}
		consumers[i] = types.AlertConsumer{
			ID:   ID,
			Type: types.ALERTCONSUMERTYPE_FLOW,
		}
	}
	return "", types.Alert{
		CommonFields: types.CommonFields{
			OwnerID:     connection.CurrentUser().ID,
			Name:        flagVals.name,
			Description: flagVals.description,
		},
		TargetTag: flagVals.tag,

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
	dispatcherIDs []string
	consumerIDs   []string
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
		if len(dispatchers) > 0 {
			vals.dispatcherIDs = make([]string, len(dispatchers))
			for i, dsp := range dispatchers {
				if dsp == "" {
					return vals, fmt.Sprintf("dispatcher %d is an empty ID string", i)
				}
			}
		}

	}
	{
		consumers, err := fs.GetStringSlice("consumers")
		if err != nil {
			clilog.LogFlagFailedGet("consumers", err)
		}
		if len(consumers) > 0 {
			vals.consumerIDs = make([]string, len(consumers))
			for i, cns := range consumers {
				if cns == "" {
					return vals, fmt.Sprintf("consumer %d is an empty ID string", i)
				}
			}
		}
	}

	clilog.Writer.Debugf("alert flags set: %#v", vals)
	return vals, ""
}
