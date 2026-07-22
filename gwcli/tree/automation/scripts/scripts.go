/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package scripts manages saved scripts.
package scripts

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/annotations"
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

func NewNav() *cobra.Command {
	return treeutils.GenerateNav("scripts", "manage automation scripts",
		"Scripting is used in two ways within Gravwell: as part of a search pipeline, and as a method to automate search launching. "+
			" See: https://docs.gravwell.io/scripting/scripting.html",
		nil,
		[]action.Pair{
			listAction(),
			create(),
			delete(),
			edit(),
			cancel(),
			backfillToggle(),
			clear(),
		},
		treeutils.NodeOptions{
			CommandAliases: []string{"script", "anko"},
		})
}

// also serves as GET
func listAction() action.Pair {
	return scaffoldlist.NewListAction(
		"list automation scripts",
		"Lists information about the scheduled scripts you can access.",
		types.ScheduledScript{},
		func(fs *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.ScheduledScript, error) {
			if id, err := fs.GetString("id"); err != nil {
				clilog.GetFlag(err)
			} else if id != "" {
				s, err := connection.Client.GetScheduledScriptEx(id, params.QueryOpts)
				return []types.ScheduledScript{s}, err
			}
			list, err := connection.Client.ListScheduledScripts(params.QueryOpts)
			return list.Results, err
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := pflag.FlagSet{}
					fs.String("id", "", "fetches the script associated with the given id.")
					return &fs
				},
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ScheduleRead},
					XPermissions: []types.Capability{types.ScheduleRead},
				},
			},
			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Name",
				"CommonFields.Description",
				"AutomationCommonFields.Schedule",
				"AutomationCommonFields.Disabled",
				"ScriptLanguage",
			},
		})
}

func create() action.Pair {
	return scaffoldcreate.NewCreateAction("script",
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
			"name":        scaffoldcreate.FieldName("script"),
			"description": scaffoldcreate.FieldDescription("script"),
			"path":        scaffoldcreate.FieldPath("script", true),
			"labels":      scaffoldcreate.FieldLabels(),
			"schedule":    scaffoldcreate.FieldFrequency(),
			"enabled": {
				Title: "Enabled?", Required: false,
				Flag: scaffoldcreate.FlagConfig{
					Usage: ft.EnableBoolUsage,
				},
				Order:    30,
				Provider: &scaffoldcreate.BoolProvider{},
			},
			"backfill": {
				Title: "Backfill?", Required: false,
				Flag: scaffoldcreate.FlagConfig{
					Name:  ft.BackfillName,
					Usage: ft.BackfillBoolUsage,
				},
				Order:    20,
				Provider: &scaffoldcreate.BoolProvider{},
			},
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			pth := fields["path"].Provider.Get()
			scriptContent, err := os.ReadFile(pth)
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
				Script:         string(scriptContent),
				ScriptLanguage: lang,
			})
			return new.ID, "", err
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ScheduleWrite},
					XPermissions: []types.Capability{types.ScheduleWrite},
				},
			},
		},
	)
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("script",
		func(dryrun bool, id string, _ *pflag.FlagSet) error {
			if dryrun {
				_, err := connection.Client.GetScheduledScript(id)
				return err
			}
			return connection.Client.DeleteScheduledScript(id)
		},
		func(params scaffolddelete.DataParameters) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListScheduledScripts(params.QueryOpts)
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(lr.Results), nil
		}, scaffolddelete.Options{
			QueryOptionsFlags: scaffold.QOInclude{Everything: true},
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ScheduleRead, types.ScheduleWrite},
					XPermissions: []types.Capability{types.ScheduleWrite},
				},
			},
		})
}

