// Package self is a limited version of the users nav that is available to all users to gather information about their own accounts.
package self

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
			admin(),
			MyInfo(),
			sessions(),
			groups(),
		})
}

//#region admin mode

func admin() action.Pair {
	const (
		use   string = "admin"
		short string = "display or modify your admin status"
		long  string = "If called bare, admin displays whether or not you are an admin (and thus can enter admin mode).\n" +
			"Use -t to toggle your admin status, which will attach admin=true to future queries.\n" +
			"Exercise caution in admin mode, as it gives access to objects belonging to other users and makes it easy to break things.\n" +
			"Admin mode does not persist between sessions."
	)
	return scaffold.NewBasicAction(use, short, long,
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			isAdministrator, err := connection.Client.IsAdmin()
			if err != nil {
				return "failed to fetch administrator status: " + err.Error(), nil
			}

			// branch on toggle flag
			if t, err := fs.GetBool("toggle"); err != nil {
				clilog.GetFlag(err)
			} else if t {
				return toggle(isAdministrator)
			}

			// display state
			inAdminMode := connection.Client.AdminMode()
			if isAdministrator {
				var not string
				if !inAdminMode {
					not = " not"
				}
				return "You are an administrator.\n" + "You are in" + not + " admin mode.", nil
			}
			var s = "You are not an administrator."
			if inAdminMode {
				s += "\nYet, you are somehow in admin mode.\nYour admin mode flag will be ignored. Please file a bug report."
			}
			return s, nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.BoolP("toggle", "t", false, "toggle your admin status")
					return fs
				},
			},
		})
}

func toggle(isAdministrator bool) (string, tea.Cmd) {
	if !isAdministrator {
		return "Only administrators can toggle admin mode", nil
	}
	if !connection.Client.AdminMode() {
		connection.Client.SetAdminMode()
		return "You are now in admin mode", nil
	}
	connection.Client.ClearAdminMode()
	return "You are no longer in admin mode", nil

}

//#endregion admin mode

// wrapper for types.Session to limit the information sessions returns.
type session struct {
	ID          uint64 `json:",omitempty"`
	UID         int32  `json:",omitempty"`
	Origin      net.IP
	LastHit     string // timestamp
	TempSession bool
	Synced      bool
}

var timeformats = []string{time.RFC3339, time.DateOnly}
var since time.Time // set in Validate
var defaultSinceDuration = 48 * time.Hour

// sessions returns all of the current users current sessions
func sessions() action.Pair {
	return scaffoldlist.NewListAction("display your active sessions",
		"Displays information about how and where you are currently logged in.\n"+
			"If --since is not set, it will default to fetching all records for the past 48 hours.", session{},
		func(fs *pflag.FlagSet) ([]session, error) {
			rawSessions, err := connection.Client.MySessions()
			if err != nil {
				return nil, err
			}
			// sort by recency, note the inversion
			slices.SortFunc(rawSessions, func(a, b types.Session) int {
				return -a.LastHit.Compare(b.LastHit)
			})

			// apply filters, if applicable
			if !since.IsZero() {
				// cut off all records before since
				i := slices.IndexFunc(rawSessions, func(s types.Session) bool {
					return s.LastHit.Before(since)
				})
				if i != -1 {
					rawSessions = rawSessions[:i]
				}
			}

			// wrap and return the session
			var ss = make([]session, len(rawSessions))
			for i, rs := range rawSessions {
				ss[i] = session{
					ID:          rs.ID,
					UID:         rs.UID,
					Origin:      rs.Origin,
					LastHit:     rs.LastHit.Local().Format(time.RFC3339),
					TempSession: rs.TempSession,
					Synced:      rs.Synced,
				}
			}

			return ss, nil
		},
		map[string]string{"ID": "SessionID"},
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "sessions",
				Aliases: []string{"session"},
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.String("since",
						"",
						"filter to records after a given time. Assumes local time if a timezone is not specified.\n"+
							"Accepts the following timestamp formats:\n- "+strings.Join(timeformats, "\n- "))
					return fs
				},
			},
			DefaultColumns: []string{"ID", "Origin", "LastHit"},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
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
				return "", nil
			},
		})
}

