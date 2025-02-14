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
	"regexp"
	"strings"
	"time"
)

// Timestamp Override Names
type Format string

const (
	AnsiC                 Format = `AnsiC`
	Unix                  Format = `Unix`
	Ruby                  Format = `Ruby`
	RFC822                Format = `RFC822`
	RFC822Z               Format = `RFC822Z`
	RFC850                Format = `RFC850`
	RFC1123               Format = `RFC1123`
	RFC1123Z              Format = `RFC1123Z`
	RFC3339               Format = `RFC3339`
	RFC3339Nano           Format = `RFC3339Nano`
	Apache                Format = `Apache`
	ApacheNoTz            Format = `ApacheNoTz`
	Syslog                Format = `Syslog`
	SyslogFile            Format = `SyslogFile`
	SyslogFileTZ          Format = `SyslogFileTZ`
	DPKG                  Format = `DPKG`
	NGINX                 Format = `NGINX`
	UnixMilli             Format = `UnixMilli`
	ZonelessRFC3339       Format = `ZonelessRFC3339`
	SyslogVariant         Format = `SyslogVariant`
	UnpaddedDateTime      Format = `UnpaddedDateTime`
	UnpaddedMilliDateTime Format = `UnpaddedMilliDateTime`
	UnixSeconds           Format = `UnixSeconds`
	UnixMs                Format = `UnixMs`
	UnixNano              Format = `UnixNano`
	LDAP                  Format = `LDAP`
	UK                    Format = `UK`
	Bind                  Format = `Bind`
	Gravwell              Format = `Gravwell`
	DirectAdmin           Format = `DirectAdmin`
)

// Timestamp Formats
const (
	AnsiCFormat                 string = `Jan _2 15:04:05 2006`
	UnixFormat                  string = `Jan _2 15:04:05 MST 2006`
	RubyFormat                  string = `Jan _2 15:04:05 -0700 2006`
	RFC822Format                string = `02 Jan 06 15:04 MST`
	RFC822ZFormat               string = `02 Jan 06 15:04 -0700`
	RFC850Format                string = `02-Jan-06 15:04:05 MST`
	RFC1123Format               string = `02 Jan 2006 15:04:05 MST`
	RFC1123ZFormat              string = `02 Jan 2006 15:04:05 -0700`
	RFC3339Format               string = `2006-01-02T15:04:05Z07:00`
	RFC3339NanoFormat           string = `2006-01-02T15:04:05.999999999Z07:00`
	ZonelessRFC3339Format       string = `2006-01-02T15:04:05.999999999`
	ApacheFormat                string = `_2/Jan/2006:15:04:05 -0700`
	ApacheNoTzFormat            string = `_2/Jan/2006:15:04:05`
	NGINXFormat                 string = `2006/01/02 15:04:05`
	SyslogFormat                string = `Jan _2 15:04:05`
	SyslogFileFormat            string = `2006-01-02T15:04:05.999999999-07:00`
	SyslogFileTZFormat          string = `2006-01-02T15:04:05.999999999-0700`
	DPKGFormat                  string = `2006-01-02 15:04:05`
	SyslogVariantFormat         string = `Jan 02 2006 15:04:05`
	UnpaddedDateTimeFormat      string = `2006-1-2 15:04:05`
	UnpaddedMilliDateTimeFormat string = `2006-1-2 15:04:05.999999999`
	UnixSecondsFormat           string = "1234567890"          // Time formatting API doesn't work, this is just for docs
	UnixMilliFormat             string = `1136473445.99`       // Time formatting API doesn't work, this is just for docs
	UnixMsFormat                string = `1136473445000`       // Time formatting API doesn't work, this is just for docs
	UnixNanoFormat              string = `1136473445000000000` // Time formatting API doesn't work, this is just for docs
	LDAPFormat                  string = `123456789012345678`  // Time formatting API doesn't work, this is just for docs
	UKFormat                    string = `02/01/2006 15:04:05,99999`
	GravwellFormat              string = `1-2-2006 15:04:05.99999`
	BindFormat                  string = `02-Jan-2006 15:04:05.999`
	DirectAdminFormat           string = `2006:01:02-15:04:05`
)

