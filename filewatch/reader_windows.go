//go:build windows
// +build windows

/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package filewatch

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/winevent"
	"github.com/gravwell/gravwell/v3/winevent/wineventlog"
)

const (
	EvtxEngine int = 2

	evtxExtension     = `.evtx`
	buffSize      int = 256 * 1024
)

var (
	magicEvtxHeader = [8]byte{0x45, 0x6c, 0x66, 0x46, 0x69, 0x6c, 0x65, 0x00}
)

func NewReader(cfg ReaderConfig) (Reader, error) {
	switch cfg.Engine {
	case RegexEngine:
		return NewRegexReader(cfg)
	case LineEngine: //default/empty is line reader
		//check if the filetype is .evtx, if it is, force the EvtxReader
		//this ONLY works on windows, its kind of a hack, but i don't want to try and
		//thread the evtx support throughout the rest of the system
		if isEvtxFile(cfg.Fin) {
			return NewEvtxReader(cfg)
		}
		return NewLineReader(cfg)
	case EvtxEngine:
		return NewEvtxReader(cfg)
	}
	return nil, errors.New("Unknown engine")
}

type EvtxReader struct {
	ReaderConfig
	hnd      wineventlog.EvtHandle
	bookmark wineventlog.EvtHandle
	fpath    string
	off      uint64
	buff     []byte
	bb       *bytes.Buffer
}

func NewEvtxReader(cfg ReaderConfig) (evr *EvtxReader, err error) {
	//update the bookmark from the starting index
	var hnd, bookmark wineventlog.EvtHandle
	var fpath string
	fpath = cfg.Fin.Name()
	if bookmark, err = wineventlog.CreateBookmarkFromRecordID(fpath, uint64(cfg.StartIndex)); err != nil {
		return
	}
	if hnd, err = wineventlog.EvtQuery(0, cfg.Fin.Name(), "", wineventlog.EvtQueryFilePath|wineventlog.EvtQueryForwardDirection); err != nil {
		return
	}
	if cfg.StartIndex > 0 {
		if err = winevent.SeekFileToBookmark(hnd, bookmark); err != nil {
			return
		}
	}

	evr = &EvtxReader{
		ReaderConfig: cfg,
		hnd:          hnd,
		bookmark:     bookmark,
		fpath:        cfg.Fin.Name(),
		off:          uint64(cfg.StartIndex),
		buff:         make([]byte, buffSize),
		bb:           bytes.NewBuffer(nil),
	}
	return
}

func (evr *EvtxReader) ID() (FileId, error) {
	return getFileId(evr.ReaderConfig.Fin)
}

func (evr *EvtxReader) FileSize() (sz int64, err error) {
	var fi os.FileInfo
	if fi, err = evr.ReaderConfig.Fin.Stat(); err != nil {
		sz = -1
	} else {
		sz = fi.Size()
	}
	return
}

func (evr *EvtxReader) LastModTime() (t time.Time, err error) {
	var fi os.FileInfo
	if fi, err = evr.ReaderConfig.Fin.Stat(); err == nil {
		t = fi.ModTime()
	}
	return
}

func (evr *EvtxReader) SeekFile(offset int64) (err error) {
	if evr.bookmark != 0 {
		wineventlog.Close(evr.bookmark)
	}
	if evr.bookmark, err = wineventlog.CreateBookmarkFromRecordID(evr.fpath, uint64(offset)); err != nil {
		return
	}
	if err = winevent.SeekFileToBookmark(evr.hnd, evr.bookmark); err != nil {
		return
	}

	evr.off = uint64(offset)
	return
}

func (evr *EvtxReader) Index() int64 {
	id, err := wineventlog.GetRecordIDFromBookmark(evr.bookmark, evr.buff, evr.bb)
	if err != nil {
		return 0
	}
	evr.off = id
	return int64(id)
}

func (evr *EvtxReader) Close() error {
	if err := wineventlog.Close(evr.hnd); err != nil {
		wineventlog.Close(evr.bookmark)
		evr.Fin.Close()
		return err
	} else if err = wineventlog.Close(evr.bookmark); err != nil {
		evr.Fin.Close()
		return err
	}
	evr.hnd = 0
	evr.bookmark = 0
	return evr.Fin.Close()
}

// ReadRemaining is special on the EvtxReader, we won't ever have "dangling" stuff
// so ReadRemaining makes no sense at all, just return A-OK!
func (evr *EvtxReader) ReadRemaining() (ln []byte, err error) {
	return
}

func (evr *EvtxReader) ReadEntry() (ln []byte, ok bool, wasEOF bool, err error) {
	var evtHnds []wineventlog.EvtHandle
	var id uint64
	if evr.hnd == 0 || evr.Fin == nil || evr.bookmark == 0 {
		err = errors.New("not ready")
		return
	}
	if evtHnds, err = wineventlog.EventHandles(evr.hnd, 1); err != nil {
		if err == wineventlog.ERROR_NO_MORE_ITEMS {
			err = nil
			wasEOF = true
		}
		return
	} else if len(evtHnds) != 1 {
		//umm.... ok.. bye?
		return
	}

	h := evtHnds[0]
	evr.bb.Reset()
	if err = wineventlog.RenderEventSimple(h, evr.buff, evr.bb); err != nil {
		return
	}
	ln = append(ln, evr.bb.Bytes()...)
	evr.bb.Reset()
	if err = wineventlog.UpdateBookmarkFromEvent(evr.bookmark, h); err != nil {
		return
	} else if id, err = wineventlog.GetRecordIDFromBookmark(evr.bookmark, evr.buff, evr.bb); err != nil {
		return
	}
	//all good
	evr.off = id
	ok = true
	return
}

func isEvtxFile(f *os.File) bool {
	if f == nil {
		return false
	}
	if strings.ToLower(filepath.Ext(f.Name())) != evtxExtension {
		return false
	}
	//now read some of the header and check it
	header := make([]byte, 8)
	if n, err := f.ReadAt(header, 0); err != nil || n != 8 {
		return false
	}
	return bytes.Compare(header, magicEvtxHeader[:]) == 0
}
