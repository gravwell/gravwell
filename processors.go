/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

//ANSIC       = "Mon Jan _2 15:04:05 2006"
package timegrinder

import (
	"regexp"
	"strconv"
	"time"
)

const (
	_                   = iota
	AnsiC           int = iota
	Unix            int = iota
	Ruby            int = iota
	RFC822          int = iota
	RFC822Z         int = iota
	RFC850          int = iota
	RFC1123         int = iota
	RFC1123Z        int = iota
	RFC3339         int = iota
	RFC3339Nano     int = iota
	Apache          int = iota
	ApacheNoTz      int = iota
	Syslog          int = iota
	SyslogFile      int = iota
	DPKG            int = iota
	Custom1Milli    int = iota
	NGINX           int = iota
	UnixMilli       int = iota
	ZonelessRFC3339 int = iota
	SyslogVariant   int = iota
)

type Processor interface {
	Extract([]byte, *time.Location) (time.Time, bool, int)
	Format() string
}

type processor struct {
	rxp    *regexp.Regexp
	format string
}

func (p *processor) Format() string {
	return p.format
}

func NewAnsiCProcessor() *processor {
	re := `[JFMASOND][anebriyunlgpctov]+\s+\d{1,2}\s+\d\d:\d\d:\d\d\s+\d{4}`
	return &processor{regexp.MustCompile(re), "Jan _2 15:04:05 2006"}
}

func NewUnixProcessor() *processor {
	re := `[JFMASOND][anebriyunlgpctov]+\s+\d{1,2}\s+\d\d:\d\d:\d\d\s+[A-Z]{3}\s+\d{4}`
	return &processor{regexp.MustCompile(re), "Jan _2 15:04:05 MST 2006"}
}

func NewRubyProcessor() *processor {
	re := `[JFMASOND][anebriyunlgpctov]+\s+\d{2}\s+\d\d:\d\d:\d\d\s+[\-|\+]\d{4}\s+\d{4}`
	return &processor{regexp.MustCompile(re), "Jan _2 15:04:05 -0700 2006"}
}

func NewRFC822Processor() *processor {
	re := `\d{2}\s[JFMASOND][anebriyunlgpctov]+\s+\d{2}\s\d\d:\d\d\s[A-Z]{3}`
	return &processor{regexp.MustCompile(re), time.RFC822}
}

func NewRFC822ZProcessor() *processor {
	re := `\d{2}\s[JFMASOND][anebriyunlgpctov]+\s+\d{2}\s\d\d:\d\d\s[\-|\+]\d{4}`
	return &processor{regexp.MustCompile(re), time.RFC822Z}
}

func NewRFC850Processor() *processor {
	re := `\d{2}\-[JFMASOND][anebriyunlgpctov]+\-\d{2}\s\d\d:\d\d:\d\d\s[A-Z]{3}`
	return &processor{regexp.MustCompile(re), `02-Jan-06 15:04:05 MST`}
}

func NewRFC1123Processor() *processor {
	re := `\d{2} [JFMASOND][anebriyunlgpctov]+ \d{4}\s\d\d:\d\d:\d\d\s[A-Z]{3}`
	return &processor{regexp.MustCompile(re), `02 Jan 2006 15:04:05 MST`}
}

func NewRFC1123ZProcessor() *processor {
	re := `\d{2} [JFMASOND][anebriyunlgpctov]+ \d{4}\s\d\d:\d\d:\d\d\s[\-|\+]\d{4}`
	return &processor{regexp.MustCompile(re), `02 Jan 2006 15:04:05 -0700`}
}

func NewRFC3339Processor() *processor {
	re := `\d{4}-\d{2}-\d{2}T\d\d:\d\d:\d\dZ`
	return &processor{regexp.MustCompile(re), time.RFC3339}
}

