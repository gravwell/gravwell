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
)

var (
	ErrNotRunning = errors.New("Not running")
	tickInterval  = time.Second
)

type handler interface {
	HandleLog([]byte, time.Time) error
}

type FileId struct {
	Major uint64
	Minor uint64
}

type follower struct {
	FileName
	filterId int
	id       FileId
	lnr      *LineReader
	state    *int64
	mtx      *sync.Mutex
	running  int32
	err      error
	abortCh  chan bool
	fsn      *fsnotify.Watcher
	wg       *sync.WaitGroup
	lh       handler
}

func NewFollower(bname, fpath string, fstate *int64, filterId int, lh handler) (*follower, error) {
	if fstate == nil {
		return nil, errors.New("Invalid file state pointer")
	}
	fin, err := openDeletableFile(fpath)
	if err != nil {
		return nil, err
	}
	id, err := getFileId(fin)
	if err != nil {
		fin.Close()
		return nil, err
	}

	if _, err := fin.Seek(*fstate, 0); err != nil {
		fin.Close()
		return nil, err
	}

	lnr, err := NewLineReader(fin, defaultMaxLine, *fstate)
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
		filterId: filterId,
		id:       id,
		lnr:      lnr,
		mtx:      &sync.Mutex{},
		wg:       &sync.WaitGroup{},
		fsn:      wtchr,
		lh:       lh,
		state:    fstate,
		FileName: FileName{
			FilePath: fpath,
			BaseName: bname,
		},
	}, nil
}

func (f *follower) FilterId() int {
	return f.filterId
}

func (f *follower) FileId() FileId {
	return f.id
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

func (f *follower) processLines() error {
	for {
		ln, ok, err := f.lnr.ReadLine()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		//actually handle the line
		if err := f.lh.HandleLog(ln, time.Now()); err != nil {
			return err
		}
		*f.state = f.lnr.Index()
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
		//this whole process is kind of racy, so every iteration we attempt to process lines
		if err := f.processLines(); err != nil {
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
				//On remove we close the liner and bail out
				f.err = f.lnr.Close()
				return
			}
		case _ = <-tckr.C:
			//just loop and attempt to get some lines
			//this is purely to deal with race conditions where lines come in when we are starting up
			//causing us to miss the event
		case <-f.abortCh:
			break routineLoop
		}
	}
	//this whole process is kind of racy, so every iteration we attempt to process lines
	if err := f.processLines(); err != nil {
		//check if its just a notexists erro, which Windows version of the liner will throw
		if !os.IsNotExist(err) {
			f.err = err
		}
	}
}
