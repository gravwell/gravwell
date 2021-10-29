/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

//gravwell log level type
type gll int

const (
	gravwellOff   gll = 3
	gravwellError gll = 2
	gravwellWarn  gll = 1
	gravwellInfo  gll = 0

	defaultLogLevel = gravwellError
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
	if im.logLevel > gravwellError {
		return nil
	}
	if im.lgr != nil {
		im.lgr.ErrorfWithDepth(4, format, args...)
	}
	return im.gravwellWriteIfHot(gravwellError, fmt.Sprintf(format, args...))
}

func (im *IngestMuxer) Warnf(format string, args ...interface{}) error {
	if im.logLevel > gravwellWarn {
		return nil
	}
	if im.lgr != nil {
		im.lgr.WarnfWithDepth(4, format, args...)
	}
	return im.gravwellWriteIfHot(gravwellWarn, fmt.Sprintf(format, args...))
}

func (im *IngestMuxer) Infof(format string, args ...interface{}) error {
	if im.logLevel > gravwellInfo {
		return nil
	}
	if im.lgr != nil {
		im.lgr.InfofWithDepth(4, format, args...)
	}
	return im.gravwellWriteIfHot(gravwellInfo, fmt.Sprintf(format, args...))
}

// Error send an error entry down the line with the gravwell tag
func (im *IngestMuxer) Error(msg string, args ...rfc5424.SDParam) error {
	if im.logLevel > gravwellError {
		return nil
	}
	if im.lgr != nil {
		im.lgr.ErrorWithDepth(4, msg, args...)
	}
	return im.gravwellWriteStructuredIfHot(gravwellError, msg, args...)
}

func (im *IngestMuxer) Warn(msg string, args ...rfc5424.SDParam) error {
	if im.logLevel > gravwellWarn {
		return nil
	}
	if im.lgr != nil {
		im.lgr.WarnWithDepth(4, msg, args...)
	}
	return im.gravwellWriteStructuredIfHot(gravwellWarn, msg, args...)
}

func (im *IngestMuxer) Info(msg string, args ...rfc5424.SDParam) error {
	if im.logLevel > gravwellInfo {
		return nil
	}
	if im.lgr != nil {
		im.lgr.InfoWithDepth(4, msg, args...)
	}
	return im.gravwellWriteStructuredIfHot(gravwellInfo, msg, args...)
}

func (im *IngestMuxer) gravwellWriteIfHot(level gll, line string) (err error) {
	if atomic.LoadInt32(&im.connHot) == 0 {
		return
	}
	ts := time.Now()
	var data []byte
	if data, err = im.generateLog(ts, level, line); err != nil {
		return
	}
	e := &entry.Entry{
		Data: data,
		TS:   entry.FromStandard(ts),
		Tag:  entry.GravwellTagId,
		SRC:  im.logSourceOverride,
	}

	return im.WriteEntryTimeout(e, logTimeout)
}

func (im *IngestMuxer) gravwellWriteStructuredIfHot(level gll, msg string, sds ...rfc5424.SDParam) (err error) {
	if atomic.LoadInt32(&im.connHot) == 0 {
		return
	}
	ts := time.Now()
	var data []byte
	if data, err = log.GenRFCMessage(ts, level.Priority(), im.hostname, im.appname, log.CallLoc(log.DEFAULT_DEPTH), msg, sds...); err != nil {
		return
	}
	e := &entry.Entry{
		Data: data,
		TS:   entry.FromStandard(ts),
		Tag:  entry.GravwellTagId,
		SRC:  im.logSourceOverride,
	}

	return im.WriteEntryTimeout(e, logTimeout)
}

func (im *IngestMuxer) gravwellWrite(level gll, line string) error {
	ts := time.Now()
	data, err := im.generateLog(ts, level, line)
	if err != nil {
		return err
	}
	e := &entry.Entry{
		Data: data,
		TS:   entry.FromStandard(ts),
		Tag:  entry.GravwellTagId,
		SRC:  im.logSourceOverride,
	}
	return im.WriteEntry(e)
}

func (im *IngestMuxer) generateLog(ts time.Time, level gll, msg string, sds ...rfc5424.SDParam) ([]byte, error) {
	return log.GenRFCMessage(ts, level.Priority(), im.hostname, im.appname, log.CallLoc(log.DEFAULT_DEPTH+1), msg, sds...)
}

func (g gll) String() string {
	switch g {
	case gravwellOff:
		return `OFF`
	case gravwellInfo:
		return `INFO`
	case gravwellWarn:
		return `WARN`
	case gravwellError:
		return `ERROR`
	}
	return `UNKNOWN`
}

func (g gll) Priority() rfc5424.Priority {
	switch g {
	case gravwellOff:
		return 0
	case gravwellInfo:
		return rfc5424.Info
	case gravwellWarn:
		return rfc5424.Warning
	case gravwellError:
		return rfc5424.Error
	}
	return rfc5424.Debug
}

// logLevel converts a string, 'v' into a Gravwell loglevel
func logLevel(v string) gll {
	v = strings.TrimSpace(strings.ToUpper(v))
	if len(v) == 0 {
		return defaultLogLevel
	}
	switch v {
	case `OFF`:
		return gravwellOff
	case `INFO`:
		return gravwellInfo
	case `WARN`:
		return gravwellWarn
	case `ERROR`:
		fallthrough
	default:
		break
	}
	return gravwellError
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