func NewRFC3339NanoProcessor() *processor {
	re := `\d{4}-\d{2}-\d{2}T\d\d:\d\d:\d\d.\d+Z`
	return &processor{regexp.MustCompile(re), time.RFC3339Nano}
}

func NewApacheProcessor() *processor {
	re := `\d{1,2}/[JFMASOND][anebriyunlgpctov]+/\d{4}:\d\d:\d\d:\d\d\s[\-|\+]\d{4}`
	return &processor{regexp.MustCompile(re), APACHE_FORMAT}
}

func NewApacheNoTZProcessor() *processor {
	re := `\d{1,2}/[JFMASOND][anebriyunlgpctov]+/\d{4}:\d\d:\d\d:\d\d`
	return &processor{regexp.MustCompile(re), APACHE_NO_TZ_FORMAT}
}

func NewSyslogFileProcessor() *processor {
	re := `\d{4}-\d{2}-\d{2}T\d\d:\d\d:\d+\.?\d*[-+]\d\d:\d\d`
	return &processor{regexp.MustCompile(re), SYSLOG_FILE_FORMAT}
}

func NewSyslogFileProcessorTZ2() *processor {
	re := `\d{4}-\d{2}-\d{2}T\d\d:\d\d:\d+\.?\d*[-+]\d\d\d\d`
	return &processor{regexp.MustCompile(re), SYSLOG_FILE_FORMAT_TZ2}
}

func NewDPKGProcessor() *processor {
	re := `(?P<ts>\d\d\d\d-\d\d-\d\d\s\d\d:\d\d:\d\d)`
	return &processor{regexp.MustCompile(re), DPKG_MSG_FORMAT}
}

func NewCustom1MilliProcessor() *processor {
	re := `(?P<ts>\d\d-\d\d-\d\d\d\d\s\d\d:\d\d:\d\d\.\d)`
	return &processor{regexp.MustCompile(re), CUSTOM1_MILLI_MSG_FORMAT}
}

func NewNGINXProcessor() *processor {
	re := `(?P<ts>\d{4}\/\d{2}\/\d{2}\s+\d{2}:\d{2}:\d{2})`
	return &processor{regexp.MustCompile(re), NGINX_FORMAT}
}

func NewZonelessRFC3339() *processor {
	re := `(?P<ts>\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.*\d*)`
	return &processor{regexp.MustCompile(re), ZONELESS_RFC3339_FORMAT}
}

func NewSyslogVariant() *processor {
	re := `[JFMASOND][anebriyunlgpctov]+\s+\d{2}\s+\d\d\d\d\s+\d\d:\d\d:\d\d`
	return &processor{regexp.MustCompile(re), SYSLOG_VARIANT}
}

type syslogProcessor struct {
	processor
}

func NewSyslogProcessor() *syslogProcessor {
	re := `[JFMASOND][anebriyunlgpctov]+\s+\d+\s+\d\d:\d\d:\d\d`
	return &syslogProcessor{
		processor: processor{regexp.MustCompile(re), SYSLOG_FORMAT},
	}
}

type unixProcessor struct {
	re     *regexp.Regexp
	format string
}

func NewUnixMilliTimeProcessor() *unixProcessor {
	return &unixProcessor{
		re:     regexp.MustCompile(`(\A\d+\.\d+)\s`),
		format: `(\A\d+\.\d+)\s`,
	}
}

func (up *unixProcessor) Format() string {
	return up.format
}

func (a processor) Extract(d []byte, loc *time.Location) (time.Time, bool, int) {
	/*
		sub := a.rxp.Find(d)
		if len(sub) == 0 {
			return time.Time{}, false
		}
	*/
	idxs := a.rxp.FindIndex(d)
	if len(idxs) != 2 {
		return time.Time{}, false, -1
	}

	t, err := time.ParseInLocation(a.format, string(d[idxs[0]:idxs[1]]), loc)
	if err != nil {
		return time.Time{}, false, -1
	}

	return t, true, idxs[0]
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
