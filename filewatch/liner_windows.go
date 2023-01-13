/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
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
	"syscall"
)

type LineReader struct {
	fpath    string
	currLine []byte
	idx      int64
	maxLine  int
}

func NewLineReader(cfg ReaderConfig) (*LineReader, error) {
	if cfg.Fin == nil {
		return nil, errors.New("Reader is nil")
	}
	if cfg.MaxLineLen < 0 {
		return nil, errors.New("maxline is invalid")
	}
	if cfg.StartIndex < 0 {
		return nil, errors.New("Invalid start index")
	}
	fpath := cfg.Fin.Name()
	return &LineReader{
		fpath:   fpath,
		idx:     cfg.StartIndex,
		maxLine: cfg.MaxLineLen,
	}, nil
}

func (lr *LineReader) SeekFile(offset int64) error {
	lr.idx = offset
	return nil
}

func (lr *LineReader) ReadEntry() (ln []byte, ok bool, sawEOF bool, err error) {
	fin, lerr := openDeletableFile(lr.fpath)
	if lerr != nil {
		if lerr == syscall.ERROR_ACCESS_DENIED {
			lerr = syscall.ERROR_PATH_NOT_FOUND
		}
		err = lerr
		return
	}
	n, lerr := fin.Seek(lr.idx, os.SEEK_SET)
	if lerr != nil {
		err = lerr
		return
	}
	if n != lr.idx {
		err = errors.New("Failed to seek to file on ReadLine")
		return
	}
	defer fin.Close()
	brdr := bufio.NewReader(fin)
	for {
		//ReadBytes garuntees that it returns err == nil ONLY when the results hit the delimiter
		b, lerr := brdr.ReadBytes(byte('\n'))
		//legit error
		if lerr != nil && lerr != io.EOF {
			err = lerr //set the error for return
			break
		}
		if lerr == io.EOF {
			sawEOF = true
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
	return nil
}
