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
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	defaultMaxLine int = 16 * 1024 * 1024

	// maxIdleCloseTime is the amount of time that data can be sitting in the file
	// where upon a close event we will force a read, basically if you write data to the file
	// and DO NOT terminate with a newline (or whatever the delimiter of choice is) and
	// we are closing the file watcher, if more than this time has passed, we eat the remaining data
	maxIdleCloseTime = time.Second

	// maxIdleDataTime is the same as above, but with out the close event
	// so if data is sitting in the file without a delimiter and we have
	// just been sitting here for that amount of time, then we will consume it
	maxIdleDataTime = 3 * time.Second
)

var (
	ErrNotRunning = errors.New("Not running")
	tickInterval  = time.Second
)

type handler interface {
	HandleLog([]byte, time.Time, string) error
	Tag() string
}

type FileId struct {
	Major uint64
	Minor uint64
}

type FollowerEngineConfig struct {
	Engine     int
	EngineArgs string
}

type FollowerConfig struct {
	FollowerEngineConfig
	BaseName string
	FilePath string
	State    *int64
	FilterID int
	Handler  handler
}

type follower struct {
	FileName
	filterId int
	id       FileId
	lnr      Reader
	state    *int64
	mtx      *sync.Mutex
	running  int32
	err      error
	abortCh  chan bool
	fsn      *fsnotify.Watcher
	wg       *sync.WaitGroup
	lh       handler
	lastAct  time.Time
}

func NewFollower(cfg FollowerConfig) (*follower, error) {
	if cfg.State == nil {
		return nil, errors.New("Invalid file state pointer")
	}
	fin, err := openDeletableFile(cfg.FilePath)
	if err != nil {
		return nil, err
	}
	id, err := getFileId(fin)
	if err != nil {
		fin.Close()
		return nil, err
	}

	if _, err := fin.Seek(*cfg.State, 0); err != nil {
		fin.Close()
		return nil, err
	}
	rdrCfg := ReaderConfig{
		Fin:        fin,
		MaxLineLen: defaultMaxLine,
		StartIndex: *cfg.State,
		Engine:     cfg.Engine,
		EngineArgs: cfg.EngineArgs,
	}
	lnr, err := NewReader(rdrCfg)
	if err != nil {
		fin.Close()
		return nil, err
	}

	wtchr, err := fsnotify.NewWatcher()
	if err != nil {
		lnr.Close()
		return nil, err
	}

	//open the file for reading and get
	return &follower{
		filterId: cfg.FilterID,
		id:       id,
		lnr:      lnr,
		mtx:      &sync.Mutex{},
		wg:       &sync.WaitGroup{},
		fsn:      wtchr,
		lh:       cfg.Handler,
		state:    cfg.State,
		FileName: FileName{
			FilePath: cfg.FilePath,
			BaseName: cfg.BaseName,
		},
		lastAct: time.Now(),
	}, nil
}

func (f *follower) FilterId() int {
	return f.filterId
}

func (f *follower) FileId() FileId {
	return f.id
}

func (f *follower) Sync(qc chan os.Signal) (bool, error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	if f.lnr == nil {
		return false, ErrNotReady
	}
	if f.abortCh != nil || f.running != 0 {
		return false, ErrAlreadyStarted
	}
	for {
		ln, ok, _, err := f.lnr.ReadEntry()
		if err != nil {
			return false, err
		} else if !ok {
			break
		}
		//actually handle the line
		now := time.Now()
		if err := f.lh.HandleLog(ln, now, f.FilePath); err != nil {
			return false, err
		}
		*f.state = f.lnr.Index()
		f.lastAct = now
		select {
		case _ = <-qc:
			f.lastAct = now
			return true, nil //just asked to quit
		default:
		}
	}
	return false, nil
}

func (f *follower) Start() error {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	if f.lnr == nil {
		return ErrNotReady
	}
	if f.abortCh != nil || f.running != 0 {
		return ErrAlreadyStarted
	}
	if err := f.fsn.Add(f.FilePath); err != nil {
		return err
	}
	f.abortCh = make(chan bool, 1)
	f.running = 1
	f.wg.Add(1)
	go f.routine()
	return nil
}

func (f *follower) Stop() error {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	if atomic.LoadInt32(&f.running) == 0 || f.abortCh == nil {
		return nil
	}
	f.stop()
	return nil
}

