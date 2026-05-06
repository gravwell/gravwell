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
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"

	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewScheduledNav() *cobra.Command {
	return treeutils.GenerateNav("scheduled", "Manage scheduled queries", "Alter and view previously scheduled queries", []string{},
		[]*cobra.Command{},
		[]action.Pair{
			create(),
			list(),
			delete(),
			edit(),
		})
}

//#region list

func list() action.Pair {
	var (
		short = "list scheduled queries"
		long  = "prints out all scheduled queries."
	)
	return scaffoldlist.NewListAction(short, long,
		types.ScheduledSearch{}, listScheduledSearch,
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{AddtlFlags: flags},
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

func flags() *pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Bool("all", false, "ADMIN ONLY. Lists all schedule searches on the system.\n"+
		"Supersedes --id.")
	addtlFlags.String("id", "", "fetches the scheduled search associated to the given id."+
		"This id can be a standard, numeric ID or a uuid.")

	return &addtlFlags
}

func listScheduledSearch(fs *pflag.FlagSet) ([]types.ScheduledSearch, error) {
	if all, err := fs.GetBool("all"); err != nil {
		uniques.ErrGetFlag("scheduled list", err)
	} else if all {
		list, err := connection.Client.ListAllScheduledSearches(nil)
		return list.Results, err
	}
	if id, err := fs.GetString("id"); err != nil {
		uniques.ErrGetFlag("scheduled list", err)
	} else if id != "" {
		ss, err := connection.Client.GetScheduledSearch(id)
		return []types.ScheduledSearch{ss}, err
	}
	list, err := connection.Client.ListScheduledSearches(nil)
	return list.Results, err
}

//#endregion list

//#region create

const ( // field keys
	createNameKey     = "name"
	createDescKey     = "desc"
	createFreqKey     = "freq"
	createQryKey      = "qry"
	createDurationKey = "dur"
)

// create creates the action for creating new scheduled queries.
func create() action.Pair {
	fields := map[string]scaffoldcreate.Field{
		createQryKey: scaffoldcreate.Field{
			Required: true,
			Title:    "query",
			Flag:     scaffoldcreate.FlagConfig{Usage: "query to schedule", Shorthand: 'q'},
			Provider: &scaffoldcreate.TextProvider{},
			Order:    150,
		},
		createDurationKey: scaffoldcreate.Field{
			Required: true,
			Title:    "duration",
			Flag:     scaffoldcreate.FlagConfig{Name: "duration", Usage: "the time span the query will look back over"},
			Provider: &scaffoldcreate.TextProvider{
				CustomInit: func() textinput.Model { ti := stylesheet.NewTI("", false); ti.Placeholder = "1h2m3s4ms"; return ti },
			},
			Order: 140,
		},
		createNameKey: scaffoldcreate.FieldName("query"),
		createDescKey: scaffoldcreate.FieldDescription("query"),

		createFreqKey: scaffoldcreate.Field{ // manually build so we have more control
			Required: true,
			Title:    "frequency",
			Flag:     scaffoldcreate.FlagConfig{Name: ft.Frequency.Name(), Usage: ft.Frequency.Usage()},
			Provider: &scaffoldcreate.TextProvider{
				CustomInit: func() textinput.Model {
					ti := stylesheet.NewTI("", false)
					ti.Placeholder = "* * * * *"
					ti.Validate = uniques.CronRuneValidator
					return ti
				},
			},
			DefaultValue: "", // no default value
			Order:        50,
		},
	}

	return scaffoldcreate.NewCreateAction("scheduled query", fields, createFunc, scaffoldcreate.Options{})
}

// driver function for scheduled create
func createFunc(cfg map[string]scaffoldcreate.Field, _ *pflag.FlagSet) (any, string, error) {
	var (
		name      = cfg[createNameKey].Provider.Get()
		desc      = cfg[createDescKey].Provider.Get()
		freq      = cfg[createFreqKey].Provider.Get()
		qry       = cfg[createQryKey].Provider.Get()
		durString = cfg[createDurationKey].Provider.Get()
	)
	dur, err := time.ParseDuration(durString)
	if err != nil { // report as invalid parameter, not an error
		return nil, err.Error(), nil
	}

	return connection.CreateScheduledSearch(name, desc, freq, qry, dur)
}

//#endregion create

//#region delete

// builds the scheduled search delete action
func delete() action.Pair {
	return scaffolddelete.NewDeleteAction(
		"query", "queries", del, func() ([]scaffolddelete.Item[string], error) {
			ss, err := connection.Client.ListScheduledSearches(nil)
			if err != nil {
				return nil, err
			}
			// sort the results on name
			slices.SortFunc(ss.Results, func(m1, m2 types.ScheduledSearch) int {
				return strings.Compare(m1.Name, m2.Name)
			})
			var items = make([]scaffolddelete.Item[string], len(ss.Results))
			for i, ssi := range ss.Results {
				items[i] = scaffolddelete.NewItem[string](ssi.Name,
					fmt.Sprintf("%v\n(looks %v seconds into the past)",
						ssi.SearchString, math.Abs(float64(ssi.Duration))),
					ssi.ID)
			}
			return items, nil
		})
}

// deletes a scheduled search
func del(dryrun bool, id string) error {
	if dryrun {
		_, err := connection.Client.GetScheduledSearch(id)
		return err
	}

	return connection.Client.DeleteScheduledSearch(id)

}

//#endregion delete

//#region edit

const ( // field keys
	editNameKey     = "name"
	editDescKey     = "description"
	editSearchKey   = "search"
	editScheduleKey = "schedule"
)

const singular string = "scheduled search"

func edit() action.Pair {
	cfg := scaffoldedit.Config{
		editNameKey: &scaffoldedit.Field{
			Required: true,
			Title:    "name",
			Usage:    ft.Name.Usage(singular),
			FlagName: ft.Name.Name(),
			Order:    100,
		},
		editDescKey: &scaffoldedit.Field{
			Required: true,
			Title:    "description",
			Usage:    ft.Description.Usage(singular),
			FlagName: ft.Description.Name(),
			Order:    80,
		},
		editSearchKey: &scaffoldedit.Field{
			Required: true,
			Title:    "query",
			Usage:    "the query executed by this scheduled search",
			FlagName: "query",
			Order:    60,
		},
		editScheduleKey: &scaffoldedit.Field{
			Required: true,
			Title:    "frequency",
			Usage:    ft.Frequency.Usage(),
			FlagName: ft.Frequency.Name(),
			Order:    40,
			CustomTIFuncInit: func() textinput.Model {
				ti := stylesheet.NewTI("", false)
				ti.Placeholder = "* * * * *"
				ti.Validate = uniques.CronRuneValidator
				return ti
			},
		},
	}

	funcs := scaffoldedit.SubroutineSet[string, types.ScheduledSearch]{
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
			case editNameKey:
				return item.Name, nil
			case editDescKey:
				return item.Description, nil
			case editSearchKey:
				return item.SearchString, nil
			case editScheduleKey:
				return item.Schedule, nil
			}

			return "", fmt.Errorf("unknown get field key: %v", fieldKey)
		},
		SetFieldSub: func(item *types.ScheduledSearch, fieldKey, val string) (invalid string, err error) {
			switch fieldKey {
			case editNameKey:
				item.Name = val
			case editDescKey:
				item.Description = val
			case editSearchKey:
				item.SearchString = val
			case editScheduleKey:
				item.Schedule = val
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
	}

	return scaffoldedit.NewEditAction(singular, "scheduled searches", cfg, funcs)
}

//#endregion edit
