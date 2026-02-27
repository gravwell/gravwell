package self

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/tree/user/admin"
	"github.com/gravwell/gravwell/v4/gwcli/tree/user/logout"
	"github.com/gravwell/gravwell/v4/gwcli/tree/user/myinfo"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
)

const (
	use   string = "self"
	short string = "manage your user and profile"
	long  string = "View and edit properties of your current, logged in user."
)

var aliases []string = []string{"me"}

func NewSelfNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases, nil,
		[]action.Pair{logout.NewUserLogoutAction(),
			admin.NewUserAdminAction(),
			myinfo.NewUserMyInfoAction()})
}
