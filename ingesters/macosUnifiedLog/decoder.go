/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	// layout of the "timestamp" field, e.g. "2026-09-30 10:02:12.345678-0600".
	// Fractional seconds are accepted on parse even though the layout omits them.
	eventTimestampLayout = `2006-01-02 15:04:05Z0700`

	maxPreambleSize = 64 * 1024
)

var (
	errEventTooLarge    = errors.New("event exceeds maximum size")
	errPreambleTooLarge = errors.New("too much non-JSON output before the first event")
	errNoEventTimestamp = errors.New("event has no timestamp")
)

// eventDecoder pulls individual events out of the output of `log stream`.
//
// With --style json, log writes a single pretty-printed JSON array that stays
// open for as long as the stream runs, and may write a status line such as
// `Filtering the log data using "..."` ahead of it. Rather than depending on
// the exact whitespace between elements, the decoder skips anything ahead of
// the first '[' or '{' and then walks the array one element at a time. A bare
// sequence of objects (as produced by --style ndjson) is handled as well.
type eventDecoder struct {
	br       *bufio.Reader
	lr       *limitReader
	dec      *json.Decoder
	maxSize  int64
	started  bool
	inArray  bool
	preamble func(string) // optional, called with each non-JSON line skipped ahead of the events
}

func newEventDecoder(r io.Reader, maxSize int64, preamble func(string)) *eventDecoder {
	br := bufio.NewReader(r)
	return &eventDecoder{
		br:       br,
		lr:       &limitReader{r: br},
		maxSize:  maxSize,
		preamble: preamble,
	}
}

// Next returns the next event compacted onto a single line.
// io.EOF means the output ended cleanly, either between events or by closing the array.
func (d *eventDecoder) Next() ([]byte, error) {
	if !d.started {
		if err := d.start(); err != nil {
			return nil, err
		}
	}
	d.lr.n = d.maxSize
	if d.inArray && !d.dec.More() {
		// More returns false on both the closing ']' and a read error; Token tells them apart
		if _, err := d.dec.Token(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	var raw json.RawMessage
	if err := d.dec.Decode(&raw); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.Grow(len(raw))
	if err := json.Compact(&buf, raw); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// start discards any leading non-JSON output and positions the JSON decoder on the first event.
func (d *eventDecoder) start() error {
	var skipped int
	for {
		b, err := d.br.ReadByte()
		if err != nil {
			return err
		}
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		case '[', '{':
			if err = d.br.UnreadByte(); err != nil {
				return err
			}
			d.dec = json.NewDecoder(d.lr)
			d.lr.n = d.maxSize
			if b == '[' {
				if _, err = d.dec.Token(); err != nil {
					return err
				}
				d.inArray = true
			}
			d.started = true
			return nil
		}
		if err = d.br.UnreadByte(); err != nil {
			return err
		}
		line, n, err := readLine(d.br, maxPreambleSize-skipped)
		if err != nil {
			return err
		}
		if skipped += n; skipped >= maxPreambleSize {
			return errPreambleTooLarge
		}
		if line = strings.TrimSpace(line); line != `` && d.preamble != nil {
			d.preamble(line)
		}
	}
}

// readLine reads through the next newline, returning at most max bytes of the line
// along with the total number of bytes consumed. It stops consuming once max is reached.
func readLine(br *bufio.Reader, max int) (string, int, error) {
	var sb strings.Builder
	var n int
	for n < max {
		frag, err := br.ReadSlice('\n')
		n += len(frag)
		if rem := max - sb.Len(); rem > 0 {
			sb.Write(frag[:min(len(frag), rem)])
		}
		if err == nil {
			break
		} else if err != bufio.ErrBufferFull {
			return ``, n, err
		}
	}
	return sb.String(), n, nil
}

// eventTimestamp extracts and parses the "timestamp" field of a compacted event.
func eventTimestamp(data []byte) (time.Time, error) {
	var ev struct {
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return time.Time{}, err
	} else if ev.Timestamp == `` {
		return time.Time{}, errNoEventTimestamp
	}
	ts, err := time.Parse(eventTimestampLayout, ev.Timestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("unrecognized timestamp %q: %w", ev.Timestamp, err)
	}
	return ts, nil
}

// limitReader caps how much can be read before it is reset. The decoder resets it
// ahead of every event so that a malformed stream, such as an unterminated string,
// fails with errEventTooLarge instead of buffering without bound.
type limitReader struct {
	r io.Reader
	n int64
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, errEventTooLarge
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}
