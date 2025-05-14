/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Core query module

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
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

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
	cmd := treeutils.NewActionCommand("query", "submit a query",
		helpDesc,
		[]string{"q", "search"}, run)

	localFS = initialLocalFlagSet()

	cmd.Example = "./gwcli query \"tag=gravwell\""

	cmd.Flags().AddFlagSet(&localFS)

	return treeutils.GenerateAction(cmd, Query)
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

func run(cmd *cobra.Command, args []string) {
	var err error

	// fetch flags
	flags, err := transmogrifyFlags(cmd.Flags())
	if err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		return
	}

	// TODO pull qry from referenceID, if given

	qry := strings.TrimSpace(strings.Join(args, " "))
	valid, err := testQryValidity(qry)

	if !valid {
		if flags.script { // fail out
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
	if flags.schedule.cronfreq != "" {
		scheduleQuery(&flags, cmd, qry)
		return
	}

	// check if this is a background query
	if flags.background {
		backgroundQuery()
	}

	// branch on script mode
	if flags.script {
		runNonInteractive(cmd, flags, qry)
		return
	}
	runInteractive(cmd, flags, qry)
}

// Returns whether or not the given query if valid (or if an non-parse error occurred while asking the backend to eval the query).
func testQryValidity(qry string) (valid bool, err error) {
	if qry == "" {
		return false, nil
	}

	err = connection.Client.ParseSearch(qry)
	// check if this is a parse error or something else
	if err != nil {
		clilog.Writer.Infof("failed to parse search %v: %v", qry, err)
		if !strings.Contains(err.Error(), "Parse error") {
			return false, err
		}
	}
	return true, nil
}

// Generates a scheduling request from the given flags, cmd, and query and attempts to schedule it.
// Internally handles (logs and prints) errors if they occur.
// Assumes the query has already been validated.
// On return, the caller can assume the query has been scheduled or the client has been notified of the error.
func scheduleQuery(flags *queryflags, cmd *cobra.Command, validatedQry string) {
	// warn about ignored flags
	if clilog.Active(clilog.WARN) { // only warn if WARN level is enabled
		if flags.outfn != "" {
			fmt.Fprint(cmd.ErrOrStderr(), uniques.WarnFlagIgnore(ft.Name.Output, ft.Name.Frequency)+"\n")
		}
		if flags.background {
			fmt.Fprint(cmd.ErrOrStderr(), uniques.WarnFlagIgnore("background", ft.Name.Frequency)+"\n")
		}
		if flags.append {
			fmt.Fprint(cmd.ErrOrStderr(), uniques.WarnFlagIgnore(ft.Name.Append, ft.Name.Frequency)+"\n")
		}
		if flags.json {
			fmt.Fprint(cmd.ErrOrStderr(), uniques.WarnFlagIgnore(ft.Name.JSON, ft.Name.Frequency)+"\n")
		}
		if flags.csv {
			fmt.Fprint(cmd.ErrOrStderr(), uniques.WarnFlagIgnore(ft.Name.CSV, ft.Name.Frequency)+"\n")
		}
	}

	// if a name was not given, populate a default name
	if flags.schedule.name == "" {
		flags.schedule.name = "cli_" + time.Now().Format(uniques.SearchTimeFormat)
	}
	// if a description was not given, populate a default description
	if flags.schedule.desc == "" {
		flags.schedule.desc = "generated in gwcli @" + time.Now().Format(uniques.SearchTimeFormat)
	}

	id, invalid, err := connection.CreateScheduledSearch(
		flags.schedule.name, flags.schedule.desc,
		flags.schedule.cronfreq, validatedQry,
		flags.duration,
	)
	if invalid != "" { // bad parameters
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), invalid)
		return
	} else if err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
	}
	clilog.Tee(clilog.INFO, cmd.OutOrStdout(),
		fmt.Sprintf("Successfully scheduled query '%v' (ID: %v)\n", flags.schedule.name, id))
	return
}

// Submits the query as a background query.
// Assumes the query has already been validated.
func backgroundQuery() {
	// TODO
}

// run function with --script given, making it entirely independent of user input.
// Results will be output to a file (if given) or dumped into stdout.
func runNonInteractive(cmd *cobra.Command, flags queryflags, qry string) {
	var err error

	if flags.schedule.cronfreq != "" { // check if it is a scheduled query
		// warn about ignored flags
		if clilog.Active(clilog.WARN) { // only warn if WARN level is enabled
			if flags.outfn != "" {
				fmt.Fprint(cmd.ErrOrStderr(), uniques.WarnFlagIgnore(ft.Name.Output, ft.Name.Frequency)+"\n")
			}
			if flags.append {
				fmt.Fprint(cmd.ErrOrStderr(), uniques.WarnFlagIgnore(ft.Name.Append, ft.Name.Frequency)+"\n")
			}
			if flags.json {
				fmt.Fprint(cmd.ErrOrStderr(), uniques.WarnFlagIgnore(ft.Name.JSON, ft.Name.Frequency)+"\n")
			}
			if flags.csv {
				fmt.Fprint(cmd.ErrOrStderr(), uniques.WarnFlagIgnore(ft.Name.CSV, ft.Name.Frequency)+"\n")
			}
		}

		// if a name was not given, populate a default name
		if flags.schedule.name == "" {
			flags.schedule.name = "cli_" + time.Now().Format(uniques.SearchTimeFormat)
		}
		// if a description was not given, populate a default description
		if flags.schedule.desc == "" {
			flags.schedule.desc = "generated in gwcli @" + time.Now().Format(uniques.SearchTimeFormat)
		}

		id, invalid, err := connection.CreateScheduledSearch(
			flags.schedule.name, flags.schedule.desc,
			flags.schedule.cronfreq, qry,
			flags.duration,
		)
		if invalid != "" { // bad parameters
			clilog.Tee(clilog.INFO, cmd.ErrOrStderr(), invalid)
			return
		} else if err != nil {
			clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		}
		clilog.Tee(clilog.INFO, cmd.OutOrStdout(),
			fmt.Sprintf("Successfully scheduled query '%v' (ID: %v)\n", flags.schedule.name, id))
		return
	}

	// submit the immediate query
	var search grav.Search
	if s, err := connection.StartQuery(qry, -flags.duration); err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		return
	} else {
		search = s
	}

	// wait for query to complete
	if err := waitForSearch(search, true); err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		return
	}

	// fetch the data from the search
	var (
		results io.ReadCloser
		format  string
	)
	if results, format, err = connection.DownloadSearch(
		&search, types.TimeRange{}, flags.csv, flags.json,
	); err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(),
			fmt.Sprintf("failed to retrieve results from search %s (format %v): %v\n",
				search.ID, format, err.Error()))
		return
	}
	defer results.Close()

	// if an output file was given, write results into it
	if flags.outfn != "" {
		// open the file
		var of *os.File
		if of, err = openFile(flags.outfn, flags.append); err != nil {
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
			connection.DownloadQuerySuccessfulString(of.Name(), flags.append, format))
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

}

