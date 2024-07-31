/**
 * Contains utilities related to interacting with existing or former queries.
 * All query creation is done at the top-level query action.
 */
package queries

import (
	"gwcli/action"
	"gwcli/tree/queries/history"
	"gwcli/tree/queries/scheduled"
	"gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
)

const (
	use   string = "queries"
	short string = "manage existing and past queries"
	long  string = "Queries contians utilities for managing auxillary query actions." +
		"Query creation is handled by the top-level `query` action."
)

var aliases []string = []string{"searches"}

func NewQueriesNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{scheduled.NewScheduledNav()},
		[]action.Pair{history.NewQueriesHistoryListAction()})
}
