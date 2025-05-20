package attach

import (
	"fmt"

	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/spf13/cobra"
)

/*
This file contains subroutines and data used by both interactive and non-interactive usage, typically to enforce consistency.
*/

// syntax composes and returns the syntax of how to use attach.
// Broken out into a subroutine (rather than a const) so the full path can be built dynamically.
func syntax(cmd *cobra.Command, scriptMode bool) string {
	path := cmd.CommandPath()

	var sid string
	if scriptMode {
		sid = ft.Mandatory("searchID")
	} else {
		sid = ft.Optional("searchID")
	}

	return fmt.Sprintf("Syntax: %s %s %s", path, ft.Optional("flags"), sid)
}
