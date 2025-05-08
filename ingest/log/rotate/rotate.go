/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package rotate implements log file rotation for the embedded Gravwell loging system
package rotate

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	mb = 1024 * 1024

	defaultMaxSize     = 4 * mb
	defaultMaxHistory  = 3
	defaultCompressOld = true

	gzExt = `.gz`
)

var (
	ErrAlreadyClosed = errors.New("already closed")
)

type FileRotator struct {
	sync.Mutex
	perm       os.FileMode
	pth        string
	baseName   string
	fout       *os.File
	currSize   int64
	maxSize    int64
	maxHistory uint
	compress   bool
}

func Open(pth string, perm os.FileMode) (*FileRotator, error) {
	return OpenEx(pth, perm, defaultMaxSize, defaultMaxHistory, defaultCompressOld)
}

func OpenEx(pth string, perm os.FileMode, maxSize int64, maxHistory uint, compressOld bool) (*FileRotator, error) {
	if maxSize <= 0 {
		maxSize = defaultMaxSize
	}
	if maxHistory == 0 {
		maxHistory = 1
	}

	//clean the filepath
	pth = filepath.Clean(pth)
	_, file := filepath.Split(pth)
	if file == `` {
		return nil, fmt.Errorf("file path does not contain a filename")
	}

	bn, _, ok := getExt(file)
	if !ok {
		return nil, fmt.Errorf("file extension required on path")
	}

	fout, sz, err := openFile(pth, perm)
	if err != nil {
		return nil, err
	}

	//build our object
	fr := &FileRotator{
		perm:       perm,
		pth:        pth,
		baseName:   bn,
		fout:       fout,
		currSize:   sz,
		maxSize:    maxSize,
		maxHistory: maxHistory,
		compress:   compressOld,
	}

	//check if we need to rotate right now
	if fr.currSize >= fr.maxSize {
		if err = fr.rotate(); err != nil {
			fr.Close()
			return nil, fmt.Errorf("failed to rotate log file %s %w", pth, err)
		}
	}

	//hand back our object
	return fr, nil
}

func (fr *FileRotator) Close() (err error) {
	fr.Lock()
	defer fr.Unlock()
	if fr.fout == nil {
		return ErrAlreadyClosed
	}
	if err = fr.fout.Close(); err != nil {
		return
	}
	fr.fout = nil
	return
}

func (fr *FileRotator) Write(buf []byte) (n int, err error) {
	var doRotate bool
	//check if we CAN rotate, we only rotate if the last character is a newline OR a newline carriage return
	fr.Lock()
	if n, err = fr.fout.Write(buf); err == nil {
		fr.currSize += int64(n)
		if fr.currSize >= fr.maxSize && newlineTerminated(buf) {
			doRotate = true
		}
	}
	fr.Unlock()
	if doRotate {
		err = fr.rotate()
	}
	return
}

func newlineTerminated(buf []byte) (ok bool) {
	l := len(buf)
	if l >= 1 && buf[l-1] == '\n' || buf[l-1] == '\r' {
		ok = true
	}
	return
}

func (fr *FileRotator) rotate() (err error) {
	fr.Lock()
	err = fr.rotateNoLock()
	fr.Unlock()
	return
}

func (fr *FileRotator) rotateNoLock() (err error) {
	if fr.maxHistory > 1 {
		if err = fr.rotateHistoryNoLock(); err != nil {
			return
		}
	}
	err = fr.rollCurrentNoLock()
	return
}

type historyFile struct {
	base      string // parent directory, EXAMPLE: /var/log, /opt/gravwell/log/
	orig      string // original filename, EXAMPLE: foo.1.log, foo.2.log.gz
	baseName  string // EXAMPLE: foo
	ext       string // EXAMPLE: .log, or .log.gz
	historyID uint   // 0 (not existent) or 1, 2, 3, 4...
}

func (hf historyFile) origpath() string {
	return filepath.Join(hf.base, hf.orig)
}

func (hf historyFile) path() string {
	return filepath.Join(hf.base, hf.name())
}

func (hf historyFile) name() string {
	if hf.historyID > 0 {
		return fmt.Sprintf("%s.%d%s", hf.baseName, hf.historyID, hf.ext)
	}
	return fmt.Sprintf("%s%s", hf.baseName, hf.ext)
}

func resolveHistory(basePath, filename string) (h historyFile, ok bool) {
	h.orig = filename
	h.base = basePath
	var tempFilename string
	if tempFilename, h.ext, ok = getExt(filename); !ok {
		return
	}
	//check if we can strip the an ID extension
	if ext := filepath.Ext(tempFilename); ext != `` {
		lext := strings.TrimPrefix(ext, ".")
		if id, err := strconv.ParseUint(lext, 10, 64); err == nil && id < math.MaxUint {
			h.historyID = uint(id)
			tempFilename = strings.TrimSuffix(tempFilename, ext)
		}
	}
	h.baseName = tempFilename

	return
}