// Regular Expression Extractors
const (
	AnsiCRegex                 string = `[JFMASOND][anebriyunlgpctov]+\s+\d{1,2}\s+\d\d:\d\d:\d\d\s+\d{4}`
	UnixRegex                  string = `[JFMASOND][anebriyunlgpctov]+\s+\d{1,2}\s+\d\d:\d\d:\d\d\s+[A-Z]{3}\s+\d{4}`
	RubyRegex                  string = `[JFMASOND][anebriyunlgpctov]+\s+\d{1,2}\s+\d\d:\d\d:\d\d\s+[\-|\+]\d{4}\s+\d{4}`
	RFC822Regex                string = `\d{2}\s[JFMASOND][anebriyunlgpctov]+\s+\d{2}\s\d\d:\d\d\s[A-Z]{3}`
	RFC822ZRegex               string = `\d{2}\s[JFMASOND][anebriyunlgpctov]+\s+\d{2}\s\d\d:\d\d\s[\-|\+]\d{4}`
	RFC850Regex                string = `\d{2}\-[JFMASOND][anebriyunlgpctov]+\-\d{2}\s\d\d:\d\d:\d\d\s[A-Z]{3}`
	RFC1123Regex               string = `\d{2} [JFMASOND][anebriyunlgpctov]+ \d{4}\s\d\d:\d\d:\d\d\s[A-Z]{3}`
	RFC1123ZRegex              string = `\d{2} [JFMASOND][anebriyunlgpctov]+ \d{4}\s\d\d:\d\d:\d\d\s[\-|\+]\d{4}`
	RFC3339Regex               string = `\d{4}-\d{2}-\d{2}T\d\d:\d\d:\d\d[Z\-+]`
	RFC3339NanoRegex           string = `\d{4}-\d{2}-\d{2}T\d\d:\d\d:\d\d.\d+[Z\-+]`
	ZonelessRFC3339Regex       string = `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.*\d*`
	ApacheRegex                string = `\d{1,2}/[JFMASOND][anebriyunlgpctov]+/\d{4}:\d\d:\d\d:\d\d\s[\-|\+]\d{4}`
	ApacheNoTzRegex            string = `\d{1,2}/[JFMASOND][anebriyunlgpctov]+/\d{4}:\d\d:\d\d:\d\d`
	SyslogRegex                string = `[JFMASOND][anebriyunlgpctov]+\s+\d+\s+\d\d:\d\d:\d\d`
	SyslogFileRegex            string = `\d{4}-\d{2}-\d{2}T\d\d:\d\d:\d+\.?\d*[-+]\d\d:\d\d`
	SyslogFileTZRegex          string = `\d{4}-\d{2}-\d{2}T\d\d:\d\d:\d+\.?\d*[-+]\d\d\d\d`
	SyslogVariantRegex         string = `[JFMASOND][anebriyunlgpctov]+\s+\d{2}\s+\d\d\d\d\s+\d\d:\d\d:\d\d`
	DPKGRegex                  string = `\d\d\d\d-\d\d-\d\d\s\d\d:\d\d:\d\d`
	NGINXRegex                 string = `\d{4}\/\d{2}\/\d{2}\s+\d{2}:\d{2}:\d{2}`
	UnpaddedDateTimeRegex      string = `\d\d\d\d-\d+-\d+\s+\d+:\d\d:\d\d`
	UnpaddedMilliDateTimeRegex string = `\d\d\d\d-\d+-\d+\s+\d+:\d\d:\d\d\.\d{1,9}`
	UnixSecondsRegex           string = `\A\s*(\d{9,10})(?:\D|$)`
	UnixMilliRegex             string = `\A\s*(\d{9,10}\.\d+)(?:\D|$)`
	UnixMsRegex                string = `\A\s*(\d{12,13})(?:\D|$)`
	UnixNanoRegex              string = `\A\s*(\d{18,19})(?:\D|$)`
	LDAPRegex                  string = `\A\s*(\d{18})(?:\D|$)`
	UKRegex                    string = `\d\d/\d\d/\d\d\d\d\s\d\d\:\d\d\:\d\d,\d{1,5}`
	GravwellRegex              string = `\d{1,2}\-\d{1,2}\-\d{4}\s+\d{1,2}\:\d{2}\:\d{2}(\.\d{1,6})?`
	BindRegex                  string = `\d{2}\-(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Sept|Oct|Nov|Dec)\-\d{4} \d{2}:\d{2}:\d{2}\.\d{1,3}`
	DirectAdminRegex           string = `\d{4}:\d{2}:\d{2}-\d{2}:\d{2}:\d{2}`

	// non base extrators
	_unixSecondsRegex  string = `\d{9,10}`
	_unixCoreRegex     string = `\s*(\d{9,10}\.\d+)\s` //notice that we are NOT at the start of a string here
	_unixMsCoreRegex   string = `\d{12,13}`            //just looking for a large integer
	_unixNanoCoreRegex string = `\d{18,19}`
	_ldapCoreRegex     string = `\d{18}`
)

