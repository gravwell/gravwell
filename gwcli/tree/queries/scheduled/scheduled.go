/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package scheduled is a nav for scheduled queries.
package scheduled

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"

	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/validate"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewScheduledNav() *cobra.Command {
	return treeutils.GenerateNav("scheduled", "Manage scheduled queries", "Alter and view previously scheduled queries", []string{},
		[]*cobra.Command{},
		[]action.Pair{
			create(),
			listAction(),
			delete(),
			edit(),
			cancel(),
			backfillToggle(),
			clear(),
			createScript(),
		})
}

// also serves as GET
func listAction() action.Pair {
	return scaffoldlist.NewListAction(
		"list scheduled queries",
		"Lists information about scheduled searches you can access.",
		types.ScheduledSearch{},
		func(fs *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.ScheduledSearch, error) {
			if id, err := fs.GetString("id"); err != nil {
				clilog.GetFlag(err)
			} else if id != "" {
				ss, err := connection.Client.GetScheduledSearchEx(id, params.QueryOpts)
				return []types.ScheduledSearch{ss}, err
			}
			list, err := connection.Client.ListScheduledSearches(params.QueryOpts)
			return list.Results, err
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{AddtlFlags: func() *pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.String("id", "", "fetches the scheduled search associated to the given id.")
				return &fs
			}},
			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Name",
				"CommonFields.Description",
				"AutomationCommonFields.Schedule",
				"AutomationCommonFields.Disabled",
				"SearchString",
			},
		})
}

func create() action.Pair {
	return scaffoldcreate.NewCreateAction("scheduled query",
		map[string]scaffoldcreate.Field{
			"qry": {
				Required: true,
				Title:    "query",
				Flag:     scaffoldcreate.FlagConfig{Usage: "query to schedule", Shorthand: 'q'},
				Provider: &scaffoldcreate.TextProvider{},
				Order:    150,
			},
			"duration": scaffoldcreate.FieldSearchDuration(true, 140),
			"name":     scaffoldcreate.FieldName("query"),
			"desc":     scaffoldcreate.FieldDescription("query"),

			"freq": { // manually build so we have more control
				Required: true,
				Title:    "frequency",
				Flag:     scaffoldcreate.FlagConfig{Name: ft.Frequency.Name(), Usage: ft.Frequency.Usage()},
				Provider: &scaffoldcreate.TextProvider{
					CustomInit: func() textinput.Model {
						ti := stylesheet.NewTI("", false)
						ti.Placeholder = "* * * * *"
						ti.Validate = validate.CronRuneValidator
						return ti
					},
				},
				DefaultValue: "", // no default value
				Order:        50,
			},
		},
		func(cfg map[string]scaffoldcreate.Field, _ *pflag.FlagSet) (any, string, error) {
			var (
				name      = cfg["name"].Provider.Get()
				desc      = cfg["desc"].Provider.Get()
				freq      = cfg["freq"].Provider.Get()
				qry       = cfg["qry"].Provider.Get()
				durString = cfg["duration"].Provider.Get()
			)
			dur, err := time.ParseDuration(durString)
			if err != nil { // report as invalid parameter, not an error
				return nil, err.Error(), nil
			}

			return connection.CreateScheduledSearch(name, desc, freq, qry, dur)
		},
		scaffoldcreate.Options{})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("query",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetScheduledSearch(id)
				return err
			}

			return connection.Client.DeleteScheduledSearch(id)

		},
		func() ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListScheduledSearches(&types.QueryOptions{AdminMode: connection.AdminMode()})
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(lr.Results), nil
		}, scaffolddelete.Options{})
}

