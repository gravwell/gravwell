/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/timegrinder"
)

const (
	csvTsLayout string = ``

	JsonFormat string = `json`
	CsvFormat  string = `csv`

	initBuffSize = 4 * 1024 * 1024
	maxBuffSize  = 128 * 1024 * 1024
)

var (
	errInvalidColumns = errors.New("invalid csv import columns")
	csvCols           = []string{`Timestamp`, `Source`, `Tag`, `Data`}
	nlBytes           = []byte("\n")
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

func (c *CSVReader) DisableEVs() {} //does nothing, CSV doesn't support EVs

type JSONReader struct {
	TagHandler
	rdr        *json.Decoder
	cnt        int
	disableEVs bool
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

func (j *JSONReader) DisableEVs() {
	j.disableEVs = true
}

// we have some duplicates here so that the decoder can handle both formats
type jsonEntry struct {
	TS         time.Time `json:",omitempty"`
	Timestamp  time.Time `json:",omitempty"` //old way
	SRC        net.IP    `json:",omitempty"`
	Src        net.IP    `json:",omitempty"` //old way
	Tag        string
	Data       []byte
	Enumerated []types.EnumeratedPair
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
	ent = &entry.Entry{
		TS:   jent.ts(),
		SRC:  jent.src(),
		Tag:  tag,
		Data: jent.Data,
	}
	if !j.disableEVs {
		for _, v := range jent.Enumerated {
			if v.RawValue.Type <= 0xff {
				//some weird type, just cast to a string and roll
				ev := entry.EnumeratedValue{
					Name:  v.Name,
					Value: entry.StringEnumData(v.Value),
				}
				ent.AddEnumeratedValue(ev)
			} else {
				var lerr error
				ev := entry.EnumeratedValue{
					Name: v.Name,
				}
				if ev.Value, lerr = entry.NewEnumeratedData(uint8(v.RawValue.Type), v.RawValue.Data); lerr != nil {
					ev.Value = entry.StringEnumData(v.Value)
				}
				ent.AddEnumeratedValue(ev)
			}
		}
	}

	return
}

type ReimportReader interface {
	ReadEntry() (*entry.Entry, error)
	OverrideTags(tg entry.EntryTag)
	DisableEVs()
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

type LineDelimitedStream struct {
	Rdr                    io.Reader
	Proc                   *processors.ProcessorSet
	Tag                    entry.EntryTag
	TG                     *timegrinder.TimeGrinder
	SRC                    net.IP
	IgnorePrefixes         [][]byte
	IgnoreGlobs            []string
	CleanQuotes            bool
	Verbose                bool
	Quotable               bool
	BatchSize              int
	AttachEnumeratedValues []entry.EnumeratedValue // These will be attached to every entry
}

func IngestLineDelimitedStream(cfg LineDelimitedStream) (uint64, uint64, error) {
	var bts []byte
	var ts time.Time
	var ok bool
	var err error
	var blk []*entry.Entry
	var count, totalBytes uint64
	if cfg.BatchSize > 0 {
		blk = make([]*entry.Entry, 0, cfg.BatchSize)
	}
	ignoreCheck := len(cfg.IgnorePrefixes) > 0 || len(cfg.IgnoreGlobs) > 0
	var prefixes []string
	for _, p := range cfg.IgnorePrefixes {
		prefixes = append(prefixes, string(p))
	}
	ignorer, err := NewIgnorer(prefixes, cfg.IgnoreGlobs)
	if err != nil {
		return 0, 0, err
	}

	scn := bufio.NewScanner(cfg.Rdr)
	if cfg.Quotable {
		scn.Split(quotableSplitter)
	}
	scn.Buffer(make([]byte, initBuffSize), maxBuffSize)

scannerLoop:
	for scn.Scan() {
		if bts = bytes.TrimSuffix(scn.Bytes(), nlBytes); len(bts) == 0 {
			continue
		}
		if cfg.CleanQuotes {
			if bts = trimQuotes(bts); len(bts) == 0 {
				continue
			}
		}
		if ignoreCheck {
			if ignorer.Ignore(bts) {
				continue scannerLoop
			}
		}
		if cfg.TG == nil {
			ts = time.Now()
		} else if ts, ok, err = cfg.TG.Extract(bts); err != nil {
			return count, totalBytes, err
		} else if !ok {
			ts = time.Now()
		}
		ent := &entry.Entry{
			TS:   entry.FromStandard(ts),
			Tag:  cfg.Tag,
			SRC:  cfg.SRC,
			Data: bytes.Clone(bts), // copy due to the scanner
		}
		for i := range cfg.AttachEnumeratedValues {
			ent.AddEnumeratedValue(cfg.AttachEnumeratedValues[i])
		}
		if cfg.BatchSize == 0 {
			if err = cfg.Proc.Process(ent); err != nil {
				return count, totalBytes, err
			}
		} else {
			blk = append(blk, ent)
			if len(blk) >= cfg.BatchSize {
				if err = cfg.Proc.ProcessBatch(blk); err != nil {
					return count, totalBytes, err
				}
				blk = make([]*entry.Entry, 0, cfg.BatchSize)
			}
		}
		if cfg.Verbose {
			fmt.Println(ent.TS, ent.Tag, ent.SRC, string(ent.Data))
		}
		count++
		totalBytes += uint64(len(ent.Data))
	}
	if len(blk) > 0 {
		if err = cfg.Proc.ProcessBatch(blk); err != nil {
			return count, totalBytes, err
		}
	}
	return count, totalBytes, scn.Err()
}

func quotableSplitter(data []byte, atEOF bool) (int, []byte, error) {
	var openQuote bool
	var escaped bool
	var r rune
	var width int
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i := 0; i < len(data); i += width {
		r, width = utf8.DecodeRune(data[i:])
		if escaped {
			//don't care what the character is, we are skipping it
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
		} else if r == '"' {
			openQuote = !openQuote
		} else if r == '\n' && !openQuote {
			// we have our full newline
			return i + 1, dropCR(data[:i]), nil
		}
	}
	if atEOF {
		return len(data), dropCR(data), nil
	}
	//request more data
	return 0, nil, nil
}

func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[0 : len(data)-1]
	}
	return data
}

func trimQuotes(data []byte) []byte {
	if len(data) >= 2 {
		if data[0] == '"' && data[len(data)-1] == '"' {
			data = data[1 : len(data)-1]
		}
	}
	return data
}
