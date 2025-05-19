/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package list

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"

	"github.com/google/uuid"
	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/spf13/pflag"
)

var (
	short          string   = "list scheduled queries"
	long           string   = "prints out all scheduled queries."
	defaultColumns []string = []string{"ID", "Name", "Description", "Duration", "Schedule"}
)

func NewScheduledQueriesListAction() action.Pair {
	return scaffoldlist.NewListAction("", short, long, defaultColumns,
		types.ScheduledSearch{}, listScheduledSearch, flags)
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Bool(ft.Name.ListAll, false, ft.Usage.ListAll("scheduled searches")+
		" Supercedes --id. Returns your searches if you are not an admin.")
	addtlFlags.String(ft.Name.ID, "", "Fetches the scheduled search associated to the given id."+
		"This id can be a standard, numeric ID or a uuid.")

	return addtlFlags
}

func listScheduledSearch(c *grav.Client, fs *pflag.FlagSet) ([]types.ScheduledSearch, error) {
	if all, err := fs.GetBool(ft.Name.ListAll); err != nil {
		clilog.LogFlagFailedGet(ft.Name.ListAll, err)
	} else if all {
		return c.GetAllScheduledSearches()
	}
	if untypedID, err := fs.GetString(ft.Name.ListAll); err != nil {
		clilog.LogFlagFailedGet(ft.Name.ListAll, err)
	} else if untypedID != "" {
		// attempt to parse as UUID first
		if uuid, err := uuid.Parse(untypedID); err == nil {
			ss, err := c.GetScheduledSearch(uuid)
			return []types.ScheduledSearch{ss}, err
		}
		// now try as int32
		if i32id, err := strconv.Atoi(untypedID); err == nil {
			ss, err := c.GetScheduledSearch(i32id)
			return []types.ScheduledSearch{ss}, err
		}

		// both have failed, error out
		errString := fmt.Sprintf("failed to parse %v as a uuid or int32 id", untypedID)
		clilog.Writer.Infof("%s", errString)

		return nil, errors.New(errString)
	}
	return c.GetScheduledSearchList()
}
