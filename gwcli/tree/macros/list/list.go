/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package list

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/spf13/pflag"

	"github.com/gravwell/gravwell/v4/client/types"
)

var (
	short string = "list your macros"
	long  string = "lists all macros associated to your user, a group," +
		"or the system itself"
	defaultColumns []string = []string{"ID", "Name", "Description", "Expansion"}
)

func NewMacroListAction() action.Pair {
	return scaffoldlist.NewListAction("", short, long, defaultColumns,
		types.SearchMacro{}, listMacros, flags)
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Bool(ft.Name.ListAll, false, ft.Usage.ListAll("macros")+"\n"+
		"Ignored if you are not an admin.\n"+
		"Supersedes --group.")
	addtlFlags.Int32("group", 0, "Fetches all macros shared with the given group id.")
	return addtlFlags
}

func listMacros(c *grav.Client, fs *pflag.FlagSet) ([]types.SearchMacro, error) {
	if all, err := fs.GetBool(ft.Name.ListAll); err != nil {
		clilog.LogFlagFailedGet(ft.Name.ListAll, err)
	} else if all {
		return c.GetAllMacros()
	}
	if gid, err := fs.GetInt32("group"); err != nil {
		clilog.LogFlagFailedGet("group", err)
	} else if gid != 0 {
		return c.GetGroupMacros(gid)
	}

	return c.GetUserMacros(connection.MyInfo.UID)
}
