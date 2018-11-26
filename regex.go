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
	"os"
	"regexp"
)

type RegexReader struct {
	baseReader
	rx  *regexp.Regexp
	scn *bufio.Scanner
}

func NewRegexReader(f *os.File, maxLine int, startIdx int64, args string) (*RegexReader, error) {
	rx, err := regexp.Compile(args)
	if err != nil {
		return nil, err
	}
	br, err := newBaseReader(f, maxLine, startIdx)
	if err != nil {
		return nil, err
	}
	rr := &RegexReader{
		baseReader: br,
		rx:         rx,
	}
	rr.scn = bufio.NewScanner(f)
	rr.scn.Split(rr.splitter)
	rr.scn.Buffer(make([]byte, maxLine), 2*maxLine)
	return rr, nil
}

func (rr *RegexReader) ReadEntry() (ln []byte, ok bool, wasEOF bool, err error) {
	if ok = rr.scn.Scan(); ok {
		ln = rr.scn.Bytes()
	} else {
		if err = rr.scn.Err(); err == nil {
			wasEOF = true
		}
	}

	return
}

func (rr *RegexReader) splitter(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if idx := rr.getREIdx(data); idx > 0 {
		return idx, data[0:idx], nil
	}
	if atEOF {
		return len(data), data, nil
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
			return
		} else {
			r = idxs[1] + idxs2[0]
		}
	}

	return
}
