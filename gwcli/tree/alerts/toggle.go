/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package alerts

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// toggleEnabled provides an action to enable/disable a given alert.
func toggleEnabled() action.Pair {
	cmd := treeutils.GenerateAction("toggle", "enable or disable an alert",
		"Toggle the enabled state of an alert. Optionally use --enable or --disable to set explicitly.",
		nil,
		func(c *cobra.Command, args []string) error {
			if c.Flags().NArg() == 0 {
				ni, err := c.Flags().GetBool(ft.NoInteractive.Name())
				if err != nil {
					clilog.GetFlag(err)
					ni = true
				}
				if !ni {
					return mother.Spawn(c.Root(), c, args)
				}
				return errors.New(phrases.AtLeast1ArgRequired("alert ID"))
			}

			enable, disable, err := getToggleEnabledFlags(c.Flags())
			if err != nil {
				return err
			}

			for _, res := range toggleAlerts(c.Flags().Args(), enable, disable) {
				if res.failure {
					fmt.Fprintln(c.ErrOrStderr(), res.result)
				} else {
					fmt.Fprintln(c.OutOrStdout(), res.result)
				}
			}

			return nil
		},
	)

	cmd.Flags().AddFlagSet(toggleEnabledFlags())
	scaffold.CommonOptions{
		Usage: fmt.Sprintf("toggle %s %s",
			ft.Optional(ft.MutuallyExclusive([]string{"--enable", "--disable"})),
			ft.VariadicArgs("alertID", true),
		),
	}.Apply(cmd)

	return action.NewPair(cmd, &toggleModel{})
}

func toggleEnabledFlags() *pflag.FlagSet {
	fs := &pflag.FlagSet{}
	fs.Bool("enable", false, "explicitly enable the alert. Does nothing if the alert is already enabled. Mutually exclusive with --disable")
	fs.Bool("disable", false, "explicitly disable the alert. Does nothing if the alert is already disabled. Mutually exclusive with --enable")
	return fs
}

// GetFlag errors are logged and swallowed.
func getToggleEnabledFlags(fs *pflag.FlagSet) (enable, disable bool, ufErr error) {
	var err error
	if enable, err = fs.GetBool("enable"); err != nil {
		clilog.GetFlag(err)
	}
	if disable, err = fs.GetBool("disable"); err != nil {
		clilog.GetFlag(err)
	}
	if enable && disable {
		return enable, disable, ft.ErrMutuallyExclusive()
	}
	return enable, disable, nil
}

// Returns an array of results, in the same order as IDs.
func toggleAlerts(IDs []string, enable, disable bool) []struct {
	result  string
	failure bool
} {
	var results = make([]struct {
		result  string
		failure bool
	}, len(IDs))
	for i, id := range IDs {
		results[i] = struct {
			result  string
			failure bool
		}{
			"", true,
		}
		alert, err := connection.Client.GetAlert(id)
		if err != nil {
			results[i].result = fmt.Sprintf("failed to get alert (ID: %s): %v", id, err)
			continue
		}
		if enable {
			alert.Disabled = false
		} else if disable {
			alert.Disabled = true
		} else {
			alert.Disabled = !alert.Disabled
		}
		state := "enable"
		if alert.Disabled {
			state = "disable"
		}

		if _, err := connection.Client.UpdateAlert(alert); err != nil {
			results[i].result = fmt.Sprintf("failed to %s alert (ID: %s): %v", state, id, err)
			continue
		}
		results[i].result = fmt.Sprintf("%sd alert (ID: %s)", state, id)
		results[i].failure = false
	}
	return results
}

//#region interactive

// toggleModel presents a multi-select list of alerts and toggles each selected one.
type toggleModel struct {
	m               multiselectlist.Model[string]
	enable, disable bool
	done            bool
}

func (c *toggleModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	fs := toggleEnabledFlags()
	if err := fs.Parse(tokens); err != nil {
		return "", nil, err
	}
	c.enable, c.disable, err = getToggleEnabledFlags(fs)
	if err != nil { // returns only ufErrors
		return err.Error(), nil, nil
	}

	alerts, err := connection.Client.ListAlerts(nil)
	if err != nil {
		clilog.Writer.Error("failed to get the list of alerts", log.KV("error", err))
		return "", nil, fmt.Errorf("failed to get the list of alerts")
	} else if len(alerts.Results) < 1 {
		return "you have no alerts that can be toggled", nil, nil
	}
	slices.SortStableFunc(alerts.Results, func(a, b types.Alert) int {
		return strings.Compare(a.Name, b.Name)
	})
	var itms = make([]multiselectlist.SelectableItem[string], len(alerts.Results))
	for i, a := range alerts.Results {
		itms[i] = &listitem.Generic{
			ID_:          a.ID,
			Name:         a.Name,
			SecondLine:   a.Description,
			ShowDisabled: true,
			Enabled:      !a.Disabled,
		}
	}
	c.m = multiselectlist.New(itms, width, height, multiselectlist.Options{})
	c.m.StatusMessageLifetime = stylesheet.StatusMessageLifetime
	c.m.StatusMessageOnSelect = true
	c.m.Title = "Select alerts to toggle"
	return "", nil, nil
}

func (c *toggleModel) Update(msg tea.Msg) (cmd tea.Cmd) {
	c.m, cmd = c.m.Update(msg)
	if c.m.Done() {
		selected := c.m.GetSelectedItems()
		if len(selected) == 0 {
			c.m.Undone()
			return c.m.NewStatusMessage("select at least 1 alert")
		}
		// collect IDs
		var IDs, cmds = make([]string, len(selected)), make([]tea.Cmd, len(selected))
		for i, li := range selected {
			IDs[i] = li.ID()
		}
		for i, res := range toggleAlerts(IDs, c.enable, c.disable) {
			cmds[i] = tea.Println(res.result)
		}
		cmd = tea.Sequence(cmds...)
		c.done = true
	}
	return cmd
}

func (c *toggleModel) View() string {
	return c.m.View()
}

func (c *toggleModel) Done() bool {
	return c.done
}

func (c *toggleModel) Reset() error {
	c.m = multiselectlist.Model[string]{}
	c.enable = false
	c.disable = false
	c.done = false
	return nil
}
