/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"bytes"
	"sync/atomic"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

var (
	logTimeout time.Duration = time.Second
)

const DefaultLogDepth = 5

// Errorf send an error entry down the line with the gravwell tag
func (im *IngestMuxer) Errorf(format string, args ...interface{}) error {
	return im.ErrorfWithDepth(DefaultLogDepth, format, args...)
}

func (im *IngestMuxer) Warnf(format string, args ...interface{}) error {
	return im.WarnfWithDepth(DefaultLogDepth, format, args...)
}

func (im *IngestMuxer) Infof(format string, args ...interface{}) error {
	return im.InfofWithDepth(DefaultLogDepth, format, args...)
}

// Error send an error entry down the line with the gravwell tag
func (im *IngestMuxer) Error(msg string, args ...rfc5424.SDParam) error {
	return im.ErrorWithDepth(DefaultLogDepth, msg, args...)
}

func (im *IngestMuxer) Warn(msg string, args ...rfc5424.SDParam) error {
	return im.WarnWithDepth(DefaultLogDepth, msg, args...)
}

func (im *IngestMuxer) Info(msg string, args ...rfc5424.SDParam) error {
	return im.InfoWithDepth(DefaultLogDepth, msg, args...)
}

func (im *IngestMuxer) WarnfWithDepth(depth int, format string, args ...interface{}) error {
	if im.lgr != nil {
		return im.lgr.WarnfWithDepth(depth, format, args...)
	}
	return nil
}

func (im *IngestMuxer) InfofWithDepth(depth int, format string, args ...interface{}) error {
	if im.lgr != nil {
		return im.lgr.InfofWithDepth(depth, format, args...)
	}
	return nil
}

func (im *IngestMuxer) ErrorfWithDepth(depth int, format string, args ...interface{}) error {
	if im.lgr != nil {
		return im.lgr.ErrorfWithDepth(depth, format, args...)
	}
	return nil
}

func (im *IngestMuxer) InfoWithDepth(depth int, format string, args ...rfc5424.SDParam) error {
	if im.lgr != nil {
		return im.lgr.InfoWithDepth(depth, format, args...)
	}
	return nil
}

func (im *IngestMuxer) WarnWithDepth(depth int, format string, args ...rfc5424.SDParam) error {
	if im.lgr != nil {
		return im.lgr.WarnWithDepth(depth, format, args...)
	}
	return nil
}

func (im *IngestMuxer) ErrorWithDepth(depth int, format string, args ...rfc5424.SDParam) error {
	if im.lgr != nil {
		return im.lgr.ErrorWithDepth(depth, format, args...)
	}
	return nil
}

func (im *IngestMuxer) Hostname() string {
	return im.hostname
}

func (im *IngestMuxer) Appname() string {
	return im.appname
}

// WriteLog writes a log entry to the muxer, making IngestMuxer compatible
// with the log.Relay interface.
func (im *IngestMuxer) WriteLog(ts time.Time, b []byte) error {
	e := entry.Entry{
		Data: bytes.TrimSpace(b), //we trim leading and trailing newlines and spaces here, they don't belong on actual ingested entries
		TS:   entry.FromStandard(ts),
		Tag:  entry.GravwellTagId,
		SRC:  im.logSourceOverride,
	}
	if atomic.LoadInt32(&im.connHot) == 0 {
		// No hot connections, drop it in the buffer and return
		im.logbuff.Add(e)
		return nil
	}

	blk := im.logbuff.Drain()
	blk = append(blk, e)

	for i := range blk {
		if err := im.WriteEntryTimeout(&blk[i], logTimeout); err != nil {
			// something went wrong, jam whatever's left back into the buffer and bail out
			im.logbuff.AddBlock(blk[i:])
			break
		}
	}
	return nil
}
