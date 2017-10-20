/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package filewatch

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
)

const (
	maxLine       int = 8 * 1024 * 1024 //8MB
	buffBlockSize int = 4096
)

type LineReader struct {
	f        *os.File
	brdr     *bufio.Reader
	currLine []byte
	idx      int64
	maxLine  int
}

func NewLineReader(f *os.File, maxLine int, startIdx int64) (*LineReader, error) {
	if f == nil {
		return nil, errors.New("Reader is nil")
	}
	if maxLine < 0 {
		return nil, errors.New("maxline is invalid")
	}
	if startIdx < 0 {
		return nil, errors.New("Invalid start index")
	}
	return &LineReader{
		f:       f,
		brdr:    bufio.NewReader(f),
		idx:     startIdx,
		maxLine: maxLine,
	}, nil
}

func (lr *LineReader) ReadLine() (ln []byte, ok bool, err error) {
	for {
		//ReadBytes garuntees that it returns err == nil ONLY when the results hit the delimiter
		b, lerr := lr.brdr.ReadBytes(byte('\n'))
		//legit error
		if lerr != nil && lerr != io.EOF {
			err = lerr //set the error for return
			break
		}

		if len(b) == 0 {
			//nothing to read, just leave
			break
		}
		//we got something, add to our index, trim, and check
		lr.idx += int64(len(b))
		b = bytes.TrimRight(b, "\r\n")
		if len(b) == 0 {
			//we just got the ending to a line that we had the beginning of
			if len(lr.currLine) != 0 {
				ln = lr.currLine
				lr.currLine = nil
				ok = true
				return
			}
			//else just an empty line, try again
			continue
		}

		//this is a partial line, add to current and return without a line
		if lerr == io.EOF {
			lr.currLine = append(lr.currLine, b...)
			return
		}

		//we have a legit line with bytes
		//if we had stuff in curr line append it, otherwise just assign
		if len(lr.currLine) != 0 {
			ln = append(lr.currLine, b...)
			lr.currLine = nil
		} else {
			ln = b
		}
		ok = true
		break
	}
	return
}

func (lr *LineReader) Index() int64 {
	return lr.idx
}

func (lr *LineReader) Close() error {
	if lr.f == nil {
		return nil
	}
	if err := lr.f.Close(); err != nil {
		return err
	}
	lr.f = nil
	return nil
}