func (f *follower) stop() {
	if atomic.LoadInt32(&f.running) != 0 {
		f.abortCh <- true
		f.wg.Wait()
	}
	close(f.abortCh)
	f.abortCh = nil
	f.running = 0
}

func (f *follower) Close() error {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	if f.abortCh != nil && atomic.LoadInt32(&f.running) != 0 {
		f.stop()
	}
	if err := f.fsn.Close(); err != nil {
		f.err = err
	}
	if err := f.lnr.Close(); err != nil {
		f.err = err
	}
	return f.err
}

func (f *follower) Running() bool {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	if f.abortCh == nil || atomic.LoadInt32(&f.running) == 0 {
		return false
	}
	return true
}

func (f *follower) IdleDuration() time.Duration {
	return time.Since(f.lastAct)
}

// writeEvent should be set to true if we're calling this as a result of
// receiving an fsnotify for a write event
// If we got a writeEvent and ReadLine returns an EOF, we need to check
// and make sure the file wasn't truncated
func (f *follower) processLines(writeEvent, closing bool) error {
	var hit bool
	for {
		ln, ok, sawEOF, err := f.lnr.ReadEntry()
		if err != nil {
			return err
		}
		if sawEOF && writeEvent {
			// We got an EOF on the file after a write
			fi, err := os.Stat(f.FilePath)
			if err != nil {
				return err
			}
			if fi.Size() < *f.state {
				// the file must have been truncated
				*f.state = 0
				if err = f.lnr.SeekFile(0); err != nil {
					return err
				}
			}
		}
		if !ok && sawEOF {
			// e.g. no trailing newline or delimiter, but what IS there has been sitting for XYZ seconds
			// go ahead and consume it
			var force bool
			if idleTime := time.Since(f.lastAct); idleTime > maxIdleDataTime {
				force = true
			} else if closing && idleTime > maxIdleCloseTime {
				force = true
			}
			if force {
				if ln, err = f.lnr.ReadRemaining(); err != nil {
					return err
				} else if len(ln) > 0 {
					if err = f.lh.HandleLog(ln, time.Now(), f.FilePath); err == nil {
						hit = true
						*f.state = f.lnr.Index()
					}
				}
				return err
			}
			break
		}
		//actually handle the line
		if err := f.lh.HandleLog(ln, time.Now(), f.FilePath); err != nil {
			return err
		}
		*f.state = f.lnr.Index()
		hit = true
	}
	if hit {
		f.lastAct = time.Now()
	}
	return nil
}

func (f *follower) routine() {
	defer f.wg.Done()
	defer func(r *int32) {
		atomic.CompareAndSwapInt32(r, 1, 0)
	}(&f.running)
	tckr := time.NewTicker(tickInterval)
	defer tckr.Stop()

routineLoop:
	for {
		if err := f.processLines(false, false); err != nil {
			f.lnr.Close()
			if !os.IsNotExist(err) {
				f.err = err
			}
			return
		}
		select {
		case err, ok := <-f.fsn.Errors:
			if !ok {
				break routineLoop
			}
			f.err = err
			break routineLoop
		case evt, ok := <-f.fsn.Events:
			if !ok {
				break routineLoop
			}
			if evt.Op == fsnotify.Remove {
				//if the file was removed, we read what we can and bail
				if err := f.processLines(false, true); err != nil {
					if !os.IsNotExist(err) {
						f.err = err
					}
				}
				//On remove we close the liner and bail out
				f.err = f.lnr.Close()
				return
			} else if evt.Op == fsnotify.Write {
				if err := f.processLines(true, false); err != nil {
					f.lnr.Close()
					if !os.IsNotExist(err) {
						f.err = err
					}
					return
				}
			}
		case _ = <-tckr.C:
			//just loop and attempt to get some lines
			//this is purely to deal with race conditions where lines come in when we are starting up
			//causing us to miss the event
			//this whole process is kind of racy, so every iteration we attempt to process lines
		case <-f.abortCh:
			break routineLoop
		}
	}
	//this whole process is kind of racy, so every iteration we attempt to process lines
	if err := f.processLines(false, true); err != nil {
		//check if its just a notexists erro, which Windows version of the liner will throw
		if !os.IsNotExist(err) {
			f.err = err
		}
	}
}
