// +build windows

/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package winevent

import (
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"sync"
)

var (
	ErrMalformedBookmarkFile = errors.New("Malformed bookmark file")
)

type BookmarkHandler struct {
	f         *os.File
	bookmarks map[string]uint64
	mtx       *sync.Mutex
}

func NewBookmark(path string) (*BookmarkHandler, error) {
	fout, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0660)
	if err != nil {
		return nil, err
	}
	bookmarks, err := loadbookmarks(fout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Bookmark file was corrupted, resetting\n")
		zeroFile(fout)
		bookmarks = map[string]uint64{}
	}
	return &BookmarkHandler{
		f:         fout,
		bookmarks: bookmarks,
		mtx:       &sync.Mutex{},
	}, nil
}

func (b *BookmarkHandler) Close() error {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if b.f == nil {
		return nil
	}
	if err := storebookmarks(b.f, b.bookmarks); err != nil {
		b.f.Close()
		b.f = nil
		return err
	}
	if err := b.f.Close(); err != nil {
		b.f = nil
		return err
	}
	b.f = nil
	return nil
}

func (b *BookmarkHandler) Update(name string, val uint64) error {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if b.f == nil {
		return errors.New("Not open")
	}
	b.bookmarks[name] = val
	return nil
}

func (b *BookmarkHandler) Get(name string) (uint64, error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if b.f == nil {
		return 0, errors.New("Not open")
	}
	v, ok := b.bookmarks[name]
	if !ok {
		return 0, nil
	}
	return v, nil
}

func (b *BookmarkHandler) Open() bool {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if b.f == nil {
		return false
	}
	return true
}

func (b *BookmarkHandler) Sync() error {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if b.f == nil {
		return errors.New("Not open")
	}
	return storebookmarks(b.f, b.bookmarks)
}

func loadbookmarks(f *os.File) (map[string]uint64, error) {
	mp := map[string]uint64{}
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	//check the size, if its zero, return an empty map
	if st.Size() == 0 {
		return mp, nil
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	if err := gob.NewDecoder(f).Decode(&mp); err != nil {
		return nil, ErrMalformedBookmarkFile
	}
	return mp, nil
}

func storebookmarks(f *os.File, mp map[string]uint64) error {
	//zero will also seek to the start
	if err := zeroFile(f); err != nil {
		return err
	}
	return gob.NewEncoder(f).Encode(&mp)
}

func zeroFile(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	return nil
}
