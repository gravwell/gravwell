// Package users provides actions related to users/accounts that require elevated permissions.
package users

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/annotations"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/tree/users/self"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	return treeutils.GenerateNav("users", "manage users", "Manage users, including yourself.",
		[]*cobra.Command{
			self.NewNav(),
		},
		[]action.Pair{
			listAction(),
			create(),
			delete(),
			edit(),
			lock(),
			unlock(),
			sessionsAction(),
			changePassword(),
			toggleAdmin(),
		})
}

func listAction() action.Pair {
	return scaffoldlist.NewListAction("list users", "Retrieves cursory information about every user in the system", types.User{},
		func(fs *pflag.FlagSet, param scaffoldlist.DataParameters) ([]types.User, error) {
			resp, err := connection.Client.ListUsers(param.QueryOpts)
			return resp.Results, err
		}, nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ListUsers},
					XPermissions: []types.Capability{types.ListUsers},
				}},
			DefaultColumns: []string{"ID", "Username", "Name", "Email", "Admin"},
		},
	)
}

func create() action.Pair {
	return scaffoldcreate.NewCreateAction("user",
		map[string]scaffoldcreate.Field{
			"username": {
				Required: true,
				Title:    "Username",
				Flag: scaffoldcreate.FlagConfig{
					Name:  "new-username",
					Usage: "unique username to assign",
				},
				Provider: &scaffoldcreate.TextProvider{},
				Order:    200,
			},
			"name": {
				Required: true,
				Title:    "Name",
				Flag: scaffoldcreate.FlagConfig{
					Name:  "new-name",
					Usage: "actual name of the user",
				},
				Provider: &scaffoldcreate.TextProvider{},
				Order:    180,
			},
			"email": {
				Required: true,
				Title:    "Email",
				Flag: scaffoldcreate.FlagConfig{
					Name:  "new-email",
					Usage: "email associated to this user",
				},
				Provider: &scaffoldcreate.TextProvider{},
				Order:    160,
			},
			"password": {
				Required: true,
				Title:    "Password",
				Flag: scaffoldcreate.FlagConfig{
					Name:  "new-password",
					Usage: "initial password for the user",
				},
				Provider: &scaffoldcreate.TextProvider{
					CustomInit: func() textinput.Model {
						ti := stylesheet.NewTI("", false)
						ti.EchoMode = textinput.EchoPassword
						return ti
					},
				},
				Order: 140,
			},
			"admin": {
				Required: false,
				Title:    "admin",
				Flag: scaffoldcreate.FlagConfig{
					Name:  "is-admin",
					Usage: "mark the new user as an admin",
				},
				Provider: &scaffoldcreate.BoolProvider{},
				Order:    120,
			},
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			admin, err := strconv.ParseBool(fields["admin"].Provider.Get())
			if err != nil {
				clilog.Writer.Error("failed to parse bool provider", log.KVErr(err))
				return 0, "", clilog.ErrInternal{}
			}
			if _, err := connection.Client.CreateUser(
				types.AddUser{Username: fields["username"].Provider.Get(), Password: fields["password"].Provider.Get(),
					Name: fields["name"].Provider.Get(), Email: fields["email"].Provider.Get(),
					Admin: admin},
			); err != nil {
				return 0, "", err
			}
			// verify the user can be found
			u, err := connection.Client.LookupUser(fields["username"].Provider.Get())
			if err != nil {
				return 0, "", fmt.Errorf("failed to find user after creation: %w\nThe user may or may not exist", err)
			}
			return u.ID, "", nil
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{Requirements: annotations.Requirements{UserIsAdmin: true}},
		})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("user",
		func(dryrun bool, id int32, _ *pflag.FlagSet) error {
			if dryrun {
				_, err := connection.Client.GetUser(id)
				return err
			}
			return connection.Client.DeleteUser(id)
		},
		func(param scaffolddelete.DataParameters) ([]multiselectlist.SelectableItem[int32], error) {
			lr, err := connection.Client.ListUsers(param.QueryOpts)
			if err != nil {
				return nil, err
			}
			var items = make([]multiselectlist.SelectableItem[int32], len(lr.Results))
			for i, u := range lr.Results {
				items[i] = listitem.NewUserItem(u, false)
			}

			return items, nil
		},
		scaffolddelete.Options{
			CommonOptions:     scaffold.CommonOptions{Requirements: annotations.Requirements{UserIsAdmin: true}},
			QueryOptionsFlags: scaffold.QOInclude{Everything: true}})
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
				userCBAC, err := connection.Client.GetUser(id)
				if err != nil {
					return types.User{}, err
				}
				return userCBAC.User, nil
			},
			FetchSub: func() (items []types.User, err error) {
				resp, err := connection.Client.ListUsers(nil)
				return resp.Results, err
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
				_, err = connection.Client.UpdateUser(data.ID, data.ToPatch())
				return data.Name, err
			},
		},
		scaffoldedit.Options{
			CommonOptions: scaffold.CommonOptions{
				// edits arbitrary users by ID; this is an admin-only operation
				Requirements: annotations.Requirements{UserIsAdmin: true},
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

//#region sessions

// wrapper for types.Session to limit the information sessions returns.
type session struct {
	SessionID   uint64 `json:",omitempty"`
	UID         int32  `json:",omitempty"`
	Origin      net.IP
	LastHit     string // timestamp
	TempSession bool
	Synced      bool
}

var timeformats = []string{time.RFC3339, time.DateOnly}
var since time.Time // set in Validate
var defaultSinceDuration = 48 * time.Hour

var sessionUIDs []int32

func sessionsAction() action.Pair {
	return scaffoldlist.NewListAction(
		"display a user's sessions",
		"Get all active sessions for the specified user IDs.\n"+
			"If --since is not set, it will default to fetching all records for the past 48 hours.",
		session{},
		func(fs *pflag.FlagSet, _ scaffoldlist.DataParameters) ([]session, error) {
			allSessions := []types.Session{}
			for _, uid := range sessionUIDs {
				userSessions, err := connection.Client.Sessions(uid)
				if err != nil {
					clilog.Writer.Error("failed to get sessions", log.KV("uid", uid), log.KV("error", err))
					continue
				}
				// sort by recency, note the inversion
				slices.SortFunc(userSessions, func(a, b types.Session) int {
					return -a.LastHit.Compare(b.LastHit)
				})
				// apply since filter, if applicable
				if !since.IsZero() {
					// cut off all records before since
					i := slices.IndexFunc(userSessions, func(s types.Session) bool {
						return s.LastHit.Before(since)
					})
					if i != -1 {
						userSessions = userSessions[:i]
					}
				}
				allSessions = append(allSessions, userSessions...)
			}
			// now sort all sessions by time
			slices.SortFunc(allSessions, func(a, b types.Session) int {
				return -a.LastHit.Compare(b.LastHit)
			})
			// wrap all sessions into our limited format
			var ss = make([]session, len(allSessions))
			for i, rs := range allSessions {
				ss[i] = session{
					SessionID:   rs.ID,
					UID:         rs.UID,
					Origin:      rs.Origin,
					LastHit:     rs.LastHit.Local().Format(time.RFC3339),
					TempSession: rs.TempSession,
					Synced:      rs.Synced,
				}
			}

			return ss, nil
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "sessions",
				Usage:   "sessions " + ft.Optional("Flags") + " " + ft.VariadicArgs("UID", true),
				Example: "sessions 1 8",
				Aliases: []string{"session"},
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.String("since",
						"",
						"filter to records after a given time. Assumes local time if a timezone is not specified.\n"+
							"Accepts the following timestamp formats:\n- "+strings.Join(timeformats, "\n- "))
					return fs
				},
				// views sessions for arbitrary users by ID; this is an admin-only operation
				Requirements: annotations.Requirements{UserIsAdmin: true},
			},
			DefaultColumns: []string{"SessionID", "Origin", "LastHit"},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				// check for since override and set default if not
				since = time.Time{} // ensure it is reset
				snc, err := fs.GetString("since")
				if err != nil {
					clilog.GetFlag(err)
				}
				if snc != "" {
					// try to parse in our supported formats, breaking on the first one
					for _, format := range timeformats {
						t, err := time.ParseInLocation(format, snc, time.Local)
						if err == nil {
							since = t
							break
						}
					}
					if since.IsZero() {
						return "failed to parse " + snc + " as an acceptable time format", nil
					}
				} else {
					since = time.Now().Add(-defaultSinceDuration)
				}

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
			QueryOptionsFlags: scaffold.QOOmit{Everything: true},
		},
	)
}

