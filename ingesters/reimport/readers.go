/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	csvTsLayout string = ``
)

var (
	errInvalidColumns = errors.New("invalid csv import columns")
	csvCols           = []string{`Timestamp`, `Source`, `Tag`, `Data`}
)

type tagHandler struct {
	tags        map[string]entry.EntryTag
	igst        *ingest.IngestMuxer
	tagOverride bool
	tag         entry.EntryTag
}

func newTagHandler(igst *ingest.IngestMuxer) tagHandler {
	return tagHandler{
		tags: map[string]entry.EntryTag{},
		igst: igst,
	}
}

func (th *tagHandler) OverrideTags(tg entry.EntryTag) {
	th.tagOverride = true
	th.tag = tg
}

func (th *tagHandler) getTag(v string) (tg entry.EntryTag, err error) {
	var ok bool
	//get the tag
	if th.tagOverride {
		tg = th.tag
	} else if tg, ok = th.tags[v]; !ok {
		if tg, err = th.igst.NegotiateTag(v); err == nil {
			th.tags[v] = tg
		} else {
			err = fmt.Errorf("Failed to get tag %s: %v", v, err)
		}
	}
	return
}

type csvReader struct {
	tagHandler
	rdr *csv.Reader
	row int
}

func newCSVReader(rdr io.Reader, igst *ingest.IngestMuxer) (*csvReader, error) {
	if rdr == nil || igst == nil {
		return nil, errors.New("invalid parameters")
	}
	crdr := csv.NewReader(rdr)
	//read the column names
	columns, err := crdr.Read()
	if err != nil {
		return nil, err
	} else if len(columns) != len(csvCols) {
		return nil, errInvalidColumns
	} else {
		for i := range columns {
			if csvCols[i] != columns[i] {
				return nil, errInvalidColumns
			}
		}
	}

	return &csvReader{
		tagHandler: newTagHandler(igst),
		rdr:        crdr,
	}, nil
}

func (c *csvReader) ReadEntry() (*entry.Entry, error) {
	var ts time.Time
	var tag entry.EntryTag
	//read columns
	cols, err := c.rdr.Read()
	if err != nil {
		return nil, err
	}
	c.row++
	if len(cols) != len(csvCols) {
		return nil, fmt.Errorf("Invalid entry on row %d: %v", c.row, err)
	}
	//parse the timestamp
	if ts, err = time.Parse(time.RFC3339Nano, cols[0]); err != nil {
		return nil, fmt.Errorf("Invalid timestmap on row %d: %v", c.row, err)
	}

	if tag, err = c.getTag(cols[2]); err != nil {
		return nil, fmt.Errorf("%v on row %d", err, c.row)
	}
	return &entry.Entry{
		Tag:  tag,
		SRC:  net.ParseIP(cols[1]), //parse the source
		TS:   entry.FromStandard(ts),
		Data: []byte(cols[3]),
	}, nil

}

type jsonReader struct {
	tagHandler
	rdr *json.Decoder
	cnt int
}

func newJSONReader(rdr io.Reader, igst *ingest.IngestMuxer) (*jsonReader, error) {
	if rdr == nil || igst == nil {
		return nil, errors.New("invalid parameters")
	}
	return &jsonReader{
		tagHandler: newTagHandler(igst),
		rdr:        json.NewDecoder(rdr),
	}, nil
}

// we have some duplicates here so that the decoder can handle both formats
type jsonEntry struct {
	TS        time.Time `json:",omitempty"`
	Timestamp time.Time `json:",omitempty"` //old way
	SRC       net.IP    `json:",omitempty"`
	Src       net.IP    `json:",omitempty"` //old way
	Tag       string
	Data      []byte
}

func (je jsonEntry) ts() (ts entry.Timestamp) {
	if !je.TS.IsZero() {
		ts = entry.FromStandard(je.TS)
	} else if !je.Timestamp.IsZero() {
		ts = entry.FromStandard(je.Timestamp)
	}
	return
}

func (je jsonEntry) src() (src net.IP) {
	if len(je.SRC) > 0 {
		src = je.SRC
	} else if len(je.Src) > 0 {
		src = je.Src
	}
	return
}

func (j *jsonReader) ReadEntry() (ent *entry.Entry, err error) {
	var jent jsonEntry
	var tag entry.EntryTag
	j.cnt++
	if err = j.rdr.Decode(&jent); err != nil {
		if err == io.EOF {
			return
		}
		err = fmt.Errorf("Failed to decode json on row %d: %v", j.cnt, err)
		return
	}
	if tag, err = j.getTag(jent.Tag); err != nil {
		err = fmt.Errorf("%v on row %d", err, j.cnt)
		return
	}
	return &entry.Entry{
		TS:   jent.ts(),
		SRC:  jent.src(),
		Tag:  tag,
		Data: jent.Data,
	}, nil
}
