/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package filewatch

import (
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

type filter struct {
	FollowerEngineConfig
	bname string //name given to the config file
	loc   string //location we are watching
	mtchs []string
	lh    handler
}

func (f *filter) Equal(x filter) bool {
	if f.FollowerEngineConfig != x.FollowerEngineConfig {
		return false
	} else if f.bname != x.bname {
		return false
	} else if f.loc != x.loc {
		return false
	} else if f.lh != x.lh {
		return false
	} else if len(f.mtchs) != len(x.mtchs) {
		return false
	}
	for i := range f.mtchs {
		if f.mtchs[i] != x.mtchs[i] {
			return false
		}
	}

	return true
}

// a unique name that allows multiple IDs pointing at the same file
type FileName struct {
	BaseName string
	FilePath string
}

type FilterManager struct {
	mtx             *sync.Mutex
	filters         []filter
	followers       map[FileName]*follower
	states          map[FileName]*int64
	stateFile       string
	stateFout       *os.File
	maxFilesWatched int
	logger          ingest.IngestLogger
}

func NewFilterManager(stateFile string) (*FilterManager, error) {
	fout, states, err := initStateFile(stateFile)
	if err != nil {
		return nil, err
	}
	if err := cleanStates(states); err != nil {
		fout.Close()
		return nil, err
	}

	return &FilterManager{
		mtx:       &sync.Mutex{},
		stateFile: stateFile,
		stateFout: fout,
		states:    states,
		followers: map[FileName]*follower{},
		logger:    ingest.NoLogger(),
	}, nil
}

func (f *FilterManager) IsWatched(fpath string) bool {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	for _, v := range f.filters {
		//check if we have an active follower
		stid := FileName{
			BaseName: v.bname,
			FilePath: fpath,
		}
		_, ok := f.followers[stid]
		if ok {
			return true
		}
	}
	return false
}

func (fm *FilterManager) SetMaxFilesWatched(max int) {
	fm.mtx.Lock()
	defer fm.mtx.Unlock()
	fm.maxFilesWatched = max
}

func (fm *FilterManager) SetLogger(lgr ingest.IngestLogger) {
	fm.mtx.Lock()
	defer fm.mtx.Unlock()

	if lgr == nil {
		fm.logger = ingest.NoLogger()
	} else {
		fm.logger = lgr
	}
}

// ExpungeOldFiles stops following files until the number of
// currently watched files is 1 less than the maxFilesWatched
// value.
// The caller MUST hold the lock
func (fm *FilterManager) expungeOldFiles() error {
	if fm.maxFilesWatched <= 0 {
		return nil
	}
	if len(fm.followers) < fm.maxFilesWatched {
		return nil
	}

	for len(fm.followers) >= fm.maxFilesWatched {
		var oldest *follower
		for _, f := range fm.followers {
			if oldest == nil || f.IdleDuration() > oldest.IdleDuration() {
				oldest = f
			}
		}

		if oldest == nil {
			return errors.New("Could not find any suitable file to stop watching to add new file.")
		}

		fm.logger.Info("expunging old log file", log.KV("path", oldest.FilePath), log.KV("state", *oldest.state))
		_, err := fm.nolockRemoveFollower(oldest.FilePath, false)
		if err != nil {
			return err
		}
	}
	return nil
}

func (fm *FilterManager) Close() (err error) {
	fm.mtx.Lock()
	defer fm.mtx.Unlock()

	//we have to actually close followers
	for _, v := range fm.followers {
		if lerr := v.Close(); lerr != nil {
			err = appendErr(err, lerr)
		}
	}
	fm.followers = nil

	//just shitcan filters, no need to close anything
	fm.filters = nil

	if err := fm.nolockDumpStates(); err != nil {
		return err
	}
	if err := fm.stateFout.Close(); err != nil {
		return err
	}
	fm.stateFout = nil
	return
}

// Followed returns the current number of following handles
// if a file matches multiple filters, it will be followed multiple
// times.  So this is NOT the number of files, but the number of follows
func (fm *FilterManager) Followed() int {
	fm.mtx.Lock()
	defer fm.mtx.Unlock()
	return len(fm.followers)
}

// Filters returns the current number of installed filters
func (fm *FilterManager) Filters() int {
	fm.mtx.Lock()
	defer fm.mtx.Unlock()
	return len(fm.filters)
}

// FlushStates flushes the current state of followed files to the disk
// periodically flushing states is a good idea, incase the device crashes, or the process is abruptly killed
func (fm *FilterManager) FlushStates() error {
	fm.mtx.Lock()
	defer fm.mtx.Unlock()
	return fm.nolockDumpStates()
}

// nolockDumpStates pushes the current set of states out to a file
// caller MUST HOLD THE LOCK
func (fm *FilterManager) nolockDumpStates() error {
	if fm.stateFout == nil {
		return nil
	}
	n, err := fm.stateFout.Seek(0, 0)
	if err != nil {
		return err
	}
	if n != 0 {
		return ErrFailedSeek
	}
	if err := fm.stateFout.Truncate(0); err != nil {
		return err
	}
	if err := gob.NewEncoder(fm.stateFout).Encode(fm.states); err != nil {
		return err
	}
	return nil
}

func (f *FilterManager) AddFilter(bname, loc string, mtchs []string, lh handler, ecfg FollowerEngineConfig) error {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	fltr := filter{
		FollowerEngineConfig: ecfg,
		bname:                bname,
		loc:                  filepath.Clean(loc),
		mtchs:                mtchs,
		lh:                   lh,
	}
	for i := range f.filters {
		if fltr.Equal(f.filters[i]) {
			return nil
		}
	}
	f.filters = append(f.filters, fltr)
	return nil
}

func (f *FilterManager) RemoveDirectory(path string) error {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	return f.nolockRemoveDirectory(path, true)
}

func (f *FilterManager) nolockRemoveDirectory(path string, purgeState bool) (err error) {
	for k, v := range f.followers {
		if k.BaseName == path {
			delete(f.followers, k)
			if purgeState {
				delete(f.states, k)
			}
			if err = v.Close(); err != nil {
				return
			}
		}
	}
	return
}

func (f *FilterManager) RemoveFollower(fpath string) (bool, error) {
	//get file path and base name
	f.mtx.Lock()
	defer f.mtx.Unlock()
	return f.nolockRemoveFollower(fpath, true)
}

func (f *FilterManager) nolockRemoveFollower(fpath string, purgeState bool) (removed bool, err error) {
	//check filters
	for _, v := range f.filters {
		//check if we have an active follower
		stid := FileName{
			BaseName: v.bname,
			FilePath: fpath,
		}
		fl, ok := f.followers[stid]
		if ok {
			delete(f.followers, stid)
			if purgeState {
				delete(f.states, stid)
			}
			if err = fl.Close(); err != nil {
				return
			}
			removed = true
		}
	}
	return
}

// walk the directory looking for files, pull the file ID and check if it matches the current file ID
func (f *FilterManager) findFileId(base string, mtchs []string, id FileId) (p string, ok bool, err error) {
	var lid FileId
	//walk the the directory
	err = filepath.Walk(base, func(fpath string, fi os.FileInfo, lerr error) (rerr error) {
		if lerr != nil || fi == nil || ok || !fi.Mode().IsRegular() {
			//is fi is nil then the file isn't there and we can continue
			return
		}

		if filepath.Dir(fpath) != base {
			return
		}

		//check if the file matches any filters
		if f.matchFile(mtchs, filepath.Base(fpath)) {
			//matches the filter, see if it matches the ID
			if lid, rerr = getFileIdFromName(fpath); rerr != nil {
				return
			}
			if lid == id {
				p = fpath
				ok = true
			}
		}
		return
	})
	return
}

// RenameFollower is designed to rename a file that is currently being followed
// We first grab the file id that matches the given fpath
// Then we scan the base directory for ALL files and attempt to match the fileId
// if a match is found, we check if it matches the current filter, if not, we delete the follower
// if it does, we update the name and leave.  If no match is found, we delete the follower
func (f *FilterManager) RenameFollower(fpath string) error {
	//get file path and base name
	stid := FileName{
		FilePath: fpath,
	}

	f.mtx.Lock()
	defer f.mtx.Unlock()

	//find the id for the potentially old filename
	var id FileId
	var hit bool
	for _, flw := range f.followers {
		if flw.FilePath == fpath {
			id = flw.FileId()
			hit = true
		}
	}
	if !hit {
		return nil
	}
	//check filters and their base locations to see if the file showed up anywhere else
	var found bool
	for i, v := range f.filters {
		//check if we have an active follower
		stid.BaseName = v.bname
		flw, ok := f.followers[stid]
		if !ok {
			continue
		}

		//check base directory and pattern match
		p, ok, err := f.findFileId(v.loc, v.mtchs, id)
		if err != nil {
			flw.Close()
			delete(f.states, stid)
			delete(f.followers, stid)
			return err
		}
		if ok {
			found = true
			//we found it, make sure its not the same damn file name
			if p == fpath {
				return nil
			}
			//different filter but we must keep tracking
			if flw.FilterId() != i {
				st, ok := f.states[stid]
				if !ok {
					flw.Close()
					delete(f.followers, stid)
					return errors.New("Failed to find old state")
				}
				delete(f.followers, stid)
				delete(f.states, stid)
				if err := flw.Close(); err != nil {
					return err
				}
				fcfg := FollowerConfig{
					BaseName:             v.bname,
					FilePath:             p,
					State:                st,
					FilterID:             i,
					Handler:              v.lh,
					FollowerEngineConfig: v.FollowerEngineConfig,
				}
				if err := f.addFollower(fcfg); err != nil {
					return err
				}
				//return nil
			} else if v.loc == p {
				//just update the names
				delete(f.followers, stid)
				flw.FileName = stid
				st, ok := f.states[stid]
				if !ok {
					flw.Close()
					return errors.New("failed to find state on rename")
				}
				stid.FilePath = p
				f.states[stid] = st
				f.followers[stid] = flw
				//return nil
			}
		}
	}
	//filename was never found, remove it
	if !found {
		if _, err := f.nolockRemoveFollower(fpath, true); err != nil {
			return err
		}
	}
	return nil
}

func (f *FilterManager) NewFollower(fpath string) (bool, error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	return f.launchFollowers(fpath, true) // we are deleting the existing state if its there
}

// addFollower gets a new follower, adds it to our list, and launches its routine
// the caller MUST hold the lock
func (f *FilterManager) addFollower(fcfg FollowerConfig) error {
	f.expungeOldFiles()
	stid := FileName{
		BaseName: fcfg.BaseName,
		FilePath: fcfg.FilePath,
	}
	id, err := getFileIdFromName(fcfg.FilePath)
	if err != nil {
		return err
	}
	if flw, ok := f.followers[stid]; ok {
		if flw.FileId() != id {
			//delete the old follower
			delete(f.followers, stid)
			delete(f.states, stid)
			if err := flw.Close(); err != nil {
				return err
			}
		} else {
			//already watching this file, don't re-add
			return nil
		}
	}
	fl, err := NewFollower(fcfg)
	if err != nil {
		return err
	}
	var tag string
	if fcfg.Handler != nil {
		tag = fcfg.Handler.Tag()
	}
	var state int64
	if fcfg.State != nil {
		state = *fcfg.State
	}
	f.logger.Info("following new file",
		log.KV("path", fcfg.FilePath),
		log.KV("follower", fcfg.BaseName),
		log.KV("tag", tag),
		log.KV("state", state))
	if err := fl.Start(); err != nil {
		fl.Close()
		return err
	}
	f.followers[stid] = fl
	return nil
}

// look for seek infor for the filename, caller MUST HOLD LOCK
func (f *FilterManager) seekInfo(bname, fpath string) *int64 {
	for k, v := range f.states {
		if k.BaseName == bname && k.FilePath == fpath {
			return v
		}
	}
	return nil
}

func (f *FilterManager) addSeekInfo(bname, fpath string) *int64 {
	stid := FileName{
		BaseName: bname,
		FilePath: fpath,
	}
	si := new(int64)
	f.states[stid] = si
	return si
}

// actually kick off the file follower
func (f *FilterManager) launchFollowers(fpath string, deleteState bool) (ok bool, err error) {
	//get ID
	id, err := getFileIdFromName(fpath)
	if err != nil {
		return false, err
	}

	//check if this is just a renaming
	isRename, err := f.checkRename(fpath, id)
	if err != nil {
		return false, err
	} else if isRename {
		return true, nil //just a file renaming, continue
	}

	//get base dir
	fname := filepath.Base(fpath)
	fdir := filepath.Dir(fpath)
	var si *int64

	//swing through all filters and launch a follower for each one that matches
	for i, v := range f.filters {
		//check base directory and pattern match
		if v.loc != fdir || !f.matchFile(v.mtchs, fname) {
			continue
		}
		si = nil
		if !deleteState {
			//see if we have state information for this file
			si = f.seekInfo(v.bname, fpath)
		}
		//if not add it
		if si == nil {
			si = f.addSeekInfo(v.bname, fpath)
		}
		fcfg := FollowerConfig{
			FollowerEngineConfig: v.FollowerEngineConfig,
			BaseName:             v.bname,
			FilePath:             fpath,
			State:                si,
			FilterID:             i,
			Handler:              v.lh,
		}
		if err := f.addFollower(fcfg); err != nil {
			return false, err
		}
		ok = true
	}
	return
}

// checkState will grab a watched file and look it up in the state tracker
// if the file is not known to the state tracker we assume it is new and return that it has work
// if it IS in the state tracker, we check if it is larger than what we logged or smaller.
// any value other than what is currently in the state tracker indicates that this file needs some work
func (f *FilterManager) checkState(wf watchedFile) (si *int64, hasWork bool, err error) {
	//get base dir
	fname := filepath.Base(wf.pth)
	fdir := filepath.Dir(wf.pth)
	//swing through all filters and for each follower that matches, check if the file has work to be done
	for _, v := range f.filters {
		//check base directory and pattern match
		if v.loc != fdir || !f.matchFile(v.mtchs, fname) {
			continue
		}
		//see if we have state information for this file
		if si = f.seekInfo(v.bname, wf.pth); si == nil {
			//no state information, if the size is > 0, go ahead and declare that there is work to do
			if wf.size > 0 {
				hasWork = true
				si = f.addSeekInfo(v.bname, wf.pth)
			}
		} else if *si < wf.size {
			//we have a state, check if there is new data
			hasWork = true
		}
	}
	return
}

// swings through our current set of followers, check if the fileID matches.  If a match is
// found we return true.  This allows us to continue to follow files that are renamed.
// we are given the basename, if a rename is found, search the filters.  If no filter is
// found that matches then we close out the follower and delete the state
// if
// we update the state base name and close out the follower.  If it match
// Caller MUST HOLD THE LOCK
func (f *FilterManager) checkRename(fpath string, id FileId) (isRename bool, err error) {
	var fname string
	var fdir string
	for k, v := range f.followers {
		var removeFollower bool
		if v.FileId() == id {
			fname = filepath.Base(fpath)
			fdir = filepath.Dir(fpath)
			//check if the new name still matches the filter
			filterId := v.FilterId()
			if filterId >= len(f.filters) || filterId < 0 {
				//filter outside of range, delete the follower
				removeFollower = true
			}
			//check the filter glob against the new name
			if f.filters[filterId].loc == fdir && f.matchFile(f.filters[filterId].mtchs, fname) {
				if fpath != k.FilePath {
					//this is just a rename, update the fpath in the follower
					delete(f.states, k)
					delete(f.followers, k)
					k.FilePath = fpath
					v.FilePath = fpath
					f.states[k] = v.state
					f.followers[k] = v
					isRename = true
				}
			} else {
				removeFollower = true
			}
			if removeFollower {
				//this is a move away from the current filter, so delete the follower
				//and delete the state
				if err = v.Close(); err != nil {
					return
				}
				delete(f.states, k)
				delete(f.followers, k)
			}
		}
	}
	return
}

func (f *FilterManager) matchFile(mtchs []string, fname string) (matched bool) {
	for _, m := range mtchs {
		if ok, err := filepath.Match(m, fname); err == nil && ok {
			matched = true
			break
		}
	}
	return
}

// CatchupFile will synchronously consume all outstanding data from the file.
// This function is typically used at startup so that we can linearly process outstanding
// data from files one at a time before turning on all our file followers.  It is a pre-optimization
// to deal with scenarios where the file follower has been offline for an extended period of time
// or a user is attempting import a large amount of data during a migration.
// Catchup will also check the last mod time of a file and use that as the indicator to consume the
// final bytes if there is not terminating delimiter
//
// the returned boolean indicates that the quitchan (qc) has fired, this allows us to pass up
// that the process has been asked to quit.
func (f *FilterManager) CatchupFile(wf watchedFile, qc chan os.Signal) (bool, error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	//get ID
	id, err := getFileIdFromName(wf.pth)
	if err != nil {
		return false, err
	}

	//check if this is just a renaming
	isRename, err := f.checkRename(wf.pth, id)
	if err != nil {
		return false, err
	} else if isRename {
		return false, nil //just a file renaming do not attempt to re-add
	}

	//get base dir
	fname := filepath.Base(wf.pth)
	fdir := filepath.Dir(wf.pth)
	var si *int64

	//swing through all filters and spin up a follower then synchronously process outstanding data
	for i, v := range f.filters {
		var hasWork bool
		//check base directory and pattern match
		if v.loc != fdir || !f.matchFile(v.mtchs, fname) {
			continue
		}
		if si, hasWork, err = f.checkState(wf); err != nil {
			return false, err
		} else if !hasWork {
			continue // no work to do
		}
		//this file needs to be caught up
		fcfg := FollowerConfig{
			FollowerEngineConfig: v.FollowerEngineConfig,
			BaseName:             v.bname,
			FilePath:             wf.pth,
			State:                si,
			FilterID:             i,
			Handler:              v.lh,
		}
		if quit, err := f.catchupFollower(fcfg, qc); err != nil || quit {
			return quit, err
		}
	}
	return false, nil

}

// catchupFollower is a linear operation to get outstanding files up to date.
func (f *FilterManager) catchupFollower(fcfg FollowerConfig, qc chan os.Signal) (bool, error) {
	f.logger.Info("performing initial catch-up preprocessing for file", log.KV("file", fcfg.FilePath))
	if fl, err := NewFollower(fcfg); err != nil {
		return false, err
	} else if quit, err := fl.Sync(qc); err != nil || quit {
		fl.Close()
		return quit, err
	} else if err = fl.Close(); err != nil {
		return false, err
	}
	f.logger.Info("file preprocessed at startup", log.KV("path", fcfg.FilePath))
	return false, nil
}

func (f *FilterManager) LoadFile(fpath string) (bool, error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	var ok bool
	var err error
	if ok, err = f.launchFollowers(fpath, false); err != nil {
		return false, err
	}
	return ok, nil
}

func (f *FilterManager) LoadFileList(lst []watchedFile) error {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	if len(lst) > f.maxFilesWatched {
		lst = lst[0:f.maxFilesWatched]
	}
	for _, wf := range lst {
		var err error
		if _, err = f.launchFollowers(wf.pth, false); err != nil {
			return err
		}
	}
	return nil
}

func appendErr(err, nerr error) error {
	if err == nil {
		return nerr
	}
	return fmt.Errorf("%v : %v", err, nerr)
}

func ReadStateFile(p string) (states map[string]int64, err error) {
	var fi os.FileInfo
	if fi, err = os.Stat(p); err != nil {
		return
	} else if !fi.Mode().IsRegular() {
		err = ErrInvalidStateFile
		return
	}
	var fin *os.File
	if fin, err = os.Open(p); err != nil {
		return
	} else if fi, err = fin.Stat(); err != nil {
		fin.Close()
		return
	} else if fi.Size() > 0 {
		temp := map[FileName]*int64{}
		if err = gob.NewDecoder(fin).Decode(&temp); err != nil {
			err = fmt.Errorf("Failed to load existing states: %v", err)
			fin.Close()
			return
		}
		if len(temp) > 0 {
			states = make(map[string]int64, len(temp))
			for k, v := range temp {
				var offset int64
				if v != nil {
					offset = *v
				}
				states[filepath.Join(k.FilePath, k.BaseName)] = offset
			}
		}
	}
	err = fin.Close()
	return
}

func initStateFile(p string) (fout *os.File, states map[FileName]*int64, err error) {
	var fi os.FileInfo
	states = map[FileName]*int64{}
	//attempt to open state file
	fi, err = os.Stat(p)
	if err != nil {
		//ensure error is a "not found" error
		if !os.IsNotExist(err) {
			err = fmt.Errorf("state file path is invalid: %v", err)
			return
		}
		//attempt to create the file and get a handle, states will be empty
		fout, err = os.OpenFile(p, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0660)
		if err != nil {
			return
		}
		return
	}
	//check that is a regular file
	if !fi.Mode().IsRegular() {
		err = ErrInvalidStateFile
		return
	}
	//is a regular file, attempt to open it RW
	fout, err = os.OpenFile(p, os.O_RDWR, 0550) //u+rw and g+rw but no nothing else
	if err != nil {
		err = fmt.Errorf("Failed to open state file RW: %v", err)
		return
	}
	//we have a valid file, attempt to load states if the file isn't empty
	fi, err = fout.Stat()
	if err != nil {
		err = fmt.Errorf("Failed to stat open file: %v", err)
		return
	}
	if fi.Size() > 0 {
		if err = gob.NewDecoder(fout).Decode(&states); err != nil {
			err = fmt.Errorf("Failed to load existing states: %v", err)
			return
		}
	}
	return
}

func cleanStates(states map[FileName]*int64) error {
	for k, v := range states {
		fi, err := os.Stat(k.FilePath)
		if err != nil {
			if os.IsNotExist(err) {
				//file is gone, delete it
				delete(states, k)
			} else {
				// TODO: decide if we need to specifically check for other errors here
				//return err
			}
		} else {
			if v == nil {
				v = new(int64)
			}
			//if file shrank, we have to assume this was a truncation, so remove the state
			if fi.Size() < *v {
				*v = 0 //reset the size
			}
		}
		//all other cases are just fine, roll
	}
	return nil
}
