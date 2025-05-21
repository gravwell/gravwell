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
	"errors"
	"fmt"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/tree/query/datascope"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/querysupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	helpDesc string = "" // TODO
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
			return errors.New("attach requires exactly 1 argument in script mode.\n" + syntax(true))
		}
		if len(args) > 1 {
			return errors.New(errWrongInteractiveArgCount())
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

	// attempt to fetch the search id
	var sid string
	if len(args) == 1 {
		sid = strings.TrimSpace(args[0])
	}

	// split on --script
	if flags.Script {
		// arg validator should ensure sid is populated by this point

		// attaching to a search non-interactively just downloads the results and spits them out
		s, err := connection.Client.AttachSearch(sid)
		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
			return
		}
		defer s.Close()

		rc, format, err := querysupport.StreamSearchResults(&s, types.TimeRange{}, flags.CSV, flags.JSON)
		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
			return
		}
		if err := querysupport.WriteDownloadResults(rc, cmd.OutOrStdout(), flags.OutPath, flags.Append, format); err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), connection.DownloadQuerySuccessfulString(flags.OutPath, flags.Append, format))
		}

		return
	}

	// if a sid was given, attempt to go directly to datascope
	if sid != "" {
		// slurp the results
		search, err := connection.Client.AttachSearch(sid)
		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
			return
		}
		results, tblMode, err := querysupport.FetchSearchResults(&search)
		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
			return
		} else if results == nil {
			fmt.Fprintln(cmd.OutOrStdout(), querysupport.NoResultsText)
			return
		}
		p, err := datascope.CobraNew(results, &search, tblMode)
		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
			return
		}

		if _, err := p.Run(); err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
			return
		}
		return
	}

	// if a sid was not given, launch Mother into bare `attach` call
	if err := mother.Spawn(cmd.Root(), cmd, args); err != nil {
		clilog.Tee(clilog.CRITICAL, cmd.ErrOrStderr(),
			"failed to spawn a mother instance: "+err.Error()+"\n")
	}
}
