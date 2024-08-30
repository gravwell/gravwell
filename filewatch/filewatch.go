/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package filewatch implements advanced utilities for tracking file changes within directories.
package filewatch

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

const (
	MAX_QUEUED_EVENTS_PATH = "/proc/sys/fs/inotify/max_queued_events"
	EVENT_QUEUE_BUFFER     = 100000

	newDirTickInterval = 10 * time.Second //how often we check for new directories that may be been removed
)

var (
	ErrNotReady         = errors.New("fsnotify watcher is not ready")
	ErrLocationNotDir   = errors.New("Watched Location is not a directory")
	ErrNoDirsWatched    = errors.New("No locations have been added to the watch list")
	ErrInvalidStateFile = errors.New("State file exists and is not a regular file")
	ErrAlreadyStarted   = errors.New("WatchManager already started")
	ErrFailedSeek       = errors.New("Failed to seek to the start of the states file")
	ErrFsNotifyOverflow = errors.New("FSNotify kernel event buffer overflow")
)

type WatchManager struct {
	mtx *sync.Mutex

	// These have to do with watching files
	fman    *FilterManager
	watcher *fsnotify.Watcher

	// This tracks directories we are watching
	watched map[string][]WatchConfig
	// This tracks directories which have been deleted and we hope will come back
	removed map[string][]WatchConfig

	routineRet chan error
	logger     ingest.IngestLogger
	ctx        context.Context
	cancel     context.CancelFunc
}

type WatchConfig struct {
	FollowerEngineConfig
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
	w, err := fsnotify.NewBufferedWatcher(EVENT_QUEUE_BUFFER)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())

	return &WatchManager{
		mtx:     &sync.Mutex{},
		fman:    fman,
		watcher: w,
		watched: map[string][]WatchConfig{},
		removed: map[string][]WatchConfig{},
		logger:  ingest.NoLogger(),
		ctx:     ctx,
		cancel:  cancel,
	}, nil
}

func (wm *WatchManager) SetMaxFilesWatched(max int) {
	wm.fman.SetMaxFilesWatched(max)
}

func (wm *WatchManager) Context() context.Context {
	return wm.ctx
}

