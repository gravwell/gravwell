/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scheduled

import (
	"github.com/gravwell/gravwell/v3/gwcli/action"
	"github.com/gravwell/gravwell/v3/gwcli/tree/queries/scheduled/create"
	"github.com/gravwell/gravwell/v3/gwcli/tree/queries/scheduled/delete"
	"github.com/gravwell/gravwell/v3/gwcli/tree/queries/scheduled/edit"
	"github.com/gravwell/gravwell/v3/gwcli/tree/queries/scheduled/list"

	"github.com/gravwell/gravwell/v3/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
)

const (
	use   string = "scheduled"
	short string = "Manage scheduled queries"
	long  string = "Alter and view previously scheduled queries"
)

func NewScheduledNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, []string{},
		[]*cobra.Command{},
		[]action.Pair{
			create.NewQueriesScheduledCreateAction(),
			list.NewScheduledQueriesListAction(),
			delete.NewQueriesScheduledDeleteAction(),
			edit.NewQueriesScheduledEditAction(),
		})
}
