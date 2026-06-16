// Package self is a limited version of the users nav that is available to all users to gather information about their own accounts.
package self

import (
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/tree/admin"
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

func NewNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases, nil,
		[]action.Pair{
			MyInfo(),
			sessions(),
			groups(),
			changePassword(),
			searchGroup(),
			update(),
			logoutAll(),
			admin.Status("admin"),
		})
}

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
		func(fs *pflag.FlagSet, _ scaffoldlist.DataParameters) ([]session, error) {
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
			Omit: scaffold.OmitFlags{Everything: true},
		})
}

func groups() action.Pair {
	return scaffoldlist.NewListAction("display your group memberships", "Display groups you are a part of.", types.Group{},
		func(fs *pflag.FlagSet, _ scaffoldlist.DataParameters) ([]types.Group, error) {
			return connection.Client.Groups()
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{Use: "groups"},
			Pretty: func(_ *pflag.FlagSet, _ []string, _ map[string]string, _ scaffoldlist.DataParameters) (string, error) {
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
			Omit: scaffold.OmitFlags{Everything: true},
		})
}

func logoutAll() action.Pair {
	return scaffold.NewBasicAction("logout-all", "logout all sessions", "Terminate all active sessions for your user.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			if err := connection.Client.LogoutAll(); err != nil {
				return err.Error(), nil
			}
			connection.End()
			return "Successfully logged out all sessions", tea.Quit
		}, scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Usage:   "logout-all",
				Aliases: []string{"boot"},
			},
		})
}
