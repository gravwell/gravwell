/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package timegrinder is a package for locating and parsing timestamps within arbitrary data.
// It is intended to be as efficient as possible, so the most-recently-successful extraction
// pattern is tracked for re-use.
// The package also provides functions for defining arbitrary extractions through the
// NewUserProcessor function.
package timegrinder

import (
	"errors"
	"fmt"
	"time"
)

const (
	DEFAULT_TIMEGRINDER_SIZE int = 16
)

var (
	monthLookup map[string]time.Month
	tg          *TimeGrinder
)

func init() {
	monthLookup = make(map[string]time.Month, 36)
	populateMonthLookup(monthLookup)
	tg, _ = New(Config{}) //get a default timegrinder up
}

type TimeGrinder struct {
	procs    []Processor
	curr     int
	count    int
	seed     bool
	override Processor
	loc      *time.Location
}

// Config defines a few configuration options when instantiating a new TimeGrinder.
type Config struct {
	//force TimeGrinder to scan all possible formats on first entry, seeding with left most
	//We assume that most streams are not going to using a bunch of different timestamps
	//so we take the hit on the first iteration to try to get the left most time format
	EnableLeftMostSeed bool
	// FormatOverride sets a format (e.g. "AnsiC") which should be tried first during parsing.
	FormatOverride string
}

func Extract(b []byte) (t time.Time, ok bool, err error) {
	if tg == nil {
		err = errors.New("not ready")
	} else {
		t, ok, err = tg.Extract(b)
	}
	return
}

// NewTimeGrinder just calls New, it is maintained for API compatability but may go away soon.  Use New.
func NewTimeGrinder(c Config) (*TimeGrinder, error) {
	return New(c)
}

// New constructs and returns a new TimeGrinder object.
// On error, it will return a nil and error variable.
// The TimeGrinder object is completely safe for concurrent use.
func New(c Config) (*TimeGrinder, error) {
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

	//build SyslogFile processor
	procs = append(procs, NewSyslogFileProcessorTZ2())

	//build DPKGProcessor
	procs = append(procs, NewDPKGProcessor())

	//build NGINXProcessor
	procs = append(procs, NewNGINXProcessor())

	//float unix time
	procs = append(procs, NewUnixMilliTimeProcessor())

	//Zoneless RFC33389
	procs = append(procs, NewZonelessRFC3339())

	// Syslog variant
	procs = append(procs, NewSyslogVariant())

	// Unpadded
	procs = append(procs, NewUnpaddedDateTime())

	// Unpadded milli
	procs = append(procs, NewUnpaddedMilliDateTime())

	// The order from here matters, as there is overlap in the time formats
	// for these formats.

	// UK format
	procs = append(procs, NewUK())

	// Gravwell format
	procs = append(procs, NewGravwell())

	// LDAP
	procs = append(procs, NewLDAPProcessor())

	// Unix seconds
	procs = append(procs, NewUnixSecondsProcessor())

	// Unix milliseconds
	procs = append(procs, NewUnixMsTimeProcessor())

	// Unix nanoseconds
	procs = append(procs, NewUnixNanoTimeProcessor())

	var proc Processor
	if c.FormatOverride != `` {
		//attempt to find the override
		for i := range procs {
			if procs[i].Name() == c.FormatOverride {
				proc = procs[i]
			}
		}
	}

	return &TimeGrinder{
		procs:    procs,
		count:    len(procs),
		loc:      time.UTC,
		seed:     c.EnableLeftMostSeed,
		override: proc,
	}, nil
}

func (tg *TimeGrinder) SetLocalTime() {
	tg.loc = time.Local
}

func (tg *TimeGrinder) SetUTC() {
	tg.loc = time.UTC
}

func (tg *TimeGrinder) SetTimezone(f string) error {
	loc, err := time.LoadLocation(f)
	if err != nil {
		return err
	}
	tg.loc = loc
	return nil
}

func (tg *TimeGrinder) OverrideProcessor() (Processor, error) {
	if tg.override != nil {
		return tg.override, nil
	}
	return nil, errors.New("No override processor set")
}

