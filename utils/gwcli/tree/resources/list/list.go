package list

import (
	"gwcli/action"
	"gwcli/clilog"
	ft "gwcli/stylesheet/flagtext"
	"gwcli/utilities/scaffold/scaffoldlist"

	grav "github.com/gravwell/gravwell/v3/client"

	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/spf13/pflag"
)

const (
	short string = "list resources on the system"
	long  string = "view resources avaialble to your user and the system"
)

var (
	defaultColumns []string = []string{"ID", "UID", "Name", "Description"}
)

func NewResourcesListAction() action.Pair {
	return scaffoldlist.NewListAction(short, long, defaultColumns,
		types.ResourceMetadata{}, list, flags)
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Bool(ft.Name.ListAll, false, ft.Usage.ListAll("resources"))
	return addtlFlags
}

func list(c *grav.Client, fs *pflag.FlagSet) ([]types.ResourceMetadata, error) {
	if all, err := fs.GetBool(ft.Name.ListAll); err != nil {
		clilog.LogFlagFailedGet(ft.Name.ListAll, err)
	} else if all {
		return c.GetAllResourceList()
	}

	return c.GetResourceList()
}
