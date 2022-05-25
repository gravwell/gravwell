/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/filewatch"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	filesStateType string = `files`
)

type files struct {
	Base_Directory            string // the base directory we will be watching
	File_Filter               string // the glob for pattern matching
	Tag_Name                  string
	Ignore_Timestamps         bool //Just apply the current timestamp to lines as we get them
	Assume_Local_Timezone     bool
	Timestamp_Format_Override string //override the timestamp format
	Timezone_Override         string
	Recursive                 bool // Should we descend into child directories?
	Ignore_Line_Prefix        []string
	Preprocessor              []string
}

func fileJob(cfgName string, ctx context.Context, uc chan string) (err error) {
	//build a list of base directories and globs
	val, ok := cfg.Files[cfgName]
	if !ok {
		return ErrNotFound
	}
	var filelist []string
	lg.Infof("Processing flat file config %s\n", cfgName)
	uc <- fmt.Sprintf("Processing flat file config %s\n", cfgName)
	if filelist, err = getFileList(val, st); err != nil {
		lg.Error("failed to get file list", log.KV("file-processor", cfgName), log.KVErr(err))
		return err
	} else if len(filelist) == 0 {
		uc <- fmt.Sprintf("Config %v has no unprocessed files", cfgName)
		return nil
	}
	//we have files, get the ingester up and rolling
	pproc, err := cfg.Preprocessor.ProcessorSet(igst, val.Preprocessor)
	if err != nil {
		lg.Error("preprocessor construction error", log.KVErr(err))
		return err
	}
	//get the tag for this listener
	tag, err := igst.GetTag(val.Tag_Name)
	if err != nil {
		lg.Error("failed to resolve tag", log.KV("watcher", cfgName), log.KV("tag", val.Tag_Name), log.KVErr(err))
		return err
	}
	var ignore [][]byte
	for _, prefix := range val.Ignore_Line_Prefix {
		if prefix != "" {
			ignore = append(ignore, []byte(prefix))
		}
	}

	var tg *timegrinder.TimeGrinder
	if !val.Ignore_Timestamps {
		tcfg := timegrinder.Config{
			EnableLeftMostSeed: true,
		}
		if tg, err = timegrinder.NewTimeGrinder(tcfg); err != nil {
			lg.Error("failed to create timegrinder", log.KVErr(err))
			return err
		} else if err = cfg.TimeFormat.LoadFormats(tg); err != nil {
			lg.Error("failed to load custom time formats", log.KVErr(err))
			return err
		}
		if val.Timestamp_Format_Override != `` {
			if err = tg.SetFormatOverride(val.Timestamp_Format_Override); err != nil {
				lg.Error("failed to set timestamp override", log.KV("timestampoverride", val.Timestamp_Format_Override), log.KVErr(err))
				return err
			}
		}
		if val.Assume_Local_Timezone && val.Timezone_Override != `` {
			return errors.New("Cannot specify AssumeLocalTZ and TimezoneOverride in the same LogHandlerConfig")
		}
		if val.Assume_Local_Timezone {
			tg.SetLocalTime()
		}
		if val.Timezone_Override != `` {
			if err = tg.SetTimezone(val.Timezone_Override); err != nil {
				lg.Error("failed to set timezone override", log.KV("timezone", val.Timezone_Override), log.KVErr(err))
				return err
			}
		}
	}

	rdrCfg := utils.LineDelimitedStream{
		Proc:           pproc,
		Tag:            tag,
		SRC:            src,
		TG:             tg,
		IgnorePrefixes: ignore,
		BatchSize:      128,
		Verbose:        *verbose,
	}

	var count, size uint64
	var fileCount int
	for _, f := range filelist {
		fileCount++
		var c, s uint64
		var rc io.ReadCloser
		if checkSig(ctx) {
			return nil
		}
		if rc, err = utils.OpenBufferedFileReader(f, 8192); err != nil {
			lg.Error("failed to open file", log.KV("path", f), log.KVErr(err))
			return err
		}
		rdrCfg.Rdr = rc
		if c, s, err = utils.IngestLineDelimitedStream(rdrCfg); err != nil {
			rc.Close()
			lg.Error("failed to ingest file", log.KV("path", f), log.KVErr(err))
			return err
		}
		count += c
		size += s
		if err = rc.Close(); err != nil {
			lg.Error("failed to close file", log.KV("path", f), log.KVErr(err))
			return err
		} else if err = st.Add(filesStateType, fileStatus{Path: f, Count: c, Size: s}); err != nil {
			lg.Error("failed to set status of file", log.KV("path", f), log.KVErr(err))
			return err
		}
		uc <- fmt.Sprintf("Ingested %d files (%d entries, %d bytes)", fileCount, count, size)
		lg.Info("migrated file", log.KV("path", f))
	}
	uc <- fmt.Sprintf("Ingested %d files (%d entries, %d bytes)", fileCount, count, size)
	return nil
}

