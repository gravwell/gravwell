package attach

import "github.com/spf13/cobra"

/*
This file contains subroutines and data used by both interactive and non-interactive usage, typically to enforce consistency.
*/

// syntax composes and returns the syntax of how to use attach.
// Broken out into a subroutine (rather than a const) so the full path can be built dynamically.
func syntax(cmd *cobra.Command, scriptMode bool) string {
	path := cmd.CommandPath()

	var sid string
	if scriptMode {
		sid = "searchID"
	} else {
		sid = "[searchID]"
	}

	return "Syntax: " + path + " [flags] " + sid // TODO add brackets for mandatory
}
