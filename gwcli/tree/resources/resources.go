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
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/tree/resources/list"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
)

const (
	use   string = "resources"
	short string = "manage persistent search data"
	long  string = "Resources store persistent data for use in searches." +
		" Resources can be manually uploaded by a user or automatically created by search modules." +
		" Resources are used by a number of modules for things such as storing lookup tables," +
		" scripts, and more. A resource is simply a stream of bytes."
)

var aliases []string = []string{}

func NewResourcesNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{},
		[]action.Pair{list.NewResourcesListAction()})
}
