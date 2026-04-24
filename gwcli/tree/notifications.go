package tree

import (
	"maps"
	"slices"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/spf13/pflag"
)

func notifications() action.Pair {
	return scaffoldlist.NewListAction("view your notifications", "review your notifications, new or previously seen", types.Notification{},
		func(fs *pflag.FlagSet) ([]types.Notification, error) {
			seen, err := fs.GetBool("seen")
			if err != nil {
				clilog.LogFlagFailedGet("seen", err)
			}
			var notifs types.NotificationSet
			if !seen {
				notifs, err = connection.Client.MyNewNotifications()
			} else {
				notifs, err = connection.Client.MyNotifications()
			}
			if err != nil {
				return nil, err
			}
			return slices.Collect(maps.Values(notifs)), nil
		},
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "notifications",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("seen", false, "include notifications you've already seen")
					return fs
				},
			},

			DefaultColumns: []string{"Broadcast", "Msg"},
		},
	)
}
