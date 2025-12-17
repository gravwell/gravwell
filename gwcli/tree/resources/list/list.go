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
		types.Resource{}, list, scaffoldlist.Options{
			DefaultColumns: []string{"ID", "Name", "Description", "Size"},
			ColumnAliases:  map[string]string{"Name": "Name", "Size": "SizeBytes"},
			AddtlFlags:     flags,
		})
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Bool("all", false, "ADMIN ONLY. Lists all schedule searches on the system")
	return addtlFlags
}

func list(fs *pflag.FlagSet) ([]types.Resource, error) {
	if all, err := fs.GetBool("all"); err != nil {
		uniques.ErrGetFlag("resources list", err)
	} else if all {
		resp, err := connection.Client.ListAllResources(nil)
		if err != nil {
			return nil, err
		}
		return resp.Results, nil
	}

	resp, err := connection.Client.ListResources(nil)
	if err != nil {
		return nil, err
	}
	return resp.Results, nil
}
