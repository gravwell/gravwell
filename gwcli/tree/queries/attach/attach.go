/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package attach implements search re-attachment, for fetching backgrounded queries.
// It bears significant similarities to the load-bearing query action, but is different enough to not be folded in.
//
// See gwcli/assets/attach_flow.drawio.svg for a flowchart of user interaction.
package attach

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/querysupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	helpDesc string = "Attach to an existing query by search ID and display its results.\n" +
		"If the query is still running, attaching to it will block until it is complete.\n" +
		"\n" +
		"In interactive mode, a list of available, attach-able queries will be displayed.\n" +
		"\n" +
		"If --json or --csv is not given when outputting to a file (`-o`), the results will be " +
		"text (if able) or an archive binary blob (if unable), depending on the query's render " +
		"module.\n" +
		"gwcli will not dump binary to terminal; you must supply -o if the results are a binary " +
		"blob (aka: your query uses a chart-style renderer)."
)

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
	// add bare argument validator (require 1 arg if --script, 0 or 1 otherwise)
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		script, err := cmd.Flags().GetBool("script")
		if err != nil {
			panic(err)
		}
		if script && len(args) != 1 {
			return errors.New(errWrongArgCount(true))
		}
		if len(args) > 1 {
			return errors.New(errWrongArgCount(false))
		}
		return nil
	}
	cmd.Example = "gwcli queries attach 123456789"

	return action.NewPair(cmd, Attach)
}

// Generates the flagset used by attach.
// In interactive mode, most flags are passed directly into datascope.
// This must be a subset of querysupport.QueryFlags to take advantage of the existing code.
func initialLocalFlagSet() pflag.FlagSet {
	fs := pflag.FlagSet{}

	fs.StringP(ft.Name.Output, "o", "", ft.Usage.Output)
	fs.Bool(ft.Name.Append, false, ft.Usage.Append)
	fs.Bool(ft.Name.JSON, false, ft.Usage.JSON)
	fs.Bool(ft.Name.CSV, false, ft.Usage.CSV)

	return fs
}

// invoked from the commandline.
// Invokes Mother if !script.
func run(cmd *cobra.Command, args []string) {
	// fetch flags
	flags := querysupport.TransmogrifyFlags(cmd.Flags())

	// check arg count
	if len(args) > 1 || (flags.Script && len(args) == 0) {
		fmt.Fprint(cmd.ErrOrStderr(), errWrongArgCount(flags.Script)+"\n")
		return
	}
	// if a sid was given, attempt to fetch results
	if len(args) == 1 {
		sid := strings.TrimSpace(args[0])
		s, err := connection.Client.AttachSearch(sid)
		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
			return
		}

		querysupport.HandleFGCobraSearch(&s, flags, cmd.OutOrStdout(), cmd.ErrOrStderr())

		if err := s.Close(); err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
			return
		}

		return
	}

	// sid was not given, launch Mother into bare `attach` call
	if err := mother.Spawn(cmd.Root(), cmd, args); err != nil {
		clilog.Tee(clilog.CRITICAL, cmd.ErrOrStderr(),
			"failed to spawn a mother instance: "+err.Error()+"\n")
	}
}
