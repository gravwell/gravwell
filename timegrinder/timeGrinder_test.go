/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package timegrinder

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

const (
	SEED             int64 = 0x7777BEEFDEADBEEF
	TEST_COUNT       int   = 16
	BENCH_LOOP_COUNT int   = 1024
	RAND_BUFF_SIZE   int   = 2048
)

var (
	baseTime         time.Time
	baseTimeError    error
	benchTimeGrinder *TimeGrinder
	randStringBuff   []byte
	cfg              Config
)

func init() {
	rand.Seed(SEED)
	baseTime, baseTimeError = time.Parse("01-02-2006 15:04:05", "07-04-2014 16:30:45")
	benchTimeGrinder, _ = New(cfg)
	randStringBuff = make([]byte, RAND_BUFF_SIZE)

	for i := 0; i < len(randStringBuff); i++ {
		randStringBuff[i] = byte(rand.Int()%94 + 32)
	}
}

func TestStart(t *testing.T) {
	if baseTimeError != nil {
		t.Fatal(baseTimeError)
	}
}

func TestGlobalExtractor(t *testing.T) {
	tests := make([]error, 8)
	formats := []string{
		time.UnixDate,
		time.RubyDate,
		time.RFC3339,
		time.Stamp,
		time.RFC850,
		`2006-01-02 15:04:05`,     //dpkg
		`Jan _2 15:04:05`,         //syslog
		`1-2-2006 15:04:05.99999`, //gravwell format
	}
	//do this in parallel to try and force collisions
	var wg sync.WaitGroup
	wg.Add(len(tests))
	for i := range tests {
		go func(errp *error, w *sync.WaitGroup, format string) {
			w.Done()
			for j := 0; j < BENCH_LOOP_COUNT; j++ {
				val := time.Now().Add(time.Duration(j) * -1).Format(format)
				if _, ok, err := Extract([]byte(val)); err != nil {
					*errp = err
				} else if !ok {
					*errp = fmt.Errorf("missed extract on %q", val)
				}
			}
			return
		}(&tests[i], &wg, formats[i])
	}
	wg.Wait()
	for i := range tests {
		if tests[i] != nil {
			t.Fatalf("Failed on #%d %q: %v", i, formats[i], tests[i])
		}
	}
}

func TestGlobalMatcher(t *testing.T) {
	tests := make([]error, 8)
	formats := []string{
		time.UnixDate,
		time.RubyDate,
		time.RFC3339,
		time.Stamp,
		time.RFC850,
		`2006-01-02 15:04:05`,     //dpkg
		`Jan _2 15:04:05`,         //syslog
		`1-2-2006 15:04:05.99999`, //gravwell format
	}
	var wg sync.WaitGroup
	wg.Add(len(tests))
	for i := range tests {
		go func(errp *error, w *sync.WaitGroup, format string) {
			w.Done()
			for j := 0; j < BENCH_LOOP_COUNT; j++ {
				val := time.Now().Add(time.Duration(j)*time.Second).Format(format) + "somedata"
				if _, _, ok := Match([]byte(val)); !ok {
					t.Errorf("did not match %s", val)
				}
			}
			return
		}(&tests[i], &wg, formats[i])
	}
	wg.Wait()
	for i := range tests {
		if tests[i] != nil {
			t.Fatalf("Failed on %q: %v", formats[i], tests[i])
		}
	}
}

