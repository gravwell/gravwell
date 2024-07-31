package delete

import (
	"fmt"
	"gwcli/action"
	"gwcli/connection"
	"gwcli/utilities/scaffold/scaffolddelete"
	"math"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v3/client/types"
)

func NewQueriesScheduledDeleteAction() action.Pair {
	return scaffolddelete.NewDeleteAction(
		"query", "queries", del, func() ([]scaffolddelete.Item[int32], error) {
			ss, err := connection.Client.GetScheduledSearchList()
			if err != nil {
				return nil, err
			}
			// sort the results on name
			slices.SortFunc(ss, func(m1, m2 types.ScheduledSearch) int {
				return strings.Compare(m1.Name, m2.Name)
			})
			var items = make([]scaffolddelete.Item[int32], len(ss))
			for i, ssi := range ss {
				items[i] = scaffolddelete.NewItem[int32](ssi.Name,
					fmt.Sprintf("%v\n(looks %v seconds into the past)",
						ssi.SearchString, math.Abs(float64(ssi.Duration))),
					ssi.ID)
			}
			return items, nil
		})
}

// deletes a scheduled search
func del(dryrun bool, id int32) error {
	if dryrun {
		_, err := connection.Client.GetScheduledSearch(id)
		return err
	}

	return connection.Client.DeleteScheduledSearch(id)

}
