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
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/busywait"
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
		defer s.Close()

		// spawn a goroutine to ping the query while we wait for it to complete
		done := make(chan bool)
		go func() {
			// check in at each expected interval, until we are signaled to be done
			sleepTime := s.Interval()
			for {
				select {
				case <-done:
					return
				default:
					s.Ping()
					time.Sleep(sleepTime)
				}
			}
		}()
		// if we are not in script mode, spawn a spinner to show that we didn't just hang
		var spnr *tea.Program
		if !flags.Script {
			spnr = busywait.CobraNew()
			spnr.Run()
		}

		err = connection.Client.WaitForSearch(s)
		// stop our other goroutines
		close(done)
		if spnr != nil {
			spnr.Quit()
		}

		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
			return
		}

		// pull results
		rc, _, err := querysupport.StreamSearchResults(&s, types.TimeRange{}, flags.CSV, flags.JSON)
		if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
			return
		}
		defer rc.Close()

		if flags.OutPath != "" { // spit the results into a file
			if err := querysupport.WriteResultsToFile(rc, flags.OutPath, flags.Append); err != nil {
				clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
			}
		} else if flags.Script { // spit the results to stdout
			if _, err := io.Copy(cmd.OutOrStdout(), rc); err != nil {
				clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
			}
		} else { // give the results to datascope

			// this will block until the user exists datascope
			datascope.CobraNew()
		}

		return

	}

	// split on --script
	if flags.Script {
		// arg validator should ensure sid is populated by this point

		// attaching to a search non-interactively just downloads the results and spits them out

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
