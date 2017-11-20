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
	"time"
)

type Processor interface {
	Extract([]byte, *time.Location) (time.Time, bool)
}

type processor struct {
	rxp    *regexp.Regexp
	format string
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

func NewDPKGProcessor() *processor {
	re := `(?P<ts>\d\d\d\d-\d\d-\d\d\s\d\d:\d\d:\d\d)`
	return &processor{regexp.MustCompile(re), DPKG_MSG_FORMAT}
}

func NewCustom1MilliProcessor() *processor {
	re := `(?P<ts>\d\d-\d\d-\d\d\d\d\s\d\d:\d\d:\d\d\.\d+)`
	return &processor{regexp.MustCompile(re), CUSTOM1_MILLI_MSG_FORMAT}
}

func NewNGINXProcessor() *processor {
	re := `(?P<ts>\d{4}\/\d{2}\/\d{2}\s+\d{2}:\d{2}:\d{2})`
	return &processor{regexp.MustCompile(re), NGINX_FORMAT}
}

type syslogProcessor struct {
	p *processor
}

func NewSyslogProcessor() *syslogProcessor {
	re := `[JFMASOND][anebriyunlgpctov]+\s+\d+\s+\d\d:\d\d:\d\d`
	return &syslogProcessor{&processor{regexp.MustCompile(re), SYSLOG_FORMAT}}
}

func (a processor) Extract(d []byte, loc *time.Location) (time.Time, bool) {
	sub := a.rxp.Find(d)
	if len(sub) == 0 || sub == nil {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(a.format, string(sub), loc)
	if err != nil {
		return time.Time{}, false
	}

	return t, true
}

func (sp syslogProcessor) Extract(d []byte, loc *time.Location) (time.Time, bool) {
	t, ok := sp.p.Extract(d, loc)
	if !ok {
		return time.Time{}, false
	}
	//check if we need to add the current year
	if t.Year() == 0 {
		return t.AddDate(time.Now().Year(), 0, 0), true
	}
	return t, true
}
