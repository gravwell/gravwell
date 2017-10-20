/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package filewatch

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/timegrinder"
)

type debugOut func(string, ...interface{})

type LogHandler struct {
	tag      entry.EntryTag
	tg       *timegrinder.TimeGrinder
	ignoreTS bool
	ch       chan *entry.Entry
	dbg      debugOut
}

func NewLogHandler(tag entry.EntryTag, ignoreTS, assumeLocal bool, ch chan *entry.Entry) (*LogHandler, error) {
	var tg *timegrinder.TimeGrinder
	var err error
	if ch == nil {
		return nil, errors.New("output channel is nil")
	}
	if !ignoreTS {
		tg, err = timegrinder.NewTimeGrinder()
		if err != nil {
			return nil, err
		}
		if assumeLocal {
			tg.SetLocalTime()
		}
	}
	if !ignoreTS && tg == nil {
		return nil, errors.New("not timegrinder but not ignoring timestamps")
	}
	return &LogHandler{
		tag:      tag,
		tg:       tg,
		ignoreTS: ignoreTS,
		ch:       ch,
	}, nil
}

func (lh *LogHandler) HandleLog(b []byte, catchts time.Time) error {
	if len(b) == 0 {
		return nil
	}
	var ok bool
	var ts time.Time
	var err error
	if !lh.ignoreTS {
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
		lh.dbg("GOT %v %s\n", ts, string(b))
	}
	lh.ch <- &entry.Entry{
		SRC:  nil, //ingest API will populate this
		TS:   entry.FromStandard(ts),
		Tag:  lh.tag,
		Data: b,
	}
	return nil
}

func (lh *LogHandler) SetLogger(lgr debugOut) {
	lh.dbg = lgr
}
