package user

import (
	"gwcli/action"
	"gwcli/tree/user/admin"
	"gwcli/tree/user/logout"
	"gwcli/tree/user/myinfo"
	"gwcli/tree/user/refreshmyinfo"
	"gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
)

const (
	use   string = "user"
	short string = "manage your user and profile"
	long  string = "View and edit properties of your current, logged in user."
)

var aliases []string = []string{"self"}

func NewUserNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases, nil,
		[]action.Pair{logout.NewUserLogoutAction(),
			admin.NewUserAdminAction(),
			myinfo.NewUserMyInfoAction(),
			refreshmyinfo.NewUserRefreshMyInfoAction()})
}
