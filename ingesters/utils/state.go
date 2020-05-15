/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"encoding/gob"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/dchest/safefile"
)

var (
	ErrInvalidStatePath = errors.New("Invalid state file path")
	ErrNoState          = errors.New("No state available")
)

type State struct {
	sync.Mutex
	fpath string
	perm  os.FileMode
}

func NewState(pth string, perm os.FileMode) (s *State, err error) {
	var fi os.FileInfo
	if pth = filepath.Clean(pth); pth == `.` {
		err = ErrInvalidStatePath
		return
	}
	//check that if it exists, it is a regular file
	if fi, err = os.Stat(pth); err == nil {
		if !fi.Mode().IsRegular() {
			err = ErrInvalidStatePath
			return
		}
	} else {
		if !os.IsNotExist(err) {
			//if its some other non is not exist error, bail
			return
		}
		err = nil //just doesn't exist yet
	}
	s = &State{
		fpath: pth,
		perm:  perm,
	}
	return
}

func (s *State) Write(f interface{}) (err error) {
	s.Lock()
	var fout *safefile.File
	if fout, err = safefile.Create(s.fpath, s.perm); err == nil {
		n := fout.Name() //incase we have to destroy it
		if err = gob.NewEncoder(fout).Encode(f); err != nil {
			fout.File.Close()
			os.Remove(n)
		} else if err = fout.Commit(); err != nil {
			fout.File.Close()
			os.Remove(n)
		}
	}
	s.Unlock()
	return
}

func (s *State) Read(f interface{}) (err error) {
	s.Lock()
	var fin *os.File
	if fin, err = os.Open(s.fpath); err != nil {
		if os.IsNotExist(err) {
			err = ErrNoState
		}
	} else {
		if err = gob.NewDecoder(fin).Decode(f); err == nil {
			err = fin.Close()
		} else {
			fin.Close()
		}
	}
	s.Unlock()
	return
}
