// Package querysupport is intended to provide functionality for querying/searching.
// Allows multiple actions that touch the search backend to operate comparably and with minimal duplicate code.
package querysupport

import (
	"fmt"
	"io"
	"os"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
)

// permissions to use for the file handle
const perm os.FileMode = 0644

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
			_, err := fmt.Fprintln(altWriter, "no results to display")
			return err
		}
		_, err := fmt.Fprint(altWriter, string(r))
		return err
	}
}
