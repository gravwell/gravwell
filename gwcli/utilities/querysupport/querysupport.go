// Package querysupport is intended to provide functionality for querying/searching.
// Allows multiple actions that touch the search backend to operate comparably and with minimal duplicate code.
//
// Much of the functionality of querysupport is wrapping the connection package; this package should be preferred over using connection direction when querying.
package querysupport

import (
	"fmt"
	"io"
	"os"
	"strings"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
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

// GetResultsForWriter waits on and downloads the given results according to their associated render type
// (JSON, CSV, if given, otherwise the normal form of the results),
// returning an io.ReadCloser to stream the results and the format they are in.
// If a TimeRange is given, only results in that timeframe will be included.
//
// This should be used to get results when they will be written ton io.Writer (a file or stdout).
//
// This call blocks until the search is completed.
//
// Typically called prior to PutResultsToWriter.
func GetResultsForWriter(s *grav.Search, tr types.TimeRange, csv, json bool) (rc io.ReadCloser, format string, err error) {
	if err := connection.Client.WaitForSearch(*s); err != nil {
		return nil, "", err
	}

	// determine the format to request results in
	if json {
		format = types.DownloadJSON
	} else if csv {
		format = types.DownloadCSV
	} else {
		switch s.RenderMod {
		case types.RenderNameHex, types.RenderNameRaw, types.RenderNameText:
			format = types.DownloadText
		case types.RenderNamePcap:
			format = types.DownloadPCAP
		default:
			format = types.DownloadArchive
		}
	}
	clilog.Writer.Infof("renderer '%s' -> '%s'", s.RenderMod, format)

	// fetch and return results
	rc, err = connection.Client.DownloadSearch(s.ID, tr, format)
	return rc, format, err
}

// PutResultsToWriter streams results into a file at the given path or into the given writer (probably stdout).
// If an output file path is found, it will spit the result into the file.
// Otherwise, it will print them to the given Writer (probably stdout).
//
// Typically called after GetResultsForWriter.
//
// ! Will not print to wr if the type is a binary. A BinaryBlobCoward error will be returned instead.
//
// ! Does not log errors; leaves that to the caller.
func PutResultsToWriter(results io.Reader, wr io.Writer, filePath string, append bool, format string) error {
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
