/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/pflag"
)

// autoingest attempts to ingest the file at each path, returning errors and successes on the given channel (if non-nil).
// Performs ingestions in parallel; once len(pairs) results have been sent, caller can assume this goroutine has returned.
// No logging is performed internally; caller is expected to log and present results.
//
// If ufErr (user-friendly error) is returned, do not wait on the channel; no values will be sent.
// if ufErr is nil, you can safely assume exactly len(pairs) will be returned.
func autoingest(res chan<- struct {
	string
	error
}, flags ingestFlags, pairs []pair) (ufErr error) {
	// basic validation
	if len(pairs) == 0 {
		return errNoFilesSpecified(flags.script)
	}

	// spin off a goro to test and ingest each pair
	for _, pair := range pairs {
		go func() {
			// invoke ingest path and return its result plus the path it operated on
			res <- struct {
				string
				error
			}{pair.path, ingestPath(flags, pair)}
		}()
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

// ingestFlags holds all flags so we don't have to keep passing around the pflag set.
type ingestFlags struct {
	script     bool
	hidden     bool   // include hidden files when ingesting directories
	recursive  bool   // recursively descend directories
	src        string // IP address to use as the source of the files; comes in as a net.IP
	ignoreTS   bool   // all entries will be tagged with the current time rather than any internal timestamping.
	localTime  bool   // use server-local timezone rather than inherent timezones
	dir        string // starting directory for interactive mode
	defaultTag string // the tag to use if not specified in the argument (or in the file itself, in the case of GW JSON)
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
	} else if srcRaw != "" {
		if src := net.ParseIP(srcRaw); src == nil {
			invalids = append(invalids, srcRaw+" is not a valid IP address")
		} else {
			flags.src = src.String()

		}
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
	if def, err := fs.GetString("default-tag"); err != nil {
		return flags, invalids, uniques.ErrFlagDNE("default-tag", "ingest")
	} else {
		flags.defaultTag = def
	}

	return flags, invalids, nil
}

// Given the bare arguments, returns a list of pairs associating each path to its tag (if a tag was supplied).
// Does not perform any coercion for paths or tag (other than skipping empty elements).
func parsePairs(args []string) []pair {
	pairs := []pair{}

	for _, a := range args {
		if a == "" {
			continue
		}
		var p pair
		p.path, p.tag, _ = strings.Cut(a, ",")
		pairs = append(pairs, p)
	}

	return pairs
}

// ingestPath validates and attempts to ingest the given pair.
// "Return" values are sent over the res channel.
//
// ! Intended to be run as a goroutine.
func ingestPath(flags ingestFlags, p pair) error {
	var err error
	// clean and validate path
	p.path = strings.TrimSpace(p.path)
	if p.path == "" {
		return errEmptyPath
	}
	info, err := os.Stat(p.path)
	if err != nil {
		return err
	} else if info.Size() <= 0 {
		return errEmptyFile
	}

	if p.tag, err = determineTag(p, flags.defaultTag); err != nil {
		return err
	}

	// if this is a directory, determine if we need to shallowly or recursively slurp its files
	var fileOrDirStr = "file"
	if info.IsDir() {
		fileOrDirStr = "directory"
	}

	// we have all the data we need, we can now attempt ingestion
	resp, err := connection.Client.IngestFile(p.path, p.tag, flags.src, flags.ignoreTS, flags.localTime)
	if err != nil {
		clilog.Writer.Warnf("failed to ingest %v at path %v: %v", fileOrDirStr, p.path, err)
		return err
	}
	clilog.Writer.Infof("successfully ingested %v at path %v (specified tag: %v | returned tags: %v)",
		fileOrDirStr, p.path, p.tag, resp.Tags)
	return nil
}

// determineTag figures out which tag to use, following the given priority:
//
// 1)
//
// 2)
//
// 3)
//
// ! It is valid for this function to return an empty tag and a nil error.
// This just means the file is a valid GWJSON file and can be ingested with the empty tag.
func determineTag(p pair, defaultTag string) (string, error) {
	if p.tag == "" {
		{
			// check if this is a GWJSON file by attempting to unmarshal it
			f, err := os.Open(p.path)
			if err != nil {
				return "", err
			}
			dcdr := json.NewDecoder(f)
			var ste types.StringTagEntry
			// try to decode a single entry (\n deliminted)
			if err := dcdr.Decode(&ste); err == nil && ste.Tag != "" {
				// successfully decoded file and read tag; we can leave our tag empty
				return "", nil
			}
		}
		// this is not a gravwell JSON file (or is one, but with an empty tag), try to use the default tag

		// try to fall back to the default tag, otherwise error out
		p.tag = defaultTag
		if p.tag == "" {
			return "", errNoTagSpecified
		}
		return p.tag, nil
	}
	if err := validateTag(p.tag); err != nil {
		return "", err
	}
	return p.tag, nil
}

func validateTag(tag string) error {
	// validate the argument tag
	for _, r := range tag {
		if slices.Contains(illegalTagCharacters, r) {
			return errInvalidTagCharacter
		}
	}
	return nil
}

type pair struct {
	path string
	tag  string
}
