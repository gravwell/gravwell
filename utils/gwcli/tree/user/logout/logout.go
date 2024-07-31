// A simple logout action that logs out the current user and ends the program
package logout

import (
	"gwcli/action"
	"gwcli/connection"
	"gwcli/utilities/scaffold"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	use   string = "logout"
	short string = "logout and end the session"
	long  string = "Ends your current session and invalids your login token, forcing the next" +
		" login to request credentials."
)

var aliases []string = []string{}

func NewUserLogoutAction() action.Pair {
	return scaffold.NewBasicAction(use, short, long, aliases,
		func(*cobra.Command, *pflag.FlagSet) (string, tea.Cmd) {
			connection.Client.Logout()
			connection.End()

			return "Successfully logged out", tea.Quit
		}, nil)
}
