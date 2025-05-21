// Package querysupport is intended to provide functionality for querying/searching.
// Allows multiple actions that touch the search backend to operate comparably and with minimal duplicate code.
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
	perm          os.FileMode = 0644
	pageSize      uint64      = 500 // fetch results page by page
	NoResultsText             = "no results found for given query"
)

// toFile slurps the given reader and spits its data into the given file.
// Assumes outPath != "".
func toFile(results io.ReadCloser, path string, append bool) error {
	var f *os.File

	// open the file
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

	if s, err := f.Stat(); err != nil {
		clilog.Writer.Warnf("Failed to stat file %s: %v", f.Name(), err)
	} else {
		clilog.Writer.Debugf("Opened file %s of size %v", f.Name(), s.Size())
	}

	// consumes the results and spits them into the open file
	if b, err := f.ReadFrom(results); err != nil {
		return err
	} else {
		clilog.Writer.Infof("Streamed %d bytes into %s", b, f.Name())
	}
	// stdout output is acceptable as the user is redirecting actual results to a file.
	/*fmt.Fprintln(cmd.OutOrStdout(),
	connection.DownloadQuerySuccessfulString(of.Name(), flags.append, format))*/
	return nil
}

// WriteDownloadResults slurps the given results and decides what to do with them.
// If an output file path is found, it will spit the result into the file.
// Otherwise, it will print them to the given Writer (probably stdout).
//
// ! Does not close results.
//
// ! Will not print to the altWriter if the type is a binary. A warning will be printed to altWriter instead
//
// ! Does not log errors; leaves that to the caller.
func WriteDownloadResults(results io.ReadCloser, altWriter io.Writer, filePath string, append bool, format string) error {
	if filePath != "" {
		return toFile(results, filePath, append)
	}
	// do not print binary to alt writer
	if format == types.DownloadArchive {
		fmt.Fprintf(altWriter, "refusing to dump binary blob (format %v) to stdout.\n"+
			"If this is intentional, re-run with -o <FILENAME>.\n"+
			"If it was not, re-run with --csv or --json to download in a more appropriate format.",
			format)
		return ErrBinaryBlobCoward{}
	}
	// print the results to alt writer
	if r, err := io.ReadAll(results); err != nil {
		return err
	} else {
		if len(r) == 0 {
			_, err := fmt.Fprintln(altWriter, NoResultsText)
			return err
		}
		_, err := fmt.Fprint(altWriter, string(r))
		return err
	}
}

// FetchSearchResults takes an attached search and pulls back available results if the search has completed
func FetchSearchResults(search *grav.Search) (results []string, tableMode bool, err error) {
	// TODO need to ensure the search is done, but no search.Done or search.Ended exists
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

	// default: did not manage to complete results earlier; fail out
	return nil, false, fmt.Errorf("unable to display results of type %v", search.RenderMod)

}

// Fetches all text results related to the given search by continually re-fetching until no more results remain.
func fetchTextResults(s *grav.Search) ([]types.SearchEntry, error) {
	// return results for output to terminal
	// batch results until we have the last of them
	var (
		results        = make([]types.SearchEntry, 0, pageSize)
		low     uint64 = 0
		high           = pageSize
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
