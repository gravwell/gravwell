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
	"errors"
	"io"
	"os"

	ft "github.com/h2non/filetype"
	"github.com/h2non/filetype/types"
)

const (
	defaultBufferSize int = 2 * 1024 * 1024
)

type ReadResetCloser interface {
	Read([]byte) (int, error)
	Close() error
	Reset() error
}

type buffReadCloser struct {
	r     ReadResetCloser
	b     io.Reader
	bsize int
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

func (brc *buffReadCloser) Reset() (err error) {
	if err = brc.r.Reset(); err != nil {
		return
	}
	brc.b = bufio.NewReaderSize(brc.r, brc.bsize)
	return
}

func OpenBufferedFileReader(p string, buffer int) (r ReadResetCloser, err error) {
	if buffer <= 0 {
		buffer = defaultBufferSize
	}
	if r, err = OpenFileReader(p); err == nil {
		r = &buffReadCloser{
			r:     r,
			b:     bufio.NewReaderSize(r, buffer),
			bsize: buffer,
		}
	}
	return
}

func OpenFileReader(p string) (r ReadResetCloser, err error) {
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

func getReader(fin *os.File, tp types.Type) (r ReadResetCloser, err error) {
	switch tp.MIME.Subtype {
	case `gzip`:
		r, err = newGzipReader(newFileReadResetCloser(fin))
	case `x-bzip2`:
		r, err = newBzip2Reader(newFileReadResetCloser(fin))
	default:
		r = newFileReadResetCloser(fin)
	}
	return
}

type fileResetter struct {
	*os.File
}

func newFileReadResetCloser(fin *os.File) ReadResetCloser {
	return fileResetter{File: fin}
}

func (fr fileResetter) Reset() (err error) {
	var n int64
	if n, err = fr.Seek(0, 0); err != nil {
		return
	} else if n != 0 {
		err = errors.New("Failed to seek")
	}
	return
}

type gzipReader struct {
	fin ReadResetCloser
	rdr *gzip.Reader
}

func newGzipReader(rdr ReadResetCloser) (gzr *gzipReader, err error) {
	gzr = &gzipReader{
		fin: rdr,
	}
	gzr.rdr, err = gzip.NewReader(gzr.fin)
	return
}

func (gr *gzipReader) Read(b []byte) (int, error) {
	return gr.rdr.Read(b)
}

func (gr *gzipReader) Close() error {
	return gr.rdr.Close()
}

func (gr *gzipReader) Reset() (err error) {
	if err = gr.fin.Reset(); err != nil {
		return
	}
	gr.rdr, err = gzip.NewReader(gr.fin)
	return
}

type bzip2Reader struct {
	fin ReadResetCloser
	rdr io.Reader
}

func newBzip2Reader(rdr ReadResetCloser) (bzr *bzip2Reader, err error) {
	bzr = &bzip2Reader{
		fin: rdr,
		rdr: bzip2.NewReader(rdr),
	}
	return
}

func (bzr *bzip2Reader) Read(b []byte) (int, error) {
	return bzr.rdr.Read(b)
}

func (bzr *bzip2Reader) Close() error {
	return bzr.fin.Close()
}

func (bzr *bzip2Reader) Reset() (err error) {
	if err = bzr.fin.Reset(); err == nil {
		bzr.rdr = bzip2.NewReader(bzr.fin)
	}
	return
}
