/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package automation controls alerts, flows, scheduled searches, and scripts.
package automation

import (
	"github.com/gravwell/gravwell/v4/gwcli/tree/automation/alerts"
	"github.com/gravwell/gravwell/v4/gwcli/tree/automation/flows"
	"github.com/gravwell/gravwell/v4/gwcli/tree/automation/scheduled"
	"github.com/gravwell/gravwell/v4/gwcli/tree/automation/scripts"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
)

func NewNav() *cobra.Command {
	return treeutils.GenerateNav("automation", "manage alerts, flows, scheduled searches, and scripts",
		"Gravwell provides several utilities to enable automated operations. See: https://docs.gravwell.io/automation.html",
		[]*cobra.Command{
			alerts.NewNav(),
			flows.NewNav(),
			scheduled.NewNav(),
			scripts.NewNav(),
		},
		nil,
		treeutils.NodeOptions{
			CommandAliases: []string{"automations"},
		})
}