func groups() action.Pair {
	return scaffoldlist.NewListAction("display your group memberships", "Display groups you are a part of.", types.Group{},
		func(fs *pflag.FlagSet) ([]types.Group, error) {
			return connection.Client.Groups()
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{Use: "groups"},
			Pretty: func(_ []string, _ map[string]string) (string, error) {
				groups, err := connection.Client.Groups()
				if err != nil {
					return "", err
				} else if len(groups) < 1 {
					return "you are not a part of any groups", nil
				}
				var sb strings.Builder
				for _, grp := range groups {
					fmt.Fprintf(&sb, "%s (ID: %d)\n", grp.Name, grp.ID)
				}
				return sb.String()[:sb.Len()-1], nil
			},
		})
}

func changePassword() action.Pair {
	return scaffold.NewBasicAction("change-password", "change your password", "Change the password for your account.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			currentPass, err := fs.GetString("current-password")
			if err != nil {
				clilog.GetFlag(err)
				return "failed to get current-password flag", nil
			}
			newPass, err := fs.GetString("new-password")
			if err != nil {
				clilog.GetFlag(err)
				return "failed to get new-password flag", nil
			}
			uid := connection.CurrentUser().ID
			if err := connection.Client.UserChangePass(uid, currentPass, newPass); err != nil {
				return err.Error(), nil
			}
			return "password changed successfully", nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.String("current-password", "", "your current password")
					fs.String("new-password", "", "your new password")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if currentPass, err := fs.GetString("current-password"); err != nil {
					clilog.GetFlag(err)
				} else if currentPass == "" {
					return "--current-password must be non-empty", nil
				}
				if newPass, err := fs.GetString("new-password"); err != nil {
					clilog.GetFlag(err)
				} else if newPass == "" {
					return "--new-password must be non-empty", nil
				}
				return "", nil
			},
		})
}

func searchGroup() action.Pair {
	return scaffold.NewBasicAction("search-group", "get or set default search groups",
		"Display or update the default search groups for your account.\nUse --set to provide a comma-separated list of group IDs, or --set=none to clear all default groups.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			uid := connection.CurrentUser().ID
			if fs.Changed("set") {
				setVal, err := fs.GetString("set")
				if err != nil {
					clilog.GetFlag(err)
					return "failed to get set flag", nil
				}
				setVal = strings.TrimSpace(setVal)
				if setVal == "" || setVal == "none" {
					if err := connection.Client.DeleteDefaultSearchGroups(uid); err != nil {
						return err.Error(), nil
					}
					return "search groups cleared", nil
				}
				var gids []int32
				for _, s := range strings.Split(setVal, ",") {
					s = strings.TrimSpace(s)
					if s == "" {
						continue
					}
					gid, err := strconv.ParseInt(s, 10, 32)
					if err != nil {
						return s + " is not a valid group ID", nil
					}
					gids = append(gids, int32(gid))
				}
				if err := connection.Client.SetDefaultSearchGroups(uid, gids); err != nil {
					return err.Error(), nil
				}
				return "search groups updated", nil
			}
			gids, err := connection.Client.GetDefaultSearchGroups(uid)
			if err != nil {
				return err.Error(), nil
			}
			return fmt.Sprintf("default search groups: %v", gids), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.String("set", "", "comma-separated list of group IDs to set as default")
					return fs
				},
			},
		})
}

func updateUser() action.Pair {
	return scaffold.NewBasicAction("update", "update your user information",
		"Update your user account information such as name and email address.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			user := connection.CurrentUser()
			changed := false
			if fs.Changed("name") {
				name, err := fs.GetString("name")
				if err != nil {
					return err.Error(), nil
				}
				user.Name = name
				changed = true
			}
			if fs.Changed("email") {
				email, err := fs.GetString("email")
				if err != nil {
					return err.Error(), nil
				}
				user.Email = email
				changed = true
			}
			if !changed {
				return "no changes specified; use --name or --email to update fields", nil
			}
			if err := connection.Client.UpdateUser(user); err != nil {
				return err.Error(), nil
			}
			return fmt.Sprintf("successfully updated user '%s'", user.Username), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.String("name", "", "new display name")
					fs.String("email", "", "new email address")
					return fs
				},
			},
		})
}
