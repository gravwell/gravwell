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

const (
	annotationAdmin = "admin_only"
)

// AdminOnly add the AdminOnly annotation to the given command.
// If the cmd's annotation map is nil, it will be allocated.
func AdminOnly(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}
	cmd.Annotations[annotationAdmin] = "1"
}

// IsAdminOnly returns if the cmd is marked as only accessible to admins
func IsAdminOnly(cmd *cobra.Command) bool {
	_, ok := cmd.Annotations[annotationAdmin]
	return ok
}

// DerivePath returns the path from root to the given command.
//
// includeSelf includes the given command in the path.
// If the command is root, it will always be included.
func DerivePath(cmd *cobra.Command, includeSelf bool) []string {
	if cmd.Parent() == nil {
		return []string{"~"}
	}
	pth := []string{}

	if includeSelf {
		pth = append(pth, cmd.Name())
	}
	// start from the command and work our way to root
	for {
		parent := cmd.Parent()
		if parent == nil {
			pth = append(pth, "~")
			break
		}
		pth = append(pth, parent.Name())

	}

	// reverse
	slices.Reverse(pth)

	return pth
}
