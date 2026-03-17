// Package self is a limited version of the users nav that is available to all users to gather information about their own accounts.
package self

import (
	"net"
	"slices"
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
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
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
				clilog.LogFlagFailedGet("toggle", err)
				return uniques.ErrGeneric.Error(), nil
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
			AddtlFlagFunc: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.BoolP("toggle", "t", false, "toggle your admin status")
				return fs
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
		scaffoldlist.Options{
			Use: "sessions",
			AddtlFlags: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.String("since",
					"",
					"filter to records after a given time. Assumes local time if a timezone is not specified.\n"+
						"Accepts the following timestamp format:\n- "+strings.Join(timeformats, "\n- "))
				return fs
			},
			DefaultColumns: []string{"ID", "Origin", "LastHit"},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				since = time.Time{} // ensure it is reset
				snc, err := fs.GetString("since")
				if err != nil {
					clilog.LogFlagFailedGet("since", err)
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
						return "failed to parse " + snc + " as an acceptible time format", nil
					}
				}
				return "", nil
			},
		})
}
