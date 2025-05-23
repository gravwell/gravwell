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
	return &syslogProcessor{
		processor: processor{
			rxp:    regexp.MustCompile(SyslogRegex),
			rxstr:  SyslogRegex,
			format: SyslogFormat,
			name:   Syslog.String(),
			min:    len(SyslogFormat),
		},
	}
}

func (sp syslogProcessor) Extract(d []byte, loc *time.Location) (time.Time, bool, int) {
	if len(d) < sp.min {
		return time.Time{}, false, -1
	}
	t, ok, offset := sp.processor.Extract(d, loc)
	if !ok {
		return time.Time{}, false, -1
	}
	//check if we need to set the year
	if t.Year() == 0 {
		t = tweakYear(t)
	}
	if sp.window.Valid(t) {
		return t, true, offset
	}
	return time.Time{}, false, -1
}

type unixProcessor struct {
	re     *regexp.Regexp
	rxstr  string
	format string
	name   string
	min    int
	window TimestampWindow
}

func NewUnixMilliTimeProcessor() *unixProcessor {
	return &unixProcessor{
		re:     regexp.MustCompile(UnixMilliRegex),
		rxstr:  UnixMilliRegex,
		format: ``, //format API doesn't work here
		name:   UnixMilli.String(),
		min:    10,
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
	return _unixCoreRegex
}

func (up *unixProcessor) SetWindow(t TimestampWindow) {
	up.window = t
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

func (up unixProcessor) Extract(d []byte, loc *time.Location) (t time.Time, ok bool, offset int) {
	if len(d) < up.min {
		return time.Time{}, false, -1
	}
	offset = -1
	idx := up.re.FindSubmatchIndex(d)
	if len(idx) != 4 {
		return
	}
	s, err := strconv.ParseFloat(string(d[idx[2]:idx[3]]), 64)
	if err != nil {
		return
	}
	offset = idx[2]
	sec := int64(s)
	nsec := int64((s - float64(sec)) * 1000000000.0)
	t = time.Unix(sec, nsec).In(loc)
	if up.window.Valid(t) {
		ok = true
	}
	return
}

func (up unixProcessor) Match(d []byte) (start, end int, ok bool) {
	idx := up.re.FindSubmatchIndex(d)
	if len(idx) == 4 {
		start, end = idx[2], idx[3]
		ok = true
	}
	return
}

type unixMsProcessor struct {
	re     *regexp.Regexp
	rxstr  string
	format string
	name   string
	min    int
	window TimestampWindow
}

// NewUnixMsTimeProcessor creates a UnixMSProcessor
// this processor assumes you are not ingesting data from 1970,
// so we look for at least 13 digits of nanoseconds
func NewUnixMsTimeProcessor() *unixMsProcessor {
	return &unixMsProcessor{
		re:     regexp.MustCompile(UnixMsRegex),
		rxstr:  UnixMsRegex,
		format: ``, //API doesn't work here
		name:   UnixMs.String(),
		min:    12,
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
	return _unixMsCoreRegex
}

func (unp *unixMsProcessor) SetWindow(t TimestampWindow) {
	unp.window = t
}

func (unp unixMsProcessor) Extract(d []byte, loc *time.Location) (t time.Time, ok bool, offset int) {
	if len(d) < unp.min {
		return
	}
	offset = -1
	idx := unp.re.FindSubmatchIndex(d)
	if len(idx) != 4 {
		return
	}
	ms, err := strconv.ParseInt(string(d[idx[2]:idx[3]]), 10, 64)
	if err != nil {
		return
	}
	t = time.Unix(0, ms*1000000).In(loc)
	if unp.window.Valid(t) {
		offset = idx[2]
		ok = true
	}
	return
}

func (unp unixMsProcessor) Match(d []byte) (start, end int, ok bool) {
	if len(d) < unp.min {
		return
	}
	idx := unp.re.FindSubmatchIndex(d)
	if len(idx) == 4 {
		start, end = idx[2], idx[3]
		ok = true
	}
	return
}

type unixNanoProcessor struct {
	re     *regexp.Regexp
	rxstr  string
	format string
	name   string
	min    int
	window TimestampWindow
}

// NewUnixNanoTimeProcessor creates a new unix nano time processor
// We assume you are not ingesting data from 1970
// so we look for at least 16 digits of nanoseconds
func NewUnixNanoTimeProcessor() *unixNanoProcessor {
	return &unixNanoProcessor{
		re:     regexp.MustCompile(UnixNanoRegex),
		rxstr:  UnixNanoRegex,
		format: ``, //api doesn't work here
		name:   UnixNano.String(),
		min:    18,
	}
}

func (unp unixNanoProcessor) Format() string {
	return unp.format
}

func (unp unixNanoProcessor) Name() string {
	return unp.name
}

func (unp unixNanoProcessor) ExtractionRegex() string {
	return _unixNanoCoreRegex
}

func (unp unixNanoProcessor) ToString(t time.Time) string {
	return fmt.Sprintf("%d", t.UnixNano())
}

func (unp *unixNanoProcessor) SetWindow(t TimestampWindow) {
	unp.window = t
}

func (unp unixNanoProcessor) Extract(d []byte, loc *time.Location) (t time.Time, ok bool, offset int) {
	if len(d) < unp.min {
		return
	}
	offset = -1
	idx := unp.re.FindSubmatchIndex(d)
	if len(idx) != 4 {
		return
	}
	nsec, err := strconv.ParseInt(string(d[idx[2]:idx[3]]), 10, 64)
	if err != nil {
		return
	}
	t = time.Unix(0, nsec).In(loc)
	if unp.window.Valid(t) {
		offset = idx[2]
		ok = true
	}
	return
}

func (unp unixNanoProcessor) Match(d []byte) (start, end int, ok bool) {
	if len(d) < unp.min {
		return
	}
	idx := unp.re.FindSubmatchIndex(d)
	if len(idx) == 4 {
		start, end = idx[2], idx[3]
		ok = true
	}
	return
}

func NewUK() Processor {
	return &ukProc{
		rx:     regexp.MustCompile(UKRegex),
		rxstr:  UKRegex,
		format: UKFormat,
		name:   UK.String(),
		min:    20, //deal with low precision
	}
}

type ukProc struct {
	rx     *regexp.Regexp
	rxstr  string
	format string
	name   string
	min    int
	window TimestampWindow
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

func (p *ukProc) SetWindow(t TimestampWindow) {
	p.window = t
}

func (p *ukProc) Extract(d []byte, loc *time.Location) (time.Time, bool, int) {
	if len(d) < p.min {
		return time.Time{}, false, -1
	}
	for len(d) > 0 {
		idxs := p.rx.FindIndex(d)
		if len(idxs) != 2 {
			return time.Time{}, false, -1
		}

		t, err := p.parse(string(d[idxs[0]:idxs[1]]), loc)
		if err != nil {
			return time.Time{}, false, -1
		}

		if p.window.Valid(t) {
			return t, true, idxs[0]
		} else {
			d = d[idxs[1]:]
			continue
		}
	}
	return time.Time{}, false, -1
}

func (p *ukProc) Match(d []byte) (start, end int, ok bool) {
	if len(d) < p.min {
		return
	}
	idxs := p.rx.FindIndex(d)
	if len(idxs) == 2 {
		start, end = idxs[0], idxs[1]
		ok = true
	}
	return
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

type ldapProcessor struct {
	re     *regexp.Regexp
	rxstr  string
	format string
	name   string
	min    int
	window TimestampWindow
}

// NewLDAPProcessor creates a new LDAP timeformat processor
// We assume you are not ingesting data from 1970
// so we look for at least 16 digits of nanoseconds
func NewLDAPProcessor() *ldapProcessor {
	return &ldapProcessor{
		re:     regexp.MustCompile(LDAPRegex),
		rxstr:  LDAPRegex,
		format: ``, //api doesn't work here
		name:   LDAP.String(),
		min:    18,
	}
}

func (lp ldapProcessor) Format() string {
	return lp.format
}

func (lp ldapProcessor) Name() string {
	return lp.name
}

func (lp ldapProcessor) ExtractionRegex() string {
	return _ldapCoreRegex
}

func (lp ldapProcessor) ToString(t time.Time) string {
	l := (t.Unix() + 11644473600) * 10000000
	return fmt.Sprintf("%d", l)
}

func (lp *ldapProcessor) SetWindow(t TimestampWindow) {
	lp.window = t
}

func (lp ldapProcessor) Extract(d []byte, loc *time.Location) (t time.Time, ok bool, offset int) {
	if len(d) < lp.min {
		return
	}
	offset = -1
	idx := lp.re.FindSubmatchIndex(d)
	if len(idx) != 4 {
		return
	}

	ldap, err := strconv.ParseInt(string(d[idx[2]:idx[3]]), 10, 64)
	if err != nil {
		return
	}

	s := (ldap / 10000000) - 11644473600
	t = time.Unix(s, 0).In(loc)
	if lp.window.Valid(t) {
		offset = idx[2]
		ok = true
	}
	return
}

func (lp ldapProcessor) Match(d []byte) (start, end int, ok bool) {
	if len(d) < lp.min {
		return
	}
	idx := lp.re.FindSubmatchIndex(d)
	if len(idx) == 4 {
		start, end = idx[2], idx[3]
		ok = true
	}
	return
}

type unixSecondsProcessor struct {
	re     *regexp.Regexp
	rxstr  string
	format string
	name   string
	min    int
	window TimestampWindow
}

func NewUnixSecondsProcessor() *unixSecondsProcessor {
	return &unixSecondsProcessor{
		re:     regexp.MustCompile(UnixSecondsRegex),
		rxstr:  UnixSecondsRegex,
		format: ``, //format API doesn't work here
		name:   UnixSeconds.String(),
		min:    9, //see regex for why
	}
}

func (up *unixSecondsProcessor) Format() string {
	return up.format
}

func (up *unixSecondsProcessor) Name() string {
	return up.name
}

func (up *unixSecondsProcessor) ToString(t time.Time) string {
	uns := t.Unix()
	return fmt.Sprintf("%d", uns)
}

func (up *unixSecondsProcessor) ExtractionRegex() string {
	return _unixSecondsRegex
}

func (up *unixSecondsProcessor) SetWindow(t TimestampWindow) {
	up.window = t
}

func (up unixSecondsProcessor) Extract(d []byte, loc *time.Location) (t time.Time, ok bool, offset int) {
	if len(d) < up.min {
		return
	}
	offset = -1
	idx := up.re.FindSubmatchIndex(d)
	if len(idx) != 4 {
		return
	}
	s, err := strconv.ParseInt(string(d[idx[2]:idx[3]]), 10, 64)
	if err != nil {
		return
	}
	t = time.Unix(s, 0).In(loc)
	if up.window.Valid(t) {
		offset = idx[2]
		ok = true
	}
	return
}

func (up unixSecondsProcessor) Match(d []byte) (start, end int, ok bool) {
	if len(d) < up.min {
		return
	}
	idx := up.re.FindSubmatchIndex(d)
	if len(idx) == 4 {
		start, end = idx[2], idx[3]
		ok = true
	}
	return
}

// tweakYear tries to figure out an appropriate year for the timestamp
// if the current year is zero.
func tweakYear(t time.Time) time.Time {
	if t.Year() != 0 {
		return t
	}
	now := time.Now()
	year := now.Year()
	// If setting the current year puts us too far in the future,
	// more than 25 hours, we'll set the previous year instead.
	if t.AddDate(year, 0, 0).Sub(now) > (25 * time.Hour) {
		return t.AddDate(year-1, 0, 0)
	}
	return t.AddDate(year, 0, 0)
}
