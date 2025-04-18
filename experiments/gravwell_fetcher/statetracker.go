/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"encoding/json"
	"time"

	"errors"
	"fmt"
	"os"
	"sync"
)

type objectTracker struct {
	sync.Mutex
	flushed   bool
	statePath string
	states    map[string]trackedObjects
}
type trackedObjects map[string]trackedObjectState

type trackedObjectState struct {
	Updated    time.Time
	LatestTime time.Time
	Key        string
}

func NewObjectTracker(pth string) (ot *objectTracker, err error) {
	if pth == `` {
		err = errors.New("invalid path")
		return
	}
	states := map[string]trackedObjects{}
	var fin *os.File
	if fin, err = os.Open(pth); err != nil {
		if !os.IsNotExist(err) {
			return
		}
		//all good, just empty
		err = nil
	} else if err = json.NewDecoder(fin).Decode(&states); err != nil {
		fin.Close()
		err = fmt.Errorf("state file is corrupt %w", err)
		return
	} else if err = fin.Close(); err != nil {
		err = fmt.Errorf("failed to close state file %w", err)
		return
	}
	ot = &objectTracker{
		flushed:   true,
		statePath: pth,
		states:    states,
	}
	return
}

func (ot *objectTracker) Flush() (err error) {
	ot.Lock()
	if ot.flushed { //no need to flush
		ot.Unlock()
		return
	}
	bb := bytes.NewBuffer(nil)
	if err = json.NewEncoder(bb).Encode(ot.states); err == nil {
		tpath := ot.statePath + `.temp`
		if err = os.WriteFile(tpath, bb.Bytes(), 0660); err == nil {
			if err = os.Rename(tpath, ot.statePath); err != nil {
				err = fmt.Errorf("failed to update state file with temporary file: %w", err)
			} else {
				ot.flushed = true
			}
			//else all good

		} else {
			err = fmt.Errorf("failed to write temporary state file %w", err)
		}
	} else {
		err = fmt.Errorf("failed to encode states %w", err)
	}
	ot.Unlock()
	return
}

func (ot *objectTracker) Set(fetcher, obj string, state trackedObjectState, forceFlush bool) (err error) {
	ot.Lock()
	ftc, ok := ot.states[fetcher]
	if !ok || ftc == nil {
		ftc = trackedObjects{}
	}
	ftc[obj] = state
	ot.states[fetcher] = ftc
	ot.flushed = false
	ot.Unlock()
	if forceFlush {
		err = ot.Flush()
	}
	return
}

func (ot *objectTracker) Get(fetcher, obj string) (state trackedObjectState, ok bool) {
	var ftc trackedObjects
	ot.Lock()
	if ftc, ok = ot.states[fetcher]; ok && ftc != nil {
		state, ok = ftc[obj]
	}
	ot.Unlock()
	return
}
