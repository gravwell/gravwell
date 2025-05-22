package attach

import (
	"fmt"

	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
)

/*
This file contains subroutines and data used by both interactive and non-interactive usage, typically to enforce consistency.
*/

// Returns an error string stating that the wrong number of arguments were given.
// Interactive mode expects 0 or 1, script mode expects exactly 1.
func errWrongArgCount(script bool) string {
	if script {
		return "attach takes exactly 1 argument in script mode.\n" + syntax(true)
	}
	return "attach takes 0 or 1 argument in interactive mode.\n" + syntax(false)
}

// syntax composes and returns the syntax of how to use attach.
// Broken out into a subroutine (rather than a const) so the full path can be built dynamically.
func syntax(script bool) string {
	var sid string
	if script {
		sid = ft.Mandatory("searchID")
	} else {
		sid = ft.Optional("searchID")
	}

	return fmt.Sprintf("Syntax: assert %s %s", ft.Optional("flags"), sid)
}
