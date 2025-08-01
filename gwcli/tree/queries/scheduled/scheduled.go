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
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"

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
		scaffoldlist.Options{
			AddtlFlags: flags,
			ExcludeColumnsFromDefault: []string{
				"DebugEvent.Type",
				"DebugEvent.Metadata.UID",
				"DebugEvent.Metadata.Username",
				"DebugEvent.Metadata.AlertID",
				"DebugEvent.Metadata.AlertName",
				"DebugEvent.Metadata.AlertActivation",
				"DebugEvent.Metadata.EventIndex",
				"DebugEvent.Metadata.TargetTag",
				"DebugEvent.Metadata.AlertLabels",
				"DebugEvent.Metadata.Dispatcher.Type",
				"DebugEvent.Metadata.Dispatcher.ID",
				"DebugEvent.Metadata.Dispatcher.Name",
				"DebugEvent.Metadata.Dispatcher.SearchID",
				"DebugEvent.Metadata.Dispatcher.Labels",
				"DebugEvent.Metadata.Dispatcher.EventCount",
				"DebugEvent.Metadata.Dispatcher.EventsElided",
				"DebugEvent.Metadata.Consumers",
				"DebugEvent.Metadata.UserMetadata",
				"DebugEvent.Metadata.ValidationProblems",
				"DebugEvent.Contents",
				"WriteAccess.Global",
				"WriteAccess.GIDs",
			},
		})
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Bool("all", false, "ADMIN ONLY. Lists all schedule searches on the system.\n"+
		"Supersedes --id.")
	addtlFlags.String("id", "", "fetches the scheduled search associated to the given id."+
		"This id can be a standard, numeric ID or a uuid.")

	return addtlFlags
}

func listScheduledSearch(fs *pflag.FlagSet) ([]types.ScheduledSearch, error) {
	if all, err := fs.GetBool("all"); err != nil {
		uniques.ErrGetFlag("scheduled list", err)
	} else if all {
		return connection.Client.GetAllScheduledSearches()
	}
	if untypedID, err := fs.GetString("id"); err != nil {
		uniques.ErrGetFlag("scheduled list", err)
	} else if untypedID != "" {
		// attempt to parse as UUID first
		if uuid, err := uuid.Parse(untypedID); err == nil {
			ss, err := connection.Client.GetScheduledSearch(uuid)
			return []types.ScheduledSearch{ss}, err
		}
		// now try as int32
		if i32id, err := strconv.Atoi(untypedID); err == nil {
			ss, err := connection.Client.GetScheduledSearch(i32id)
			return []types.ScheduledSearch{ss}, err
		}

		// both have failed, error out
		errString := fmt.Sprintf("failed to parse %v as a uuid or int32 id", untypedID)
		clilog.Writer.Infof("%s", errString)

		return nil, errors.New(errString)
	}
	return connection.Client.GetScheduledSearchList()
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
	fields := scaffoldcreate.Config{
		createNameKey: scaffoldcreate.NewField(true, "name", 100),
		createDescKey: scaffoldcreate.NewField(false, "description", 90),
		createDurationKey: scaffoldcreate.Field{
			Required:         true,
			Title:            "duration",
			Usage:            "the time span the query will look back over",
			Type:             scaffoldcreate.Text,
			FlagName:         "duration",
			Order:            140,
			CustomTIFuncInit: func() textinput.Model { ti := stylesheet.NewTI("", false); ti.Placeholder = "1h2m3s4ms"; return ti },
		},
		createQryKey: scaffoldcreate.NewField(true, "query", 150),
		createFreqKey: scaffoldcreate.Field{ // manually build so we have more control
			Required:     true,
			Title:        "frequency",
			Usage:        ft.Frequency.Usage(),
			Type:         scaffoldcreate.Text,
			FlagName:     ft.Frequency.Name(), // custom flag name
			DefaultValue: "",                  // no default value
			Order:        50,
			CustomTIFuncInit: func() textinput.Model {
				ti := stylesheet.NewTI("", false)
				ti.Placeholder = "* * * * *"
				ti.Validate = uniques.CronRuneValidator
				return ti
			},
		},
	}

	return scaffoldcreate.NewCreateAction("scheduled query", fields, createFunc, nil)
}

// driver function for scheduled create
func createFunc(_ scaffoldcreate.Config, vals map[string]string, _ *pflag.FlagSet) (any, string, error) {
	var (
		name      = vals[createNameKey]
		desc      = vals[createDescKey]
		freq      = vals[createFreqKey]
		qry       = vals[createQryKey]
		durString = vals[createDurationKey]
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
		"query", "queries", del, func() ([]scaffolddelete.Item[int32], error) {
			ss, err := connection.Client.GetScheduledSearchList()
			if err != nil {
				return nil, err
			}
			// sort the results on name
			slices.SortFunc(ss, func(m1, m2 types.ScheduledSearch) int {
				return strings.Compare(m1.Name, m2.Name)
			})
			var items = make([]scaffolddelete.Item[int32], len(ss))
			for i, ssi := range ss {
				items[i] = scaffolddelete.NewItem[int32](ssi.Name,
					fmt.Sprintf("%v\n(looks %v seconds into the past)",
						ssi.SearchString, math.Abs(float64(ssi.Duration))),
					ssi.ID)
			}
			return items, nil
		})
}

// deletes a scheduled search
func del(dryrun bool, id int32) error {
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

	funcs := scaffoldedit.SubroutineSet[int32, types.ScheduledSearch]{
		// GetScheduledSearch can take an int32 or uuid
		SelectSub: func(id int32) (item types.ScheduledSearch, err error) {
			return connection.Client.GetScheduledSearch(id)
		},
		FetchSub: func() (items []types.ScheduledSearch, err error) {
			return connection.Client.GetScheduledSearchList()
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
