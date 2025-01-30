/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package delete

import (
	"fmt"
	"github.com/gravwell/gravwell/v3/gwcli/action"
	"github.com/gravwell/gravwell/v3/gwcli/connection"
	"github.com/gravwell/gravwell/v3/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v3/gwcli/utilities/scaffold/scaffolddelete"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v3/client/types"
)

func NewMacroDeleteAction() action.Pair {
	return scaffolddelete.NewDeleteAction("macro", "macros", del,
		func() ([]scaffolddelete.Item[uint64], error) {
			ms, err := connection.Client.GetUserGroupsMacros()
			if err != nil {
				return nil, err
			}
			slices.SortFunc(ms, func(m1, m2 types.SearchMacro) int {
				return strings.Compare(m1.Name, m2.Name)
			})
			var items = make([]scaffolddelete.Item[uint64], len(ms))
			for i, m := range ms {
				items[i] = scaffolddelete.NewItem(
					m.Name,
					fmt.Sprintf("Expansion: '%v'\n%v",
						stylesheet.Header2Style.Render(m.Expansion), m.Description),
					m.ID)
			}
			return items, nil
		})
}

func del(dryrun bool, id uint64) error {
	if dryrun {
		_, err := connection.Client.GetMacro(id)
		return err
	}
	return connection.Client.DeleteMacro(id)
}
