// Package users contains actions for managing user accounts.
package users

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/crewjam/rfc5424"
	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	const (
		use   string = "users"
		short string = "manage users"
		long  string = "View and edit properties of users in the system."
	)

	return treeutils.GenerateNav(use, short, long, nil, []*cobra.Command{},
		[]action.Pair{list(), get()})
}

func list() action.Pair {
	return scaffoldlist.NewListAction("list users", "Retrieves cursory information about every user in the system", types.User{},
		func(fs *pflag.FlagSet) ([]types.User, error) {
			return connection.Client.GetAllUsers()
		}, scaffoldlist.Options{DefaultColumns: []string{"ID", "Username", "Name", "Email", "Admin"}})
}

// local wrapper struct to contain all data related to a single user
type userDetails struct {
	types.User
	Admin struct { // details that will only be retrieved if the calling user is an admin
		Capabilities []string
	}
}

// get returns all information about a specified user or users.
func get() action.Pair {
	return scaffoldlist.NewListAction("get user details",
		"Retrieves details about specified users, based on list of usernames provided as arguments.\n"+
			"Fetches additional information if you are an admin.",
		userDetails{},
		func(fs *pflag.FlagSet) ([]userDetails, error) {
			var users = []userDetails{}
			// map each username to get its id
			for _, username := range fs.Args() {
				userInfo, err := connection.Client.LookupUser(username)
				if err != nil {
					if !errors.Is(err, grav.ErrNotFound) {
						return nil, err
					}
					clilog.Writer.Infof("failed to find a user with username %v", username)
					continue
				}
				item := userDetails{User: userInfo}
				if connection.CurrentUser().Admin {
					//item.Admin = struct{ types.CapabilityState }{}
					if caps, err := connection.Client.GetUserCapabilities(userInfo.ID); err != nil {
						clilog.Writer.Warn("failed to fetch user capabilities",
							rfc5424.SDParam{Name: "user id", Value: strconv.FormatInt(int64(userInfo.ID), 10)},
							rfc5424.SDParam{Name: "error", Value: err.Error()},
						)
					} else {
						item.Admin.Capabilities = caps.Grants
					}
				}
				users = append(users, item)

			}

			/*connection.Client.GetUserGroups()

			// try to fetch admin-only info
			if connection.CurrentUser().Admin {

			}
			connection.Client.GetUserCapabilities()
			*/
			return users, nil
		}, scaffoldlist.Options{
			Use: "get",
			CmdMods: func(c *cobra.Command) {
				c.SetUsageFunc(func(c *cobra.Command) error {
					fmt.Fprint(c.OutOrStdout(), "get "+ft.Optional("flags")+" USERNAME USERNAME ...")
					return nil
				})
				c.Example = "get --csv bart homer lisa maggie marge"
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if len(fs.Args()) < 1 {
					return "you must provide at least one username", nil
				}
				return "", nil
			},
		})
}