// TestYearGuess validates timegrinder's year-guessing logic for
// timestamps which don't include a year (e.g. SyslogFormat)
func TestYearGuess(t *testing.T) {
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Set to local time
	tg.SetLocalTime()

	// Define a custom format and add it
	custom := CustomFormat{
		Name:   `customfoo`,
		Regex:  `\d{1,2}\.\d{1,2}_\d{1,2}\.\d{1,2}\.\d{1,2}(.\d+)?`, //example 2021.2.14_13:54:22.9
		Format: `01.02_15.04.05.999999999`,
	}
	if err := custom.Validate(); err != nil {
		t.Fatal(err)
	}
	if proc, err := NewCustomProcessor(custom); err != nil {
		t.Fatal(err)
	} else {
		tg.AddProcessor(proc)
	}

	tester := func(name, format string, tg *TimeGrinder) {
		// Grab the current time ("now")
		now := time.Now()
		// Subtract 24 hours from now, format it as syslog
		// Attempt to extract with timegrinder
		x := []byte(now.Add(-24 * time.Hour).Format(format))
		out, ok, err := tg.Extract(x)
		if !ok {
			t.Fatalf("%v could not extract from %s", name, x)
		} else if err != nil {
			t.Fatalf("%v could not extract from %s: %v", name, x, err)
		}
		// The result should be in the past
		if !out.Before(now) {
			t.Fatalf("%v timestamp from 24 hours ago is not in the past", name)
		}

		// Add 24 hours to now, format it as syslog
		// Attempt to extract with timegrinder
		x = []byte(now.Add(24 * time.Hour).Format(format))
		out, ok, err = tg.Extract(x)
		if !ok {
			t.Fatalf("%v could not extract from %s", name, x)
		} else if err != nil {
			t.Fatalf("%v could not extract from %s: %v", name, x, err)
		}
		// The result should be in the future
		if !out.After(now) {
			t.Fatalf("%v timestamp from 24 hours ahead is not in the future", name)
		}

		// Add 26 hours to now, format it as syslog
		// Attempt to extract with timegrinder
		x = []byte(now.Add(26 * time.Hour).Format(format))
		out, ok, err = tg.Extract(x)
		if !ok {
			t.Fatalf("%v could not extract from %s", name, x)
		} else if err != nil {
			t.Fatalf("%v could not extract from %s: %v", name, x, err)
		}
		// The result should be in the past (threshold is 25 hours)
		if !out.Before(now) {
			t.Fatalf("%v timestamp from 26 hours ahead is not in the past", name)
		}
	}

	tester("syslog", SyslogFormat, tg)
	tester(custom.Name, custom.Format, tg)
}

