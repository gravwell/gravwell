package users

import (
	"fmt"
	"strconv"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
)

// This file implements user account locking

func lock() action.Pair {
	cmd := treeutils.GenerateAction("lock", "lock a user account",
		"Locks a user account.\n"+
			"The user will be unable to log in until unlocked, and all existing sessions will be terminated.",
		nil,
		func(c *cobra.Command, args []string) {
			// if an ID was specified, try to lock that user
			switch c.Flags().NArg() {
			case 0: // fail out or spawn mother
				ni, err := c.Flags().GetBool(ft.NoInteractive.Name())
				if err != nil {
					clilog.LogFlagFailedGet(ft.NoInteractive.Name(), err)
					ni = true // better we assume no-interactive
				}
				if ni {
					if err := mother.Spawn(c.Root(), c, args); err != nil {
						clilog.Tee(clilog.CRITICAL, c.ErrOrStderr(),
							"failed to spawn a mother instance: "+err.Error()+"\n")
					}
					return
				}
				fmt.Fprintln(c.ErrOrStderr(), "you must specify a single user ID to lock")
				return
			case 1: // attempt to lock this user
				uid, err := strconv.ParseInt(c.Flags().Arg(0), 10, 32)
				if err != nil {
					clilog.Tee(clilog.INFO, c.ErrOrStderr(), "\""+c.Flags().Arg(0)+"\" is not a valid integer")
					return
				}
				if err := connection.Client.LockUserAccount(int32(uid)); err != nil {
					clilog.Tee(clilog.INFO, c.ErrOrStderr(), fmt.Sprintf("failed to lock user account %d: %v"))
					return
				}
			case 2:
				fmt.Fprintln(c.ErrOrStderr(), "you must specify exactly one user ID to lock")
				return
			}

		}, treeutils.GenerateActionOptions{
			Usage:   "<uid>",
			Example: "7",
		})

	return action.NewPair(cmd)
}
