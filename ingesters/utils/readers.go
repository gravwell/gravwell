/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	csvTsLayout string = ``

	JsonFormat string = `json`
	CsvFormat  string = `csv`
)

var (
	errInvalidColumns = errors.New("invalid csv import columns")
	csvCols           = []string{`Timestamp`, `Source`, `Tag`, `Data`}
)

type TagHandler interface {
	OverrideTags(entry.EntryTag)
	GetTag(string) (entry.EntryTag, error)
}

type ingestTagHandler struct {
	tags        map[string]entry.EntryTag
	igst        *ingest.IngestMuxer
	tagOverride bool
	tag         entry.EntryTag
}

func NewIngestTagHandler(igst *ingest.IngestMuxer) TagHandler {
	return &ingestTagHandler{
		tags: map[string]entry.EntryTag{},
		igst: igst,
	}
}

func (th *ingestTagHandler) OverrideTags(tg entry.EntryTag) {
	th.tagOverride = true
	th.tag = tg
}

func (th *ingestTagHandler) GetTag(v string) (tg entry.EntryTag, err error) {
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

type CSVReader struct {
	TagHandler
	rdr *csv.Reader
	row int
}

func NewCSVReader(rdr io.Reader, th TagHandler) (*CSVReader, error) {
	if rdr == nil || th == nil {
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

	return &CSVReader{
		TagHandler: th,
		rdr:        crdr,
	}, nil
}

func (c *CSVReader) ReadEntry() (*entry.Entry, error) {
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

	if tag, err = c.GetTag(cols[2]); err != nil {
		return nil, fmt.Errorf("%v on row %d", err, c.row)
	}
	return &entry.Entry{
		Tag:  tag,
		SRC:  net.ParseIP(cols[1]), //parse the source
		TS:   entry.FromStandard(ts),
		Data: []byte(cols[3]),
	}, nil

}

type JSONReader struct {
	TagHandler
	rdr *json.Decoder
	cnt int
}

func NewJSONReader(rdr io.Reader, th TagHandler) (*JSONReader, error) {
	if rdr == nil || th == nil {
		return nil, errors.New("invalid parameters")
	}
	return &JSONReader{
		TagHandler: th,
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

func (j *JSONReader) ReadEntry() (ent *entry.Entry, err error) {
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
	if tag, err = j.GetTag(jent.Tag); err != nil {
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

type ReimportReader interface {
	ReadEntry() (*entry.Entry, error)
	OverrideTags(tg entry.EntryTag)
}

func GetImportReader(format string, fin io.ReadCloser, th TagHandler) (ir ReimportReader, err error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case CsvFormat:
		if ir, err = NewCSVReader(fin, th); err != nil {
			err = fmt.Errorf("Failed to make CSV reader: %v\n", err)
		}
	case JsonFormat:
		if ir, err = NewJSONReader(fin, th); err != nil {
			err = fmt.Errorf("Failed to make JSON reader: %v\n", err)
		}
	default:
		err = fmt.Errorf("Invalid format %v\n", format)
	}
	return
}

func GetImportFormat(override, fp string) (format string, err error) {
	override = strings.ToLower(strings.TrimSpace(override))
	if override == `` {
		override = filepath.Ext(fp)
	}
	switch override {
	case `.json`:
		fallthrough
	case JsonFormat:
		format = JsonFormat
	case `.csv`:
		fallthrough
	case CsvFormat:
		format = CsvFormat
	default:
		err = fmt.Errorf("Failed to determine input format")
	}
	return
}
