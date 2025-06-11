/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package group enforces consistent IDs and Titles by centralizing them.
//
// It was born out of avoiding import cycles.
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
