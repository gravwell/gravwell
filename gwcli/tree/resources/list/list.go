/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package list lists current resources.
package list

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/spf13/pflag"
)

const (
	short string = "list resources on the system"
	long  string = "view resources avaialble to your user and the system"
)

func NewResourcesListAction() action.Pair {
	return scaffoldlist.NewListAction(short, long,
		types.ResourceMetadata{}, list, scaffoldlist.Options{
			DefaultColumns: []string{"UID", "ResourceName", "Description", "Size", "Global"},
			ColumnAliases:  map[string]string{"ResourceName": "Name"},
			AddtlFlags:     flags,
		})
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Bool("all", false, "ADMIN ONLY. Lists all schedule searches on the system")
	return addtlFlags
}

func list(fs *pflag.FlagSet) ([]types.ResourceMetadata, error) {
	if all, err := fs.GetBool("all"); err != nil {
		uniques.ErrGetFlag("resources list", err)
	} else if all {
		return connection.Client.GetAllResourceList()
	}

	return connection.Client.GetResourceList()
}
