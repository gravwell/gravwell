// Package admin_users provides actions related to users/accounts that require elevated permissions.
package admin_users

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	const (
		use   string = "users"
		short string = "manage users"
		long  string = "Perform user actions that require elevated privileges."
	)

	return treeutils.GenerateNav(use, short, long, nil, []*cobra.Command{},
		[]action.Pair{
			create(),
			delete(),
			edit(),
			lockAction(),
			unlockAction(),
			sessionsAction(),
		})
}

func create() action.Pair {
	return scaffoldcreate.NewCreateAction("user",
		map[string]scaffoldcreate.Field{
			"username": {
				Required: true,
				Title:    "Username",
				Usage:    "unique username to assign",
				Type:     scaffoldcreate.Text,
				Order:    200,
			},
			"name": {
				Required: true,
				Title:    "Name",
				Usage:    "actual name of the user",
				Type:     scaffoldcreate.Text,
				Order:    180,
			},
			"email": {
				Required: true,
				Title:    "Email",
				Usage:    "email associated to this user",
				Type:     scaffoldcreate.Text,
				Order:    160,
			},
			// TODO include admin bool
			"password": {
				Required: true,
				Title:    "Password",
				Usage:    "initial password for the user",
				Type:     scaffoldcreate.Text,
				Order:    140,
				CustomTIFuncInit: func() textinput.Model {
					ti := stylesheet.NewTI("", false)
					ti.EchoMode = textinput.EchoPassword
					return ti
				},
			},
		},
		func(cfg scaffoldcreate.Config, fieldValues map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error) {
			if err := connection.Client.AddUser(
				fieldValues["username"], fieldValues["password"],
				fieldValues["name"], fieldValues["email"],
				false, // TODO admin
			); err != nil {
				return 0, "", err
			}
			// verify the user can be found
			u, err := connection.Client.LookupUser(fieldValues["username"])
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

var sessionUIDs []int32

func sessionsAction() action.Pair {
	return scaffoldlist.NewListAction(
		"display a user's sessions",
		"Get all active sessions for the specified user IDs.",
		types.Session{}, func(fs *pflag.FlagSet) ([]types.Session, error) {
			toRet := []types.Session{}
			for _, uid := range sessionUIDs {
				sessions, err := connection.Client.Sessions(uid)
				if err != nil {
					clilog.Writer.Error("failed to get sessions", log.KV("uid", uid), log.KV("error", err))
					continue
				}
				toRet = append(toRet, sessions...)
			}
			return toRet, nil
		},
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "sessions",
				Usage:   fmt.Sprintf("sessions %s %s %s ...", ft.Optional("FLAGS"), ft.Mandatory("UserID1"), ft.Optional("UserID2")),
				Example: "sessions 1 8",
			},
			DefaultColumns: []string{"ID", "UID", "UDets.Username", "UDets.Admin", "UDets.Locked"},
			ColumnAliases:  map[string]string{"ID": "SessionID"},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				sessionUIDs = []int32{} // clear out IDs
				if fs.NArg() < 1 {
					return phrases.AtLeast1ArgRequired("user IDs"), nil
				}
				users, err := connection.Client.GetUserMap()
				if err != nil {
					return "", err
				}
				for _, arg := range fs.Args() {
					arg = strings.TrimSpace(arg)
					if arg == "" {
						continue
					}
					// validate that uid is an integer
					uid, err := strconv.ParseInt(arg, 10, 32)
					if err != nil {
						return arg + " is not a valid integer", nil
					} else if uid < 0 {
						return "uids must be positive (" + arg + ")", nil
					}
					// validate that each uid points to an actual user
					if _, ok := users[int32(uid)]; !ok {
						return "uid " + arg + " does not point to a valid user", nil
					}
					sessionUIDs = append(sessionUIDs, int32(uid))
				}
				return "", nil
			},
		},
	)
}
