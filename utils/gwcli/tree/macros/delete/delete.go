package delete

import (
	"fmt"
	"gwcli/action"
	"gwcli/connection"
	"gwcli/stylesheet"
	"gwcli/utilities/scaffold/scaffolddelete"
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
