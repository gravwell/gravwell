/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gravwell/ingest/v3"
)

type stateTracker struct {
	sync.Mutex
	igst     *ingest.IngestMuxer
	stateMap map[string]time.Time
	tempMap  map[string]time.Time

	horizon time.Duration // how long should we wait before pulling stuff out of the state map

	filePath  string
	stateFout *os.File
}

func NewTracker(statePath string, horizon time.Duration, igst *ingest.IngestMuxer) (*stateTracker, error) {
	st := &stateTracker{
		filePath: statePath,
		igst:     igst,
		horizon:  horizon,
	}

	st.initStates()

	return st, nil
}

func (st *stateTracker) initStates() error {
	var fi os.FileInfo
	st.stateMap = map[string]time.Time{}
	st.tempMap = map[string]time.Time{}
	//attempt to open state file
	fi, err := os.Stat(st.filePath)
	if err != nil {
		//ensure error is a "not found" error
		if !os.IsNotExist(err) {
			return fmt.Errorf("state file path is invalid: %v", err)
		}
		//attempt to create the file and get a handle, states will be empty
		st.stateFout, err = os.OpenFile(st.filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0660)
		if err != nil {
			return err
		}
		return nil
	}
	//check that is a regular file
	if !fi.Mode().IsRegular() {
		return ErrInvalidStateFile
	}
	//is a regular file, attempt to open it RW
	st.stateFout, err = os.OpenFile(st.filePath, os.O_RDWR, 0550) //u+rw and g+rw but no nothing else
	if err != nil {
		return fmt.Errorf("Failed to open state file RW: %v", err)
	}
	//we have a valid file, attempt to load states if the file isn't empty
	fi, err = st.stateFout.Stat()
	if err != nil {
		return fmt.Errorf("Failed to stat open file: %v", err)
	}
	if fi.Size() > 0 {
		if err = gob.NewDecoder(st.stateFout).Decode(&st.stateMap); err != nil {
			return fmt.Errorf("Failed to load existing states: %v", err)
		}
	}
	return nil

}

func (st *stateTracker) dumpStatesNoLock() error {
	if st.stateFout == nil {
		return nil
	}
	n, err := st.stateFout.Seek(0, 0)
	if err != nil {
		return err
	}
	if n != 0 {
		return ErrFailedSeek
	}
	if err := st.stateFout.Truncate(0); err != nil {
		return err
	}
	if err := gob.NewEncoder(st.stateFout).Encode(st.stateMap); err != nil {
		return err
	}
	return nil
}

func (st *stateTracker) cleanStatesNoLock() {
	now := time.Now()
	// purge anything that's too old
	for k, v := range st.stateMap {
		if now.Sub(v) > st.horizon {
			delete(st.stateMap, k)
		}
	}
}

func (st *stateTracker) tick() {
	// Sync the muxer while we're here
	err := st.igst.Sync(2 * time.Second)
	if err != nil {
		return
	}

	// transfer from temp map to state map
	for k, v := range st.tempMap {
		st.stateMap[k] = v
	}
	// clean the state map
	st.cleanStatesNoLock()
	// reset the temp map
	st.tempMap = make(map[string]time.Time)
	// dump the map
	st.dumpStatesNoLock()
}

func (st *stateTracker) IdExists(id string) bool {
	st.Lock()
	st.Unlock()
	_, ok := st.stateMap[id]
	if ok {
		return true
	}
	_, ok = st.tempMap[id]
	return ok
}

func (st *stateTracker) RecordId(id string, t time.Time) error {
	st.Lock()
	defer st.Unlock()

	if st.tempMap == nil {
		return errors.New("not yet initialized")
	}
	st.tempMap[id] = t
	return nil
}

func (st *stateTracker) Start() {
	go func() {
		t := time.Tick(30 * time.Second)
		for range t {
			st.tick()
		}
	}()
}

func (st *stateTracker) Close() {
	st.Lock()
	st.tick()
	st.Unlock()
}