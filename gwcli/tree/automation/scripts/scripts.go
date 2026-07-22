/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package scripts manages saved scripts.
package scripts

import (
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
)

func NewNav() *cobra.Command {
	return treeutils.GenerateNav("scripts", "manage automation scripts",
		"Scripting is used in two ways within Gravwell: as part of a search pipeline, and as a method to automate search launching. "+
			" See: https://docs.gravwell.io/scripting/scripting.html",
		nil,
		nil,
		treeutils.NodeOptions{
			CommandAliases: []string{"script", "anko"},
		})
}
