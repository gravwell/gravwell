/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package resources defines the resources nav, which holds data related to persistent data.
*/
package resources

import (
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewResourcesNav() *cobra.Command {
	const (
		use   string = "resources"
		short string = "manage persistent search data"
		long  string = "Resources store persistent data for use in searches." +
			" Resources can be manually uploaded by a user or automatically created by search modules." +
			" Resources are used by a number of modules for things such as storing lookup tables," +
			" scripts, and more. A resource is simply a stream of bytes."
	)
	return treeutils.GenerateNav(use, short, long, nil,
		[]*cobra.Command{},
		[]action.Pair{
			list(),
			delete(),
		})
}

func list() action.Pair {
	const (
		short string = "list resources on the system"
		long  string = "view resources available to your user and the system"
	)
	return scaffoldlist.NewListAction(short, long,
		types.Resource{}, func(fs *pflag.FlagSet) ([]types.Resource, error) {
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
		},
		scaffoldlist.Options{
			DefaultColumns: []string{
				"ID",
				"Name",
				"Description",
				"Size"},
			ColumnAliases: map[string]string{
				"Size": "SizeBytes",
			},
			AddtlFlags: flags,
		})
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Bool("all", false, "ADMIN ONLY. Lists all resources on the system")
	return addtlFlags
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("resource", "resources",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetResourceMetadata(id)
				return err
			}
			return connection.Client.DeleteResource(id)
		},
		func() ([]scaffolddelete.Item[string], error) {
			resources, err := connection.Client.ListResources(nil)
			if err != nil {
				return nil, err
			}
			slices.SortStableFunc(resources.Results,
				func(a, b types.Resource) int {
					return strings.Compare(a.Name, b.Name)
				})
			var items = make([]scaffolddelete.Item[string], len(resources.Results))
			for i, r := range resources.Results {
				items[i] = scaffolddelete.NewItem(r.Name, r.Description, r.ID)
			}
			return items, nil
		})
}
