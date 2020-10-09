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
	var wg sync.WaitGroup
	wg.Add(len(tests))
	for i := range tests {
		go func(errp *error, w *sync.WaitGroup, format string) {
			w.Done()
			for j := 0; j < 128; j++ {
				if _, ok, err := Extract([]byte(time.Now().Format(format))); err != nil {
					*errp = err
				} else if !ok {
					*errp = errors.New("missed extract")
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

func runFullSecTestsCurr(format string) error {
	tg, err := New(cfg)
	if err != nil {
		return err
	}
	b := time.Now()
	b = b.Round(time.Second)
	for i := 0; i < TEST_COUNT; i++ {
		t := b.Add(time.Duration(rand.Int63()%100000) * time.Second)
		ts := genTimeLine(t.UTC(), format)
		tgt, ok, err := tg.Extract(ts)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("Failed to extract timestamp [%s]", ts)
		}
		if !t.UTC().Equal(tgt) {
			return fmt.Errorf("Timestamps not equal: %v != %v", t.UTC(), tgt)
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
		t := baseTime.Add(time.Duration(rand.Int63()%100000) * time.Second)
		ts := genTimeLine(t.UTC(), format)
		tgt, ok, err := tg.Extract(ts)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("Failed to extract timestamp (%s) [%s]", format, ts)
		}
		if !t.Equal(tgt) {
			return fmt.Errorf("Timestamps not equal: %v != %v", t, tgt)
		}
	}
	return nil
}

func runFullSecTestsSingle(p Processor, format string) error {
	for i := 0; i < TEST_COUNT; i++ {
		t := baseTime.Add(time.Duration(rand.Int63()%100000) * time.Second)
		ts := genTimeLine(t.UTC(), format)
		tgt, ok, _ := p.Extract(ts, time.UTC)
		if !ok {
			return fmt.Errorf("Failed to extract timestamp (%s)(%s) {%s}\n[%s]",
				format, p.Format(), p.ExtractionRegex(), ts)
		}
		if !t.Equal(tgt) {
			return fmt.Errorf("Timestamps not equal: %v != %v", t, tgt)
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
