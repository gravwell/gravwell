/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package attach implements search re-attachment, for fetching backgrounded queries.
// It bears significant similarities to the load-bearing query action, but is different enough to not be folded in.
package attach

import (
	"github.com/gravwell/gravwell/v4/gwcli/action"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const helpDesc = "" // TODO

// NewAttachAction creates an attach action of the form `./gwcli ... attach 123456789`
func NewAttachAction() action.Pair {
	cmd := treeutils.GenerateAction(
		"attach",
		"re-attach to a backgrounded query",
		helpDesc,
		[]string{"reattach"},
		run)

	localFS := initialLocalFlagSet()
	cmd.Flags().AddFlagSet(&localFS)
	cmd.Example = "gwcli queries attach 123456789"

	// TODO attach to the queries nav
	return action.NewPair(cmd, Attach)

}

// Generates the flagset used by attach.
func initialLocalFlagSet() pflag.FlagSet {
	fs := pflag.FlagSet{}

	fs.StringP(ft.Name.Output, "o", "", ft.Usage.Output)
	fs.Bool(ft.Name.Append, false, ft.Name.Append)
	fs.Bool(ft.Name.JSON, false, ft.Usage.JSON)
	fs.Bool(ft.Name.CSV, false, ft.Usage.CSV)

	fs.BoolP("background", "b", false, "run this search in the background, rather than awaiting and loading the results as soon as they are ready")

	// scheduled searches
	fs.StringP(ft.Name.Name, "n", "", "SCHEDULED."+ft.Usage.Name("scheduled search"))
	fs.StringP(ft.Name.Desc, "d", "", "SCHEDULED."+ft.Usage.Desc("scheduled search"))
	fs.StringP(ft.Name.Frequency, "f", "", "SCHEDULED."+ft.Usage.Frequency)

	return fs
}

func run(_ *cobra.Command, _ []string) {

}
