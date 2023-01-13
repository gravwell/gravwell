/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	encRaw    string = `raw`
	encJSON   string = `json`
	encSYSLOG string = `syslog`
)

var (
	ErrUnknownType   = errors.New("Unknown entry encoder type")
	ErrInvalidWriter = errors.New("Writer is nil")
)

type EntryEncoder interface {
	Encode(*entry.Entry) error
	Reset(io.Writer)
}

type tagTrans struct {
	mp  map[entry.EntryTag]string
	tgr Tagger
}

func newTagTrans(tgr Tagger) *tagTrans {
	return &tagTrans{
		mp:  make(map[entry.EntryTag]string, 8),
		tgr: tgr,
	}
}

func (tt *tagTrans) TagName(tag entry.EntryTag) (s string) {
	var ok bool
	if s, ok = tt.mp[tag]; ok {
		return
	}
	if s, ok = tt.tgr.LookupTag(tag); ok {
		tt.mp[tag] = s
	}
	return
}

type jsonEncoder struct {
	*json.Encoder
	wtr  io.Writer
	bwtr *bufio.Writer
	tt   *tagTrans
}

func newJSONEncoder(wtr io.Writer, tgr Tagger) (*jsonEncoder, error) {
	if wtr == nil {
		return nil, ErrInvalidWriter
	}
	bwtr := bufio.NewWriter(wtr)
	return &jsonEncoder{
		Encoder: json.NewEncoder(bwtr),
		wtr:     wtr,
		bwtr:    bwtr,
		tt:      newTagTrans(tgr),
	}, nil
}

type tagStringEntry struct {
	*entry.Entry
	Tag string
}

// Encode will throw an empty JSON object rather than nothing on nil entries
func (je *jsonEncoder) Encode(ent *entry.Entry) (err error) {
	if ent == nil {
		if _, err = je.bwtr.WriteString("{}\n"); err != nil {
			return
		}
	} else {
		tse := tagStringEntry{
			Entry: ent,
			Tag:   je.tt.TagName(ent.Tag),
		}
		if err = je.Encoder.Encode(tse); err != nil {
			return
		}
	}
	err = je.bwtr.Flush()
	return
}

func (je *jsonEncoder) Reset(wtr io.Writer) {
	je.wtr = wtr
	je.bwtr.Reset(wtr)
	je.Encoder = json.NewEncoder(je.bwtr)
}

type rawEncoder struct {
	wtr   io.Writer
	bwtr  *bufio.Writer
	delim []byte
}

func newRawEncoder(wtr io.Writer, delim []byte) (*rawEncoder, error) {
	if wtr == nil {
		return nil, ErrInvalidWriter
	}
	return &rawEncoder{
		wtr:   wtr,
		bwtr:  bufio.NewWriter(wtr),
		delim: delim,
	}, nil
}

// Encode will throw an empty JSON object rather than nothing on nil entries
func (re *rawEncoder) Encode(ent *entry.Entry) (err error) {
	if ent != nil && ent.Data != nil {
		if _, err = re.bwtr.Write(ent.Data); err != nil {
			return
		}
	}
	if re.delim != nil {
		if _, err = re.bwtr.Write(re.delim); err != nil {
			return
		}
	}
	err = re.bwtr.Flush()
	return
}

func (re *rawEncoder) Reset(wtr io.Writer) {
	re.wtr = wtr
	re.bwtr.Reset(wtr)
}

type syslogEncoder struct {
	wtr io.Writer
	tt  *tagTrans
}

func newSyslogEncoder(wtr io.Writer, tgr Tagger) (*syslogEncoder, error) {
	if wtr == nil {
		return nil, ErrInvalidWriter
	}
	return &syslogEncoder{
		wtr: wtr,
		tt:  newTagTrans(tgr),
	}, nil
}

// Encode will throw an empty JSON object rather than nothing on nil entries
func (se *syslogEncoder) Encode(ent *entry.Entry) (err error) {
	_, err = fmt.Fprintf(se.wtr, "<134>1 %s gravwell %s - - - %s\n",
		ent.TS.Format(time.RFC3339Nano), se.tt.TagName(ent.Tag), string(ent.Data))
	return
}

func (se *syslogEncoder) Reset(wtr io.Writer) {
	se.wtr = wtr
}
