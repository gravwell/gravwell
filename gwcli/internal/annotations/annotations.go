/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package annotations

import "github.com/spf13/cobra"

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