const (
	ms int64 = 1000
	μs int64 = ms * 1000
	ns int64 = μs * 1000

	tzRegexMatch string = `^((Z)|([+\-]))(\d\d:\d\d)?`
)

var (
	errUnknownFormatName = errors.New("Unknown format name")
)

var (
	overrides = []Format{
		AnsiC,
		Unix,
		Ruby,
		RFC822,
		RFC822Z,
		RFC850,
		RFC1123,
		RFC1123Z,
		RFC3339,
		RFC3339Nano,
		Apache,
		ApacheNoTz,
		Syslog,
		SyslogFile,
		SyslogFileTZ,
		DPKG,
		NGINX,
		UnixMilli,
		ZonelessRFC3339,
		SyslogVariant,
		UnpaddedDateTime,
		UnpaddedMilliDateTime,
		UnixSeconds,
		UnixMs,
		UnixNano,
		LDAP,
		UK,
		Gravwell,
		Bind,
		DirectAdmin,
	}
)

type Processor interface {
	Extract([]byte, *time.Location) (time.Time, bool, int)
	Match([]byte) (int, int, bool)
	Format() string
	ToString(time.Time) string
	ExtractionRegex() string
	Name() string
	SetWindow(TimestampWindow)
}

type processor struct {
	rxp    *regexp.Regexp
	trxpEx *regexp.Regexp // a tail regex to exclude (used for timezones)
	rxstr  string
	format string
	name   string
	min    int
	window TimestampWindow
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

func (p *processor) SetWindow(t TimestampWindow) {
	p.window = t
}

func extract(rx, rxt *regexp.Regexp, d []byte, format string, loc *time.Location, window TimestampWindow) (t time.Time, ok bool, off int) {
	var err error
	off = -1
	for len(d) > 0 {
		idxs := rx.FindIndex(d)
		if len(idxs) != 2 {
			return
		}
		if rxt != nil {
			if x := d[idxs[1]:]; len(x) > 0 {
				if rxt.Match(x) {
					//exclusion match hit, bail
					return
				}
			}
		}

		if t, err = time.ParseInLocation(format, string(d[idxs[0]:idxs[1]]), loc); err != nil {
			return
		}
		if !t.IsZero() && (window.Valid(t) || (t.Year() == 0 && window.Valid(tweakYear(t)))) {
			// if the year comes out as zero, we assume it's because the format doesn't include
			// the year, and we'll fix it upstream
			ok = true
			off = idxs[0]
			return
		} else {
			d = d[idxs[1]:]
		}
	}
	return
}

func (a *processor) Extract(d []byte, loc *time.Location) (time.Time, bool, int) {
	if len(d) < a.min {
		return time.Time{}, false, -1 //cannot possibly hit
	}
	return extract(a.rxp, a.trxpEx, d, a.format, loc, a.window)
}

func match(rx, rxt *regexp.Regexp, d []byte) (start, end int, ok bool) {
	idxs := rx.FindIndex(d)
	if len(idxs) != 2 {
		return
	}
	if rxt != nil {
		if x := d[idxs[1]:]; len(x) > 0 {
			if rxt.Match(x) {
				//exclusion match hit, bail
				return
			}
		}
	}
	start, end = idxs[0], idxs[1]
	ok = true
	return
}

func (a *processor) Match(d []byte) (int, int, bool) {
	if len(d) < a.min {
		return -1, -1, false //cannot possibly hit
	}
	return match(a.rxp, a.trxpEx, d)
}

func NewAnsiCProcessor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(AnsiCRegex),
		rxstr:  AnsiCRegex,
		format: AnsiCFormat,
		name:   AnsiC.String(),
		min:    len(AnsiCFormat),
	}
}

