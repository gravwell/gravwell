/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package cmdutils provides helper functions for working with cobra.Commands as they relate to gwcli.
package cmdutils

import (
	"slices"

	"github.com/spf13/cobra"
)

// DerivePath returns the path from root to the given command.
//
// !includeRoot omits "~" from the path
func DerivePath(cmd *cobra.Command, includeRoot bool) []string {
	if cmd == nil || cmd.Parent() == nil {
		if includeRoot {
			return []string{"~"}
		}
		return []string{}
	}
	pth := []string{cmd.Name()}

	// start from the command and work our way to root
	x := cmd
	for {
		x = x.Parent()
		if x.Parent() == nil { // we are at root
			if includeRoot {
				pth = append(pth, "~")
			}
			break
		}
		pth = append(pth, x.Name())

	}

	// reverse
	slices.Reverse(pth)

	return pth
}
