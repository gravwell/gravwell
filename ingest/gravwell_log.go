/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"sync/atomic"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var (
	logTimeout time.Duration = time.Second
)

type IngestLogger interface {
	Errorf(string, ...interface{}) error
	Warnf(string, ...interface{}) error
	Infof(string, ...interface{}) error
	Error(string, ...rfc5424.SDParam) error
	Warn(string, ...rfc5424.SDParam) error
	Info(string, ...rfc5424.SDParam) error
}

// Errorf send an error entry down the line with the gravwell tag
func (im *IngestMuxer) Errorf(format string, args ...interface{}) error {
	if im.lgr != nil {
		return im.lgr.ErrorfWithDepth(4, format, args...)
	}
	return nil
}

func (im *IngestMuxer) Warnf(format string, args ...interface{}) error {
	if im.lgr != nil {
		return im.lgr.WarnfWithDepth(4, format, args...)
	}
	return nil
}

func (im *IngestMuxer) Infof(format string, args ...interface{}) error {
	if im.lgr != nil {
		return im.lgr.InfofWithDepth(4, format, args...)
	}
	return nil
}

// Error send an error entry down the line with the gravwell tag
func (im *IngestMuxer) Error(msg string, args ...rfc5424.SDParam) error {
	if im.lgr != nil {
		return im.lgr.ErrorWithDepth(4, msg, args...)
	}
	return nil
}

func (im *IngestMuxer) Warn(msg string, args ...rfc5424.SDParam) error {
	if im.lgr != nil {
		return im.lgr.WarnWithDepth(4, msg, args...)
	}
	return nil
}

func (im *IngestMuxer) Info(msg string, args ...rfc5424.SDParam) error {
	if im.lgr != nil {
		return im.lgr.InfoWithDepth(4, msg, args...)
	}
	return nil
}

type nilLogger struct{}

func (n nilLogger) Errorf(s string, i ...interface{}) error                    { return nil }
func (n nilLogger) Warnf(s string, i ...interface{}) error                     { return nil }
func (n nilLogger) Infof(s string, i ...interface{}) error                     { return nil }
func (n nilLogger) ErrorfWithDepth(x int, s string, i ...interface{}) error    { return nil }
func (n nilLogger) WarnfWithDepth(x int, s string, i ...interface{}) error     { return nil }
func (n nilLogger) InfofWithDepth(x int, s string, i ...interface{}) error     { return nil }
func (n nilLogger) Error(a string, i ...rfc5424.SDParam) error                 { return nil }
func (n nilLogger) Warn(a string, i ...rfc5424.SDParam) error                  { return nil }
func (n nilLogger) Info(a string, i ...rfc5424.SDParam) error                  { return nil }
func (n nilLogger) ErrorWithDepth(a int, b string, c ...rfc5424.SDParam) error { return nil }
func (n nilLogger) WarnWithDepth(a int, b string, c ...rfc5424.SDParam) error  { return nil }
func (n nilLogger) InfoWithDepth(a int, b string, c ...rfc5424.SDParam) error  { return nil }
func (n nilLogger) Hostname() string                                           { return `` }
func (n nilLogger) Appname() string                                            { return `` }

func NoLogger() Logger {
	return &nilLogger{}
}

// WriteLog writes a log entry to the muxer, making IngestMuxer compatible
// with the log.Relay interface.
func (im *IngestMuxer) WriteLog(ts time.Time, b []byte) error {
	e := entry.Entry{
		Data: b,
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