func NewUnixProcessor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(UnixRegex),
		rxstr:  UnixRegex,
		format: UnixFormat,
		name:   Unix.String(),
		min:    len(UnixFormat) - 2, //for shorter timezones
	}
}

func NewRubyProcessor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(RubyRegex),
		rxstr:  RubyRegex,
		format: RubyFormat,
		name:   Ruby.String(),
		min:    17, //deal with lack of timezone offsets
	}
}

func NewRFC822Processor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(RFC822Regex),
		rxstr:  RFC822Regex,
		format: RFC822Format,
		name:   RFC822.String(),
		min:    len(RFC822Format) - 2, //for shorter timezones
	}
}

func NewRFC822ZProcessor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(RFC822ZRegex),
		rxstr:  RFC822ZRegex,
		format: RFC822ZFormat,
		name:   RFC822Z.String(),
		min:    len(RFC822ZFormat) - 5, //deal with lack of timezone in some formats
	}
}

func NewRFC850Processor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(RFC850Regex),
		rxstr:  RFC850Regex,
		format: RFC850Format,
		name:   RFC850.String(),
		min:    len(RFC850Format) - 1, //for shorter timezones
	}
}

func NewRFC1123Processor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(RFC1123Regex),
		rxstr:  RFC1123Regex,
		format: RFC1123Format,
		name:   RFC1123.String(),
		min:    len(RFC1123Format) - 2, //for shorter timezones
	}
}

func NewRFC1123ZProcessor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(RFC1123ZRegex),
		rxstr:  RFC1123ZRegex,
		format: RFC1123ZFormat,
		name:   RFC1123Z.String(),
		min:    20, //deal with lack of timezone in some formats
	}
}

func NewRFC3339Processor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(RFC3339Regex),
		rxstr:  RFC3339Regex,
		format: RFC3339Format,
		name:   RFC3339.String(),
		min:    len(RFC3339Format) - 5, //to deal with the lack of a timezone in some formats
	}
}

func NewRFC3339NanoProcessor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(RFC3339NanoRegex),
		rxstr:  RFC3339NanoRegex,
		format: RFC3339NanoFormat,
		name:   RFC3339Nano.String(),
		min:    22, //deal with really small precision and no timezone
	}
}

func NewApacheProcessor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(ApacheRegex),
		rxstr:  ApacheRegex,
		format: ApacheFormat,
		name:   Apache.String(),
		min:    20, //deal with missing timezone and low precision
	}
}

func NewApacheNoTZProcessor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(ApacheNoTzRegex),
		trxpEx: regexp.MustCompile(`^\s?[-+]{1}\d{4}`),
		rxstr:  ApacheNoTzRegex,
		format: ApacheNoTzFormat,
		name:   ApacheNoTz.String(),
		min:    15, //deal with no tz and no offset
	}
}

func NewSyslogFileProcessor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(SyslogFileRegex),
		rxstr:  SyslogFileRegex,
		format: SyslogFileFormat,
		name:   SyslogFile.String(),
		min:    len(SyslogFileFormat) - 13, //to deal with low precision output and lack of timezone
	}
}

func NewSyslogFileProcessorTZ2() *processor {
	return &processor{
		rxp:    regexp.MustCompile(SyslogFileTZRegex),
		rxstr:  SyslogFileTZRegex,
		format: SyslogFileTZFormat,
		name:   SyslogFileTZ.String(),
		min:    19, //to deal with low precision output and lack of timezone
	}
}

