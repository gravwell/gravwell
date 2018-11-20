/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package filewatch

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/gravwell/ingest"
)

var (
	ErrNotReady         = errors.New("fsnotify watcher is not ready")
	ErrLocationNotDir   = errors.New("Watched Location is not a directory")
	ErrNoDirsWatched    = errors.New("No locations have been added to the watch list")
	ErrInvalidStateFile = errors.New("State file exists and is not a regular file")
	ErrAlreadyStarted   = errors.New("WatchManager already started")
	ErrFailedSeek       = errors.New("Failed to seek to the start of the states file")
)

type WatchManager struct {
	mtx        *sync.Mutex
	fman       *FilterManager
	watcher    *fsnotify.Watcher
	watched    map[string][]WatchConfig
	routineRet chan error
	logger     ingest.IngestLogger
}

type WatchConfig struct {
	ConfigName string
	BaseDir    string
	FileFilter string
	Hnd        handler
	Recursive  bool
}

func NewWatcher(stateFilePath string) (*WatchManager, error) {
	fman, err := NewFilterManager(stateFilePath)
	if err != nil {
		return nil, err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &WatchManager{
		mtx:     &sync.Mutex{},
		fman:    fman,
		watcher: w,
		watched: map[string][]WatchConfig{},
		logger:  ingest.NoLogger(),
	}, nil
}

func (wm *WatchManager) SetMaxFilesWatched(max int) {
	wm.fman.SetMaxFilesWatched(max)
}

func (wm *WatchManager) SetLogger(lgr ingest.IngestLogger) {
	wm.mtx.Lock()
	defer wm.mtx.Unlock()

	if lgr == nil {
		wm.logger = ingest.NoLogger()
	} else {
		wm.logger = lgr
	}
}

func (wm *WatchManager) Followers() int {
	wm.mtx.Lock()
	defer wm.mtx.Unlock()
	if wm.fman == nil {
		return 0
	}
	return wm.fman.Followed()
}

func (wm *WatchManager) Filters() int {
	wm.mtx.Lock()
	defer wm.mtx.Unlock()
	if wm.fman == nil {
		return 0
	}
	return wm.fman.Filters()
}

func (wm *WatchManager) Close() error {
	var retCh chan error
	wm.mtx.Lock()
	if wm.watcher != nil {
		if err := wm.watcher.Close(); err != nil {
			wm.mtx.Unlock()
			return err
		}
		if wm.routineRet != nil {
			retCh = wm.routineRet
		}
	}
	wm.mtx.Unlock() //we have to unlock and wait for the routine to exit
	var err error
	if retCh != nil {
		err = <-retCh
		close(retCh)
	}

	//we can lock for the duration of this call
	wm.mtx.Lock()
	defer wm.mtx.Unlock()
	if wm.fman != nil {
		if err := wm.fman.Close(); err != nil {
			return err
		}
	}

	wm.watcher = nil
	wm.fman = nil
	return err
}

func (wm *WatchManager) Add(c WatchConfig) error {
	wm.mtx.Lock()
	defer wm.mtx.Unlock()
	return wm.addNoLock(c)
}

func (wm *WatchManager) addNoLock(c WatchConfig) error {
	if wm.watcher == nil || wm.watched == nil {
		return ErrNotReady
	}
	//check that we have been handed a directory
	fi, err := os.Stat(c.BaseDir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return ErrLocationNotDir
	}

	//extract all the filters from the match
	fltrs, err := extractFilters(c.FileFilter)
	if err != nil {
		return err
	}

	//check if we need to watch the directory
	//we do not add again if it's already in the list
	var doAdd bool
	if existing, ok := wm.watched[c.BaseDir]; !ok {
		doAdd = true
	} else {
		doAdd = true
		for _, e := range existing {
			if e == c {
				doAdd = false
				break
			}
		}
	}

	if doAdd {
		if err := wm.watcher.Add(c.BaseDir); err != nil {
			return err
		}
		wm.watched[c.BaseDir] = append(wm.watched[c.BaseDir], c)
	}

	if err := wm.fman.AddFilter(c.ConfigName, c.BaseDir, fltrs, c.Hnd); err != nil {
		return err
	}
	// Now add the subdirectories
	if c.Recursive {
		f, err := os.Open(c.BaseDir)
		if err != nil {
			return err
		}
		files, err := f.Readdir(0)
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.IsDir() {
				newConfig := c
				newConfig.BaseDir = filepath.Join(c.BaseDir, file.Name())
				wm.addNoLock(newConfig)
			}
		}
	}
	return nil
}

func extractFilters(ff string) ([]string, error) {
	if strings.HasPrefix(ff, "{") && strings.HasSuffix(ff, "}") {
		ff = strings.TrimPrefix(strings.TrimSuffix(ff, "}"), "{")
	}
	flds := strings.Split(ff, ",")
	for _, f := range flds {
		if _, err := filepath.Match(f, "asdf"); err != nil {
			return nil, err
		}
	}
	return flds, nil
}