// AddProcessor inserts a new Processor at the *beginning* of the processor list.
// For compatibility, it still returns the index of the inserted processor, but that
// index will always be 0.
func (tg *TimeGrinder) AddProcessor(p Processor) (idx int, err error) {
	//grab the name of the processor
	name := p.Name()
	for i := range tg.procs {
		if tg.procs[i].Name() == name {
			err = fmt.Errorf("Name collision, processor name %s already present", name)
			return
		}
	}
	tg.procs = append([]Processor{p}, tg.procs...)
	tg.count++
	idx = 0
	return
}

func (tg *TimeGrinder) setSeed(data []byte) (hit bool) {
	var offset int
	var leftmost int
	var i int
	var ok bool

	//go until we get a hit
	for i < len(tg.procs) {
		if _, ok, leftmost = tg.procs[i].Extract(data, tg.loc); ok {
			tg.curr = i
			hit = true
			break
		}
		i++
	}
	//search for something even more left
	for i < len(tg.procs) {
		if _, ok, offset = tg.procs[i].Extract(data, tg.loc); ok {
			if offset < leftmost {
				leftmost = offset
				tg.curr = i
				hit = true
			}
		}
		i++
	}
	return
}

// Extract returns time and error.  If no time can be extracted time is the zero
// value and bool is false.  Error indicates a catastrophic failure.
func (tg *TimeGrinder) Extract(data []byte) (t time.Time, ok bool, err error) {
	var i int
	var c int

	if tg.override != nil {
		if t, ok, _ = tg.override.Extract(data, tg.loc); ok {
			return
		}
	}

	if tg.seed {
		if ok := tg.setSeed(data); ok {
			tg.seed = false
		}
	}

	i = tg.curr
	for c = 0; c < tg.count; c++ {
		t, ok, _ = tg.procs[i].Extract(data, tg.loc)
		if ok {
			tg.curr = i
			return
		}
		//move the current forward
		i = (i + 1) % tg.count
	}
	//if we hit here we failed to extract a timestamp, reset to zero the attempts at zero
	tg.curr = 0
	ok = false
	return
}

// Match identifies where in a byte array a properly formatted timestamp could be
// and returns the indexes in the data slice of that format.  It DOES NOT attempt to parse
// the timestamp.  This is a faster way to say "a timestamp could be here".
// ok is always true on successful match
func (tg *TimeGrinder) Match(data []byte) (start, end int, ok bool) {
	var i int
	var c int

	if tg.override != nil {
		if start, end, ok = tg.override.Match(data); ok {
			return
		}
	}

	if tg.seed {
		if lok := tg.setSeed(data); lok {
			tg.seed = false
		}
	}

	i = tg.curr
	for c = 0; c < tg.count; c++ {
		if start, end, ok = tg.procs[i].Match(data); ok {
			tg.curr = i
			return
		}
		//move the current forward
		i = (i + 1) % tg.count
	}
	//if we hit here we failed to extract a timestamp, reset to zero for a fresh run
	tg.curr = 0
	ok = false
	return
}

/* DebugExtract returns a time, offset, and error.  If no time was extracted, the offset is -1
   Error indicates a catastrophic failure. */
func (tg *TimeGrinder) DebugExtract(data []byte) (t time.Time, offset int, err error) {
	var i int
	var c int

	if tg.override != nil {
		if t, _, offset = tg.override.Extract(data, tg.loc); offset < 0 {
			return
		}
	}

	if tg.seed {
		if ok := tg.setSeed(data); ok {
			tg.seed = false
		}
	}

	i = tg.curr
	for c = 0; c < tg.count; c++ {
		t, _, offset = tg.procs[i].Extract(data, tg.loc)
		if offset >= 0 {
			tg.curr = i
			return
		}
		//move the current forward
		i = (i + 1) % tg.count
	}
	//if we hit here we failed to extract a timestamp, reset to zero the attempts at zero
	tg.curr = 0
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