func NewDPKGProcessor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(DPKGRegex),
		rxstr:  DPKGRegex,
		format: DPKGFormat,
		name:   DPKG.String(),
		min:    len(DPKGFormat),
	}
}

func NewNGINXProcessor() *processor {
	return &processor{
		rxp:    regexp.MustCompile(NGINXRegex),
		rxstr:  NGINXRegex,
		format: NGINXFormat,
		name:   NGINX.String(),
		min:    len(NGINXFormat),
	}
}

func NewZonelessRFC3339() *processor {
	return &processor{
		rxp:    regexp.MustCompile(ZonelessRFC3339Regex),
		trxpEx: regexp.MustCompile(tzRegexMatch),
		rxstr:  ZonelessRFC3339Regex,
		format: ZonelessRFC3339Format,
		name:   ZonelessRFC3339.String(),
		min:    len(ZonelessRFC3339Format) - 13, //todeal with low precision and lack of timezone
	}
}

func NewSyslogVariant() *processor {
	return &processor{
		rxp:    regexp.MustCompile(SyslogVariantRegex),
		rxstr:  SyslogVariantRegex,
		format: SyslogVariantFormat,
		name:   SyslogVariant.String(),
		min:    17, //deal with no timezone
	}
}

func NewUnpaddedDateTime() *processor {
	return &processor{
		rxp:    regexp.MustCompile(UnpaddedDateTimeRegex),
		rxstr:  UnpaddedDateTimeRegex,
		format: UnpaddedDateTimeFormat,
		name:   UnpaddedDateTime.String(),
		min:    len(UnpaddedDateTimeFormat), //to deal with low precision
	}
}

func NewUnpaddedMilliDateTime() *processor {
	return &processor{
		rxp:    regexp.MustCompile(UnpaddedMilliDateTimeRegex),
		rxstr:  UnpaddedMilliDateTimeRegex,
		format: UnpaddedMilliDateTimeFormat,
		name:   UnpaddedMilliDateTime.String(),
		min:    len(UnpaddedMilliDateTimeFormat) - 8, //to deal with low precision
	}
}

func NewGravwell() *processor {
	return &processor{
		rxp:    regexp.MustCompile(GravwellRegex),
		rxstr:  GravwellRegex,
		format: GravwellFormat,
		name:   Gravwell.String(),
		min:    len(GravwellFormat) - 4, //to deal with lower precision
	}
}

func NewBind() *processor {
	return &processor{
		rxp:    regexp.MustCompile(BindRegex),
		rxstr:  BindRegex,
		format: BindFormat,
		name:   Bind.String(),
		min:    len(BindFormat) - 3, //to deal with lower precision
	}
}

func NewDirectAdmin() *processor {
	return &processor{
		rxp:    regexp.MustCompile(DirectAdminRegex),
		rxstr:  DirectAdminRegex,
		format: DirectAdminFormat,
		name:   DirectAdmin.String(),
		min:    len(DirectAdminFormat) - 2, //deal with missing leading zeros
	}
}

func (o Format) ToLower() string {
	return strings.ToLower(string(o))
}

func (o Format) String() string {
	return string(o)
}

// FormatDirective takes a string and attempts to match it against a case insensitive format directive
// This function is useful in taking string designations for time formats, checking if they are valid
// and converting them to an iota int for overriding the timegrinder
//
// Deprecated: The directive string should be entirely handled by an initialized timegrinder
func FormatDirective(s string) (r Format, err error) {
	t := strings.ToLower(s)
	for _, v := range overrides {
		if v.ToLower() == t {
			r = v
			return
		}
	}
	err = errors.New(s + " Is not a valid timestamp name")
	return
}

func ValidateFormatOverride(s string) (err error) {
	t := strings.ToLower(s)
	for _, v := range overrides {
		if v.ToLower() == t {
			return
		}
	}
	err = errors.New(s + " Is not a valid timestamp name")
	return
}
