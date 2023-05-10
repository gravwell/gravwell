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
	"os"
)

type FileState struct {
	BaseName string
	FilePath string
	State    int64
}

func DecodeStateFile(sf string) (states []FileState, err error) {
	native := map[FileName]*int64{}
	var fin *os.File
	if fin, err = os.Open(sf); err != nil {
		return
	} else if err = gob.NewDecoder(fin).Decode(&native); err != nil {
		fin.Close()
		return
	} else if err = fin.Close(); err != nil {
		return
	}
	for k, v := range native {
		var st int64
		if v != nil {
			st = *v
		}
		states = append(states, FileState{
			BaseName: k.BaseName,
			FilePath: k.FilePath,
			State:    st,
		})
	}
	return
}

func EncodeStateFile(sf string, states []FileState) (err error) {
	native := make(map[FileName]*int64, len(states))
	for _, s := range states {
		native[FileName{BaseName: s.BaseName, FilePath: s.FilePath}] = &s.State
	}
	var fout *os.File
	if fout, err = os.Create(sf); err != nil {
		return
	} else if err = gob.NewEncoder(fout).Encode(native); err != nil {
		fout.Close()
		return
	}
	err = fout.Close()
	return
}