func edit() action.Pair {
	return scaffoldedit.NewEditAction("script", "scripts",
		scaffoldedit.Config{
			"name":        scaffoldedit.FieldName("script"),
			"description": scaffoldedit.FieldDescription("script"),
			"labels":      scaffoldedit.FieldLabels(),
			"language": &scaffoldedit.Field{
				Required: true,
				Title:    "Language",
				Usage:    "the language the script is written in. Possible values: 'go', 'anko'.",
				FlagName: "language",
				Order:    150,
				CustomTIFuncInit: func() textinput.Model {
					ti := stylesheet.NewTI("", false)
					ti.Validate = func(s string) error {
						_, err := types.ParseScriptLang(s)
						return err
					}
					return ti
				},
			},
			// TODO introduce script as a textarea after the scaffoldcreate/edit merge
			"frequency": &scaffoldedit.Field{
				Required: true,
				Title:    "Frequency",
				Usage:    ft.Frequency.Usage(),
				FlagName: ft.Frequency.Name(),
				Order:    110,
				CustomTIFuncInit: func() textinput.Model {
					ti := stylesheet.NewTI("", false)
					ti.Placeholder = "* * * * *"
					ti.Validate = validate.CronRuneValidator
					return ti
				},
			},
			// TODO introduce enabled after the scaffoldcreate/edit merge
			// TODO introduce backfill after the scaffoldcreate/edit merge
		},
		scaffoldedit.SubroutineSet[string, types.ScheduledScript]{
			// GetScheduledScript can take an int32 or uuid
			SelectSub: func(id string) (item types.ScheduledScript, err error) {
				return connection.Client.GetScheduledScript(id)
			},
			FetchSub: func() (items []types.ScheduledScript, err error) {
				list, err := connection.Client.ListScheduledScripts(nil)
				return list.Results, err
			},
			GetFieldSub: func(item types.ScheduledScript, fieldKey string) (value string, err error) {
				switch fieldKey {
				case "name":
					return item.Name, nil
				case "description":
					return item.Description, nil
				case "labels":
					return strings.Join(item.Labels, ","), nil
				case "language":
					return item.ScriptLanguage.String(), nil
				case "frequency":
					return item.Schedule, nil
				}

				return "", fmt.Errorf("unknown get field key: %v", fieldKey)
			},
			SetFieldSub: func(item *types.ScheduledScript, fieldKey, val string) (invalid string, err error) {
				switch fieldKey {
				case "name":
					item.Name = val
				case "description":
					item.Description = val
				case "labels":
					item.Labels = strings.Split(val, ",")
				case "language":
					lang, err := types.ParseScriptLang(val)
					if err != nil {
						return err.Error(), nil
					}
					item.ScriptLanguage = lang
				case "frequency":
					item.Schedule = val
				default:
					return "", fmt.Errorf("unknown set field key: %v", fieldKey)
				}

				return "", nil

			},
			GetTitleSub: func(item types.ScheduledScript) string {
				return fmt.Sprintf("%s (%s script)", item.Name, item.ScriptLanguage)
			},
			GetDescriptionSub: func(item types.ScheduledScript) string {
				return fmt.Sprintf("(%s) %s", item.Schedule, item.Description)
			},
			UpdateSub: func(data *types.ScheduledScript) (identifier string, err error) {
				return data.Name, connection.Client.UpdateScheduledScript(*data)
			},
		},
		scaffoldedit.Options{CommonOptions: scaffold.CommonOptions{
			Requirements: annotations.Requirements{
				IPermissions: []types.Capability{types.ScheduleRead, types.ScheduleWrite},
				XPermissions: []types.Capability{types.ScheduleRead, types.ScheduleWrite},
			},
		}})
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
	return scaffoldselect.NewSelectAction("cancel running scripts",
		"Cancel one or several scripts by ID, killing any active runs.",
		"script",
		func(_ *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListScheduledScripts(nil)
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(lr.Results), nil
		},
		func(IDs []string, _ *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				if err := connection.Client.CancelScheduledScript(ID); err != nil {
					results[i] = scaffold.Result{Success: false, Output: err.Error()}
				} else {
					results[i] = scaffold.Result{Success: true, Output: "successfully cancelled script " + ID}
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "cancel",
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ScheduleRead, types.ScheduleWrite},
					XPermissions: []types.Capability{types.ScheduleWrite},
				},
			},
		})
}

func backfillToggle() action.Pair {
	return scaffoldselect.NewSelectAction("toggle script backfill",
		"Toggle backfill for one or several scripts.\n"+
			"Backfill causes the automation to run for missed time periods.\n"+
			"Use --enable or --disable to set explicitly.",
		"script",
		func(fs *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			enable, disable, err := getBackfillFlags(fs)
			if err != nil {
				return nil, err
			}
			l, err := connection.Client.ListScheduledScripts(nil)
			if err != nil {
				return nil, err
			}
			itms := make([]types.ScheduledScript, 0, len(l.Results))
			for _, s := range l.Results {
				if enable && s.BackfillEnabled {
					continue
				} else if disable && !s.BackfillEnabled {
					continue
				}

				itms = append(itms, s)
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
				s, err := connection.Client.GetScheduledScript(ID)
				if err != nil {
					results[i] = scaffold.Result{Success: false, Output: err.Error()}
					continue
				}
				s.BackfillEnabled = !s.BackfillEnabled
				if enable {
					s.BackfillEnabled = true
				} else if disable {
					s.BackfillEnabled = false
				}

				if err := connection.Client.UpdateScheduledScript(s); err != nil {
					results[i] = scaffold.Result{Success: false, Output: err.Error()}
				} else {
					state := "enabled"
					if !s.BackfillEnabled {
						state = "disabled"
					}
					results[i] = scaffold.Result{Success: true, Output: fmt.Sprintf("script '%s' backfill %s", ID, state)}
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
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ScheduleRead, types.ScheduleWrite},
					XPermissions: []types.Capability{types.ScheduleRead, types.ScheduleWrite},
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				_, _, err = getBackfillFlags(fs)
				return "", err
			},
		})
}

func clear() action.Pair {
	return scaffoldselect.NewSelectAction("clear results for scripts",
		"Clear the execution results (including errors and state) for one or several scripts.",
		"script",
		func(_ *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListScheduledScripts(nil)
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(lr.Results), nil
		},
		func(IDs []string, _ *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				if err := connection.Client.ClearScheduledScriptResults(ID); err != nil {
					results[i] = scaffold.Result{Success: false, Output: err.Error()}
				} else {
					results[i] = scaffold.Result{Success: true, Output: fmt.Sprintf("successfully cleared results for script %s", ID)}
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "clear",
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ScheduleRead, types.ScheduleWrite},
					XPermissions: []types.Capability{types.ScheduleWrite},
				},
			},
		})
}
