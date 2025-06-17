/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"slices"
	"strings"

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
	if len(filepaths) == 0 {
		return errNoFilesSpecified
	}
	// check that tag len is 1 or == file len
	if len(tags) != 1 && len(tags) != len(filepaths) {
		return errBadTagCount(uint(len(filepaths)))
	}

	// if there is only 1 tag, validate it immediately rather than on repeat
	if len(tags) == 1 {
		tags[0] = strings.TrimSpace(tags[0])
		if err := validateTag(tags[0]); err != nil {
			return errInvalidTagCharacter
		}
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
				// validate each tag
				tag = strings.TrimSpace(tags[i])
				if err := validateTag(tags[0]); err != nil {
					// send this error over the wire, rather than attempting ingestion
					if res != nil {
						res <- struct {
							string
							error
						}{fp, err}
					}
					return
				}
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

// Given a tag for the file to be ingested, validates that it is non-nil and does not have illegal characters.
func validateTag(tag string) error {
	if tag == "" {
		return errEmptyTag
	}
	// test for illegal characters
	for _, r := range tag {
		if slices.Contains(illegalTagCharacters, r) {
			return errInvalidTagCharacter
		}
	}
	return nil
}
