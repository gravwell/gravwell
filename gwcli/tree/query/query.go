/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package query represents the tools used to compose and submit queries to a Gravwell backend.

Query is important and complex enough to be broken into multiple files; this is the shared and central module entrypoint.

User Interaction Model:

```mermaid

flowchart

	start["user submit query<br>via terminal"] --> validate{"valid query?"} --"yes"--> sched{"scheduled<br>search?"}
	validate{"valid query?"} --"no"--> scriptMode{"script mode"} --"yes"--> failed(("fail out"))
	scriptMode{"script mode"} --"no"--> populateMother["pass flag<br>to Mother"] --> spawnMother(("pass control<br>to Mother"))
	sched --"yes"--> validSched["validate scheduling parameters"]
	  --> createSched["create scheduled query"] --> done(("job's done"))

	sched --"no"--> background{"background?"}
	  --"yes"--> submitQry["submit query"] --> done

	background{"background?"} --"no"--> interactive{"interactive?"}
	  --"yes"--> datascope(("pass control to datascope"))
	interactive --"no"--> printResults["print results to screen"] -->done

```
*/
package query

/**
 * When working on the query system, keep in mind that it functionally has three entry points:
 * 1. Cobra non-interactive (from cli, with --script)
 * 2. Cobra interactive (from cli, omitting --script)
 * 3. Mother
 * All efforts have been made to consolidate the code for these entry points and provide a
 * consistent flow. That being said, you have to make sure that changes get reflected across each.
 * Entry points 2 and 3 will boot DataScope, meaning they must fetch results via
 * grav.Get*Results methods, implemented locally by the fetch*Results.
 * Entry point 1 only uses grav.DownloadSearch, but keep in mind that DataScope's Download tab also
 * relies on grav.DownloadSearch() and these outcomes should be identical.
 */

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/querysupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	defaultDuration = 1 * time.Hour

	pageSize = 500 // fetch results page by page

	helpDesc = "Generate and send a query to the remote server either by arguments or " +
		"the interactive query builder.\n" +
		"All bare arguments after `query` will be passed to the instance as the query string.\n" +
		"\n" +
		"Omitting --script will open the results in an interactive viewing pane with additional" +
		"functionality for downloading the results to a file or scheduling this query to run in " +
		"the future" +
		"\n" +
		"If --json or --csv is not given when outputting to a file (`-o`), the results will be " +
		"text (if able) or an archive binary blob (if unable), depending on the query's render " +
		"module.\n" +
		"gwcli will not dump binary to terminal; you must supply -o if the results are a binary " +
		"blob (aka: your query uses a chart-style renderer)."
)

var (
	ErrSuperfluousQuery = "query is empty and therefore ineffectual"
)

var localFS pflag.FlagSet

//#region command/action set up

func NewQueryAction() action.Pair {
	cmd := treeutils.GenerateAction("query", "submit a query",
		helpDesc,
		[]string{"q", "search"}, run)

	localFS = initialLocalFlagSet()

	cmd.Example = "./gwcli query \"tag=gravwell\""

	cmd.Flags().AddFlagSet(&localFS)

	return action.NewPair(cmd, Query)
}

func initialLocalFlagSet() pflag.FlagSet {
	fs := pflag.FlagSet{}

	fs.DurationP("duration", "t", time.Hour*1,
		"the historical timeframe from now the query should pour over.\n"+
			"Ex: '1h' = the past hour, '5s500ms'= the previous 5 and a half seconds")
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

//#endregion

//#region cobra command

// Cobra command called when query is invoked directly from the commandline.
// Walks through the given flags and checks them in order: scheduled query, background query, normal query.
// Invokes Mother iff query is called bare.
// Invokes a Motherless datascope if a valid query is given and --script is not given.
func run(cmd *cobra.Command, args []string) {
	var err error

	// fetch flags
	flags := querysupport.TransmogrifyFlags(cmd.Flags())

	// TODO pull qry from referenceID, if given

	qry := strings.TrimSpace(strings.Join(args, " "))
	valid, err := testQryValidity(qry)

	if !valid {
		if flags.Script { // fail out
			var errMsg string
			if err != nil {
				errMsg = err.Error()
			} else if qry == "" {
				errMsg = "query cannot be empty"
			}
			clilog.Tee(clilog.INFO, cmd.OutOrStdout(), "invalid query: "+errMsg+"\n")
			return
		}

		// spawn mother on the query prompt
		// NOTE(rlandau): we hit the backend to validate the query twice (once now, once by Mother). This is unavoidable without complicating Mother further.
		if err := mother.Spawn(cmd.Root(), cmd, args); err != nil {
			clilog.Tee(clilog.CRITICAL, cmd.ErrOrStderr(),
				"failed to spawn a mother instance: "+err.Error()+"\n")
		}
		return
	}

	// check if this is a scheduled query
	if flags.Schedule.CronFreq != "" {
		ssid, warnings, invalid, err := scheduleQuery(&flags, qry)
		for _, warn := range warnings {
			fmt.Fprint(cmd.ErrOrStderr(), warn+"\n")
		}
		if invalid != "" {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), invalid+"\n")
		} else if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		} else {
			clilog.Tee(clilog.INFO, cmd.OutOrStdout(),
				fmt.Sprintf("Successfully scheduled query '%v' (ID: %v)\n", flags.Schedule.Name, ssid))
		}
		return
	}

	// submit the query
	var s grav.Search
	if s, err = connection.StartQuery(qry, -flags.Duration, flags.Background); err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		return
	}

	clilog.Tee(clilog.INFO, cmd.OutOrStdout(),
		querySubmissionSuccess(s.ID, flags.Background))

	// if this is a background query, we are done
	if flags.Background {
		warnings := warnBackgroundFlagConflicts(flags)
		for _, warn := range warnings {
			fmt.Fprint(cmd.ErrOrStderr(), "\n"+warn)
		}

		clilog.Tee(clilog.DEBUG, cmd.OutOrStdout(),
			fmt.Sprintf("Backgrounded query: ID: %v|UID: %v|GID: %v|eQuery: %v\n", s.ID, s.UID, s.GID, s.EffectiveQuery))

		// close our handle to the search
		// it will survive, as it is backgrounded
		s.Close()
		return
	}

	querysupport.HandleFGCobraSearch(&s, flags, cmd.OutOrStdout(), cmd.ErrOrStderr())
	if err := s.Close(); err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		return
	}
}

//#endregion

// Opens and returns a file handle, configured by the state of append.
//
// Errors are logged to clilogger internally
func openFile(path string, append bool) (*os.File, error) {
	var flags = os.O_WRONLY | os.O_CREATE
	if append { // check append
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	f, err := os.OpenFile(path, flags, 0644)
	if err != nil {
		clilog.Writer.Errorf("Failed to open file %s (flags %d, mode %d): %v", path, flags, 0644, err)
		return nil, err
	}

	if s, err := f.Stat(); err != nil {
		clilog.Writer.Warnf("Failed to stat file %s: %v", f.Name(), err)
	} else {
		clilog.Writer.Debugf("Opened file %s of size %v", f.Name(), s.Size())
	}

	return f, nil
}
