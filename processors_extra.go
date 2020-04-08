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
	return &unixProcessor{
		re:     regexp.MustCompile(UnixMilliRegex),
		rxstr:  UnixMilliRegex,
		format: ``, //format API doesn't work here
		name:   UnixMilli.String(),
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
}

// We assume you're not ingesting data from 1970, so we look for at least 13 digits of nanoseconds
func NewUnixMsTimeProcessor() *unixMsProcessor {
	return &unixMsProcessor{
		re:     regexp.MustCompile(UnixMsRegex),
		rxstr:  UnixMsRegex,
		format: ``, //API doesn't work here
		name:   UnixMs.String(),
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

func (unp unixMsProcessor) Match(d []byte) (start, end int, ok bool) {
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
}

// We assume you're not ingesting data from 1970, so we look for at least 16 digits of nanoseconds
func NewUnixNanoTimeProcessor() *unixNanoProcessor {
	return &unixNanoProcessor{
		re:     regexp.MustCompile(UnixNanoRegex),
		rxstr:  UnixNanoRegex,
		format: ``, //api doesn't work here
		name:   UnixNano.String(),
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

func (unp unixNanoProcessor) Match(d []byte) (start, end int, ok bool) {
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

func (p *ukProc) Match(d []byte) (start, end int, ok bool) {
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
}

// We assume you're not ingesting data from 1970, so we look for at least 16 digits of nanoseconds
func NewLDAPProcessor() *ldapProcessor {
	return &ldapProcessor{
		re:     regexp.MustCompile(LDAPRegex),
		rxstr:  LDAPRegex,
		format: ``, //api doesn't work here
		name:   LDAP.String(),
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
	// TODO
	return fmt.Sprintf("%d", t.UnixNano())
}

func (lp ldapProcessor) Extract(d []byte, loc *time.Location) (t time.Time, ok bool, offset int) {
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
	ok = true
	return
}

func (lp ldapProcessor) Match(d []byte) (start, end int, ok bool) {
	idx := lp.re.FindSubmatchIndex(d)
	if len(idx) == 4 {
		start, end = idx[2], idx[3]
		ok = true
	}
	return
}
