// Package users contains actions for managing user accounts.
package users

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/crewjam/rfc5424"
	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
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
		[]action.Pair{
			list(),
			get(),
			create(),
			delete(),
			edit(),
		})
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

func create() action.Pair {
	return scaffoldcreate.NewCreateAction("user",
		map[string]scaffoldcreate.Field{
			"username": {
				Required: true,
				Title:    "Username",
				Flag:     scaffoldcreate.FlagConfig{Usage: "unique username to assign"},
				Provider: &scaffoldcreate.TextProvider{},
				Order:    200,
			},
			"name": {
				Required: true,
				Title:    "Name",
				Flag:     scaffoldcreate.FlagConfig{Usage: "actual name of the user"},
				Provider: &scaffoldcreate.TextProvider{},
				Order:    180,
			},
			"email": {
				Required: true,
				Title:    "Email",
				Flag:     scaffoldcreate.FlagConfig{Usage: "email associated to this user"},
				Provider: &scaffoldcreate.TextProvider{},
				Order:    160,
			},
			// TODO include admin bool
			"password": {
				Required: true,
				Title:    "Password",
				Flag:     scaffoldcreate.FlagConfig{Usage: "initial password for the user"},
				Provider: &scaffoldcreate.TextProvider{
					CustomInit: func() textinput.Model {
						ti := stylesheet.NewTI("", true)
						ti.EchoMode = textinput.EchoPassword
						return ti
					},
				},
				Order: 140,
			},
		},
		func(cfg scaffoldcreate.Config, fs *pflag.FlagSet) (id any, invalid string, err error) {
			if err := connection.Client.AddUser(
				cfg["username"].Provider.Get(), cfg["password"].Provider.Get(),
				cfg["name"].Provider.Get(), cfg["email"].Provider.Get(),
				false, // TODO admin
			); err != nil {
				return 0, "", err
			}
			// verify the user can be found
			u, err := connection.Client.LookupUser(cfg["username"].Provider.Get())
			if err != nil {
				return 0, "", fmt.Errorf("failed to find user after creation: %w\nThe user may or may not exist.", err)
			}
			return u.ID, "", nil
		},
		scaffoldcreate.Options{})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("user", "users",
		func(dryrun bool, id int32) error {
			if dryrun {
				_, err := connection.Client.GetUserInfo(id)
				return err
			}
			return connection.Client.DeleteUser(id)
		},
		func() ([]scaffolddelete.Item[int32], error) {
			users, err := connection.Client.GetAllUsers()
			if err != nil {
				return nil, err
			}
			var items = make([]scaffolddelete.Item[int32], len(users))
			for i, user := range users {

				items[i] = scaffolddelete.NewItem(user.Name, descriptionLine(user.Admin, user.Email), user.ID)
			}
			return items, nil
		})
}

func edit() action.Pair {
	return scaffoldedit.NewEditAction("user", "users",
		scaffoldedit.Config{
			"username": {
				Required: true,
				Title:    "Username",
				Usage:    "unique username to assign",
				Order:    200,
			},
			"name": {
				Required: true,
				Title:    "Name",
				Usage:    "actual name of the user",
				Order:    180,
			},
			"email": {
				Required: true,
				Title:    "Email",
				Usage:    "email associated to this user",
				Order:    160,
			},
			// TODO include admin bool
		},
		scaffoldedit.SubroutineSet[int32, types.User]{
			SelectSub: func(id int32) (item types.User, err error) {
				userCBAC, err := connection.Client.GetUserInfo(id)
				if err != nil {
					return types.User{}, err
				}
				return userCBAC.User, nil
			},
			FetchSub: func() (items []types.User, err error) {
				return connection.Client.GetAllUsers()
			},
			GetFieldSub: func(item types.User, fieldKey string) (value string, err error) {
				switch fieldKey {
				case "username":
					return item.Username, nil
				case "name":
					return item.Name, nil
				case "email":
					return item.Email, nil
				}
				return "", fmt.Errorf("unknown field key: %v", fieldKey)
			},
			SetFieldSub: func(item *types.User, fieldKey, val string) (invalid string, err error) {
				if item == nil {
					return "", errors.New("cannot set nil item")
				}
				switch fieldKey {
				case "username":
					item.Username = val
				case "name":
					item.Name = val
				case "email":
					item.Email = val
				default:
					return "", fmt.Errorf("unknown field key: %v", fieldKey)
				}
				return
			},
			GetTitleSub: func(item types.User) string {
				return item.Name
			},
			GetDescriptionSub: func(item types.User) string {
				return descriptionLine(item.Admin, item.Email)
			},
			UpdateSub: func(data *types.User) (identifier string, err error) {
				// transmute user -> user details
				ud := types.UserDetails{
					UID:   data.ID,
					User:  data.Username,
					Name:  data.Name,
					Email: data.Email,
					Admin: data.Admin,
				}

				return strconv.FormatInt(int64(data.ID), 10), connection.Client.UpdateUser(data.ID, ud)
			},
		},
	)
}

func descriptionLine(admin bool, email string) string {
	adminStr := ""
	if admin {
		adminStr = "(admin) "
	}
	return adminStr + email
}
