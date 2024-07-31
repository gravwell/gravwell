package resources

import (
	"gwcli/action"
	"gwcli/tree/resources/list"
	"gwcli/utilities/treeutils"

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
