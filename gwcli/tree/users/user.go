package users

import (
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/tree/users/self"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewUsersNav() *cobra.Command {
	const (
		use   string = "users"
		short string = "manage users"
		long  string = "View and edit properties of users in the system."
	)

	return treeutils.GenerateNav(use, short, long, nil, []*cobra.Command{self.NewSelfNav()},
		[]action.Pair{list()})
}

func list() action.Pair {
	return scaffoldlist.NewListAction("list users", "Retrieves cursory information about every user in the system", types.User{},
		func(fs *pflag.FlagSet) ([]types.User, error) {
			return connection.Client.GetAllUsers()
		}, scaffoldlist.Options{DefaultColumns: []string{"ID", "Username", "Name", "Email", "Admin"}})
}
