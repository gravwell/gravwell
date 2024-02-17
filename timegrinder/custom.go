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
	ErrMissingName         = errors.New("Missing time extraction name")
	ErrMissingRegex        = errors.New("Missing extraction regular expression")
	ErrMissingFormat       = errors.New("Missing extraction time format")
	ErrInvalidFormat       = errors.New("Invalid time format, could not format and extract current time")
	ErrRegexFormatMismatch = errors.New("Could not match regex against provided format")
)

var (
	//this is so we can get a time structure that matches failed extractions
	zeroTime, _ = time.Parse(``, ``)
)

type CustomFormat struct {
	//Normal Custom Format Extractions
	Name   string
	Regex  string
	Format string

	// optional pre-extraction system that can go get the meat of a timestamp before actually trying to handle the timestamp
	Extraction_Regex string

	dateMissing bool // indicates that the extraction only gets time, so add date
	yearMissing bool // indicates that the extraction doesn't set the year

	pre preExtractor
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
	} else if cf.Format == `` {
		return ErrMissingFormat
	}

	if cf.Regex != `` {
		if _, ok := tg.GetProcessor(cf.Format); ok {
			err = fmt.Errorf("Cannot specify a format name (%v) and a regex", cf.Format)
			return
		}
		//check that the regex compiles
		var rx *regexp.Regexp
		if rx, err = regexp.Compile(cf.Regex); err != nil {
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
		if t.Year() == 0 {
			cf.yearMissing = true
		}
		// Try to match it
		if _, _, ok := match(rx, nil, []byte(time.Now().Format(cf.Format))); !ok {
			err = ErrRegexFormatMismatch
			return
		}
	} else if cf.Extraction_Regex == `` {
		// if we don't have a regex, then we MUST have a pre-extraction regex
		err = errors.New("Extraction-Regex is required when using a named format")
		return
	} else {
		//ok, they have a pre-extraction regex and no regular regex, make sure the name exists
		//make sure that the format is one defined in our default set
		if _, ok := tg.GetProcessor(cf.Format); !ok {
			err = fmt.Errorf("Timegrinder format %q does not exist", cf.Format)
			return
		}
	}

	if cf.Extraction_Regex != `` {
		//if there is a extraction regex, make sure it works
		if cf.pre, err = newPreExtractor(cf.Extraction_Regex); err != nil {
			err = fmt.Errorf("Extraction-Regex is invalid %w", err)
			return
		}
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
	if cf.Regex != `` {
		cp := &customProcessor{
			CustomFormat: cf,
		}
		if cp.rx, err = regexp.Compile(cf.Regex); err != nil {
			return
		}
		p = cp
	} else {
		var ok bool
		if p, ok = tg.GetProcessor(cf.Format); !ok {
			err = fmt.Errorf("Failed to find %s", cf.Format)
			return
		}
	}
	if cf.pre.rx != nil {
		cp := preExtractProcessor{
			Processor: p,
			px:        cf.pre,
			name:      cf.Name,
		}
		p = cp
	}
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
	//check if we need to set the year
	if cp.yearMissing {
		t = tweakYear(t)
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

// preExtractor is a regex extraction system used to pick timestamps out of potentially difficult to process structures.
// A common use case is to grab an integer that represents Unix Seconds or Unix Milli out of some data structure before
// passing the extracted data into another processor
type preExtractor struct {
	rx     *regexp.Regexp
	idx    int //extraction subindex pulled from capture group
	setidx int // the offset into our match index (always idx * 2)
}

// newPreExtractor will take a regular expression definition and a named capture group and create a pre-extractor
func newPreExtractor(rxstr string) (pe preExtractor, err error) {
	//compile the regex
	if pe.rx, err = regexp.Compile(rxstr); err != nil {
		return
	}

	// go find the first name
	names := pe.rx.SubexpNames()
	for i := range names {
		if len(names[i]) > 0 {
			if pe.idx > 0 {
				err = fmt.Errorf("Multiple named capture groups defined")
				return
			}
			pe.idx = i
		}
	}
	if pe.idx == 0 {
		err = errors.New("missing named capture group for pre-extractor")
	} else {
		pe.setidx = pe.idx * 2
	}
	return
}

func (pe preExtractor) extract(d []byte) (val []byte, offset int) {
	// try to do the pre-extraction
	matches := pe.rx.FindSubmatchIndex(d)
	if r := len(matches); r == 0 || (r&1) != 0 {
		//complete miss
		offset = -1
	} else if (r + 1) < pe.setidx {
		//named extractor was not pulled out
		offset = -1
	} else if start, end := matches[pe.setidx], matches[pe.setidx+1]; start < 0 || end < 0 || start >= len(d) || start >= end {
		offset = -1
	} else {
		offset = start
		val = d[start:end]
	}
	return
}

type preExtractProcessor struct {
	Processor
	px   preExtractor
	name string
}

func (pep preExtractProcessor) Extract(d []byte, loc *time.Location) (t time.Time, ok bool, offset int) {
	pval, poff := pep.px.extract(d)
	if poff < 0 || len(pval) == 0 {
		return // we missed
	} else if t, ok, offset = pep.Processor.Extract(pval, loc); ok {
		offset += poff
	}
	return
}

func (pep preExtractProcessor) Name() string {
	return pep.name
}