func edit() action.Pair {
	return scaffoldedit.NewEditAction("scheduled search", "scheduled searches",
		scaffoldedit.Config{
			"name": &scaffoldedit.Field{
				Required: true,
				Title:    "Name",
				Usage:    ft.Name.Usage("scheduled search"),
				FlagName: ft.Name.Name(),
				Order:    200,
			},
			"description": &scaffoldedit.Field{
				Required: true,
				Title:    "Description",
				Usage:    ft.Description.Usage("scheduled search"),
				FlagName: ft.Description.Name(),
				Order:    180,
			},
			"search": &scaffoldedit.Field{
				Required: true,
				Title:    "Search",
				Usage:    "the search executed by this scheduled search",
				FlagName: "search",
				Order:    160,
			},
			"frequency": &scaffoldedit.Field{
				Required: true,
				Title:    "Frequency",
				Usage:    ft.Frequency.Usage(),
				FlagName: ft.Frequency.Name(),
				Order:    140,
				CustomTIFuncInit: func() textinput.Model {
					ti := stylesheet.NewTI("", false)
					ti.Placeholder = "* * * * *"
					ti.Validate = validate.CronRuneValidator
					return ti
				},
			},
			"duration": &scaffoldedit.Field{
				Required: true,
				Title:    "Duration",
				Usage:    "Time span the query will look back over",
				FlagName: "duration",
				Order:    120,
				CustomTIFuncInit: func() textinput.Model {
					ti := stylesheet.NewTI("", false)
					ti.Placeholder = "1h2m3s4ms"
					ti.Validate = func(s string) error {
						_, err := time.ParseDuration(s)
						return err
					}
					return ti
				},
			},
			"offset": &scaffoldedit.Field{
				Required: true,
				Title:    "Offset",
				Usage:    "how many seconds to offset the search timeframe. Must be negative.",
				FlagName: "offset",
				Order:    100,
				CustomTIFuncInit: func() textinput.Model {
					ti := stylesheet.NewTI("", false)
					ti.Validate = func(s string) error {
						if s == "" {
							return errors.New("offset is required")
						}
						return validate.NegativeNumber(s)
					}
					return ti
				},
			},
			// TODO introduce SearchSinceLastRun bool after the scaffoldcreate/edit merge
			// TODO introduce enabled after the scaffoldcreate/edit merge
			// TODO introduce backfill after the scaffoldcreate/edit merge
		},
		scaffoldedit.SubroutineSet[string, types.ScheduledSearch]{
			// GetScheduledSearch can take an int32 or uuid
			SelectSub: func(id string) (item types.ScheduledSearch, err error) {
				return connection.Client.GetScheduledSearch(id)
			},
			FetchSub: func() (items []types.ScheduledSearch, err error) {
				list, err := connection.Client.ListScheduledSearches(nil)
				return list.Results, err
			},
			GetFieldSub: func(item types.ScheduledSearch, fieldKey string) (value string, err error) {
				switch fieldKey {
				case "name":
					return item.Name, nil
				case "description":
					return item.Description, nil
				case "search":
					return item.SearchString, nil
				case "frequency":
					return item.Schedule, nil
				case "duration":
					return strconv.FormatInt(item.Duration, 10), nil
				case "offset":
					return strconv.FormatInt(item.TimeframeOffset, 10), nil
				}

				return "", fmt.Errorf("unknown get field key: %v", fieldKey)
			},
			SetFieldSub: func(item *types.ScheduledSearch, fieldKey, val string) (invalid string, err error) {
				switch fieldKey {
				case "name":
					item.Name = val
				case "description":
					item.Description = val
				case "search":
					item.SearchString = val
				case "frequency":
					item.Schedule = val
				case "duration":
					dur, err := time.ParseDuration(val)
					if err != nil {
						return err.Error(), nil
					}
					item.Duration = -int64(dur.Abs().Seconds())
				case "offset":
					offset, err := strconv.ParseInt(val, 10, 64)
					if err != nil {
						return err.Error(), nil
					}
					item.TimeframeOffset = offset
				default:
					return "", fmt.Errorf("unknown set field key: %v", fieldKey)
				}

				return "", nil

			},
			GetTitleSub: func(item types.ScheduledSearch) string {
				return fmt.Sprintf("%s (executes '%s')", item.Name, item.SearchString)
			},
			GetDescriptionSub: func(item types.ScheduledSearch) string {
				return fmt.Sprintf("(%s) %s", item.Schedule, item.Description)
			},
			UpdateSub: func(data *types.ScheduledSearch) (identifier string, err error) {
				return data.Name, connection.Client.UpdateScheduledSearch(*data)
			},
		})
}

func getBackfillFlags(fs *pflag.FlagSet) (enable, disable bool, err error) {
	enable, err = fs.GetBool("enable")
	if err != nil {
		clilog.GetFlag(err)
		return
	}
	disable, err = fs.GetBool("disable")
	if err != nil {
		clilog.GetFlag(err)
		return
	}
	if enable && disable {
		return false, false, ft.ErrMutuallyExclusive("enable", "disable")
	}
	return
}

