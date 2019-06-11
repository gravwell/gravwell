/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"io"
	"os"

	ft "github.com/h2non/filetype"
	"github.com/h2non/filetype/types"
)

const (
	defaultBufferSize int = 2 * 1024 * 1024
)

type buffReadCloser struct {
	r io.ReadCloser
	b io.Reader
}

func (brc *buffReadCloser) Read(x []byte) (int, error) {
	if brc.b != nil {
		return brc.b.Read(x)
	}
	return brc.r.Read(x)
}

func (brc *buffReadCloser) Close() error {
	return brc.r.Close()
}

func OpenBufferedFileReader(p string, buffer int) (r io.ReadCloser, err error) {
	if buffer <= 0 {
		buffer = defaultBufferSize
	}
	if r, err = OpenFileReader(p); err == nil {
		r = &buffReadCloser{
			r: r,
			b: bufio.NewReaderSize(r, buffer),
		}
	}
	return
}

func OpenFileReader(p string) (r io.ReadCloser, err error) {
	var fin *os.File
	var tp types.Type
	if tp, err = ft.MatchFile(p); err != nil {
		return
	}
	if fin, err = os.Open(p); err != nil {
		return
	}
	if r, err = getReader(fin, tp); err != nil {
		fin.Close()
	}
	return
}

func getReader(fin *os.File, tp types.Type) (r io.ReadCloser, err error) {
	switch tp.MIME.Subtype {
	case `gzip`:
		r, err = gzip.NewReader(fin)
	case `x-bzip2`:
		r = newReadCloser(fin, bzip2.NewReader(fin))
	default:
		r = fin
	}
	return
}

type rc struct {
	fin *os.File
	rdr io.Reader
}

func newReadCloser(fin *os.File, rdr io.Reader) io.ReadCloser {
	return &rc{
		fin: fin,
		rdr: rdr,
	}
}

func (r *rc) Close() error {
	return r.fin.Close()
}

func (r *rc) Read(b []byte) (int, error) {
	return r.rdr.Read(b)
}
