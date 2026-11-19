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

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/timegrinder"
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

// flow header times outside of this window around now are replaced with now
var defaultHeaderTimeWindow = timegrinder.TimestampWindow{
	MaxPastDelta:   8 * time.Hour,
	MaxFutureDelta: time.Hour,
}

type bindConfig struct {
	tag                entry.EntryTag
	ch                 chan *entry.Entry
	wg                 *sync.WaitGroup
	ignoreTS           bool
	disableTSControl   bool
	tsWindow           timegrinder.TimestampWindow
	localTZ            bool
	igst               *ingest.IngestMuxer
	lastInfoDump       time.Time
	sessionDumpEnabled bool
}

// headerTime turns a flow header export time into an entry timestamp.
// Unless timestamp control is disabled, a malformed or out of window time becomes now.
func (bc bindConfig) headerTime(sec, nsec uint32) entry.Timestamp {
	if bc.ignoreTS {
		return entry.Now()
	}
	ts := entry.UnixTime(int64(sec), int64(nsec))
	if bc.disableTSControl {
		return ts
	}
	if nsec >= uint32(time.Second) || !bc.tsWindow.Valid(time.Unix(int64(sec), int64(nsec))) {
		return entry.Now()
	}
	return ts
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