func cancel() action.Pair {
	return scaffoldselect.NewSelectAction("cancel running scheduled searches",
		"Cancel one or several currently-executing scheduled searches by ID.",
		"scheduled search",
		func(_ *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListScheduledSearches(nil)
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(lr.Results), nil
		},
		func(IDs []string, _ *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				if err := connection.Client.CancelScheduledSearch(ID); err != nil {
					results[i] = scaffold.Result{Success: false, Output: err.Error()}
				} else {
					results[i] = scaffold.Result{Success: true, Output: "successfully cancelled scheduled search " + ID}
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{Use: "cancel"},
		})
}

func backfillToggle() action.Pair {
	return scaffoldselect.NewSelectAction("toggle scheduled search backfill",
		"Toggle backfill for one or several scheduled searches.\n"+
			"Backfill causes the automation to run for missed time periods.\n"+
			"Use --enable or --disable to set explicitly.",
		"scheduled search",
		func(fs *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			enable, disable, err := getBackfillFlags(fs)
			if err != nil {
				return nil, err
			}
			l, err := connection.Client.ListScheduledSearches(nil)
			if err != nil {
				return nil, err
			}
			itms := make([]types.ScheduledSearch, 0, len(l.Results))
			for _, ss := range l.Results {
				if enable && ss.BackfillEnabled {
					continue
				} else if disable && !ss.BackfillEnabled {
					continue
				}

				itms = append(itms, ss)
			}

			return listitem.WrapAssets(slices.Clip(itms)), nil
		},
		func(IDs []string, fs *pflag.FlagSet) (results []scaffold.Result, _ error) {
			enable, disable, err := getBackfillFlags(fs)
			if err != nil {
				return nil, err
			}

			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				ss, err := connection.Client.GetScheduledSearch(ID)
				if err != nil {
					results[i] = scaffold.Result{Success: false, Output: err.Error()}
					continue
				}
				ss.BackfillEnabled = !ss.BackfillEnabled
				if enable {
					ss.BackfillEnabled = true
				} else if disable {
					ss.BackfillEnabled = false
				}

				if err := connection.Client.UpdateScheduledSearch(ss); err != nil {
					results[i] = scaffold.Result{Success: false, Output: err.Error()}
				} else {
					state := "enabled"
					if !ss.BackfillEnabled {
						state = "disabled"
					}
					results[i] = scaffold.Result{Success: true, Output: fmt.Sprintf("scheduled search '%s' backfill %s", ID, state)}
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "backfill",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("enable", false, "enable backfill")
					fs.Bool("disable", false, "disable backfill")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				_, _, err = getBackfillFlags(fs)
				return "", err
			},
		})
}

func clear() action.Pair {
	return scaffoldselect.NewSelectAction("clear results for scheduled searches",
		"Clear the execution results (including errors and state) for one or several scheduled searches.",
		"scheduled search",
		func(_ *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListScheduledSearches(nil)
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(lr.Results), nil
		},
		func(IDs []string, _ *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				if err := connection.Client.ClearScheduledSearchResults(ID); err != nil {
					results[i] = scaffold.Result{Success: false, Output: err.Error()}
				} else {
					results[i] = scaffold.Result{Success: true, Output: fmt.Sprintf("successfully cleared results for scheduled search %s", ID)}
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{Use: "clear"},
		})
}

func createScript() action.Pair {
	return scaffoldcreate.NewCreateAction("scheduled script",
		map[string]scaffoldcreate.Field{
			"lang": {
				Title:    "Language",
				Required: true,
				Flag: scaffoldcreate.FlagConfig{
					Name: "language",
					Usage: "Set the language the script is written in." +
						"Possible values: 'go', 'anko'.",
				},
				DefaultValue: "anko",
				Order:        200,
				Provider: &scaffoldcreate.TextProvider{
					CustomInit: func() textinput.Model {
						ti := stylesheet.NewTI("anko", false)
						ti.Validate = func(s string) error {
							_, err := types.ParseScriptLang(s)
							return err
						}
						return ti
					},
				},
			},
			"name":        scaffoldcreate.FieldName("scheduled script"),
			"description": scaffoldcreate.FieldDescription("scheduled script"),
			"path":        scaffoldcreate.FieldPath("script", true),
			"labels":      scaffoldcreate.FieldLabels(),
			"schedule":    scaffoldcreate.FieldFrequency(),
			"enabled": {
				Title: "Enabled?", Required: false,
				Flag:     scaffoldcreate.FlagConfig{},
				Order:    30,
				Provider: &scaffoldcreate.BoolProvider{},
			},
			"backfill": {
				Title: "Backfill?", Required: false,
				Flag:     scaffoldcreate.FlagConfig{},
				Order:    20,
				Provider: &scaffoldcreate.BoolProvider{},
			},
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			pth := fields["path"].Provider.Get()
			flowContent, err := os.ReadFile(pth)
			if err != nil {
				return 0, "", err
			}

			enabled, err := strconv.ParseBool(fields["enabled"].Provider.Get())
			if err != nil {
				return 0, err.Error(), nil
			}
			backfill, err := strconv.ParseBool(fields["backfill"].Provider.Get())
			if err != nil {
				return 0, err.Error(), nil
			}

			lang, err := types.ParseScriptLang(fields["lang"].Provider.Get())
			if err != nil {
				return 0, err.Error(), nil
			}

			new, err := connection.Client.CreateScheduledScript(types.ScheduledScript{
				CommonFields: types.CommonFields{
					Name:        fields["name"].Provider.Get(),
					Description: fields["description"].Provider.Get(),
					Labels:      scaffoldcreate.GetLabelsFromField(fields["labels"]),
				},
				AutomationCommonFields: types.AutomationCommonFields{
					Schedule:        fields["schedule"].Provider.Get(),
					Disabled:        !enabled,
					BackfillEnabled: backfill,
				},
				Script:         string(flowContent),
				ScriptLanguage: lang,
			})
			return new.ID, "", err
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "script",
				Aliases: []string{"from-script", "create-script"},
			},
		},
	)
}
