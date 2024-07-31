package list

import (
	"gwcli/action"
	"gwcli/clilog"
	"gwcli/connection"
	ft "gwcli/stylesheet/flagtext"
	"gwcli/utilities/scaffold/scaffoldlist"

	grav "github.com/gravwell/gravwell/v3/client"
	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/spf13/pflag"
)

var (
	short          string   = "list dashboards"
	long           string   = "list dashboards available to you and the system"
	defaultColumns []string = []string{"ID", "Name", "Description"}
)

func NewDashboardsListAction() action.Pair {
	return scaffoldlist.NewListAction(short, long, defaultColumns,
		types.Dashboard{}, list, flags)
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Bool(ft.Name.ListAll, false, ft.Usage.ListAll("dashboards"))

	return addtlFlags
}

func list(c *grav.Client, fs *pflag.FlagSet) ([]types.Dashboard, error) {
	if all, err := fs.GetBool(ft.Name.ListAll); err != nil {
		clilog.LogFlagFailedGet(ft.Name.ListAll, err)
	} else if all {
		return c.GetAllDashboards()
	}
	return c.GetUserDashboards(connection.MyInfo.UID)
}
