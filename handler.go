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
	"fmt"
	"os"
	"time"

	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/timegrinder"
)

type debugOut func(string, ...interface{})

type LogHandler struct {
	LogHandlerConfig
	tg  *timegrinder.TimeGrinder
	dbg debugOut
	ch  chan *entry.Entry
}

type LogHandlerConfig struct {
	Tag                     entry.EntryTag
	IgnoreTS                bool
	AssumeLocalTZ           bool
	IgnorePrefixes          [][]byte
	TimestampFormatOverride int
	TimezoneOverride        string
}

func NewLogHandler(cfg LogHandlerConfig, ch chan *entry.Entry) (*LogHandler, error) {
	var tg *timegrinder.TimeGrinder
	var err error
	if ch == nil {
		return nil, errors.New("output channel is nil")
	}
	if !cfg.IgnoreTS {
		tcfg := timegrinder.Config{
			EnableLeftMostSeed: true,
		}
		if cfg.TimestampFormatOverride > 0 {
			tcfg.FormatOverride = cfg.TimestampFormatOverride
		}
		tg, err = timegrinder.NewTimeGrinder(tcfg)
		if err != nil {
			return nil, err
		}
		if cfg.AssumeLocalTZ && cfg.TimezoneOverride != `` {
			return nil, errors.New("Cannot specify AssumeLocalTZ and TimezoneOverride in the same LogHandlerConfig")
		}
		if cfg.AssumeLocalTZ {
			tg.SetLocalTime()
		}
		if cfg.TimezoneOverride != `` {
			err = tg.SetTimezone(cfg.TimezoneOverride)
			if err != nil {
				return nil, err
			}
		}
	}
	if !cfg.IgnoreTS && tg == nil {
		return nil, errors.New("no timegrinder but not ignoring timestamps")
	}
	return &LogHandler{
		LogHandlerConfig: cfg,
		ch:               ch,
		tg:               tg,
	}, nil
}

func (lh *LogHandler) HandleLog(b []byte, catchts time.Time) error {
	if len(b) == 0 {
		return nil
	}
	var ok bool
	var ts time.Time
	var err error
	for _, prefix := range lh.IgnorePrefixes {
		if bytes.HasPrefix(b, prefix) {
			return nil
		}
	}
	if !lh.IgnoreTS {
		ts, ok, err = lh.tg.Extract(b)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Catastrophic timegrinder failure: %v\n", err)
			return err
		}
	}
	if !ok {
		ts = catchts
	}
	if lh.dbg != nil {
		lh.dbg("GOT %s %s\n", ts.Format(time.RFC3339), string(b))
	}
	lh.ch <- &entry.Entry{
		SRC:  nil, //ingest API will populate this
		TS:   entry.FromStandard(ts),
		Tag:  lh.Tag,
		Data: b,
	}
	return nil
}

func (lh *LogHandler) SetLogger(lgr debugOut) {
	lh.dbg = lgr
}
