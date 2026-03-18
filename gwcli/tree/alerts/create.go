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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
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
			// --dispatchers and --consumers are both required in non-interactive mode.
			// Because they are not required in interactive move, we must validate them here, now.

			// pull data from flags
			var dispatcherIDs, consumerIDs []string
			{
				var err error
				dispatcherIDs, err = c.Flags().GetStringSlice("dispatchers")
				if err != nil {
					clilog.LogFlagFailedGet("dispatchers", err)
				}
				if len(dispatcherIDs) < 1 {
					fmt.Fprintln(c.OutOrStdout(), "--dispatchers is required in non-interactive mode")
					return
				}
				// validate that all given IDs are known
				for _, ID := range dispatcherIDs {
				}
			}
			{
				var err error
				consumerIDs, err = c.Flags().GetStringSlice("consumers")
				if err != nil {
					clilog.LogFlagFailedGet("consumers", err)
				}
				if len(consumerIDs) < 1 {
					fmt.Fprintln(c.OutOrStdout(), "--consumers is required in non-interactive mode")
				}
				// validate that all given IDs are known

			}
		},
		treeutils.GenerateActionOptions{})

	// attach flags
	cmd.Flags().StringSlice("dispatchers", nil, "Comma-separated list of IDs of scheduled searches to use as dispatchers.\n"+
		"Use `queries scheduled list` to view all available scheduled queries.")
	cmd.Flags().StringSlice("consumers", nil, "Comma-separated list of IDs of flows to use as consumers.\n"+
		"Use `flows list` to view all available flows.")
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
	return "interactivity not yet implemented"
}

func (c *createModel) Done() bool {
	return true
}

func (c *createModel) Reset() error {
	return nil
}

func (c *createModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	return "", nil, nil
}
