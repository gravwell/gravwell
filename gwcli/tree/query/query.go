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
	"io"
	"os"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/busywait"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/tree/query/datascope"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/querysupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	defaultDuration = 1 * time.Hour

	pageSize = 500 // fetch results page by page

	NoResultsText = "No results found for given query"

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
	var search grav.Search
	if search, err = connection.StartQuery(qry, -flags.Duration, flags.Background); err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		return
	}

	clilog.Tee(clilog.INFO, cmd.OutOrStdout(),
		querySubmissionSuccess(search.ID, flags.Background))

	// if this is a background query, we are done
	if flags.Background {
		warnings := warnBackgroundFlagConflicts(flags)
		for _, warn := range warnings {
			fmt.Fprint(cmd.ErrOrStderr(), warn+"\n")
		}

		clilog.Tee(clilog.DEBUG, cmd.OutOrStdout(),
			fmt.Sprintf("Backgrounded query: ID: %v|UID: %v|GID: %v|eQuery: %v\n", search.ID, search.UID, search.GID, search.EffectiveQuery))

		// close our handle to the search
		// it will survive, as it is backgrounded
		search.Close()
		return
	}

	// wait for results
	if err := waitForSearch(search, flags.Script); err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		return
	}

	// if we are in script mode, spit the result into a file or stdout
	if flags.Script {
		// fetch the data from the search
		var (
			results io.ReadCloser
			format  string
		)
		if results, format, err = connection.DownloadSearch(
			&search, types.TimeRange{}, flags.CSV, flags.JSON,
		); err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(),
				fmt.Sprintf("failed to retrieve results from search %s (format %v): %v\n",
					search.ID, format, err.Error()))
			return
		}
		defer results.Close()

		// if an output file was given, write results into it
		if flags.OutPath != "" {
			// open the file
			var of *os.File
			if of, err = openFile(flags.OutPath, flags.Append); err != nil {
				clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
				return
			}
			defer of.Close()

			// consumes the results and spit them into the open file
			if b, err := of.ReadFrom(results); err != nil {
				clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
				return
			} else {
				clilog.Writer.Infof("Streamed %d bytes (format %v) into %s", b, format, of.Name())
			}
			// stdout output is acceptable as the user is redirecting actual results to a file.
			fmt.Fprintln(cmd.OutOrStdout(),
				connection.DownloadQuerySuccessfulString(of.Name(), flags.Append, format))
			return
		} else if format == types.DownloadArchive { // check for binary output
			fmt.Fprintf(cmd.OutOrStdout(), "refusing to dump binary blob (format %v) to stdout.\n"+
				"If this is intentional, re-run with -o <FILENAME>.\n"+
				"If it was not, re-run with --csv or --json to download in a more appropriate format.",
				format)
		} else { // text results, stdout
			if r, err := io.ReadAll(results); err != nil {
				clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
				return
			} else {
				if len(r) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no results to display")
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "%s", r)
				}
			}
		}

		return
	}

	// if we made it this far, we can fetch the results and pass them to datascope
	// NOTE(rlandau): this function does not share the result-fetching methodology of script mode (connection.DownloadSearch) because datascope requires additional formatting.
	invokeDatascope(cmd, flags, &search)
}

// run function without --script given, making it acceptable to rely on user input
// NOTE: download and schedule flags are handled inside of datascope
func invokeDatascope(cmd *cobra.Command, flags querysupport.QueryFlags, search *grav.Search) {
	// get results to pass to data scope
	var (
		results   []string
		tableMode bool
	)
	results, tableMode, err := querysupport.FetchSearchResults(search)
	if err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		return
	} else if results == nil {
		fmt.Fprintln(cmd.OutOrStdout(), NoResultsText)
		return
	}

	// pass results into datascope
	// spin up a scrolling pager to display
	p, err := datascope.CobraNew(
		results, search, tableMode,
		datascope.WithAutoDownload(flags.OutPath, flags.Append, flags.JSON, flags.CSV),
		datascope.WithSchedule(flags.Schedule.CronFreq, flags.Schedule.Name, flags.Schedule.Desc))
	if err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
		return
	}

	if _, err := p.Run(); err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error())
		return
	}

}

// Stops execution and waits for the given search to complete.
// Adds a spinner if not in script mode.
func waitForSearch(s grav.Search, scriptMode bool) error {
	// in script mode, wait synchronously
	if scriptMode {
		if err := connection.Client.WaitForSearch(s); err != nil {
			return err
		}
	} else {
		// outside of script mode wait via goroutine so we can display a spinner
		spnrP := busywait.CobraNew()
		go func() {
			if err := connection.Client.WaitForSearch(s); err != nil {
				clilog.Writer.Error(err.Error())
			}
			spnrP.Quit()
		}()

		if _, err := spnrP.Run(); err != nil {
			return err
		}
	}
	return nil
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
