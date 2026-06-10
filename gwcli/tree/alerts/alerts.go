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
	"fmt"
	"slices"
	"time"

	"strings"

	"github.com/dustin/go-humanize/english"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	alertscreate "github.com/gravwell/gravwell/v4/gwcli/tree/alerts/create"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	const (
		use   string = "alerts"
		short string = "manage alerts"
		long  string = "Alerts allow you to tie sources of intelligence (such as periodic scheduled searches) to actions (such as a flow that files a ticket)." +
			" This can make it much simpler to take automatic action when something of interest occurs."
	)
	return treeutils.GenerateNav(use, short, long, []string{"alert"}, []*cobra.Command{},
		[]action.Pair{
			alertsList(),
			toggle(),
			delete(),
			alertscreate.Action(),
			dispatchers(),
			save(),
		})
}

//#region helpers

func alertsToGeneric(a types.AlertListResponse) (g []multiselectlist.SelectableItem[string]) {
	// sort on name
	slices.SortStableFunc(a.Results,
		func(a, b types.Alert) int {
			return strings.Compare(a.Name, b.Name)
		})
	g = make([]multiselectlist.SelectableItem[string], len(a.Results))
	for i, a := range a.Results {
		g[i] = &listitem.Generic{
			Selected_:  false,
			ID_:        a.ID,
			Name:       a.Name,
			SecondLine: a.Description,

			ShowDisabled: true,
			Enabled:      !a.Disabled,
		}
	}
	return g
}

//#region actions

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
		func(fs *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.Alert, error) {

			if listConsumerID != "" {
				params.QueryOpts.Filters = append(params.QueryOpts.Filters, types.Filter{
					Key:       "Consumers.ID",
					Operation: "=",
					Values:    []any{listConsumerID},
				})
				resp, err := connection.Client.ListAlerts(params.QueryOpts)
				return resp.Results, err

			} else if listDispatcherID != "" {
				params.QueryOpts.Filters = append(params.QueryOpts.Filters, types.Filter{
					Key:       "Dispatchers.ID",
					Operation: "=",
					Values:    []any{listDispatcherID},
				})
				resp, err := connection.Client.ListAlerts(params.QueryOpts)
				return resp.Results, err
			}

			resp, err := connection.Client.ListAlerts(params.QueryOpts)
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
					return ft.ErrMutuallyExclusive("consumer", "dispatcher").Error(), nil
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
		func() ([]multiselectlist.SelectableItem[string], error) {
			alerts, err := connection.Client.ListAlerts(nil)
			if err != nil {
				return nil, err
			}
			return alertsToGeneric(alerts), nil
		}, scaffolddelete.Options{})
}

var toggleEnable, toggleDisable bool

func toggle() action.Pair {
	return scaffoldselect.NewSelectAction("enable or disable an alert",
		"Toggle the enabled state of an alert. Optionally use --enable or --disable to set explicitly.",
		"alert",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListAlerts(nil)
			if err != nil {
				return nil, err
			}

			// if a flag was specified, hide alerts already in the state of the flag
			var enable, disable bool
			if enable, err = addtlFlags.GetBool("enable"); err != nil {
				clilog.GetFlag(err)
			}
			if disable, err = addtlFlags.GetBool("disable"); err != nil {
				clilog.GetFlag(err)
			}

			items := make([]multiselectlist.SelectableItem[string], 0, len(lr.Results))
			for _, alert := range lr.Results {
				if enable && !alert.Disabled {
					continue
				} else if disable && alert.Disabled {
					continue
				}
				items = append(items, &listitem.Generic{
					Selected_:    false,
					ID_:          alert.ID,
					Name:         alert.Name,
					SecondLine:   alert.Description,
					ShowDisabled: !enable && !disable, // only if it wasn't explicit
					Enabled:      !alert.Disabled,
				})
			}
			return items, nil
		},
		func(IDs []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				alert, err := connection.Client.GetAlert(ID)
				if err != nil {
					results[i] = scaffold.Result{
						Output: fmt.Sprintf("failed to fetch base alert %s (ID: %s)", alert.Name, alert.ID),
					}
					continue
				}
				alert.Disabled = !alert.Disabled
				if toggleEnable {
					alert.Disabled = false
				} else if toggleDisable {
					alert.Disabled = true
				}
				if _, err := connection.Client.UpdateAlert(alert); err != nil {
					results[i] = scaffold.Result{
						Output: fmt.Sprintf("failed to update alert %s (ID: %s) (to Disabled=%v)", alert.Name, alert.ID, alert.Disabled),
					}
					continue
				}
				verb := "enabled"
				if alert.Disabled {
					verb = "disabled"
				}
				results[i] = scaffold.Result{
					Output:  fmt.Sprintf("Alert %s (ID: %s) %s", alert.Name, alert.ID, verb),
					Success: false,
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "toggle",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("enable", false, "explicitly enable selected alerts. No-op on alerts already enabled. Mutually exclusive with --disable")
					fs.Bool("disable", false, "explicitly disable selected alerts. No-op on alerts already disabled. Mutually exclusive with --enable")
					return fs
				},
			},
			NoItemsError: func(fs *pflag.FlagSet) string {
				if toggleEnable {
					return "You have no alerts that can be enabled."
				} else if toggleDisable {
					return "You have no alerts that can be disabled."
				}
				return "You have no alerts that can be toggled."
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				// ensure !(enable && disable)
				toggleEnable, err = fs.GetBool("enable")
				if err != nil {
					clilog.GetFlag(err)
				}
				toggleDisable, err = fs.GetBool("disable")
				if err != nil {
					clilog.GetFlag(err)
				}
				if toggleEnable && toggleDisable {
					return ft.ErrMutuallyExclusive("enable", "disable").Error(), nil
				}
				return "", nil
			},
		})
}