type fileStatus struct {
	Path  string
	Count uint64
	Size  uint64
}

func getFileList(val *files, st *StateTracker) ([]string, error) {
	var obj fileStatus
	mp := map[string]fileStatus{}
	// populate the list of files that have been handled so far
	if err := st.GetStates(filesStateType, &obj, func(val interface{}) error {
		if val == nil {
			return nil ///ummm ok?
		}
		fs, ok := val.(*fileStatus)
		if !ok {
			return fmt.Errorf("invalid file status decode value %T", val) // this really should not be possible...
		} else if fs == nil {
			return fmt.Errorf("nil file status")
		}
		mp[fs.Path] = *fs
		return nil
	}); err != nil {
		return nil, fmt.Errorf("Failed to decode file states %w", err)
	}
	fltrs, err := filewatch.ExtractFilters(val.File_Filter)
	if err != nil {
		return nil, err
	}
	if val.Recursive {
		return getRecursiveDir(fltrs, val, mp)
	}
	return getSingleDir(fltrs, val, mp)
}

func getSingleDir(fltrs []string, val *files, mp map[string]fileStatus) ([]string, error) {
	fl := &fileList{}
	//walk the directory and decide if we should bring the file in
	if des, err := fs.ReadDir(os.DirFS(val.Base_Directory), `.`); err != nil {
		return nil, fmt.Errorf("Failed to read directory %v: %w", val.Base_Directory, err)
	} else {
		for _, de := range des {
			if !de.Type().IsRegular() {
				continue
			} else if matchFile(fltrs, de.Name()) {
				fullPath := filepath.Join(val.Base_Directory, de.Name())
				if _, ok := mp[fullPath]; !ok {
					fl.add(fullPath, de)
				}
			}
		}
	}
	return fl.paths(), nil
}

func getRecursiveDir(fltrs []string, val *files, mp map[string]fileStatus) ([]string, error) {
	fl := &fileList{}
	cb := func(pth string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		} else if de.Type().IsRegular() {
			if matchFile(fltrs, de.Name()) {
				fullPath := filepath.Join(val.Base_Directory, pth)
				if _, ok := mp[fullPath]; !ok {
					fl.add(fullPath, de)
				}
			}
		}
		return nil
	}
	if err := fs.WalkDir(os.DirFS(val.Base_Directory), `.`, cb); err != nil {
		return nil, err
	}
	return fl.paths(), nil
}

func matchFile(fltrs []string, name string) bool {
	for _, f := range fltrs {
		if matched, err := filepath.Match(f, name); err == nil && matched {
			return true
		}
	}
	return false
}

func (f *files) Validate(procs processors.ProcessorConfig) (err error) {
	if len(f.Base_Directory) == 0 {
		return errors.New("No Base-Directory provided")
	}
	if len(f.Tag_Name) == 0 {
		f.Tag_Name = entry.DefaultTagName
	}
	if strings.ContainsAny(f.Tag_Name, ingest.FORBIDDEN_TAG_SET) {
		return errors.New("Invalid characters in the Tag-Name")
	}
	f.Base_Directory = filepath.Clean(f.Base_Directory)
	if f.Timezone_Override != "" {
		if f.Assume_Local_Timezone {
			// cannot do both
			return fmt.Errorf("Cannot specify Assume-Local-Timezone and Timezone-Override")
		}
		if _, err = time.LoadLocation(f.Timezone_Override); err != nil {
			return fmt.Errorf("Invalid timezone override %v: %v", f.Timezone_Override, err)
		}
	}
	if _, err = filewatch.ExtractFilters(f.File_Filter); err != nil {
		return err
	}
	if err = procs.CheckProcessors(f.Preprocessor); err != nil {
		return fmt.Errorf("Files preprocessor invalid: %v", err)
	}
	return
}

func (f files) TimestampOverride() (v string, err error) {
	v = strings.TrimSpace(f.Timestamp_Format_Override)
	return
}

func (f files) TimezoneOverride() string {
	return f.Timezone_Override
}

type fileEnt struct {
	pth string
	mod time.Time
}

type fileList struct {
	lst []fileEnt
}

func (fl *fileList) add(pth string, de fs.DirEntry) {
	var mod time.Time
	if fi, err := de.Info(); err == nil && fi != nil {
		mod = fi.ModTime()
	}
	fl.lst = append(fl.lst, fileEnt{pth: pth, mod: mod})
}

func (fl *fileList) paths() (r []string) {
	if fl == nil || len(fl.lst) == 0 {
		return
	}
	sort.SliceStable(fl.lst, func(i, j int) bool {
		return fl.lst[i].mod.Before(fl.lst[j].mod)
	})
	r = make([]string, 0, len(fl.lst))
	for _, fe := range fl.lst {
		r = append(r, fe.pth)
	}
	return
}
