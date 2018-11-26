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
	"errors"
	"regexp"
	"strings"
	"time"
)

const (
	_                    = iota
	AnsiC            int = iota
	Unix             int = iota
	Ruby             int = iota
	RFC822           int = iota
	RFC822Z          int = iota
	RFC850           int = iota
	RFC1123          int = iota
	RFC1123Z         int = iota
	RFC3339          int = iota
	RFC3339Nano      int = iota
	Apache           int = iota
	ApacheNoTz       int = iota
	Syslog           int = iota
	SyslogFile       int = iota
	SyslogFileTZ     int = iota
	DPKG             int = iota
	Custom1Milli     int = iota
	NGINX            int = iota
	UnixMilli        int = iota
	ZonelessRFC3339  int = iota
	SyslogVariant    int = iota
	UnpaddedDateTime int = iota
	UnixMs           int = iota
	UnixNano         int = iota
	_lastProcessor   int = iota
)

const (
	ms int64 = 1000
	μs int64 = ms * 1000
	ns int64 = μs * 1000
)

var (
	errUnknownFormatName = errors.New("Unknown format name")
)

type Processor interface {
	Extract([]byte, *time.Location) (time.Time, bool, int)
	Format() string
	ToString(time.Time) string
	ExtractionRegex() string
	Name() string
}

type processor struct {
	rxp    *regexp.Regexp
	rxstr  string
	format string
	name   string
}

func (p *processor) Format() string {
	return p.format
}

func (p *processor) ToString(t time.Time) string {
	return t.Format(p.format)
}

func (p *processor) Regex() string {
	return p.rxstr
}

func (p *processor) ExtractionRegex() string {
	return p.rxstr
}

func (p *processor) Name() string {
	return p.name
}

func NewAnsiCProcessor() *processor {
	re := `[JFMASOND][anebriyunlgpctov]+\s+\d{1,2}\s+\d\d:\d\d:\d\d\s+\d{4}`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: "Jan _2 15:04:05 2006",
		name:   `ansic`,
	}
}

func NewUnixProcessor() *processor {
	re := `[JFMASOND][anebriyunlgpctov]+\s+\d{1,2}\s+\d\d:\d\d:\d\d\s+[A-Z]{3}\s+\d{4}`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: "Jan _2 15:04:05 MST 2006",
		name:   `unix`,
	}
}

func NewRubyProcessor() *processor {
	re := `[JFMASOND][anebriyunlgpctov]+\s+\d{2}\s+\d\d:\d\d:\d\d\s+[\-|\+]\d{4}\s+\d{4}`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: "Jan _2 15:04:05 -0700 2006",
		name:   `ruby`,
	}
}

func NewRFC822Processor() *processor {
	re := `\d{2}\s[JFMASOND][anebriyunlgpctov]+\s+\d{2}\s\d\d:\d\d\s[A-Z]{3}`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: time.RFC822,
		name:   `rfc822`,
	}
}

func NewRFC822ZProcessor() *processor {
	re := `\d{2}\s[JFMASOND][anebriyunlgpctov]+\s+\d{2}\s\d\d:\d\d\s[\-|\+]\d{4}`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: time.RFC822Z,
		name:   `rfc822z`,
	}
}

func NewRFC850Processor() *processor {
	re := `\d{2}\-[JFMASOND][anebriyunlgpctov]+\-\d{2}\s\d\d:\d\d:\d\d\s[A-Z]{3}`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: `02-Jan-06 15:04:05 MST`,
		name:   `rfc850`,
	}
}

func NewRFC1123Processor() *processor {
	re := `\d{2} [JFMASOND][anebriyunlgpctov]+ \d{4}\s\d\d:\d\d:\d\d\s[A-Z]{3}`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: `02 Jan 2006 15:04:05 MST`,
		name:   `rfc1123`,
	}
}

func NewRFC1123ZProcessor() *processor {
	re := `\d{2} [JFMASOND][anebriyunlgpctov]+ \d{4}\s\d\d:\d\d:\d\d\s[\-|\+]\d{4}`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: `02 Jan 2006 15:04:05 -0700`,
		name:   `rfc1123z`,
	}
}

func NewRFC3339Processor() *processor {
	re := `\d{4}-\d{2}-\d{2}T\d\d:\d\d:\d\dZ`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: time.RFC3339,
		name:   `rfc3339`,
	}
}

func NewRFC3339NanoProcessor() *processor {
	re := `\d{4}-\d{2}-\d{2}T\d\d:\d\d:\d\d.\d+Z`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: time.RFC3339Nano,
		name:   `rfc3339nano`,
	}
}

