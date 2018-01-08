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
	watched    map[string]bool
	routineRet chan error
	logger     ingest.IngestLogger
}

type WatchConfig struct {
	ConfigName string
	BaseDir    string
	FileFilter string
	Hnd        handler
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
		watched: map[string]bool{},
		logger:  ingest.NoLogger(),
	}, nil
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
	if _, ok := wm.watched[c.BaseDir]; !ok {
		if err := wm.watcher.Add(c.BaseDir); err != nil {
			return err
		}
		wm.watched[c.BaseDir] = true
	}
	if err := wm.fman.AddFilter(c.ConfigName, c.BaseDir, fltrs, c.Hnd); err != nil {
		return err
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
				if ok, err := wm.watchNewFile(evt.Name); err != nil {
					wm.logger.Error("file_follower failed to watch new file %s due to %v", evt.Name, err)
				} else if ok {
					wm.logger.Info("file_follower now watching %s", evt.Name)
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
			}
		case _ = <-tckr.C:
			if err := wm.fman.FlushStates(); err != nil {
				wm.logger.Error("file_follower failed to flush states: %v", err)
			}
		}
	}
	errch <- err
}
