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
	"io"
	"regexp"
	"time"
)

type RegexReader struct {
	baseReader
	rx       *regexp.Regexp
	brdr     *bufio.Reader
	currLine []byte
	lastRead time.Time
}

func NewRegexReader(cfg ReaderConfig) (*RegexReader, error) {
	rx, err := regexp.Compile(cfg.EngineArgs)
	if err != nil {
		return nil, err
	}
	br, err := newBaseReader(cfg.Fin, cfg.MaxLineLen, cfg.StartIndex)
	if err != nil {
		return nil, err
	}
	rr := &RegexReader{
		baseReader: br,
		rx:         rx,
		currLine:   make([]byte, 0, cfg.MaxLineLen),
		brdr:       bufio.NewReader(cfg.Fin),
		lastRead:   time.Now(),
	}
	return rr, nil
}

func (rr *RegexReader) ReadEntry() (ln []byte, ok bool, wasEOF bool, err error) {
	// Attempt to pull an entry (<pattern> + bytes up to next pattern incidence) from currLine
	// If successful, trim currLine, increment rx.idx, and return the entry
	var newIdx int
	newIdx, ln, err = rr.splitter(rr.currLine)
	if newIdx != 0 {
		// We got some data
		rr.currLine = rr.currLine[newIdx:]
		rr.idx += int64(len(ln))
		ok = true
		return
	}

	// Otherwise, read some bytes
	b := make([]byte, 8*1024)
	n, lerr := rr.brdr.Read(b)
	if lerr != nil && lerr != io.EOF {
		// something bad happened
		err = lerr
		return
	}
	// set wasEOF if appropriate
	if lerr == io.EOF {
		wasEOF = true
	}
	if n == 0 {
		// didn't read anything, alas
		return
	}

	// update the last time we managed to read from the file
	rr.lastRead = time.Now()

	// Append bytes to currLine
	rr.currLine = append(rr.currLine, b[:n]...)
	if len(rr.currLine) > rr.maxLine {
		// too long, trim it
		rr.currLine = rr.currLine[len(rr.currLine)-rr.maxLine:]
	}

	// Attempt to pull an entry (<pattern> + bytes up to next pattern incidence) from currLine
	// If successful, trim currLine, increment rx.idx, and return the entry
	newIdx, ln, err = rr.splitter(rr.currLine)
	if newIdx != 0 {
		// We got some data
		rr.currLine = rr.currLine[newIdx:]
		rr.idx += int64(len(ln))
		ok = true
		return
	}

	return
}

func (rr *RegexReader) splitter(data []byte) (int, []byte, error) {
	if len(data) == 0 {
		return 0, nil, nil
	}
	if idx := rr.getREIdx(data); idx > 0 {
		return idx, data[0:idx], nil
	}
	//request more data
	return 0, nil, nil
}

func (rr *RegexReader) getREIdx(data []byte) (r int) {
	r = -1
	//attempt to get the index of our regexp
	idxs := rr.rx.FindIndex(data)
	if idxs == nil || len(idxs) != 2 {
		return
	}
	if idxs[0] > 0 {
		//index is offset into the buffer, we are good
		r = idxs[0]
	} else {
		//index is at location zero, find the next one
		if idxs2 := rr.rx.FindIndex(data[idxs[1]:]); len(idxs2) != 2 {
			// did not find one
			// If it's been a while, just take what we have
			if time.Now().Sub(rr.lastRead) > 5*time.Second {
				r = len(data)
			}
			return
		} else {
			r = idxs[1] + idxs2[0]
		}
	}

	return
}