func dispatchers() action.Pair {
	return scaffoldselect.NewSelectAction("set the dispatchers for a set of alerts",
		"Add, remove, or replace dispatchers (triggers) for an alert. "+
			"Use --add to add dispatchers, --remove to remove them, or neither to replace the entire list.",
		"alert ID",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			a, err := connection.Client.ListAlerts(&types.QueryOptions{AdminMode: connection.AdminMode()})
			if err != nil {
				return nil, err
			}
			return alertsToGeneric(a), nil

		},
		func(IDs []string, fs *pflag.FlagSet) (results []scaffold.Result, _ error) {
			// we've already checked all flags
			dIDs, _ := fs.GetStringSlice("dispatcher-ids")
			add, _ := fs.GetBool("add")
			remove, _ := fs.GetBool("remove")

			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				a, err := connection.Client.GetAlert(ID)
				if err != nil {
					results[i] = scaffold.Result{Output: fmt.Sprintf("failed to get alert %s (ID: %s): %v", a.Name, ID, err)}
				}
				// add or remove each specified dispatcher
				if add {
					clilog.Writer.Info("adding dispatchers to alert", log.KV("alert ID", ID), log.KV("dispatcher IDs", dIDs))
					added, duplicate := 0, 0
					for _, dID := range dIDs {
						if !slices.ContainsFunc(a.Dispatchers, func(d types.AlertDispatcher) bool { return d.ID == dID }) {
							a.Dispatchers = append(a.Dispatchers, types.AlertDispatcher{ID: dID, Type: types.ALERTDISPATCHERTYPE_SCHEDULEDSEARCH})
							added += 1
						} else {
							duplicate += 1
						}
					}
					results[i] = scaffold.Result{Success: true}
					results[i].Output = fmt.Sprintf("added %s to alert %s (ID: %s)", english.Plural(added, "dispatcher", ""), a.Name, a.ID)
					if duplicate > 0 {
						results[i].Output += fmt.Sprintf("; skipped %d duplicates", duplicate)
					}
				} else if remove {
					found := 0
					clilog.Writer.Info("removing dispatchers from alert", log.KV("alert ID", ID), log.KV("dispatcher IDs", dIDs))
					a.Dispatchers = slices.DeleteFunc(a.Dispatchers, func(ad types.AlertDispatcher) bool {
						for _, dID := range dIDs {
							if ad.ID == dID {
								found += 1
								return true
							}
						}
						return false
					})
					results[i] = scaffold.Result{
						Success: true,
						Output: fmt.Sprintf("removed %d (of %d given) %s from alert %s (ID: %s); %d remaining",
							found, len(dIDs), english.PluralWord(found, "dispatcher", ""), a.Name, a.ID, len(a.Dispatchers)),
					}
				} else {
					clilog.Writer.Info("replacing dispatchers on alert", log.KV("alert ID", ID), log.KV("new dispatcher IDs", dIDs), log.KV("old dispatchers", a.Dispatchers))
					a.Dispatchers = make([]types.AlertDispatcher, len(dIDs))
					for i, dID := range dIDs {
						a.Dispatchers[i] = types.AlertDispatcher{ID: dID, Type: types.ALERTDISPATCHERTYPE_SCHEDULEDSEARCH}
					}
					results[i] = scaffold.Result{
						Success: true,
						Output:  fmt.Sprintf("replaced dispatchers on alert %s (ID: %s)", a.Name, a.ID),
					}
				}

				if _, err := connection.Client.UpdateAlert(a); err != nil {
					results[i] = scaffold.Result{
						Success: false,
						Output:  fmt.Sprintf("failed to update alert: %v\nOriginal operation: %v", err, results[i].Output),
					}
				}
				// successful result is already built
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "dispatchers",
				Aliases: []string{"dispatcher"},
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.StringSlice("dispatcher-ids", nil, "REQUIRED. IDs of the dispatchers to add/remove/replace from each alert")
					fs.Bool("add", false, "add the dispatchers specified by --dispatcher-ids to each alert"+
						" Mutually exclusive with --remove")
					fs.Bool("remove", false, "remove the dispatchers specified by --dispatcher-ids from each alert."+
						" Mutually exclusive with --add")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if dIDs, err := fs.GetStringSlice("dispatcher-ids"); err != nil { // this is a fatal error
					return "", clilog.GetFlag(err)
				} else if len(dIDs) < 1 {
					return phrases.ErrFlagIsRequired("dispatcher-ids").Error(), nil
				} else {
					// ensure each dispatcher ID is valid
					lr, err := connection.Client.ListScheduledSearches(&types.QueryOptions{AdminMode: connection.AdminMode()})
					if err != nil {
						return "", err
					}

					for _, dID := range dIDs {
						if !slices.ContainsFunc(lr.Results, func(ss types.ScheduledSearch) bool { return ss.ID == dID }) {
							return phrases.ErrUnknownIdentifier(dID, "scheduled search ID").Error(), nil
						}
					}
				}
				add, err := fs.GetBool("add")
				clilog.GetFlag(err)
				remove, err := fs.GetBool("remove")
				clilog.GetFlag(err)
				if add && remove {
					return ft.ErrMutuallyExclusive("add", "remove").Error(), nil
				}
				return "", nil
			},
		})
}

