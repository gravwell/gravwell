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
	"io"
)

type LineReader struct {
	baseReader
	brdr     *bufio.Reader
	currLine []byte
}

func NewLineReader(cfg ReaderConfig) (*LineReader, error) {
	br, err := newBaseReader(cfg.Fin, cfg.MaxLineLen, cfg.StartIndex)
	if err != nil {
		return nil, err
	}
	return &LineReader{
		baseReader: br,
		brdr:       bufio.NewReader(cfg.Fin),
	}, nil
}

func (lr *LineReader) ReadEntry() (ln []byte, ok bool, wasEOF bool, err error) {
	for {
		//ReadBytes garuntees that it returns err == nil ONLY when the results hit the delimiter
		b, lerr := lr.brdr.ReadBytes(byte('\n'))
		//legit error
		if lerr != nil && lerr != io.EOF {
			err = lerr //set the error for return
			break
		}
		if lerr == io.EOF {
			wasEOF = true
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
