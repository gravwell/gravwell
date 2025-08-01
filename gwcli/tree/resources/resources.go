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

	"github.com/crewjam/rfc5424"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/tree/resources/list"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
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
			list.NewResourcesListAction(),
			delete(),
		})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("resource", "resources",
		func(dryrun bool, id uuid.UUID) error {
			if dryrun {
				_, err := connection.Client.GetResourceMetadata(id.String())
				return err
			}
			return connection.Client.DeleteResource(id.String())
		},
		func() ([]scaffolddelete.Item[uuid.UUID], error) {
			resources, err := connection.Client.GetResourceList()
			if err != nil {
				return nil, err
			}
			slices.SortStableFunc(resources,
				func(a, b types.ResourceMetadata) int {
					return strings.Compare(a.ResourceName, b.ResourceName)
				})
			var items = make([]scaffolddelete.Item[uuid.UUID], len(resources))
			for i, r := range resources {
				id, err := uuid.Parse(r.GUID)
				if err != nil {
					clilog.Writer.Warn("failed to parse GUID of resource",
						rfc5424.SDParam{Name: "GUID", Value: r.GUID},
						rfc5424.SDParam{Name: "Name", Value: r.ResourceName},
					)
					id = uuid.Nil
				}
				items[i] = scaffolddelete.NewItem(r.ResourceName, r.Description, id)
			}
			return items, nil
		})
}