// run function without --script given, making it acceptable to rely on user input
// NOTE: download and schedule flags are handled inside of datascope
func runInteractive(cmd *cobra.Command, flags queryflags, qry string) {
	// submit the immediate query
	var search grav.Search
	if s, err := connection.StartQuery(qry, -flags.duration); err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		return
	} else {
		search = s
	}

	// wait for query to complete
	if err := waitForSearch(search, false); err != nil {
		clilog.Tee(clilog.ERROR, cmd.ErrOrStderr(), err.Error()+"\n")
		return
	}

	// get results to pass to data scope
	var (
		results   []string
		tableMode bool
	)
	results, tableMode, err := fetchResults(&search)
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
		results, &search, tableMode,
		datascope.WithAutoDownload(flags.outfn, flags.append, flags.json, flags.csv),
		datascope.WithSchedule(flags.schedule.cronfreq, flags.schedule.name, flags.schedule.desc))
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

// just enough information to schedule a given query
type schedule struct {
	name     string
	desc     string
	cronfreq string // run frequency in cron format
}

// Opens and returns a file handle, configured by the state of append.
//
// Errors are logged to clilogger internally
func openFile(path string, append bool) (*os.File, error) {
	var flags int = os.O_WRONLY | os.O_CREATE
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

// Given an active search handle associated to a completed search,
// fetchResults pulls back all available results, using the appropriate Get function based on the
// search's renderer
func fetchResults(search *grav.Search) (results []string, tableMode bool, err error) {
	clilog.Writer.Infof("fetching results of type %v", search.RenderMod)
	switch search.RenderMod {
	case types.RenderNameTable:
		if columns, rows, err := fetchTableResults(search); err != nil {
			return nil, false, err
		} else if len(rows) != 0 {
			// format the table for datascope
			// basically a csv
			results = make([]string, len(rows)+1)
			results[0] = strings.Join(columns, ",") // first entry is the header
			for i, row := range rows {
				results[i+1] = strings.Join(row.Row, ",")
			}
			return results, true, nil
		}
		// no results
		return nil, true, nil
	case types.RenderNameRaw, types.RenderNameText, types.RenderNameHex:
		if rawResults, err := fetchTextResults(search); err != nil {
			return nil, false, err
		} else if len(rawResults) != 0 {
			// format the data for datascope
			results = make([]string, len(rawResults))
			for i, r := range rawResults {
				results[i] = string(r.Data)
			}
			return results, false, nil
		}
		// no results
		return nil, false, nil
	}

	// did not manage to complete results earlier; fail out
	return nil, false, fmt.Errorf("unable to display results of type %v", search.RenderMod)
}

// Fetches all text results related to the given search by continually re-fetching until no more
// results remain
func fetchTextResults(s *grav.Search) ([]types.SearchEntry, error) {
	// return results for output to terminal
	// batch results until we have the last of them
	var (
		results []types.SearchEntry = make([]types.SearchEntry, 0, pageSize)
		low     uint64              = 0
		high    uint64              = pageSize
	)
	for { // accumulate the results
		r, err := connection.Client.GetTextResults(*s, low, high)
		if err != nil {
			return nil, err
		}
		results = append(results, r.Entries...)
		if !r.AdditionalEntries { // all records obtained
			break
		}
		// ! Get*Results is half-open [)
		low = high
		high = high + pageSize
	}

	clilog.Writer.Infof("%d results obtained", len(results))

	return results, nil
}

// Sister subroutine to fetchTextResults()
func fetchTableResults(s *grav.Search) (
	columns []string, rows []types.TableRow, err error,
) {
	// return results for output to terminal
	// batch results until we have the last of them
	var (
		low  uint64 = 0
		high uint64 = pageSize
		r    types.TableResponse
	)
	rows = make([]types.TableRow, 0, pageSize)
	for { // accumulate the row results
		r, err = connection.Client.GetTableResults(*s, low, high)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, r.Entries.Rows...)
		if !r.AdditionalEntries { // all records obtained
			break
		}
		// ! Get*Results is half-open [)
		low = high
		high = high + pageSize
	}

	// save off columns
	columns = r.Entries.Columns

	clilog.Writer.Infof("%d results obtained", len(rows))

	return
}
