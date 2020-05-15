/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"sync"
	"time"

	"github.com/gravwell/ingest/v3"
	"github.com/gravwell/ingest/v3/entry"
)

var (
	mtx         sync.Mutex
	connClosers map[int]closer
	connId      int
)

type closer interface {
	Close() error
}

func addConn(c closer) int {
	mtx.Lock()
	connId++
	id := connId
	connClosers[connId] = c
	mtx.Unlock()
	return id
}

func delConn(id int) {
	mtx.Lock()
	delete(connClosers, id)
	mtx.Unlock()
}

func connCount() int {
	mtx.Lock()
	defer mtx.Unlock()
	return len(connClosers)
}

type bindConfig struct {
	tag                entry.EntryTag
	ch                 chan *entry.Entry
	wg                 *sync.WaitGroup
	ignoreTS           bool
	localTZ            bool
	igst               *ingest.IngestMuxer
	lastInfoDump       time.Time
	sessionDumpEnabled bool
}

type BindHandler interface {
	Listen(string) error
	Close() error
	Start(int) error
	String() string
}

func (bc bindConfig) Validate() error {
	if bc.ch == nil {
		return errors.New("nil channel")
	}
	if bc.wg == nil {
		return errors.New("nil wait group")
	}
	if bc.igst == nil {
		return errors.New("Nil ingest muxer")
	}
	return nil
}
