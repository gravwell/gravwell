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

			if enable, err := c.Flags().GetBool("enable"); err != nil {
				clilog.GetFlag(err)
			} else if enable {
				alert.Disabled = false
			}
			if disable, err := c.Flags().GetBool("disable"); err != nil {
				clilog.GetFlag(err)
			} else if disable {
				alert.Disabled = true
			}

			for _, id := range c.Flags().Args() {

			}
			alert, err := connection.Client.GetAlert(id)
			if err != nil {
				return err
			}
			alert.Disabled = !alert.Disabled

			_, err = connection.Client.UpdateAlert(alert)
			if err != nil {
				return err
			}
			state := "enabled"
			if alert.Disabled {
				state = "disabled"
			}
			fmt.Fprintf(c.OutOrStdout(), "alert '%s' (ID: %s) %s\n", alert.Name, id, state)
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
func getToggleEnabledFlags(fs *pflag.FlagSet) (enable, disable bool, err error) {
	if enable, err = fs.GetBool("enable"); err != nil {
		clilog.GetFlag(err)
	}
	if disable, err = fs.GetBool("disable"); err != nil {
		clilog.GetFlag(err)
	}
	if enable && disable {
		return ft.Mutu
	}
}

//#region interactive

type alertItem struct {
	Selected_ bool
	ID_       string
	name      string
	desc      string
	disabled  bool
}

func (i alertItem) FilterValue() string {
	return i.name + i.desc
}

func (i alertItem) Title() string {
	return i.name
}

func (i alertItem) ID() string {
	return i.ID_
}

func (i alertItem) Description() string {
	state := "enabled"
	if i.disabled {
		state = "disabled"
	}
	return fmt.Sprintf("(%s) %s", state, i.desc)
}

func (i *alertItem) SetSelected(selected bool) {
	i.Selected_ = selected
}

func (i alertItem) Selected() bool {
	return i.Selected_
}

// toggleModel presents a multi-select list of alerts and toggles each selected one.
type toggleModel struct {
	m multiselectlist.Model[string]
}

func (c *toggleModel) Init() tea.Cmd {
	return nil
}

func (c *toggleModel) Update(msg tea.Msg) (cmd tea.Cmd) {
	c.m, cmd = c.m.Update(msg)
	if c.m.Done() {
		selected := c.m.GetSelectedItems()
		if len(selected) == 0 {
			c.m.Undone()
			return c.m.NewStatusMessage("select at least 1 alert")
		}
		var cmds []tea.Cmd
		for _, li := range selected {
			alert, err := connection.Client.GetAlert(li.ID())
			if err != nil {
				cmds = append(cmds, tea.Printf("failed to get alert '%s': %v", li.Title(), err))
				continue
			}
			alert.Disabled = !alert.Disabled
			if _, err := connection.Client.UpdateAlert(alert); err != nil {
				cmds = append(cmds, tea.Printf("failed to toggle alert '%s': %v", li.Title(), err))
				continue
			}
			state := "enabled"
			if alert.Disabled {
				state = "disabled"
			}
			cmds = append(cmds, tea.Printf("alert '%s' (ID: %s) %s", li.Title(), li.ID(), state))
		}
		cmd = tea.Sequence(cmds...)
	}
	return cmd
}

func (c *toggleModel) View() string {
	return c.m.View()
}

func (c *toggleModel) Done() bool {
	return c.m.Done()
}

func (c *toggleModel) Reset() error {
	c.m = multiselectlist.Model[string]{}
	return nil
}

func (c *toggleModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	alerts, err := connection.Client.ListAlerts(nil)
	if err != nil {
		clilog.Writer.Error("failed to get the list of alerts", log.KV("error", err))
		return "", nil, fmt.Errorf("failed to get the list of alerts")
	}
	slices.SortStableFunc(alerts.Results, func(a, b types.Alert) int {
		return strings.Compare(a.Name, b.Name)
	})
	var itms = make([]multiselectlist.SelectableItem[string], 0, len(alerts.Results))
	for _, a := range alerts.Results {
		itms = append(itms, &alertItem{
			ID_:      a.ID,
			name:     a.Name,
			desc:     a.Description,
			disabled: a.Disabled,
		})
	}
	itms = slices.Clip(itms)
	if len(itms) == 0 {
		return "there are no alerts", nil, nil
	}
	c.m = multiselectlist.New(itms, width, height, multiselectlist.Options{})
	c.m.StatusMessageLifetime = stylesheet.StatusMessageLifetime
	c.m.StatusMessageOnSelect = true
	c.m.Title = "Select alerts to toggle"
	return "", nil, nil
}

//#endregion interactive
