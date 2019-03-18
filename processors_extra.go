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
	"strconv"
	"strings"
	"time"
)

//the processor_extra file contains grinder formats that don't fit into the traditional
//model and may be a little more expensive to actually process

type syslogProcessor struct {
	processor
}

func NewSyslogProcessor() *syslogProcessor {
	re := `[JFMASOND][anebriyunlgpctov]+\s+\d+\s+\d\d:\d\d:\d\d`
	return &syslogProcessor{
		processor: processor{
			rxp:    regexp.MustCompile(re),
			rxstr:  re,
			format: SYSLOG_FORMAT,
			name:   `syslog`,
		},
	}
}

type unixProcessor struct {
	re     *regexp.Regexp
	rxstr  string
	format string
	name   string
}

func NewUnixMilliTimeProcessor() *unixProcessor {
	restr := `\A\s*(\d+\.\d+)\s`
	return &unixProcessor{
		re:     regexp.MustCompile(restr),
		rxstr:  restr,
		format: ``, //format API doesn't work here
		name:   `unixmilli`,
	}
}

func (up *unixProcessor) Format() string {
	return up.format
}

func (up *unixProcessor) Name() string {
	return up.name
}

func (up *unixProcessor) ToString(t time.Time) string {
	uns := t.Unix()
	return fmt.Sprintf("%d.%d", uns, t.UnixNano()-(uns*ns))
}

func (up *unixProcessor) ExtractionRegex() string {
	return `\s*(\d+\.\d+)\s` //notice that we are NOT at the start of a string here
}

func NewUserProcessor(name, rxps, fmts string) (*processor, error) {
	//check that regex compiles
	rx, err := regexp.Compile(rxps)
	if err != nil {
		return nil, err
	}

	//check that you can format NOW using the format string
	//then reparse the output and get something sensible
	x := time.Now().Format(fmts)
	if _, err = time.Parse(fmts, x); err != nil {
		return nil, err
	}

	//check that the regex hits on the format string
	bts := []byte(x)
	idxs := rx.FindIndex(bts)
	if len(idxs) != 2 {
		return nil, errors.New("Regular expression does not trigger on format")
	}
	//check that the extraction via the regex can be parsed
	x = string(bts[idxs[0]:idxs[1]])
	if _, err = time.Parse(fmts, x); err != nil {
		return nil, err
	}

	//return the processor
	return &processor{
		rxp:    rx,
		rxstr:  rxps,
		format: fmts,
		name:   name,
	}, nil
}

func (sp syslogProcessor) Extract(d []byte, loc *time.Location) (time.Time, bool, int) {
	t, ok, offset := sp.processor.Extract(d, loc)
	if !ok {
		return time.Time{}, false, -1
	}
	//check if we need to add the current year
	if t.Year() == 0 {
		return t.AddDate(time.Now().Year(), 0, 0), true, -1
	}
	return t, true, offset
}

func (up unixProcessor) Extract(d []byte, loc *time.Location) (t time.Time, ok bool, offset int) {
	idx := up.re.FindSubmatchIndex(d)
	if len(idx) != 4 {
		return
	}
	s, err := strconv.ParseFloat(string(d[idx[2]:idx[3]]), 64)
	if err != nil {
		return
	}
	sec := int64(s)
	nsec := int64((s - float64(sec)) * 1000000000.0)
	t = time.Unix(sec, nsec).In(loc)
	ok = true
	return
}

type unixMsProcessor struct {
	re     *regexp.Regexp
	rxstr  string
	format string
	name   string
}

// We assume you're not ingesting data from 1970, so we look for at least 13 digits of nanoseconds
func NewUnixMsTimeProcessor() *unixMsProcessor {
	re := `(\A\d{13,18})[\s,;]`
	return &unixMsProcessor{
		re:     regexp.MustCompile(re),
		rxstr:  re,
		format: ``, //API doesn't work here
		name:   `unixms`,
	}
}

func (unp unixMsProcessor) Format() string {
	return unp.format
}

func (unp unixMsProcessor) Name() string {
	return unp.name
}

func (unp unixMsProcessor) ToString(t time.Time) string {
	return fmt.Sprintf("%d", t.UnixNano()/μs)
}

func (unp unixMsProcessor) ExtractionRegex() string {
	return `\d{13,18}` //just looking for a large integer
}

func (unp unixMsProcessor) Extract(d []byte, loc *time.Location) (t time.Time, ok bool, offset int) {
	idx := unp.re.FindSubmatchIndex(d)
	if len(idx) != 4 {
		return
	}
	ms, err := strconv.ParseInt(string(d[idx[2]:idx[3]]), 10, 64)
	if err != nil {
		return
	}
	t = time.Unix(0, ms*1000000).In(loc)
	ok = true
	return
}

type unixNanoProcessor struct {
	re     *regexp.Regexp
	rxstr  string
	format string
	name   string
}

// We assume you're not ingesting data from 1970, so we look for at least 16 digits of nanoseconds
func NewUnixNanoTimeProcessor() *unixNanoProcessor {
	re := `(\A\d{16,})[\s,;]`
	return &unixNanoProcessor{
		re:     regexp.MustCompile(re),
		rxstr:  re,
		format: ``, //api doesn't work here
		name:   `unixnano`,
	}
}

func (unp unixNanoProcessor) Format() string {
	return unp.format
}

func (unp unixNanoProcessor) Name() string {
	return unp.name
}

func (unp unixNanoProcessor) ExtractionRegex() string {
	return `\d{16,}`
}

func (unp unixNanoProcessor) ToString(t time.Time) string {
	return fmt.Sprintf("%d", t.UnixNano())
}

func (unp unixNanoProcessor) Extract(d []byte, loc *time.Location) (t time.Time, ok bool, offset int) {
	idx := unp.re.FindSubmatchIndex(d)
	if len(idx) != 4 {
		return
	}
	nsec, err := strconv.ParseInt(string(d[idx[2]:idx[3]]), 10, 64)
	if err != nil {
		return
	}
	t = time.Unix(0, nsec).In(loc)
	ok = true
	return
}

func NewUK() Processor {
	re := `\d\d/\d\d/\d\d\d\d\s\d\d\:\d\d\:\d\d,\d{1,5}`
	return &ukProc{
		format: `02/01/2006 15:04:05.99999`,
		rxstr:  re,
		rx:     regexp.MustCompile(re),
		name:   `uk`,
	}
}

type ukProc struct {
	rx     *regexp.Regexp
	rxstr  string
	format string
	name   string
}

func (p *ukProc) Format() string {
	return p.format
}

func (p *ukProc) ToString(t time.Time) string {
	return t.Format(`02/01/2006 15:04:05`) + "," + fmt.Sprintf("%d", t.Nanosecond()/int(μs))
}

func (p *ukProc) ExtractionRegex() string {
	return p.rxstr
}

func (p *ukProc) Name() string {
	return p.name
}

func (p *ukProc) Extract(d []byte, loc *time.Location) (time.Time, bool, int) {
	idxs := p.rx.FindIndex(d)
	if len(idxs) != 2 {
		return time.Time{}, false, -1
	}

	t, err := p.parse(string(d[idxs[0]:idxs[1]]), loc)
	if err != nil {
		return time.Time{}, false, -1
	}

	return t, true, idxs[0]
}

func (p *ukProc) parse(value string, loc *time.Location) (time.Time, error) {
	if i := strings.IndexByte(value, ','); i >= 0 {
		t, err := time.Parse(p.format, value[:i]+"."+value[i+1:])
		if err == nil {
			return t, err
		}
	}
	return time.Parse(p.format, value)
}
