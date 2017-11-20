/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/* EXAMPLES
   UnixDate    = "Mon Jan _2 15:04:05 MST 2006"
   RubyDate    = "Mon Jan 02 15:04:05 -0700 2006"
   RFC822      = "02 Jan 06 15:04 MST"
   RFC822Z     = "02 Jan 06 15:04 -0700" // RFC822 with numeric zone
   RFC850      = "Monday, 02-Jan-06 15:04:05 MST"
   RFC1123     = "Mon, 02 Jan 2006 15:04:05 MST"
   RFC1123Z    = "Mon, 02 Jan 2006 15:04:05 -0700" // RFC1123 with numeric zone
   RFC3339     = "2006-01-02T15:04:05Z07:00"
   RFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"

   SysLog      = "Jan 02 15:04:05"
*/

package timegrinder

import (
	"time"
)

const (
	DEFAULT_TIMEGRINDER_SIZE int = 16

	APACHE_FORMAT            string = `_2/Jan/2006:15:04:05 -0700`
	APACHE_NO_TZ_FORMAT      string = `_2/Jan/2006:15:04:05`
	NGINX_FORMAT             string = `2006/01/02 15:04:05`
	SYSLOG_FORMAT            string = `Jan _2 15:04:05`
	SYSLOG_FILE_FORMAT       string = `2006-01-02T15:04:05.999999999-07:00`
	DPKG_MSG_FORMAT          string = `2006-01-02 15:04:05`
	CUSTOM1_MILLI_MSG_FORMAT string = `01-02-2006 15:04:05.000000`
)

var (
	monthLookup map[string]time.Month
)

func init() {
	monthLookup = make(map[string]time.Month, 36)
	populateMonthLookup(monthLookup)
}

type TimeGrinder struct {
	procs []Processor
	curr  int
	count int
	loc   *time.Location
}

/* NewTimeGrinder constructs and returns a new TimeGrinder object
 * On error, it will return a nil and error variable
 * The TimeGrinder object is completely safe for concurrent use.
 */
func NewTimeGrinder() (*TimeGrinder, error) {
	procs := make([]Processor, 0, 16)

	//build ANSIC processor
	procs = append(procs, NewAnsiCProcessor())

	//build UnixDate processor
	procs = append(procs, NewUnixProcessor())

	//build RubyDate processor
	procs = append(procs, NewRubyProcessor())

	//build RFC822 processor
	procs = append(procs, NewRFC822Processor())

	//build RFC822Z processor
	procs = append(procs, NewRFC822ZProcessor())

	//build RFC850 processor
	procs = append(procs, NewRFC850Processor())

	//build RFC1123 processor
	procs = append(procs, NewRFC1123Processor())

	//build RFC1123Z processor
	procs = append(procs, NewRFC1123ZProcessor())

	//build RFC3339 processor
	procs = append(procs, NewRFC3339Processor())

	//build RFC3339Nano processor
	procs = append(procs, NewRFC3339NanoProcessor())

	//build Apache processor
	procs = append(procs, NewApacheProcessor())

	//build Apache processor with no timezone
	procs = append(procs, NewApacheNoTZProcessor())

	//build Syslog processor
	procs = append(procs, NewSyslogProcessor())

	//build SyslogFile processor
	procs = append(procs, NewSyslogFileProcessor())

	//build DPKGProcessor
	procs = append(procs, NewDPKGProcessor())

	//build DPKGMilliProcessor
	procs = append(procs, NewCustom1MilliProcessor())

	//build NGINXProcessor
	procs = append(procs, NewNGINXProcessor())

	return &TimeGrinder{
		procs: procs,
		count: len(procs),
		loc:   time.UTC,
	}, nil
}

func (tg *TimeGrinder) SetLocalTime() {
	tg.loc = time.Local
}

func (tg *TimeGrinder) SetUTC() {
	tg.loc = time.UTC
}

/* Extract returns time and error.  If no time can be extracted time is the zero
   value and bool is false.  Error indicates a catastrophic failure. */
func (tg *TimeGrinder) Extract(data []byte) (t time.Time, ok bool, err error) {
	var i int
	var c int

	i = tg.curr
	for c = 0; c < tg.count; c++ {
		t, ok = tg.procs[i].Extract(data, tg.loc)
		if ok {
			tg.curr = i
			return
		}
		//move the current forward
		i = (i + 1) % tg.count
	}
	//if we hit here we failed to extract a timestamp
	ok = false
	return
}

func populateMonthLookup(ml map[string]time.Month) {
	//jan
	monthLookup["Jan"] = time.January
	monthLookup["jan"] = time.January
	monthLookup["JAN"] = time.January
	monthLookup["January"] = time.January
	monthLookup["january"] = time.January

	//feb
	monthLookup["feb"] = time.February
	monthLookup["FEB"] = time.February
	monthLookup["Feb"] = time.February
	monthLookup["feburary"] = time.February
	monthLookup["February"] = time.February

	//mar
	monthLookup["mar"] = time.March
	monthLookup["Mar"] = time.March
	monthLookup["MAR"] = time.March
	monthLookup["march"] = time.March
	monthLookup["March"] = time.March

	//april
	monthLookup["apr"] = time.April
	monthLookup["Apr"] = time.April
	monthLookup["APR"] = time.April
	monthLookup["april"] = time.April
	monthLookup["April"] = time.April

	//may
	monthLookup["may"] = time.May
	monthLookup["May"] = time.May
	monthLookup["MAY"] = time.May

	//june
	monthLookup["jun"] = time.June
	monthLookup["Jun"] = time.June
	monthLookup["JUN"] = time.June
	monthLookup["june"] = time.June
	monthLookup["June"] = time.June
	monthLookup["JUNE"] = time.June

	//july
	monthLookup["Jul"] = time.July
	monthLookup["jul"] = time.July
	monthLookup["JUL"] = time.July
	monthLookup["july"] = time.July
	monthLookup["July"] = time.July
	monthLookup["JULY"] = time.July

	//aug
	monthLookup["aug"] = time.August
	monthLookup["Aug"] = time.August
	monthLookup["AUG"] = time.August
	monthLookup["August"] = time.August
	monthLookup["august"] = time.August

	//sept
	monthLookup["Sept"] = time.September
	monthLookup["sept"] = time.September
	monthLookup["SEPT"] = time.September
	monthLookup["September"] = time.September
	monthLookup["september"] = time.September

	//oct
	monthLookup["oct"] = time.October
	monthLookup["Oct"] = time.October
	monthLookup["OCT"] = time.October
	monthLookup["October"] = time.October
	monthLookup["october"] = time.October

	//nov
	monthLookup["nov"] = time.November
	monthLookup["Nov"] = time.November
	monthLookup["NOV"] = time.November
	monthLookup["November"] = time.November
	monthLookup["november"] = time.November

	//dec
	monthLookup["dec"] = time.December
	monthLookup["Dec"] = time.December
	monthLookup["DEC"] = time.December
	monthLookup["December"] = time.December
	monthLookup["december"] = time.December
}
