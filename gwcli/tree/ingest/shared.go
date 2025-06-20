/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"errors"
	"net"
	"os"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/pflag"
)

// autoingest attempts to ingest the data at each path, returning errors and successes on the given channel (if non-nil).
// Performs ingestions in parallel; once len(filepaths) results have been send (cumulative across both channels), caller can assume this goroutine has returned.
// No logging is performed internally; caller is expected to log and present results.
//
// If ufErr (user-friendly error) is returned, do not wait on the channel; no values will be sent.
func autoingest(res chan<- struct {
	string
	error
}, paths, tags []string, ignoreTS, localTime bool, src string) (ufErr error) {
	if len(paths) == 0 {
		return errNoFilesSpecified
	}
	// check that tag len is 1 or == file len
	if len(tags) != 1 && len(tags) != len(paths) {
		return errBadTagCount(uint(len(paths)))
	}

	// if there is only 1 tag, validate it immediately rather than on repeat
	if len(tags) == 1 {
		tags[0] = strings.TrimSpace(tags[0])
		if err := validateTag(tags[0]); err != nil {
			return errInvalidTagCharacter
		}
	}
	// try to ingest each file
	for i, fp := range paths {
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

// validateDirFlag is a helper function for checking that, if a path was given, it points to a valid *directory*.
// Returns the full directory path if it is valid. Otherwise, it returns a user-friendly 'invalid' reason or an error.
func validateDirFlag(dir string) (invalid string, err error) {
	if dir != "" {
		// a directory was specified; validate it
		info, err := os.Stat(dir)
		if err != nil {
			return "", err
		}

		if !info.IsDir() {
			return "--dir must point to a directory", nil
		}
	}
	return "", nil
}

type ingestFlags struct {
	script    bool
	hidden    bool   // include hidden files when ingesting directories
	recursive bool   // recursively descend directories
	src       net.IP // IP address to use as the source of the files
	ignoreTS  bool   // all entries will be tagged with the current time rather than any internal timestamping.
	localTime bool   // use server-local timezone rather than inherent timezones
	dir       string // starting directory for interactive mode
}

// transmogrifyFlags takes a *parsed* flagset and returns a structured, types, and (in the case of strings) trimmed representation of the flags therein.
// Validates each flag, returning either a populated flagset or the first error encountered.
// Errors are logged automatically; caller can just give the error back to the client and exit.
// Encountering an invalid argument does *not* return early.
func transmogrifyFlags(fs *pflag.FlagSet) (ingestFlags, []string, error) {
	if !fs.Parsed() {
		return ingestFlags{}, nil, errors.New("flagset must be parsed prior to transmogrification.")
	}

	var (
		invalids []string
	)

	flags := ingestFlags{}

	if script, err := fs.GetBool("script"); err != nil {
		return flags, invalids, uniques.ErrFlagDNE("script", "ingest")
	} else {
		flags.script = script
	}
	if includeHidden, err := fs.GetBool("hidden"); err != nil {
		return flags, invalids, uniques.ErrFlagDNE("hidden", "ingest")
	} else {
		flags.hidden = includeHidden
	}
	if recursive, err := fs.GetBool("recursive"); err != nil {
		return flags, invalids, uniques.ErrFlagDNE("recursive", "ingest")
	} else {
		flags.recursive = recursive
	}
	if srcRaw, err := fs.GetString("source"); err != nil {
		return flags, invalids, uniques.ErrFlagDNE("source", "ingest")
	} else if src := net.ParseIP(srcRaw); src == nil {
		invalids = append(invalids, srcRaw+" is not a valid IP address")
	} else {
		flags.src = src
	}
	if ignoreTS, err := fs.GetBool("ignore-timestamp"); err != nil {
		return flags, invalids, uniques.ErrFlagDNE("ignore-timestamp", "ingest")
	} else {
		flags.ignoreTS = ignoreTS
	}
	if localTime, err := fs.GetBool("local-time"); err != nil {
		return flags, invalids, uniques.ErrFlagDNE("local-time", "ingest")
	} else {
		flags.localTime = localTime
	}
	if dir, err := fs.GetString("dir"); err != nil {
		return flags, invalids, uniques.ErrFlagDNE("dir", "ingest")
	} else {
		dir = strings.TrimSpace(dir)
		if invalid, err := validateDirFlag(dir); err != nil {
			clilog.Writer.Errorf("%v", err)
			return flags, invalids, err
		} else if invalid != "" {
			invalids = append(invalids, invalid)
		} else {
			flags.dir = dir
		}
	}

	return flags, invalids, nil
}

// Given the bare arguments, returns a list of pairs associating each path to its tag (if a tag was supplied).
// Does not perform any coercion for paths or tag.
func parsePairs(args []string) []struct {
	path string
	tag  string
} {
	pairs := []struct {
		path string
		tag  string
	}{}

	for _, a := range args {
		if a == "" {
			continue
		}
		p := struct {
			path string
			tag  string
		}{}
		p.path, p.tag, _ = strings.Cut(a, ",")
		pairs = append(pairs, p)
	}

	return pairs
}
