/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package timegrinder

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

var (
	ErrMissingName   = errors.New("Missing time extraction name")
	ErrMissingRegex  = errors.New("Missing extraction regular expression")
	ErrMissingFormat = errors.New("Missing extraction time format")
	ErrInvalidFormat = errors.New("Invalid time format, could not format and extract current time")
)

var (
	//this is so we can get a time structure that matches failed extractions
	zeroTime, _ = time.Parse(``, ``)
)

type CustomFormat struct {
	Name   string
	Regex  string
	Format string

	dateMissing bool // indicates that the extraction only gets time, so add date
}

// Validate will check that the custom format is well formed and usable
// we require a name, extraction regex, and time decoding format.
// We will attempt to compile the regex and will also try to encode and decode
// the timeformat.  The time format must be capable of encoding and decoding.
// Validate will also detect if we are missing a date so that extractions will compensate
func (cf *CustomFormat) Validate() (err error) {
	//check that everything is specified
	if cf.Name == `` {
		return ErrMissingName
	} else if cf.Regex == `` {
		return ErrMissingRegex
	} else if cf.Format == `` {
		return ErrMissingFormat
	}

	//check that the regex compiles
	if _, err = regexp.Compile(cf.Regex); err != nil {
		return
	}

	//check that we can produce and consume a timestamp using the format
	var t time.Time
	if t, err = time.Parse(cf.Format, time.Now().Format(cf.Format)); err != nil {
		err = fmt.Errorf("Invalid time format: %w", err)
		return
	} else if t.IsZero() || t.Equal(zeroTime) {
		err = ErrInvalidFormat
		return
	} else if t.Year() == 0 && t.Month() == 1 && t.Day() == 1 {
		//this is ok, but we need to add the date
		cf.dateMissing = true
	}
	return
}

func (cf CustomFormat) ExtractionRegex() string {
	return cf.Regex
}

type customProcessor struct {
	CustomFormat
	rx *regexp.Regexp
}

func NewCustomProcessor(cf CustomFormat) (p Processor, err error) {
	if err = cf.Validate(); err != nil {
		return
	}
	cp := &customProcessor{
		CustomFormat: cf,
	}
	if cp.rx, err = regexp.Compile(cf.Regex); err != nil {
		return
	}
	p = cp
	return
}

func (cp *customProcessor) ToString(t time.Time) string {
	return t.Format(cp.CustomFormat.Format)
}

func (cp *customProcessor) Match(d []byte) (int, int, bool) {
	return match(cp.rx, nil, d)
}

func (cp *customProcessor) Extract(d []byte, loc *time.Location) (t time.Time, ok bool, offset int) {
	if t, ok, offset = extract(cp.rx, nil, d, cp.CustomFormat.Format, loc); ok && cp.dateMissing {
		t = addDate(t)
	}
	return
}

func (cp *customProcessor) Name() string {
	return cp.CustomFormat.Name
}

func (cp *customProcessor) Format() string {
	return cp.CustomFormat.Format
}

func addDate(t time.Time) time.Time {
	now := time.Now().UTC().Truncate(24 * time.Hour)
	return now.Add(t.Sub(zeroTime))
}
