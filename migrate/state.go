/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
)

type StateEntry struct {
	Type string
	Obj  json.RawMessage
}

type StateTracker struct {
	sync.Mutex
	fout    *os.File
	enc     *json.Encoder
	entries []StateEntry
}

func NewStateTracker(pth string) (st *StateTracker, err error) {
	var entries []StateEntry
	var fout *os.File
	if entries, err = loadExistingState(pth); err != nil {
		return
	}
	if fout, err = os.OpenFile(pth, os.O_RDWR|os.O_CREATE, 0750); err != nil {
		return
	} else if _, err = fout.Seek(0, os.SEEK_END); err != nil {
		fout.Close()
		return
	}
	st = &StateTracker{
		fout:    fout,
		enc:     json.NewEncoder(fout),
		entries: entries,
	}
	return
}

func loadExistingState(pth string) (ents []StateEntry, err error) {
	var fin *os.File
	if fin, err = os.Open(pth); err != nil {
		//if the file does not exist, then we are fresh, just return
		if os.IsNotExist(err) {
			err = nil
		}
		return
	}
	dec := json.NewDecoder(fin)
	for {
		var ent StateEntry
		if err = dec.Decode(&ent); err != nil {
			if err == io.EOF {
				//reached end of state file
				err = nil
				break
			}
			fin.Close()
			return //something else is wrong
		}
		ents = append(ents, ent)
	}

	err = fin.Close()
	return
}

func (st *StateTracker) Close() (err error) {
	st.Lock()
	err = st.fout.Close()
	st.Unlock()
	return
}

func (st *StateTracker) Add(tp string, value interface{}) (err error) {
	var rob json.RawMessage
	if tp == `` || value == nil {
		return errors.New("invalid parameters")
	}
	if rob, err = json.Marshal(value); err != nil {
		return
	}
	st.Lock()
	defer st.Unlock()
	ent := StateEntry{
		Type: tp,
		Obj:  json.RawMessage(rob),
	}
	err = st.writeEntry(ent)
	return
}

func (st *StateTracker) writeEntry(ent StateEntry) (err error) {
	if err = st.enc.Encode(ent); err == nil {
		st.entries = append(st.entries, ent)
	}
	return
}

type stcallback func(interface{}) error

func (st *StateTracker) GetStates(tp string, value interface{}, cb stcallback) (err error) {
	if cb == nil || tp == `` {
		err = errors.New(`invalid parameters`)
		return
	}
	st.Lock()
	defer st.Unlock()
	for _, s := range st.entries {
		if s.Type != tp {
			continue
		}
		if err = json.Unmarshal(s.Obj, value); err != nil {
			return
		} else if err = cb(value); err != nil {
			return
		}
	}
	return
}