const defaultDuration = 1 * time.Hour

// save lets the user configure whether triggered searches should be saved and for how long.
func save() action.Pair {
	return scaffoldselect.NewSelectAction("configure search-saving for a set of alerts",
		"Configure whether searches that trigger an alert should be automatically saved and for how long.\n"+
			"Use --enable (with a duration) or --disable to set explicitly. "+
			"If you do not provide either flag, the alert will be toggled and retain whatever its previous duration was. "+
			"If an alert would be enabled but have a save duration of 0, it will default to "+defaultDuration.String()+".",
		"alert ID",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			a, err := connection.Client.ListAlerts(&types.QueryOptions{AdminMode: connection.AdminMode()})
			if err != nil {
				return nil, err
			}
			return alertsToGeneric(a), nil

		},
		func(IDs []string, fs *pflag.FlagSet) (results []scaffold.Result, _ error) {
			// checked by validate
			enable, _ := fs.GetBool("enable")
			duration, _ := fs.GetDuration("duration")
			disable, _ := fs.GetBool("disable")

			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				a, err := connection.Client.GetAlert(ID)
				if err != nil {
					results[i] = scaffold.Result{Output: fmt.Sprintf("failed to fetch alert by ID %s: %v", ID, err)}
					continue
				}
				a.SaveSearchEnabled = !a.SaveSearchEnabled
				if duration != 0 { // duration was provided
					a.SaveSearchDuration = int32(duration.Seconds())
				}
				if enable {
					a.SaveSearchEnabled = true
				} else if disable {
					a.SaveSearchEnabled = false
				}

				// if save would be enabled with a duration of 0, default it
				if a.SaveSearchEnabled && a.SaveSearchDuration == 0 {
					a.SaveSearchDuration = int32(defaultDuration.Seconds())
				}

				a, err = connection.Client.UpdateAlert(a)
				if err != nil {
					results[i] = scaffold.Result{Output: fmt.Sprintf("failed to update alert %s (ID: %s): %v", a.Name, ID, err)}
					continue
				}
				results[i] = scaffold.Result{}

				if a.SaveSearchEnabled {
					results[i].Output = fmt.Sprintf("alert %s (ID: %s) will save triggering searches for %d seconds", a.Name, a.ID, a.SaveSearchDuration)
				} else {
					results[i].Output = fmt.Sprintf("alert %s (ID: %s) will not save triggering searches", a.Name, a.ID)
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "save",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("enable", false, "enable search saving.\n"+
						"Mutually exclusive with --disable")
					fs.Duration("duration", 0, "duration for which to save a triggering search")
					fs.Bool("disable", false, "disable search saving.\n"+
						"Mutually exclusive with --enable")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				enable, err := fs.GetBool("enable")
				clilog.GetFlag(err)
				disable, err := fs.GetBool("disable")
				clilog.GetFlag(err)
				if enable && disable {
					return ft.ErrMutuallyExclusive("enable", "disable").Error(), nil
				}
				return "", nil
			},
		})
}
