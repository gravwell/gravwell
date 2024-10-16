/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package macros

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/tree/macros/create"
	"github.com/gravwell/gravwell/v4/gwcli/tree/macros/delete"
	"github.com/gravwell/gravwell/v4/gwcli/tree/macros/edit"
	"github.com/gravwell/gravwell/v4/gwcli/tree/macros/list"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
)

const (
	use   string = "macros"
	short string = "manage search macros"
	long  string = "Macros are search keywords that expand to set phrases on use within a query."
)

var aliases []string = []string{"macro", "m"}

func NewMacrosNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases, []*cobra.Command{},
		[]action.Pair{list.NewMacroListAction(),
			create.NewMacroCreateAction(),
			delete.NewMacroDeleteAction(),
			edit.NewMacroEditAction()})
}