func (wm *WatchManager) Start() error {
	wm.mtx.Lock()
	defer wm.mtx.Unlock()
	if wm.fman == nil || wm.watcher == nil {
		return ErrNotReady
	}
	if len(wm.watched) == 0 {
		return ErrNoDirsWatched
	}
	if wm.routineRet != nil {
		return ErrAlreadyStarted
	}

	//first scan all files, loading existing states as we go
	if err := wm.initExisting(); err != nil {
		return err
	}

	//then kick off routine watching for new files
	wm.routineRet = make(chan error, 1)
	go wm.routine(wm.routineRet)

	return nil
}

func (wm *WatchManager) initExisting() error {
	//ready all files in the directory, this COULD potentially be millions
	//if someone is dumb enough to drop that many files for follwing in a single directory
	//we will slow down and most likely puke when we attempt to register fsnotify handlers
	//this is an OS/user problem, not ours
	for k := range wm.watched {
		fis, err := ioutil.ReadDir(k)
		if err != nil {
			return fmt.Errorf("Failed to initialize %v: %v", k, err)
		}
		for i := range fis {
			if !fis[i].Mode().IsRegular() {
				continue
			}
			//check if we have a state for this file
			fpath := filepath.Join(k, fis[i].Name())
			//potentially load existing state
			if err := wm.fman.LoadFile(fpath); err != nil {
				return err
			}
		}
	}
	return nil
}

func (wm *WatchManager) watchNewFile(fpath string) (bool, error) {
	wm.mtx.Lock()
	defer wm.mtx.Unlock()
	return wm.fman.NewFollower(fpath)
}

func (wm *WatchManager) deleteWatchedFile(fpath string) (bool, error) {
	wm.mtx.Lock()
	defer wm.mtx.Unlock()
	return wm.fman.RemoveFollower(fpath)
}

func (wm *WatchManager) renameWatchedFile(fpath string) error {
	wm.mtx.Lock()
	defer wm.mtx.Unlock()
	return wm.fman.RenameFollower(fpath)
}

func (wm *WatchManager) routine(errch chan error) {
	var ok bool
	var err error
	tckr := time.NewTicker(time.Minute)
	defer tckr.Stop()

watchRoutine:
	for {
		select {
		case err, ok = <-wm.watcher.Errors:
			//we bail on error, not sure if any of this is recoverable, look into it
			if !ok {
				break watchRoutine
			}
			wm.logger.Error("file_follower filesystem notification error %v", err)
		case evt, ok := <-wm.watcher.Events:
			if !ok {
				break watchRoutine
			}
			if evt.Op == fsnotify.Create {
				fi, err := os.Stat(evt.Name)
				if err != nil {
					continue
				}
				if fi.IsDir() {
					parents, ok := wm.watched[filepath.Dir(evt.Name)]
					if !ok {
						wm.logger.Error("file_follower failed to find parent directory for %s", evt.Name)
						continue
					}
					for _, parent := range parents {
						if !parent.Recursive {
							wm.logger.Info("file_follower not adding watcher for subdirectory %v: parent not recusive", evt.Name)
							continue
						}
						parent.BaseDir = evt.Name
						wm.logger.Info("file_follower adding watcher for subdirectory %v, patterns = %v", evt.Name, parent.FileFilter)
						if err := wm.Add(parent); err != nil {
							wm.logger.Error("file_follower failed to add watcher for new directory %v: %v", evt.Name, err)
							continue
						}
					}
				} else {
					if ok, err := wm.watchNewFile(evt.Name); err != nil {
						wm.logger.Error("file_follower failed to watch new file %s due to %v", evt.Name, err)
					} else if ok {
						wm.logger.Info("file_follower now watching %s", evt.Name)
					}
				}
			} else if evt.Op == fsnotify.Remove {
				if ok, err := wm.deleteWatchedFile(evt.Name); err != nil {
					wm.logger.Error("file_follower failed to stop watching %s due to %v", evt.Name, err)
				} else if ok {
					wm.logger.Info("file_follower stopped watching %s", evt.Name)
				}
			} else if evt.Op == fsnotify.Rename {
				if err := wm.renameWatchedFile(evt.Name); err != nil {
					wm.logger.Error("file_follower failed to track renamed file %s due to %v", evt.Name, err)
				}
			} else if evt.Op == fsnotify.Write {
				// write event, check if we are watching the file, add if needed
				if !wm.fman.IsWatched(evt.Name) {
					if err := wm.fman.LoadFile(evt.Name); err != nil {
						wm.logger.Error("file_follower failed to watch file %s due to %v", evt.Name, err)
					} else {
						wm.logger.Info("file_follower now watching %s", evt.Name)
					}
				}
			}
		case _ = <-tckr.C:
			if err := wm.fman.FlushStates(); err != nil {
				wm.logger.Error("file_follower failed to flush states: %v", err)
			}
		}
	}
	errch <- err
}

// Returns a string containing information about the WatchManager
func (wm *WatchManager) Dump() string {
	var b strings.Builder

	fmt.Fprintf(&b, "Filter manager followers:\n")
	for k, v := range wm.fman.followers {
		fmt.Fprintf(&b, "Follower %v: %#v\n", k, v)
	}
	fmt.Fprintf(&b, "Filter manager states:\n")
	for k, v := range wm.fman.states {
		fmt.Fprintf(&b, "State %v: %d\n", k, *v)
	}

	return b.String()
}