func (wm *WatchManager) SetLogger(lgr ingest.IngestLogger) {
	wm.mtx.Lock()
	defer wm.mtx.Unlock()

	if lgr == nil {
		wm.logger = ingest.NoLogger()
	} else {
		wm.logger = lgr
	}
	wm.fman.SetLogger(wm.logger)
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
	defer wm.cancel() //cancel the context dead last
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
	if wm.watcher == nil || wm.watched == nil || wm.removed == nil {
		return ErrNotReady
	}
	//check that we have been handed a directory
	fi, err := os.Stat(c.BaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			//we are tracking something that doesn't exist yet
			//add it to the watched map and then call remove so that we can re-use
			//all the tested logic
			wm.watched[c.BaseDir] = []WatchConfig{c}
			return wm.removeNoLock(c.BaseDir)
		}
		return err //some other error
	}
	if !fi.IsDir() {
		return ErrLocationNotDir
	}

	//extract all the filters from the match
	fltrs, err := ExtractFilters(c.FileFilter)
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

	if err := wm.fman.AddFilter(c.ConfigName, c.BaseDir, fltrs, c.Hnd, c.FollowerEngineConfig); err != nil {
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
			} else if file.Mode().IsRegular() {
				// go ahead and try to watch this, see if it matches
				fpath := filepath.Join(c.BaseDir, file.Name())
				if _, err := wm.fman.LoadFile(fpath); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (wm *WatchManager) scanRemoved() error {
	wm.mtx.Lock()
	defer wm.mtx.Unlock()
	for k, _ := range wm.removed {
		fi, err := os.Stat(k)
		if err != nil {
			continue
		}
		if fi.IsDir() {
			if err := wm.checkNewDirectoryNoLock(k); err != nil {
				continue
			}
		}
	}
	return nil
}

func (wm *WatchManager) CheckNewDirectory(dir string) error {
	wm.mtx.Lock()
	defer wm.mtx.Unlock()
	return wm.checkNewDirectoryNoLock(dir)
}

func (wm *WatchManager) checkNewDirectoryNoLock(dir string) error {
	if wm.watcher == nil || wm.watched == nil || wm.removed == nil {
		return ErrNotReady
	}

	configs, ok := wm.removed[dir]
	if !ok {
		return nil
	}
	for _, c := range configs {
		wm.addNoLock(c)
	}
	delete(wm.removed, dir)

	toProcess, err := wm.getWatchedFilesInDir(dir)
	if err != nil {
		return err
	}
	for _, f := range toProcess {
		if _, err := wm.fman.LoadFile(f.pth); err != nil {
			return err
		}
	}
	return nil
}

func (wm *WatchManager) Remove(dir string) error {
	wm.mtx.Lock()
	defer wm.mtx.Unlock()
	return wm.removeNoLock(dir)
}

// Take a directory out of the watched list and stick it into the removed list
func (wm *WatchManager) removeNoLock(path string) error {
	if wm.watcher == nil || wm.watched == nil || wm.removed == nil {
		return ErrNotReady
	}
	// See if it's already in the watched list
	configs, ok := wm.watched[path]
	if !ok {
		// not watched, that's fine
		return nil
	}

	if existing, ok := wm.removed[path]; ok {
		// deduplicate configs
	dedupLoop:
		for _, a := range configs {
			for _, b := range existing {
				if a == b {
					continue dedupLoop
				}
			}
			existing = append(existing, a)
		}
		wm.removed[path] = existing
	} else {
		wm.removed[path] = configs
	}

	delete(wm.watched, path)
	wm.fman.RemoveDirectory(path)
	wm.watcher.Remove(path)
	return nil
}

func ExtractFilters(ff string) ([]string, error) {
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
	if len(wm.watched) == 0 && len(wm.removed) == 0 {
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
	//generate list of files that could be processed
	//this could be MANNY files if people are doing some mass ingest
	toProcess, err := wm.getWatchedFileList()
	if err != nil {
		return err
	}
	return wm.fman.LoadFileList(toProcess)
}

// Catchup is used to synchronously process files that have outstanding work to be done.
// The purpose of this is so that when the file follower first starts with a large number of outstanding
// files to be processed, it can more intelligently process them one at a time.
// The real purpose is so that the usecase where a user points the follower at a massive number of files
// during an improt scenario we don't start grabbing things all willy nilly and with high concurrency
// we are better off ordering the work to be done and doing it synchronously
//
// the input parameter is a quit channel, basically wired to the signal handler
// the return values are a shouldQuit(booL) and error
// the boolean value is true when the signal handler fired, telling us that the ingester should exit
func (wm *WatchManager) Catchup(qc chan os.Signal) (bool, error) {
	wm.mtx.Lock()
	defer wm.mtx.Unlock()
	if wm.fman == nil || wm.watcher == nil {
		return false, ErrNotReady
	}
	if wm.routineRet != nil {
		return false, ErrAlreadyStarted
	}

	if len(wm.watched) == 0 {
		//nothing to catch up on, just leave
		return false, nil
	}

	wm.logger.Info("beginning initial file catch-up")
	defer wm.logger.Info("completed initial file catch-up")

	//generate list of files that could be processed
	//this could be MANNY files if people are doing some mass ingest
	toProcess, err := wm.getWatchedFileList()
	if err != nil {
		return false, err
	}

	for _, wf := range toProcess {
		if quit, err := wm.fman.CatchupFile(wf, qc); err != nil || quit {
			return quit, err
		}
	}
	return false, nil
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
	mkdirTicker := time.NewTicker(newDirTickInterval)
	defer mkdirTicker.Stop()

watchRoutine:
	for {
		select {
		case err, ok = <-wm.watcher.Errors:
			if !ok {
				break watchRoutine
			}

			if err == fsnotify.ErrEventOverflow {
				// grab the current value of the queue depth
				d, rerr := os.ReadFile(MAX_QUEUED_EVENTS_PATH)
				if rerr != nil {
					// ignore the error but let us know please
					d = []byte(fmt.Sprintf("could not read from %v", MAX_QUEUED_EVENTS_PATH))
				}
				wm.logger.Error("Filesystem notification error. Events are being dropped. Increase the queued events kernel parameter or decrease the number of tracked files.",
					log.KVErr(err),
					log.KV("max_queued_events", string(d)),
					log.KV("help", "https://docs.gravwell.io/ingesters/file_follow.html#kernel-parameter-tuning"))
			} else {
				wm.logger.Error("fsnotify event queue error", log.KVErr(err)) //log it and continue
			}
			err = nil
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
						wm.logger.Error("failed to find parent directory", log.KV("path", evt.Name))
						continue
					}
					for _, parent := range parents {
						if !parent.Recursive {
							wm.logger.Info("not adding watcher for subdirectory, parent not recusive", log.KV("directory", evt.Name))
							continue
						}
						parent.BaseDir = evt.Name
						wm.logger.Info("adding watcher for subdirectory", log.KV("path", evt.Name), log.KV("patterns", parent.FileFilter))
						if err := wm.Add(parent); err != nil {
							wm.logger.Error("failed to add watcher for new directory", log.KV("path", evt.Name), log.KVErr(err))
							continue
						}
					}
				} else {
					if ok, err := wm.watchNewFile(evt.Name); err != nil {
						wm.logger.Error("failed to watch new file", log.KV("path", evt.Name), log.KVErr(err))
					} else if ok {
						wm.logger.Info("watching new file", log.KV("path", evt.Name))
					}
				}
			} else if evt.Op == fsnotify.Remove {
				// Try to remove it in case this was a directory we're watching
				if err := wm.Remove(evt.Name); err != nil {
					wm.logger.Error("error when trying to unwatch potential removed directory", log.KV("path", evt.Name), log.KVErr(err))
				}
				if ok, err := wm.deleteWatchedFile(evt.Name); err != nil {
					wm.logger.Error("failed to stop watching file", log.KV("path", evt.Name), log.KVErr(err))
				} else if ok {
					wm.logger.Info("stopped watching file", log.KV("path", evt.Name))
				}
			} else if evt.Op == fsnotify.Rename {
				if err := wm.renameWatchedFile(evt.Name); err != nil {
					wm.logger.Error("failed to track renamed file", log.KV("path", evt.Name), log.KVErr(err))
				}
			} else if evt.Op == fsnotify.Write {
				// write event, check if we are watching the file, add if needed
				if !wm.fman.IsWatched(evt.Name) {
					if ok, err := wm.fman.LoadFile(evt.Name); err != nil {
						wm.logger.Error("failed to watch file", log.KV("path", evt.Name), log.KVErr(err))
					} else if ok {
						wm.logger.Info("watching file", log.KV("path", evt.Name))
					}
				}
			}
		case _ = <-tckr.C:
			if err := wm.fman.FlushStates(); err != nil {
				wm.logger.Error("failed to flush states", log.KVErr(err))
			}
		case _ = <-mkdirTicker.C:
			// Scan all the removed directories and see if they got recreated
			if err := wm.scanRemoved(); err != nil {
				wm.logger.Error("failed to check removed directories", log.KVErr(err))
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

type watchedFile struct {
	pth     string
	size    int64
	modTime time.Time
}

func (wm *WatchManager) getWatchedFileList() (wfs []watchedFile, err error) {
	for k := range wm.watched {
		var tmp []watchedFile
		tmp, err = wm.getWatchedFilesInDir(k)
		if err != nil {
			return
		}
		wfs = append(wfs, tmp...)
	}
	//now sort by last modified date as a minor optimization
	if len(wfs) > 0 {
		sort.SliceStable(wfs, func(i, j int) bool {
			return wfs[i].modTime.Before(wfs[i].modTime)
		})
	}
	return
}

func (wm *WatchManager) getWatchedFilesInDir(dir string) (wfs []watchedFile, err error) {
	var fis []os.FileInfo
	if fis, err = ioutil.ReadDir(dir); err != nil {
		err = fmt.Errorf("Failed to initialize %v: %w", dir, err)
		return
	}
	for i := range fis {
		if !fis[i].Mode().IsRegular() {
			continue
		}
		wfs = append(wfs, watchedFile{
			pth:     filepath.Join(dir, fis[i].Name()),
			size:    fis[i].Size(),
			modTime: fis[i].ModTime(),
		})
	}
	return
}
