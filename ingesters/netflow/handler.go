/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
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

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var (
	ErrAlreadyListening = errors.New("Already listening")
	ErrAlreadyClosed    = errors.New("Already closed")
	ErrNotReady         = errors.New("Not Ready")
)

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
