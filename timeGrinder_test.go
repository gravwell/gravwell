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
	benchTimeGrinder, _ = NewTimeGrinder(cfg)
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

func TestUnixMilli(t *testing.T) {
	tg, err := NewTimeGrinder(cfg)
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
}

func TestCustomManual(t *testing.T) {
	tg, err := NewTimeGrinder(cfg)
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

func TestCustom(t *testing.T) {
	if err := runFullSecTests(CUSTOM1_MILLI_MSG_FORMAT); err != nil {
		t.Fatal(err)
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
	err := runFullSecTests(APACHE_FORMAT)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSyslog(t *testing.T) {
	err := runFullSecTestsCurr(SYSLOG_FORMAT)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSyslogFile(t *testing.T) {
	err := runFullSecTestsCurr(SYSLOG_FILE_FORMAT)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSyslogFileTZ2(t *testing.T) {
	err := runFullSecTestsCurr(SYSLOG_FILE_FORMAT_TZ2)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDPKGFile(t *testing.T) {
	if err := runFullSecTestsCurr(DPKG_MSG_FORMAT); err != nil {
		t.Fatal(err)
	}
}

func TestNGINXFile(t *testing.T) {
	if err := runFullSecTestsCurr(NGINX_FORMAT); err != nil {
		t.Fatal(err)
	}
}

func TestZonelessRFC3339(t *testing.T) {
	if err := runFullSecTestsCurr(ZONELESS_RFC3339_FORMAT); err != nil {
		t.Fatal(err)
	}
}

func TestSyslogVariant(t *testing.T) {
	if err := runFullSecTests(SYSLOG_VARIANT); err != nil {
		t.Fatal(err)
	}
}

func TestUnpaddedDateTime(t *testing.T) {
	err := runFullSecTestsCurr(UNPADDED_DATE_TIME)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSeedHit(t *testing.T) {
	lcfg := Config{
		EnableLeftMostSeed: true,
	}
	tval := []byte(`2018-04-19T05:50:19-07:00 2018-04-15T00:00:00Z 1234567890 02-03-2018 12:30:00`)
	tg, err := NewTimeGrinder(lcfg)
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
		`Custom1Milli`, `NGINX`, `UnixMilli`, `ZonelessRFC3339`, `SyslogVariant`}
	bad := []string{`stuff`, "thigns and other stuff", "", "sdlkfjdslkj fsldkj"}
	for i := range good {
		if _, err := FormatDirective(good[i]); err != nil {
			t.Fatal("Failed to find directive for", good[i], err)
		}
	}
	for i := range bad {
		if id, err := FormatDirective(bad[i]); err == nil || id != -1 {
			t.Fatal("Accidentally saw", bad[i], "as good")
		}
	}
}

func runFullSecTestsCurr(format string) error {
	tg, err := NewTimeGrinder(cfg)
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
	tg, err := NewTimeGrinder(cfg)
	if err != nil {
		return err
	}
	for i := 0; i < TEST_COUNT; i++ {
		t := baseTime.Add(time.Duration(rand.Int63()%100000) * time.Second)
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

func runFullNoSecTests(format string) error {
	tg, err := NewTimeGrinder(cfg)
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
