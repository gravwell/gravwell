/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package list

import (
	"github.com/gravwell/gravwell/v3/gwcli/action"
	"github.com/gravwell/gravwell/v3/gwcli/clilog"
	ft "github.com/gravwell/gravwell/v3/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v3/gwcli/utilities/scaffold/scaffoldlist"

	grav "github.com/gravwell/gravwell/v3/client"

	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/spf13/pflag"
)

const (
	short string = "list resources on the system"
	long  string = "view resources avaialble to your user and the system"
)

var (
	defaultColumns []string = []string{"ID", "UID", "Name", "Description"}
)

func NewResourcesListAction() action.Pair {
	return scaffoldlist.NewListAction("", short, long, defaultColumns,
		types.ResourceMetadata{}, list, flags)
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Bool(ft.Name.ListAll, false, ft.Usage.ListAll("resources"))
	return addtlFlags
}

func list(c *grav.Client, fs *pflag.FlagSet) ([]types.ResourceMetadata, error) {
	if all, err := fs.GetBool(ft.Name.ListAll); err != nil {
		clilog.LogFlagFailedGet(ft.Name.ListAll, err)
	} else if all {
		return c.GetAllResourceList()
	}

	return c.GetResourceList()
}
