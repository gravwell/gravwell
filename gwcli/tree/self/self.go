// Package self is a limited version of the users nav that is available to all users to gather information about their own accounts.
package self

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/tree/users/self/admin"
	"github.com/gravwell/gravwell/v4/gwcli/tree/users/self/myinfo"
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
		[]action.Pair{
			admin.NewUserAdminAction(),
			myinfo.NewUserMyInfoAction(),
		})
}
