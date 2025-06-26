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
	"path"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/pflag"
)

// autoingest attempts to ingest the file at each path, returning errors and successes on the given channel (if non-nil).
// Returns the number of files to be ingested; caller can safely await exactly count results from the channel (again, if non-nil).
// Performs ingestions in parallel.
func autoingest(res chan<- struct {
	string
	error
}, flags ingestFlags, pairs []pair) (count uint) {
	if len(pairs) == 0 {
		return 0
	}

	var (
		paths     = make(map[string]string) // path -> tag
		errPaths  = make(map[string]error)  // path -> collection error
		fileCount uint
	)
	// determine the number of files we are going to ingest and build a list of full paths
	for _, pair := range pairs {
		toIng, err := collectPathsForIngestions(pair.path, flags.recursive)
		for path := range toIng {
			// set aside paths that error so we can immediately return them as an error
			if err != nil {
				errPaths[path] = err
			} else {
				paths[path] = pair.tag
			}
			fileCount += 1
		}
	}

	// spin off a goroutine per path to ingest each file
	for path, tag := range paths {
		go func(p, t string) {
			err := ingestPath(flags, p, t)
			if res != nil {
				res <- struct {
					string
					error
				}{p, err}
			}
		}(path, tag)
	}

	// spin off a single goroutine to pass errors from collect
	go func() {
		if res != nil {
			for p, err := range errPaths {
				res <- struct {
					string
					error
				}{p, err}
			}
		}
	}()

	return fileCount
}

// given a path, collectPathsForIngestion identifies the full paths for each file to be uploaded.
// collectPathsForIngestions traverses directories, descending iff recur.
//
// The returned map is a set; values are not important.
func collectPathsForIngestions(pathToIngest string, recur bool) (map[string]bool, error) {
	if pathToIngest == "" {
		return nil, nil
	} else if info, err := os.Stat(pathToIngest); err != nil {
		return nil, err
	} else if !info.IsDir() {
		// if this is a file, return just its path
		return map[string]bool{pathToIngest: true}, nil
	}

	paths := map[string]bool{}

	// traverse the directory
	entries, err := os.ReadDir(pathToIngest)
	if err != nil {
		clilog.Writer.Warnf("failed to walk directory rooted at %v: %v", pathToIngest, err)
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if recur {
				subdir, err := collectPathsForIngestions(path.Join(pathToIngest, entry.Name()), true)
				if err != nil {
					return nil, err
				}
				// add these paths to our known paths
				for path := range subdir {
					paths[path] = true
				}
			} // if !recur, ignore directory
		} else {
			// just add the file to our map
			paths[path.Join(pathToIngest, entry.Name())] = true
		}
	}
	return paths, nil
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
		return flags, nil, uniques.ErrFlagDNE("script", "ingest")
	} else {
		flags.script = script
	}
	if includeHidden, err := fs.GetBool("hidden"); err != nil {
		return flags, nil, uniques.ErrFlagDNE("hidden", "ingest")
	} else {
		flags.hidden = includeHidden
	}
	if recursive, err := fs.GetBool("recursive"); err != nil {
		return flags, nil, uniques.ErrFlagDNE("recursive", "ingest")
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
	} else if err := validateTag(def); err != nil {
		return flags, invalids, err
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

// ingestPath validates and attempts to ingest the file at the given path.
func ingestPath(flags ingestFlags, pth, tag string) error {
	var err error
	// clean and validate path
	pth = strings.TrimSpace(pth)
	if pth == "" {
		return errEmptyPath
	}
	info, err := os.Stat(pth)
	if err != nil {
		return err
	} else if info.Size() <= 0 {
		return errEmptyFile
	} else if info.IsDir() {
		// this likely means there is a bug, as it should be only individual files at this point
		return errUnwalkedDirectory(pth)
	}

	if tag, err = determineTag(pth, tag, flags.defaultTag); err != nil {
		return err
	}

	return ingestFile(pth, tag, flags)
}

// given a directory, walkDir ingests each file within and, if recur, recursively ingests each file in each subdirectory.
// Halts on the first error.
//
// Single-threaded.
func walkDir(dirpath string, tag string, flags ingestFlags) error {
	// Recursive ingestion is depth-first (entering directories as soon as they are found).

	entries, err := os.ReadDir(dirpath)
	if err != nil {
		clilog.Writer.Warnf("failed to walk directory rooted at %v: %v", dirpath, err)
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() && flags.recursive { // if it is a directory, ignore it or enter it (depending on -r)
			if err := walkDir(path.Join(dirpath, entry.Name()), tag, flags); err != nil {
				return err
			}
		} else if !entry.IsDir() { // if this is a file, ingest it
			// recompose full path and ingest
			if err := ingestFile(path.Join(dirpath, entry.Name()), tag, flags); err != nil {
				return err
			}
		}
	}
	return nil
}

// wrapper for Client.IngestFile that logs and returns the outcome.
func ingestFile(path, tag string, flags ingestFlags) error {
	resp, err := connection.Client.IngestFile(path, tag, flags.src, flags.ignoreTS, flags.localTime)
	if err != nil {
		clilog.Writer.Warnf("failed to ingest file at path %v: %v", path, err)
		return err
	}
	clilog.Writer.Infof("successfully ingested file at path %v (specified tag: %v | returned tags: %v)",
		path, tag, resp.Tags)
	return nil
}

// determineTag figures out which tag to use, following the given priority:
//
// 1) tag included in the pair (parsed from the bare arguments given)
//
// 2) tag embedded into the file (in the case of a GWJSON)
//
// 3) default tag given via --default-tag
//
// ! It is valid for this function to return an empty tag and a nil error.
// This just means the file is a valid GWJSON file and can be ingested with the empty tag.
func determineTag(pth, tag, defaultTag string) (string, error) {
	if tag == "" {
		{
			// check if this is a GWJSON file by attempting to unmarshal it
			f, err := os.Open(pth)
			if err != nil {
				return "", err
			}
			dcdr := json.NewDecoder(f)
			var ste types.StringTagEntry
			// try to decode a single entry (\n delimited)
			if err := dcdr.Decode(&ste); err == nil && ste.Tag != "" {
				// successfully decoded file and read tag; we can leave our tag empty
				return "", nil
			}
		}
		// this is not a gravwell JSON file (or is one, but with an empty tag), try to use the default tag

		// try to fall back to the default tag, otherwise error out
		tag = defaultTag
		if tag == "" {
			return "", errNoTagSpecified
		}
		return tag, nil
	}
	if err := validateTag(tag); err != nil {
		return "", err
	}
	return tag, nil
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
