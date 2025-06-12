package ingest

import (
	"fmt"

	"github.com/gravwell/gravwell/v4/gwcli/connection"
)

// autoingest attempts to ingest the data at each file path, returning errors and successes on the given channel (if non-nil).
// Performs ingestions in parallel; once len(filepaths) results have been send (cumulative across both channels), caller can assume this goroutine has returned.
// No logging is performed internally; caller is expected to log and present results.
//
// If ufErr (user-friendly error) is returned, do not wait on the channel; no values will be sent.
func autoingest(res chan<- struct {
	string
	error
}, filepaths, tags []string, ignoreTS, localTime bool, src string) (ufErr error) {
	// check that tag len is 1 or == file len
	if len(tags) != 1 && len(tags) != len(filepaths) {
		return fmt.Errorf("tag count must be 1 or equal to the number of files specified (%v)", len(filepaths))
	}

	// try to ingest each file
	for i, fp := range filepaths {
		if fp == "" {
			continue
		}

		go func() {
			var tag string
			if len(tags) == 1 {
				tag = tags[0]
			} else {
				tag = tags[i]
			}

			_, err := connection.Client.IngestFile(fp, tag, src, ignoreTS, localTime)
			if res != nil {
				res <- struct {
					string
					error
				}{fp, err}
			}
		}()
	}
	return nil
}