func TestTooManyDigitsUnix(t *testing.T) {
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	candidate := `1511802599000000000000 CQsz7E4Wiy30uCtBR3 199.58.81.140 37358 198.46.205.70 9998 data_before_established	- F bro`
	_, ok, err := tg.Extract([]byte(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("Failed to extract timestamp " + candidate)
	}
}

func TestExactUnix(t *testing.T) {
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctime, err := time.Parse(time.RFC3339Nano, `2017-11-27T17:09:59Z`)
	if err != nil {
		t.Fatal(err)
	}
	candidate := `1511802599`
	ts, ok, err := tg.Extract([]byte(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Failed to extract timestamp " + candidate)
	}
	if ctime != ts {
		t.Fatalf("Timestamp extraction is wrong: %v != %v", ctime, ts)
	}
}

func TestNonDigitUnix(t *testing.T) {
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctime, err := time.Parse(time.RFC3339Nano, `2017-11-27T17:09:59Z`)
	if err != nil {
		t.Fatal(err)
	}
	candidate := `1511802599CQsz7E4Wiy30uCtBR3 199.58.81.140 37358 198.46.205.70 9998 data_before_established	- F bro`
	ts, ok, err := tg.Extract([]byte(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Failed to extract timestamp " + candidate)
	}
	if ctime != ts {
		t.Fatalf("Timestamp extraction is wrong: %v != %v", ctime, ts)
	}
}

func TestLDAP(t *testing.T) {
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctime, err := time.Parse(time.RFC3339Nano, `2017-11-27T17:09:59Z`)
	if err != nil {
		t.Fatal(err)
	}
	candidate := `131562761990000000 CQsz7E4Wiy30uCtBR3 199.58.81.140 37358 198.46.205.70 9998 data_before_established	- F bro`
	ts, ok, err := tg.Extract([]byte(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Failed to extract timestamp " + candidate)
	}
	if ctime != ts {
		t.Fatalf("Timestamp extraction is wrong: %v != %v", ctime, ts)
	}
}

func TestUnixMilli(t *testing.T) {
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctime, err := time.Parse(time.RFC3339Nano, `2017-11-27T17:09:59.453396081Z`)
	if err != nil {
		t.Fatal(err)
	}
	candidate := `1511802599.453396 CQsz7E4Wiy30uCtBR3 199.58.81.140 37358 198.46.205.70 9998 data_before_established	- F bro`
	ts, ok, err := tg.Extract([]byte(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Failed to extract timestamp " + candidate)
	}
	if ctime != ts {
		t.Fatal("Timestamp extraction is wrong")
	}

	candidate = ` 1511802599.453396 CQsz7E4Wiy30uCtBR3 199.58.81.140 37358 198.46.205.70 9998 data_before_established	- F bro`
	ts, ok, err = tg.Extract([]byte(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Failed to extract timestamp " + candidate)
	}
	if ctime != ts {
		t.Fatal("Timestamp extraction is wrong")
	}

	candidate = `	   1511802599.453396 CQsz7E4Wiy30uCtBR3 199.58.81.140 37358 198.46.205.70 9998 data_before_established	- F bro`
	ts, ok, err = tg.Extract([]byte(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Failed to extract timestamp " + candidate)
	}
	if ctime != ts {
		t.Fatal("Timestamp extraction is wrong")
	}

	candidate = `foobar	   1511802599.453396 CQsz7E4Wiy30uCtBR3 199.58.81.140 37358 198.46.205.70 9998 data_before_established	- F bro`
	ts, ok, err = tg.Extract([]byte(candidate))
	if ok {
		t.Fatalf("Improperly extracted unix milli from line with prefixed text")
	}

	candidate = `1511802599.453396`
	ts, ok, err = tg.Extract([]byte(candidate))
	if err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("Failed to extract timestamp " + candidate)
	} else if ctime != ts {
		t.Fatal("Timestamp extraction is wrong")
	}
}

func TestUnixNano(t *testing.T) {
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctime, err := time.Parse(time.RFC3339Nano, `2017-11-27T17:09:59.453396081Z`)
	if err != nil {
		t.Fatal(err)
	}
	candidate := `1511802599453396081 CQsz7E4Wiy30uCtBR3 199.58.81.140 37358 198.46.205.70 9998 data_before_established	- F bro`
	ts, ok, err := tg.Extract([]byte(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Failed to extract timestamp " + candidate)
	}
	if ctime != ts {
		t.Fatalf("Timestamp extraction is wrong: %v != %v", ctime, ts)
	}
}

func TestUnixSeconds(t *testing.T) {
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctime, err := time.Parse(time.RFC3339Nano, `2017-11-27T17:09:59Z`)
	if err != nil {
		t.Fatal(err)
	}
	candidate := `1511802599 CQsz7E4Wiy30uCtBR3 199.58.81.140 37358 198.46.205.70 9998 data_before_established	- F bro`
	ts, ok, err := tg.Extract([]byte(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Failed to extract timestamp " + candidate)
	}
	if ctime != ts {
		t.Fatalf("Timestamp extraction is wrong: %v != %v", ctime, ts)
	}
}

func TestUnixMs(t *testing.T) {
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctime, err := time.Parse(time.RFC3339Nano, `2017-11-27T17:09:59.453Z`)
	if err != nil {
		t.Fatal(err)
	}
	candidate := `1511802599453 CQsz7E4Wiy30uCtBR3 199.58.81.140 37358 198.46.205.70 9998 data_before_established	- F bro`
	ts, ok, err := tg.Extract([]byte(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Failed to extract timestamp " + candidate)
	}
	if ctime != ts {
		t.Fatalf("Timestamp extraction is wrong: %v != %v", ctime, ts)
	}
}

func TestCustomManual(t *testing.T) {
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, ok, err := tg.Extract([]byte(`11-20-2017 10:04:56.407 [80000037] webserver/bgSearch.go:502`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Failed to extract timestamp")
	}
}

func TestAnsiC(t *testing.T) {
	if err := runFullSecTests(time.ANSIC); err != nil {
		t.Fatal(err)
	}
}

func TestUnixDate(t *testing.T) {
	err := runFullSecTests(time.UnixDate)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRubyDate(t *testing.T) {
	err := runFullSecTests(time.RubyDate)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRFC822(t *testing.T) {
	err := runFullNoSecTests(time.RFC822)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRFC822Z(t *testing.T) {
	err := runFullNoSecTests(time.RFC822)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRFC850(t *testing.T) {
	err := runFullSecTests(time.RFC850)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRFC1123(t *testing.T) {
	err := runFullSecTests(time.RFC1123)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRFC1123Z(t *testing.T) {
	err := runFullSecTests(time.RFC1123Z)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRFC3339(t *testing.T) {
	err := runFullSecTests(time.RFC3339)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRFC3339Nano(t *testing.T) {
	err := runFullSecTests(time.RFC3339Nano)
	if err != nil {
		t.Fatal(err)
	}
}

func TestApache(t *testing.T) {
	if err := runFullSecTests(ApacheFormat); err != nil {
		t.Fatal(err)
	}

	//now run on TZ and non TZ to make sure the right one matches
	apTz := NewApacheProcessor()
	apNtz := NewApacheNoTZProcessor()

	cand := []byte(`test 14/Mar/2019:12:13:00 -0700`)

	if _, ok, _ := apTz.Extract(cand, nil); !ok {
		t.Fatal("failed extraction")
	} else if _, ok, _ = apNtz.Extract(cand, nil); ok {
		t.Fatal("extrated on tz apache")
	}

}

func TestSyslog(t *testing.T) {
	err := runFullSecTestsCurr(SyslogFormat)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSyslogFile(t *testing.T) {
	err := runFullSecTestsCurr(SyslogFileFormat)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSyslogFileTZ2(t *testing.T) {
	err := runFullSecTestsCurr(SyslogFileTZFormat)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDPKGFile(t *testing.T) {
	if err := runFullSecTestsCurr(DPKGFormat); err != nil {
		t.Fatal(err)
	}
}

func TestNGINXFile(t *testing.T) {
	if err := runFullSecTestsCurr(NGINXFormat); err != nil {
		t.Fatal(err)
	}
}

func TestZonelessRFC3339(t *testing.T) {
	if err := runFullSecTestsCurr(ZonelessRFC3339Format); err != nil {
		t.Fatal(err)
	}
}

func TestSyslogVariant(t *testing.T) {
	if err := runFullSecTests(SyslogVariantFormat); err != nil {
		t.Fatal(err)
	}
}

func TestUnpaddedDateTime(t *testing.T) {
	if err := runFullSecTestsCurr(UnpaddedDateTimeFormat); err != nil {
		t.Fatal(err)
	}
}

func TestGravwell(t *testing.T) {
	p := NewGravwell()
	if err := runFullSecTestsSingle(p, GravwellFormat); err != nil {
		t.Fatal(err)
	}
	if err := runFullSecTests(GravwellFormat); err != nil {
		t.Fatal(err)
	}
}

func TestNewUserProc(t *testing.T) {
	fstr := `02/01/2006 15:04:05.99999`
	rspStr := `\d\d/\d\d/\d\d\d\d\s\d\d\:\d\d\:\d\d.\d{1,5}`
	tstr := `14/12/1984 12:55:33.43212`
	tg, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewUserProcessor(`britishtime`, rspStr, fstr)
	if err != nil {
		t.Fatal(err)
	}
	n, err := tg.AddProcessor(p)
	if err != nil {
		t.Fatal(err)
	}
	if n < 0 {
		t.Fatal("bad add")
	}

	x := []byte(`test ` + tstr)
	tt, ok, err := tg.Extract(x)
	if !ok {
		t.Fatal("Failed")
	} else if err != nil {
		t.Fatal(err)
	} else if tt.Format(fstr) != tstr {
		t.Fatal("times not equiv", tstr, tt.Format(fstr))
	}
}

func TestSeedHit(t *testing.T) {
	lcfg := Config{
		EnableLeftMostSeed: true,
	}
	tval := []byte(`2018-04-19T05:50:19-07:00 2018-04-15T00:00:00Z 1234567890 02-03-2018 12:30:00`)
	tg, err := New(lcfg)
	if err != nil {
		t.Fatal(err)
	}
	tt, err := time.Parse(time.RFC3339, `2018-04-19T05:50:19-07:00`)
	if err != nil {
		t.Fatal(err)
	}
	tgt, ok, err := tg.Extract(tval)
	if err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("Missed")
	}
	if !tt.UTC().Equal(tgt) {
		t.Fatal(fmt.Errorf("grabbed wrong time: %v != %v", tt.UTC(), tgt.UTC()))
	}
}

func TestOverrideFormat(t *testing.T) {
	good := []string{`AnsiC`, `Unix`, `Ruby`, `RFC822`, `RFC822Z`, `RFC850`, `RFC1123`, `RFC1123Z`,
		`RFC3339`, `RFC3339Nano`, `Apache`, `ApacheNoTz`, `Syslog`, `SyslogFile`, `DPKG`,
		`Gravwell`, `NGINX`, `UnixMilli`, `ZonelessRFC3339`, `SyslogVariant`}
	bad := []string{`stuff`, "thigns and other stuff", "", "sdlkfjdslkj fsldkj"}
	for i := range good {
		if _, err := FormatDirective(good[i]); err != nil {
			t.Fatal("Failed to find directive for", good[i], err)
		}
	}
	for i := range bad {
		if v, err := FormatDirective(bad[i]); err == nil || v != `` {
			t.Fatal("Accidentally saw", bad[i], "as good")
		}
	}
}

func TestFormatChange(t *testing.T) {
	config := Config{
		EnableLeftMostSeed: true,
	}
	tg, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	tg.SetUTC()
	ts, err := time.Parse(`2006-01-02T15:04:05-07:00`, "2019-03-18T01:00:00.000+09:00")
	if err != nil {
		t.Fatal(err)
	}
	vals := []string{
		`2019-03-18T01:00:00.000+09:00`,
		`2019-03-17T17:00:00.000`,
		`2019-03-18T03:00:00.000+09:00`,
	}
	for i := range vals {
		et, ok, err := tg.Extract([]byte(vals[i]))
		if err != nil {
			t.Fatal(err)
		} else if !ok {
			t.Fatal("Failed to extract")
		}
		if !ts.UTC().Equal(et.UTC()) {
			t.Fatalf("Extracted time is bad\n%v != %v\n%v",
				ts.Format(time.RFC3339),
				et.Format(time.RFC3339),
				vals[i])
		}
		ts = ts.Add(time.Hour)
	}

}

func TestCustomFormats(t *testing.T) {
	cf := CustomFormat{
		Name:   `customfoo`,
		Regex:  `\d{4}\.\d{1,2}\.\d{1,2}_\d{1,2}\.\d{1,2}\.\d{1,2}(.\d+)?`, //example 2021.2.14_13:54:22.9
		Format: `2006.01.02_15.04.05.999999999`,
	}
	if p, err := NewCustomProcessor(cf); err != nil {
		t.Fatal(err)
	} else if err := runFullSecTestsSingle(p, p.Format()); err != nil {
		t.Fatal(err)
	}

	if p, err := NewCustomProcessor(cf); err != nil {
		t.Fatal(err)
	} else if tg, err := New(cfg); err != nil {
		t.Fatal(err)
	} else if _, err = tg.AddProcessor(p); err != nil {
		t.Fatal(err)
	} else if err := runFullSecTestsCustom(tg, cf.Format); err != nil {
		t.Fatal(err)
	}

	cf = CustomFormat{
		Name:   `customfoo2`,
		Regex:  `\d{4}\.\d{1,2}\.\d{1,2}_\d{1,2}\.\d{1,2}\.\d{1,2}(_\d+)?`, //example 2021.2.14_13:54:22.9
		Format: `2006.01.02_15.04.05_999999999`,
	}

	if p, err := NewCustomProcessor(cf); err != nil {
		t.Fatal(err)
	} else if tg, err := New(cfg); err != nil {
		t.Fatal(err)
	} else if _, err = tg.AddProcessor(p); err != nil {
		t.Fatal(err)
	} else if err := runFullSecTestsCustom(tg, cf.Format); err != nil {
		t.Fatal(err)
	}
}

func TestCustomCollision(t *testing.T) {
	cf := CustomFormat{
		Name:   string(AnsiC),
		Regex:  `\d{4}\.\d{1,2}\.\d{1,2}_\d{1,2}\.\d{1,2}\.\d{1,2}(.\d+)?`, //example 2021.2.14_13:54:22.9
		Format: `2006.01.02_15.04.05.999999999`,
	}
	//check with a duplicate, make sure it gets kicked
	if p, err := NewCustomProcessor(cf); err != nil {
		t.Fatal(err)
	} else if tg, err := New(cfg); err != nil {
		t.Fatal(err)
	} else if _, err = tg.AddProcessor(p); err == nil {
		t.Fatal("Failed to catch duplicate name")
	}
}

func TestCustomMissingDate(t *testing.T) {
	cf := CustomFormat{
		Name:   `missingdate`,
		Regex:  `\d{1,2}\.\d{1,2}\.\d{1,2}(.\d+)?`, //example 2021.2.14_13:54:22.9
		Format: `15.04.05.999999999`,
	}

	if p, err := NewCustomProcessor(cf); err != nil {
		t.Fatal(err)
	} else if err := runFullSecTestsSingleCurr(p, p.Format()); err != nil {
		t.Fatal(err)
	}

	//check with a format that does not have a date, make sure today's date gets applied
	if p, err := NewCustomProcessor(cf); err != nil {
		t.Fatal(err)
	} else if tg, err := New(cfg); err != nil {
		t.Fatal(err)
	} else if _, err = tg.AddProcessor(p); err != nil {
		t.Fatal(err)
	} else if err := runFullSecTestsCustomCurr(tg, cf.Format); err != nil {
		t.Fatal(err)
	}
}

func TestAll(t *testing.T) {
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range tg.procs {
		if layout := p.Format(); layout != `` {
			ts := time.Now().UTC()
			val := ts.Format(layout)
			//try it with the specific processor
			if _, _, ok := p.Match([]byte(val)); !ok {
				t.Fatalf("%s missed match on %q %q", p.Name(), layout, val)
			} else if _, _, loc := p.Extract([]byte("foobar "+val+" barbaz"), time.UTC); loc < 0 {
				t.Fatalf("%s missed extract on %q %q %d", p.Name(), layout, val, loc)
			}
		}
	}
}

type testSet struct {
	name string
	data string
	ts   time.Time
}

var testSetList = []testSet{
	testSet{name: `AnsiC`, data: `this is AnsiC Jan 10 12:44:18 2022`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `Unix`, data: `some Unix Jan 10 12:44:18 UTC 2022`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `Ruby`, data: `ruby format Jan 10 12:44:18 +0000 2022`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `RFC822`, data: `stuff 10 Jan 22 12:44 UTC`, ts: time.Date(2022, time.January, 10, 12, 44, 0, 0, time.UTC)},
	testSet{name: `RFC822Z`, data: `stuff 10 Jan 22 12:44 +0000`, ts: time.Date(2022, time.January, 10, 12, 44, 0, 0, time.UTC)},
	testSet{name: `RFC850`, data: `10-Jan-22 12:44:18 UTC`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `RFC1123`, data: `stuff 10 Jan 2022 12:44:18 UTC `, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `RFC1123Z`, data: `stuff 10 Jan 2022 12:44:18 +0000 `, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `RFC3339`, data: `lsdkfj 2022-01-10T12:44:18Z00:00`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `RFC3339Nano`, data: `lsdkfj 2022-01-10T12:44:18.123456000Z00:00`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 123456000, time.UTC)},
	testSet{name: `ZonelessRFC3339`, data: `lsdkfj 2022-01-10T12:44:18.123456000`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 123456000, time.UTC)},
	testSet{name: `Apache`, data: `apache 10/Jan/2022:12:44:18 +0000`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `ApacheNoTz`, data: `apache 10/Jan/2022:12:44:18`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `NGINX`, data: `nginx 2022/01/10 12:44:18`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `Syslog`, data: fmt.Sprintf(`xyz %s %d 12:44:18`, monthAbrev(), today()), ts: time.Date(year(), month(), today(), 12, 44, 18, 0, time.UTC)},
	testSet{name: `SyslogFile`, data: `sf 2022-01-10T12:44:18.123456000+00:00`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 123456000, time.UTC)},
	testSet{name: `SyslogFileTZ`, data: `sf 2022-01-10T12:44:18.123456000+0000`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 123456000, time.UTC)},
	testSet{name: `SyslogVariant`, data: `sf Jan 10 2022 12:44:18`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `DPKG`, data: `dpgk 2022-01-10 12:44:18`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `UnpaddedDateTime`, data: `2022-1-10 12:44:18`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `UnpaddedMilliDateTime`, data: `2022-1-10 12:44:18.123456000`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 123456000, time.UTC)},
	testSet{name: `Gravwell`, data: `1-10-2022 12:44:04.12345`, ts: time.Date(2022, time.January, 10, 12, 44, 4, 123450000, time.UTC)},
	testSet{name: `Bind`, data: `10-Jan-2022 12:44:04.123`, ts: time.Date(2022, time.January, 10, 12, 44, 4, 123000000, time.UTC)},
	testSet{name: `UK`, data: `10/01/2022 12:44:04,12345`, ts: time.Date(2022, time.January, 10, 12, 44, 4, 123450000, time.UTC)},
	testSet{name: `LDAP`, data: `132862922580000000 ldap garbage`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `UnixSeconds`, data: `1641818658 unix`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `UnixMs`, data: `1641818658123 unix`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 123000000, time.UTC)},
	testSet{name: `UnixNano`, data: `1641818658123456000 unix`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 123456000, time.UTC)},
	testSet{name: `UnixMilli`, data: `1641818658.0 unix`, ts: time.Date(2022, time.January, 10, 12, 44, 18, 0, time.UTC)},
	testSet{name: `DirectAdmin`, data: `2022:12:16-15:14:23: 'xxx.xx.xx.xx' failed login attempt. Account 'admin'`, ts: time.Date(2022, time.December, 16, 15, 14, 23, 0, time.UTC)},
}

func TestExtractions(t *testing.T) {
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	for _, tsl := range testSetList {
		if p, ok := tg.GetProcessor(tsl.name); !ok {
			t.Fatalf("Failed to find processor %s", tsl.name)
		} else if ts, ok, loc := p.Extract([]byte(tsl.data), time.UTC); !ok || loc < 0 {
			t.Fatalf("processor %s did not match %s - %v %v", tsl.name, tsl.data, ok, loc)
		} else if !ts.Equal(tsl.ts) {
			t.Fatalf("processor %s output not equal: %v != %v", tsl.name, ts, tsl.ts)
		}
	}
}

func runFullSecTestsCurr(format string) error {
	tg, err := New(cfg)
	if err != nil {
		return err
	}
	return runFullSecTestsCustomCurr(tg, format)
}

func runFullSecTestsCustomCurr(tg *TimeGrinder, format string) error {
	b := time.Now().UTC()
	b = b.Truncate(24 * time.Hour)
	for i := 0; i < TEST_COUNT; i++ {
		diff := time.Duration(rand.Int63()%120)*time.Second + time.Duration(rand.Int63()%1000)*time.Millisecond
		t := b.Add(diff)
		ts := genTimeLine(t, format)
		tgt, ok, err := tg.Extract(ts)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("Failed to extract timestamp [%s]", ts)
		}
		if !t.UTC().Equal(tgt) {
			//check if its just missing some precision, some formats don't have it
			if !t.Truncate(time.Second).Equal(tgt) {
				return fmt.Errorf("Timestamps not equal: %v != %v", t.UTC(), tgt)
			}
		}
	}
	return nil
}
func runFullSecTests(format string) error {
	tg, err := New(cfg)
	if err != nil {
		return err
	}
	return runFullSecTestsCustom(tg, format)
}

func runFullSecTestsCustom(tg *TimeGrinder, format string) error {
	for i := 0; i < TEST_COUNT; i++ {
		diff := time.Duration(rand.Int63()%100000)*time.Second + time.Duration(rand.Int63()%1000)*time.Millisecond
		t := baseTime.Add(diff)
		ts := genTimeLine(t.UTC(), format)
		tgt, ok, err := tg.Extract(ts)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("Failed to extract timestamp (%s / %s) [%s]", format, t.UTC(), ts)
		}
		if !t.Equal(tgt) {
			//check if its just missing some precision, some formats don't have it
			if !t.Truncate(time.Second).Equal(tgt) {
				return fmt.Errorf("Timestamps not equal: %v != %v", t, tgt)
			}
		}
	}
	return nil
}

func runFullSecTestsSingle(p Processor, format string) error {
	for i := 0; i < TEST_COUNT; i++ {
		diff := time.Duration(rand.Int63()%100000)*time.Second + time.Duration(rand.Int63()%1000)*time.Millisecond
		t := baseTime.Add(diff)
		ts := genTimeLine(t.UTC(), format)
		tgt, ok, _ := p.Extract(ts, time.UTC)
		if !ok {
			return fmt.Errorf("Failed to extract timestamp (%s)(%s) {%s}\n[%s]",
				format, p.Format(), p.ExtractionRegex(), ts)
		}
		if !t.Equal(tgt) {
			//check if its just missing some precision, some formats don't have it
			if !t.Truncate(time.Second).Equal(tgt) {
				return fmt.Errorf("Timestamps not equal: %v != %v", t, tgt)
			}
		}
	}
	return nil

}

func runFullSecTestsSingleCurr(p Processor, format string) error {
	b := time.Now().UTC().Truncate(24 * time.Hour)
	for i := 0; i < TEST_COUNT; i++ {
		diff := time.Duration(rand.Int63()%60)*time.Second + time.Duration(rand.Int63()%1000)*time.Millisecond
		t := b.Add(diff)
		ts := genTimeLine(t.UTC(), format)
		tgt, ok, _ := p.Extract(ts, time.UTC)
		if !ok {
			return fmt.Errorf("Failed to extract timestamp (%s)(%s) {%s}\n[%s]",
				format, p.Format(), p.ExtractionRegex(), ts)
		}
		if !t.Equal(tgt) {
			if !t.Truncate(time.Second).Equal(tgt) {
				return fmt.Errorf("Timestamps not equal: %v != %v", t.UTC(), tgt.UTC())
			}
		}
	}
	return nil

}

func runFullNoSecTests(format string) error {
	tg, err := New(cfg)
	if err != nil {
		return err
	}
	for i := 0; i < TEST_COUNT; i++ {
		s := baseTime.Unix() + (rand.Int63() % 100000)
		s = s - (s % 60)
		t := time.Unix(s, 0)
		ts := genTimeLine(t.UTC(), format)
		tgt, ok, err := tg.Extract(ts)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("Failed to extract timestamp [%s]", ts)
		}
		if !t.Equal(tgt) {
			return fmt.Errorf("Timestamps not equal: %v != %v", t, tgt)
		}
	}
	return nil
}

func genTimeLine(t time.Time, format string) []byte {
	size := rand.Int() % RAND_BUFF_SIZE
	offset := rand.Int() % (RAND_BUFF_SIZE - size)
	end := offset + size
	return []byte(fmt.Sprintf("%s %v", randStringBuff[offset:end], t.Format(format)))
}

func BenchmarkAnsiC(b *testing.B) {
	b.StopTimer()
	candidateTime := baseTime.Format(time.ANSIC)
	benchTimeGrinder.Extract([]byte(candidateTime)) //to get it cached
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		_, ok, err := benchTimeGrinder.Extract([]byte(candidateTime))
		if err != nil {
			b.Fatal(err)
		}
		if !ok {
			b.Fatal("Missed extraction")
		}
	}
}

func BenchmarkAnsiCNoCheck(b *testing.B) {
	b.StopTimer()
	candidateTime := baseTime.Format(time.ANSIC)
	benchTimeGrinder.Extract([]byte(candidateTime))
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		_, ok, _ := benchTimeGrinder.Extract([]byte(candidateTime))
		if !ok {
			b.Fatal("Missed extraction")
		}
	}
}

func year() int {
	return time.Now().UTC().Year()
}

func today() int {
	return time.Now().UTC().Day()
}

func month() time.Month {
	return time.Now().UTC().Month()
}

func monthAbrev() string {
	switch month() {
	case time.January:
		return `Jan`
	case time.February:
		return `Feb`
	case time.March:
		return `Mar`
	case time.April:
		return `Apr`
	case time.May:
		return `May`
	case time.June:
		return `June`
	case time.July:
		return `July`
	case time.August:
		return `Aug`
	case time.September:
		return `Sept`
	case time.October:
		return `Oct`
	case time.November:
		return `Nov`
	case time.December:
		return `Dec`
	}
	return ``
}
