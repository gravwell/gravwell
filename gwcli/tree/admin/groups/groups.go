// Package groups introduces actions to managing groups.
//
// Only available to admins.
package groups

import (
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	const (
		use   string = "groups"
		short string = "manage groups"
		long  string = "View and edit groups"
	)

	return treeutils.GenerateNav(use, short, long, []string{"group"},
		nil,
		[]action.Pair{
			list(),
			create(),
		})
}

// lists all groups the current user is able to see
func list() action.Pair {
	return scaffoldlist.NewListAction("list groups", "Retrieves a list of groups available on the system",
		types.Group{},
		func(fs *pflag.FlagSet) ([]types.Group, error) {
			resp, err := connection.Client.ListGroups(nil)
			return resp.Results, err
		},
		scaffoldlist.Options{})
}

func create() action.Pair {
	return scaffoldcreate.NewCreateAction("group",
		scaffoldcreate.Config{
			"name": scaffoldcreate.FieldName("group"),
			"desc": scaffoldcreate.FieldDescription("group"),
		},

		func(fields scaffoldcreate.Config, fs *pflag.FlagSet) (id any, invalid string, err error) {
			result, err := connection.Client.CreateGroup(types.Group{
				Name:        fields["name"].Provider.Get(),
				Description: fields["desc"].Provider.Get(),
			})
			return result.Name, "", err
		}, scaffoldcreate.Options{})
}

// TODO this probably requires a custom action to ensure it is as usable as possible
/*func addUser() action.Pair {
	return scaffold.NewBasicAction("adduser", "add a user to a group", "Add a user to a group",
		func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
			var (
				uid, gid int32
				err      error
			)

			uid, err = fs.GetInt32("uid")
			if err != nil {
				clilog.LogFlagFailedGet("uid", err)
			}
			gid, err = fs.GetInt32("gid")
			if err != nil {
				clilog.LogFlagFailedGet("gid", err)
			}
			if err := connection.Client.AddUserToGroup(uid, gid); err != nil {
				return err.Error(), nil
			}
			return fmt.Sprintf("Successfully added user %d to group %d", uid, gid), nil
		},
		scaffold.BasicOptions{
			AddtlFlagFunc: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.Int32("uid", 0, "id of the user to add")
				fs.Int32("gid", 0, "id of the group to add the user to")
				return fs
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if !connection.CurrentUser().Admin {
					return "you must be an admin to use this function", nil
				}
				uid, err := fs.GetInt32("uid")
				if err != nil {
					clilog.LogFlagFailedGet("uid", err)
				}
				gid, err := fs.GetInt32("gid")
				if err != nil {
					clilog.LogFlagFailedGet("gid", err)
				}
				if gid == 0 || uid == 0 {
					return "you must specify both --uid and --gid", nil
				}
				return "", nil
			},
		})
}*/

// TODO get users in group (as `groups <username>`)

// TODO delete
