// A simple action to tell the user whether or not they are logged in as an admin.
package admin

import (
	"fmt"
	"gwcli/action"
	"gwcli/connection"
	"gwcli/utilities/scaffold"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	use   string = "admin"
	short string = "prints your admin status"
	long  string = "Displays whether or not your current user has admin permissions."
)

var aliases []string = []string{}

func NewUserAdminAction() action.Pair {
	p := scaffold.NewBasicAction(use, short, long, aliases,
		func(*cobra.Command, *pflag.FlagSet) (string, tea.Cmd) {
			var not string
			// todo what is the difference re: MyAdminStatus?
			if !connection.Client.AdminMode() {
				not = " not"
			}
			return fmt.Sprintf("You are%v in admin mode", not), nil
		}, nil)
	return p
}