func lock() action.Pair {
	return scaffoldselect.NewSelectAction(
		"lock user accounts", "Lock one or several user accounts.\n"+
			"The user will be unable to log in until unlocked, and all existing sessions will be terminated.",
		"account",
		func(_ *pflag.FlagSet) ([]multiselectlist.SelectableItem[int32], error) {
			ulr, err := connection.Client.ListUsers(nil)
			if err != nil {
				return nil, err
			}
			items := make([]multiselectlist.SelectableItem[int32], 0, len(ulr.Results))
			for _, user := range ulr.Results {
				if user.ID == connection.CurrentUser().ID || user.Locked {
					continue
				}
				items = append(items, listitem.NewUserItem(user, false))
			}
			items = slices.Clip(items)
			return items, nil
		},
		func(IDs []int32, _ *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(IDs))
			for i, id := range IDs {
				if err := connection.Client.LockUserAccount(id); err != nil {
					results[i] = scaffold.Result{Success: false, Output: fmt.Sprintf("failed to lock user account %d: %v", id, err)}
				} else {
					results[i] = scaffold.Result{Success: true, Output: fmt.Sprintf("User %d locked", id)}
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:          "lock",
				Requirements: annotations.Requirements{UserIsAdmin: true},
			},
			NoItemsError: func(fs *pflag.FlagSet) string { return "There are no unlocked accounts you can lock." },
		})
}

func unlock() action.Pair {
	return scaffoldselect.NewSelectAction("unlock user accounts", "Unlock one or several user accounts.", "account",
		func(_ *pflag.FlagSet) ([]multiselectlist.SelectableItem[int32], error) {
			ulr, err := connection.Client.ListUsers(nil)
			if err != nil {
				return nil, err
			}
			items := make([]multiselectlist.SelectableItem[int32], 0, len(ulr.Results))
			for _, user := range ulr.Results {
				if user.ID == connection.CurrentUser().ID || !user.Locked {
					continue
				}
				items = append(items, listitem.NewUserItem(user, false))
			}
			items = slices.Clip(items)
			return items, nil
		},
		func(IDs []int32, _ *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				if err := connection.Client.UnlockUserAccount(ID); err != nil {
					results[i] = scaffold.Result{Success: false, Output: fmt.Sprintf("failed to unlock user account %d: %v", ID, err)}
				} else {
					results[i] = scaffold.Result{Success: true, Output: fmt.Sprintf("User %d unlocked", ID)}
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:          "unlock",
				Requirements: annotations.Requirements{UserIsAdmin: true},
			},
			NoItemsError: func(fs *pflag.FlagSet) string { return "There are no locked accounts you can unlock." },
		})
}
