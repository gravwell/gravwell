// Package querysupport is intended to provide functionality for querying/searching.
// Allows multiple actions that touch the search backend to operate comparably and with minimal duplicate code.
//
// There is some logical overlap between the querysupport and connection packages and which query-related functions belong to each can seem somewhat arbitrary.
// The differentiation is specifically: "does it touch the backend?" If yes, it goes in the connection package.
// Subroutines are also split across both to prevent import cycles (which are typically a sign of poor package differentiation anyways).
package querysupport

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/busywait"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/tree/query/datascope"
)

// permissions to use for the file handle
const (
	perm      os.FileMode = 0644
	pageSize  uint64      = 500 // fetch results page by page
	NoResults             = "no results found for given query"
)

// toFile streams the data in rd into the file at path.
//
// Assumes path != "".
func toFile(rd io.Reader, path string, append bool) error {
	// open the file for writing
	var f *os.File
	var flags = os.O_WRONLY | os.O_CREATE
	if append { // check append
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	f, err := os.OpenFile(path, flags, perm)
	if err != nil {
		clilog.Writer.Errorf("failed to open file %s (flags %d, mode %d): %v", path, flags, perm, err)
		return err
	}
	defer f.Close()

	// stream the reader into the file
	if b, err := io.Copy(f, rd); err != nil {
		return err
	} else {
		clilog.Writer.Infof("Streamed %d bytes into %s", b, f.Name())
	}

	// Close() swallows its error, so catch sync's error instead
	if err := f.Sync(); err != nil {
		clilog.Writer.Errorf("failed to flush file %v", err)
		return err
	}
	return nil
}

// putResultsToWriter streams results into a file at the given path or into the given writer (probably stdout).
// If an output file path is found, it will spit the result into the file.
// Otherwise, it will print them to the given Writer (probably stdout).
//
// Typically called after GetResultsForWriter.
//
// ! Will not print to wr if the type is a binary. A BinaryBlobCoward error will be returned instead.
//
// ! Does not log errors; leaves that to the caller.
func putResultsToWriter(results io.Reader, wr io.Writer, filePath string, append bool, format string) error {
	if filePath != "" {
		return toFile(results, filePath, append)
	}
	if format == types.DownloadArchive {
		return ErrBinaryBlobCoward(format)
	}
	// print the results to alt writer
	written, err := io.Copy(wr, results)
	if err != nil {
		return err
	}
	if written == 0 {
		_, err := fmt.Fprintln(wr, NoResults)
		return err
	}
	return nil
}

// GetResultsForDataScope takes an attached search and pulls back available results if the search has completed.
// If the search turned by no results, results will be nil and the caller should print the NoResults text.
//
// This call blocks until the search is completed.
func GetResultsForDataScope(s *grav.Search) (results []string, tableMode bool, err error) {
	if s == nil {
		panic("search obj cannot be nil")
	}
	clilog.Writer.Debugf("awaiting %s", s.ID)
	if err := connection.Client.WaitForSearch(*s); err != nil {
		return nil, false, err
	}
	clilog.Writer.Infof("fetching results of type %v", s.RenderMod)
	switch s.RenderMod {
	case types.RenderNameTable:
		columns, rows, err := fetchTableResults(s)
		if err != nil {
			return nil, true, err
		} else if len(rows) == 0 {
			return nil, true, nil
		}
		// format the table for datascope (basically as csv)
		results = make([]string, len(rows)+1)
		results[0] = strings.Join(columns, ",") // first entry is the header
		for i, row := range rows {
			results[i+1] = strings.Join(row.Row, ",")
		}
		return results, true, nil
	case types.RenderNameRaw, types.RenderNameText, types.RenderNameHex:
		rawResults, err := fetchTextResults(s)
		if err != nil {
			return nil, false, err
		} else if len(rawResults) == 0 {
			return nil, false, nil
		}
		// format the data for datascope
		results = make([]string, len(rawResults))
		for i, r := range rawResults {
			results[i] = string(r.Data)
		}
		return results, false, nil
	}

	// default
	return nil, false, fmt.Errorf("unable to display results of type %v", s.RenderMod)

}

// Helper subroutine for GetResultsForDataScope.
// Sister subroutine to fetchTextResults().
func fetchTableResults(s *grav.Search) (columns []string, rows []types.TableRow, err error) {
	// batch results until we have the last of them
	var (
		low  uint64 = 0
		high        = pageSize
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

// Helper subroutine for GetResultsForDataScope.
// Slurps text results related to the given search by continually re-fetching until no more results remain.
func fetchTextResults(s *grav.Search) ([]types.SearchEntry, error) {
	// return results for output to terminal
	// batch results until we have the last of them
	var (
		low     uint64 = 0
		high           = pageSize
		results        = make([]types.SearchEntry, 0, pageSize)
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

// HandleFGCobraSearch is intended to be called from a search-related action's run function once it acquires a handle to a foreground search.
// Waits on the search,
// pings the search according to its interval,
// displays a spinner if !script,
// and fetches the results according to whether they are intended for a writer (file/stdout) or datascope.
// If the results a
//
// If placed in datascope, this subroutine will block until datascope returns.
//
// When this subroutine returns, you can safely exit (after closing the search and printing the error, if applicable).
//
// ! Does not close the search
func HandleFGCobraSearch(s *grav.Search, flags QueryFlags, stdout, stderr io.Writer) {
	// spawn a goroutine to ping the query while we wait for it to complete
	ping := make(chan bool)
	go func() {
		// check in at each expected interval, until we are signaled to be done
		sleepTime := s.Interval()
		for {
			select {
			case <-ping:
				return
			default:
				s.Ping()
				clilog.Writer.Debugf("pinged fg query %s", s.ID)
				time.Sleep(sleepTime)
			}
		}
	}()
	// if we are not in script mode, spawn a spinner to show that we didn't just hang during processing
	var spnr *tea.Program
	if !flags.Script {
		spnr = busywait.CobraNew()
		go spnr.Run()
	}

	// if we are in script mode or were given an output file, the results will be streamed
	if flags.Script || flags.OutPath != "" {
		// ensure we stop our other goroutines when the job is done
		defer func() {
			close(ping)
			if spnr != nil {
				spnr.Quit()
			}
		}()
		// pull results
		rc, format, err := connection.GetResultsForWriter(s, types.TimeRange{}, flags.CSV, flags.JSON)
		if err != nil {
			clilog.Tee(clilog.ERROR, stderr, err.Error())
			return
		}
		defer rc.Close()

		// put results to file or stdout
		if err := putResultsToWriter(rc, stdout, flags.OutPath, flags.Append, format); err != nil {
			clilog.Tee(clilog.ERROR, stderr, err.Error())
		}
		return
	}
	// otherwise, the results will be slurped for datascope
	results, tbl, err, killed := killableAwaitDSResults(s)
	// once results are ready (or we were killed), kill our other goroutines
	close(ping)
	if spnr != nil {
		spnr.Quit()
	}
	if killed {
		clilog.Writer.Infof("search interrupted by signal")
		// no need to close s; the caller is expected to do so
		return
	} else if err != nil {
		clilog.Tee(clilog.ERROR, stderr, err.Error())
		return
	}

	// build datascope options, if applicable
	opts := make([]datascope.DataScopeOption, 0)
	if flags.Schedule.CronFreq != "" {
		opts = append(opts, datascope.WithSchedule(flags.Schedule.CronFreq, flags.Schedule.Name, flags.Schedule.Desc))
	}
	if flags.Schedule.CronFreq != "" {
		opts = append(opts, datascope.WithAutoDownload(flags.OutPath, flags.Append, flags.JSON, flags.CSV))
	}

	// pass control off to datascope
	p, err := datascope.CobraNew(results, s, tbl, opts...)
	if err != nil {
		clilog.Tee(clilog.ERROR, stderr, err.Error())
		return
	}
	if _, err := p.Run(); err != nil {
		clilog.Tee(clilog.ERROR, stderr, err.Error())
		return
	}
}

// helper subroutine for HandleFGCobraSearch.
// Calls GetResultsForDataScope and awaits it or a SIGINT from the user to return early.
// Returns the results of GetResultsForDataScope or killed.
func killableAwaitDSResults(s *grav.Search) (results []string, tbl bool, err error, killed bool) {
	// set up the two possible outcome channels
	kill := make(chan os.Signal, 1)
	signal.Notify(kill, os.Interrupt)
	res := make(chan bool) // res returning means values are not available in our named returns; a value is never actually sent over res

	// spin off goro to await results
	go func() {
		results, tbl, err = GetResultsForDataScope(s)
		close(res)
	}()

	// return on first outcome
	select {
	case <-res:
		// clean up after ourself
		signal.Stop(kill)
		return results, tbl, err, false
	case <-kill:
		// clean up after ourself
		signal.Stop(kill)
		return nil, false, nil, true
	}

}
