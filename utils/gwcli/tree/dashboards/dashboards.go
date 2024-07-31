package dashboards

import (
	"gwcli/action"
	"gwcli/tree/dashboards/delete"
	"gwcli/tree/dashboards/list"
	"gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
)

const (
	use   string = "dashboards"
	short string = "manage your dashboards"
	long  string = "Manage and view your available web dashboards." +
		"Dashboards are not usable from the CLI, but can be altered."
)

var aliases []string = []string{"dashboard", "dash"}

func NewDashboardNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{},
		[]action.Pair{
			list.NewDashboardsListAction(),
			delete.NewDashboardDeleteAction(),
		})
}
