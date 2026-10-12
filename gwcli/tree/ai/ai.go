/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package ai provides the nav that groups the AI assets: agents and the
// skills they can load.
package ai

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/tree/ai/agents"
	"github.com/gravwell/gravwell/v4/gwcli/tree/ai/skills"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
)

// NewNav returns the ai nav, which holds the agents and skills navs.
func NewNav() *cobra.Command {
	return treeutils.GenerateNav("ai", "manage AI agents and skills",
		"Agents are node graphs that drive the Gravwell AI; skills are markdown documents an agent can load on demand.\n"+
			"Both are defined in files (gaftool writes agent definitions) and uploaded with the actions beneath this nav.",
		[]*cobra.Command{
			agents.NewNav(),
			skills.NewNav(),
		},
		[]action.Pair{})
}
