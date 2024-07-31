// This pkg enforces consistent IDs and Titles by centralizing them.
// It was born out of avoiding import cycles
package group

import "github.com/spf13/cobra"

type GroupID = string

const (
	ActionID GroupID = "action"
	NavID    GroupID = "nav"
)

func AddNavGroup(cmd *cobra.Command) {
	cmd.AddGroup(&cobra.Group{ID: NavID, Title: "Navigation"})
}
func AddActionGroup(cmd *cobra.Command) {
	cmd.AddGroup(&cobra.Group{ID: ActionID, Title: "Actions"})
}