func NewApacheProcessor() *processor {
	re := `\d{1,2}/[JFMASOND][anebriyunlgpctov]+/\d{4}:\d\d:\d\d:\d\d\s[\-|\+]\d{4}`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: APACHE_FORMAT,
		name:   `apache`,
	}
}

func NewApacheNoTZProcessor() *processor {
	re := `\d{1,2}/[JFMASOND][anebriyunlgpctov]+/\d{4}:\d\d:\d\d:\d\d`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: APACHE_NO_TZ_FORMAT,
		name:   `apachenotz`,
	}
}

func NewSyslogFileProcessor() *processor {
	re := `\d{4}-\d{2}-\d{2}T\d\d:\d\d:\d+\.?\d*[-+]\d\d:\d\d`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: SYSLOG_FILE_FORMAT,
		name:   `syslogfile`,
	}
}

func NewSyslogFileProcessorTZ2() *processor {
	re := `\d{4}-\d{2}-\d{2}T\d\d:\d\d:\d+\.?\d*[-+]\d\d\d\d`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: SYSLOG_FILE_FORMAT_TZ2,
		name:   `syslogfiletz`,
	}
}

func NewDPKGProcessor() *processor {
	re := `(?P<ts>\d\d\d\d-\d\d-\d\d\s\d\d:\d\d:\d\d)`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: DPKG_MSG_FORMAT,
		name:   `dpkg`,
	}
}

func NewCustom1MilliProcessor() *processor {
	re := `(?P<ts>\d\d-\d\d-\d\d\d\d\s\d\d:\d\d:\d\d\.\d)`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: CUSTOM1_MILLI_MSG_FORMAT,
		name:   `custom1milli`,
	}
}

func NewNGINXProcessor() *processor {
	re := `(?P<ts>\d{4}\/\d{2}\/\d{2}\s+\d{2}:\d{2}:\d{2})`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: NGINX_FORMAT,
		name:   `nginx`,
	}
}

func NewZonelessRFC3339() *processor {
	re := `(?P<ts>\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.*\d*)`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: ZONELESS_RFC3339_FORMAT,
		name:   `rfc3339nano`,
	}
}

func NewSyslogVariant() *processor {
	re := `[JFMASOND][anebriyunlgpctov]+\s+\d{2}\s+\d\d\d\d\s+\d\d:\d\d:\d\d`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: SYSLOG_VARIANT,
		name:   `syslogvariant`,
	}
}

func NewUnpaddedDateTime() *processor {
	re := `\d\d\d\d-\d+-\d+\s+\d+:\d\d:\d\d`
	return &processor{
		rxp:    regexp.MustCompile(re),
		rxstr:  re,
		format: UNPADDED_DATE_TIME,
		name:   `unpaddeddatetime`,
	}
}

// FormatDirective tkes a string and attempts to match it against a case insensitive format directive
// This function is useful in taking string designations for time formats, checking if they are valid
// and converting them to an iota int for overriding the timegrinder
//
// Deprecated: The directive string should be entirely handled by an initialized timegrinder
func FormatDirective(s string) (v int, err error) {
	s = strings.ToLower(s)
	switch s {
	case `ansic`:
		v = AnsiC
	case `unix`:
		v = Unix
	case `ruby`:
		v = Ruby
	case `rfc822`:
		v = RFC822
	case `rfc822z`:
		v = RFC822Z
	case `rfc850`:
		v = RFC850
	case `rfc1123`:
		v = RFC1123
	case `rfc1123z`:
		v = RFC1123Z
	case `rfc3339`:
		v = RFC3339
	case `rfc3339nano`:
		v = RFC3339Nano
	case `apache`:
		v = Apache
	case `apachenotz`:
		v = ApacheNoTz
	case `syslog`:
		v = Syslog
	case `syslogfile`:
		v = SyslogFile
	case `syslogfiletz`:
		v = SyslogFileTZ
	case `dpkg`:
		v = DPKG
	case `custom1milli`:
		v = Custom1Milli
	case `nginx`:
		v = NGINX
	case `unixmilli`:
		v = UnixMilli
	case `zonelessrfc3339`:
		v = ZonelessRFC3339
	case `syslogvariant`:
		v = SyslogVariant
	case `unpaddeddatetime`:
		v = UnpaddedDateTime
	case `unixnano`:
		v = UnixNano
	case `unixms`:
		v = UnixMs
	default:
		v = -1
		err = errUnknownFormatName
	}
	return
}
