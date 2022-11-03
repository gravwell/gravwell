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
)

const (
	maxLine       int = 8 * 1024 * 1024 //8MB
	buffBlockSize int = 4096
)

const (
	LineEngine  int = 0
	RegexEngine int = 1
)

type Reader interface {
	SeekFile(int64) error
	ReadEntry() ([]byte, bool, bool, error)
	Index() int64
	Close() error
}

type ReaderConfig struct {
	Fin        *os.File
	MaxLineLen int
	StartIndex int64
	Engine     int
	EngineArgs string
}

type baseReader struct {
	f       *os.File
	idx     int64
	maxLine int
}

func newBaseReader(f *os.File, maxLine int, startIdx int64) (br baseReader, err error) {
	var n int64
	if f == nil {
		err = errors.New("Reader is nil")
	} else if maxLine < 0 {
		err = errors.New("maxline is invalid")
	} else if startIdx < 0 {
		err = errors.New("Invalid start index")
	} else if n, err = f.Seek(startIdx, 0); err != nil {
		return
	} else if n != startIdx {
		err = errors.New("Failed to seek")
	}
	if err == nil {
		br.f = f
		br.idx = startIdx
		br.maxLine = maxLine
	}
	return
}

func (br *baseReader) SeekFile(offset int64) error {
	_, err := br.f.Seek(offset, 0)
	br.idx = offset
	return err
}

func (br *baseReader) Index() int64 {
	return br.idx
}

func (br *baseReader) Close() error {
	if br.f == nil {
		return nil
	}
	if err := br.f.Close(); err != nil {
		return err
	}
	br.f = nil
	return nil
}