// getHistoryNoLock takes the base file path and breaks it into a filename and directory.
// We then scan the base path looking for files that are prefixed with the base file name
// and then try to resolve the extension and history id
func (fr *FileRotator) getHistoryNoLock() (r []historyFile, err error) {
	var dents []fs.DirEntry
	dir, file := filepath.Split(fr.pth)
	if dir == `` {
		dir = `.`
	}
	if dents, err = os.ReadDir(dir); err != nil {
		return
	}
	for _, dent := range dents {
		if !dent.Type().IsRegular() {
			continue
		} else if name := dent.Name(); name == file {
			//skip current and unrelated fiels
			continue
		} else if h, ok := resolveHistory(dir, name); !ok {
			continue
		} else if h.baseName != fr.baseName {
			continue
		} else {
			r = append(r, h)
		}
	}
	sort.SliceStable(r, func(i, j int) bool {
		return r[i].historyID < r[j].historyID
	})
	return
}

// rotateHistoryNoLock rotates the history files out, deleting the oldest
// if we had two history files and a maxHistory of , the following actions happen:
//
//	foo.log.4 -> DELETED
//	foo.log.3 -> foo.log.2
//	foo.log.2 -> foo.log.1
//
// this function will get the history and then rotate the history, deleting files that are out of queue.
// if you ask for a history of 3, when this is done there will be 2 history files because we are assuming
// the current is about to be rolled in (to make 3)
func (fr *FileRotator) rotateHistoryNoLock() (err error) {
	var hist []historyFile
	if hist, err = fr.getHistoryNoLock(); err != nil {
		err = fmt.Errorf("failed to get log history for %v %w", fr.pth, err)
		return
	}
	max := fr.maxHistory
	if max > 0 {
		max--
	}
	if uint(len(hist)) >= max {
		toDelete := hist[max:]
		hist = hist[0:max]

		for _, v := range toDelete {
			if err = os.Remove(v.origpath()); err != nil {
				err = fmt.Errorf("failed to remove old file %v %w", v.origpath(), err)
				return
			}
		}
	}
	//short circuit out
	if len(hist) == 0 {
		return
	}

	//iterate in reverse, incrementing the ID and renaming the files
	for i := len(hist) - 1; i >= 0; i-- {
		h := hist[i]
		h.historyID = h.historyID + 1
		if err = os.Rename(h.origpath(), h.path()); err != nil {
			err = fmt.Errorf("failed to rotate %v -> %v %w", h.origpath(), h.path(), err)
			return
		}
	}
	return //all good
}

func (fr *FileRotator) rollCurrentNoLock() (err error) {
	dir, name := filepath.Split(fr.pth)
	h, ok := resolveHistory(dir, name)
	if !ok {
		err = fmt.Errorf("failed to resolve history state of (%v) %v", name, fr.pth)
		return
	}
	h.historyID += 1
	if fr.compress {
		h.ext = h.ext + gzExt
	}
	nf := h.path()     //new path
	of := h.origpath() //old path

	if err = fr.fout.Close(); err != nil {
		err = fmt.Errorf("failed to close %v %w", fr.pth, err)
		return
	}
	if !fr.compress {
		if err = os.Rename(of, nf); err != nil {
			err = fmt.Errorf("failed to rename %v -> %v %w", of, nf, err)
			return
		}
	} else {
		//we are compressing old files, so do that
		if err = compressFile(of, nf, fr.perm); err != nil {
			return
		} else if err = os.Remove(of); err != nil { //destroy the original
			err = fmt.Errorf("failed to remove original file %s after compression %w", of, err)
			return
		}

	}
	if fr.fout, fr.currSize, err = openFile(fr.pth, fr.perm); err != nil {
		err = fmt.Errorf("failed to open %v (%v) %w", fr.pth, fr.perm, err)
	}
	return
}

func openFile(pth string, perm os.FileMode) (fout *os.File, sz int64, err error) {
	//open the file
	if fout, err = os.OpenFile(pth, os.O_CREATE|os.O_WRONLY, perm); err != nil {
		return
	}

	//seek to the end and get the size
	if sz, err = fout.Seek(0, io.SeekEnd); err != nil {
		fout.Close()
		err = fmt.Errorf("Failed to detect filesize %w", err)
	}
	return
}

func compressFile(src, dst string, perm os.FileMode) (err error) {
	var fin, fout *os.File
	var wtr *gzip.Writer
	if fin, err = os.Open(src); err != nil {
		return
	}
	defer fin.Close()
	if fout, err = os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, perm); err != nil {
		return
	}
	defer fout.Close()
	if wtr, err = gzip.NewWriterLevel(fout, gzip.BestCompression); err != nil {
		err = fmt.Errorf("failed to create gzip writer on %v %w", dst, err)
		return
	}
	if _, err = io.Copy(wtr, fin); err == nil {
		err = wtr.Close()
	}
	if err != nil {
		err = fmt.Errorf("failed to compress file %v -> %v %w", src, dst, err)
	}
	return
}

// getExt will grab the extension of a file and strip the leading dot (.)
// if the extension is `.gz` then we strip that and grab the next extension as a special case
func getExt(v string) (base, ext string, ok bool) {
	if ext = filepath.Ext(v); ext == `` {
		base = v
		return //no extension
	}
	base = strings.TrimSuffix(v, ext)

	if ext == gzExt {
		if ext = filepath.Ext(base); ext == `` {
			ext, ok = gzExt, true //just the gz, send it back
			return
		} else if _, lerr := strconv.ParseUint(strings.TrimPrefix(ext, "."), 10, 64); lerr == nil {
			//extensionless, just send back with gz
			ext, ok = gzExt, true //just the gz, send it back
			return
		}
		base = strings.TrimSuffix(base, ext)
		ext = ext + gzExt
	}
	ok = true
	return
}
