/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package edit

import (
	"fmt"
	"github.com/gravwell/gravwell/v3/gwcli/action"
	"github.com/gravwell/gravwell/v3/gwcli/connection"
	ft "github.com/gravwell/gravwell/v3/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v3/gwcli/utilities/scaffold/scaffoldedit"

	"github.com/gravwell/gravwell/v3/client/types"
)

const ( // field keys
	kname     = "name"
	kdesc     = "description"
	ksearch   = "search"
	kschedule = "schedule"
)

const singular string = "scheduled search"

func NewQueriesScheduledEditAction() action.Pair {
	cfg := scaffoldedit.Config{
		kname: &scaffoldedit.Field{
			Required: true,
			Title:    "Name",
			Usage:    ft.Usage.Name(singular),
			FlagName: ft.Name.Name,
			Order:    100,
		},
		kdesc: &scaffoldedit.Field{
			Required: true,
			Title:    "Description",
			Usage:    ft.Usage.Desc(singular),
			FlagName: ft.Name.Desc,
			Order:    80,
		},
		ksearch: &scaffoldedit.Field{
			Required: true,
			Title:    "Query",
			Usage:    "the query executed by this scheduled search",
			FlagName: ft.Name.Query,
			Order:    60,
		},
		kschedule: &scaffoldedit.Field{
			Required: true,
			Title:    "Schedule",
			Usage:    ft.Usage.Frequency,
			FlagName: "schedule",
			Order:    40,
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
			case kname:
				return item.Name, nil
			case kdesc:
				return item.Description, nil
			case ksearch:
				return item.SearchString, nil
			case kschedule:
				return item.Schedule, nil
			}

			return "", fmt.Errorf("unknown get field key: %v", fieldKey)
		},
		SetFieldSub: func(item *types.ScheduledSearch, fieldKey, val string) (invalid string, err error) {
			switch fieldKey {
			case kname:
				item.Name = val
			case kdesc:
				item.Description = val
			case ksearch:
				item.SearchString = val
			case kschedule:
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
