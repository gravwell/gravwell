/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var origTS = entry.UnixTime(1700000000, 0)

func TestTSBoundsConfig(t *testing.T) {
	if b, err := newTSBounds(``, ``); err != nil {
		t.Fatal(err)
	} else if b.window.MaxPastDelta != DefaultTimestampMaxPastDelta || b.window.MaxFutureDelta != DefaultTimestampMaxFutureDelta {
		t.Fatalf("bad default window: %+v", b.window)
	}
	if b, err := newTSBounds(`-48h`, `1h`); err != nil {
		t.Fatal(err)
	} else if b.window.MaxPastDelta != 48*time.Hour || b.window.MaxFutureDelta != time.Hour {
		t.Fatalf("bad window: %+v", b.window)
	}
	for _, v := range [][2]string{{`foo`, ``}, {``, `bar`}, {`0`, ``}, {``, `0s`}} {
		if _, err := newTSBounds(v[0], v[1]); err == nil {
			t.Fatalf("failed to catch bad deltas %q %q", v[0], v[1])
		}
	}
	if _, err := NewJsonTimestamp(JsonTimestampConfig{Path: `ts`, Timestamp_Max_Future_Delta: `bad`}); err == nil {
		t.Fatal("failed to catch bad jsontimeextract delta")
	}
	if _, err := NewVpcProcessor(VpcConfig{Timestamp_Max_Past_Delta: `bad`}); err == nil {
		t.Fatal("failed to catch bad vpc delta")
	}
}

func TestJsonTimestampOutOfRange(t *testing.T) {
	jt, err := NewJsonTimestamp(JsonTimestampConfig{Path: `ts`})
	if err != nil {
		t.Fatal(err)
	}
	good := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	tests := []struct {
		ts    string
		valid bool
	}{
		{`9999-12-31T23:59:59Z`, false},
		{`1969-12-31T23:59:59Z`, false},
		{time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339), false},
		{time.Now().Add(-2 * 365 * 24 * time.Hour).UTC().Format(time.RFC3339), false},
		{good.Format(time.RFC3339), true},
	}
	for _, tst := range tests {
		ent := &entry.Entry{TS: origTS, Data: []byte(fmt.Sprintf(`{"ts":%q}`, tst.ts))}
		if _, err := jt.Process([]*entry.Entry{ent}); err != nil {
			t.Fatal(err)
		}
		if ent.TS.Sec < 0 {
			t.Fatalf("%s produced negative TS %d", tst.ts, ent.TS.Sec)
		} else if tst.valid && !ent.TS.StandardTime().Equal(good) {
			t.Fatalf("%s was not applied: %v", tst.ts, ent.TS)
		} else if !tst.valid && ent.TS != origTS {
			t.Fatalf("%s was applied: %v", tst.ts, ent.TS)
		}
	}

	// year zero is treated as a missing year, never as year zero
	ent := &entry.Entry{TS: origTS, Data: []byte(`{"ts":"0000-06-15T12:00:00Z"}`)}
	if _, err := jt.Process([]*entry.Entry{ent}); err != nil {
		t.Fatal(err)
	} else if yr := ent.TS.StandardTime().UTC().Year(); yr != time.Now().Year() && yr != time.Now().Year()-1 {
		t.Fatalf("year zero timestamp was not tweaked: %v", ent.TS)
	}

	// a custom window applies too
	if jt, err = NewJsonTimestamp(JsonTimestampConfig{Path: `ts`, Timestamp_Max_Past_Delta: `24h`}); err != nil {
		t.Fatal(err)
	}
	ent = &entry.Entry{TS: origTS, Data: []byte(fmt.Sprintf(`{"ts":%q}`, time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339)))}
	if _, err := jt.Process([]*entry.Entry{ent}); err != nil {
		t.Fatal(err)
	} else if ent.TS != origTS {
		t.Fatalf("timestamp outside past delta was applied: %v", ent.TS)
	}
}

func TestRegexTimestampOutOfRange(t *testing.T) {
	rt, err := NewRegexTimestampProcessor(RegexTimestampConfig{
		Regex:         `ts=(?P<ts>\S+)`,
		TS_Match_Name: `ts`,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{`9999-12-31T23:59:59Z`, `1969-12-31T23:59:59Z`} {
		ent := &entry.Entry{TS: origTS, Data: []byte(`ts=` + v)}
		if _, err := rt.Process([]*entry.Entry{ent}); err != nil {
			t.Fatal(err)
		} else if ent.TS != origTS {
			t.Fatalf("%s was applied: %v", v, ent.TS)
		}
	}
	good := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	ent := &entry.Entry{TS: origTS, Data: []byte(`ts=` + good.Format(time.RFC3339))}
	if _, err := rt.Process([]*entry.Entry{ent}); err != nil {
		t.Fatal(err)
	} else if !ent.TS.StandardTime().Equal(good) {
		t.Fatalf("valid timestamp was not applied: %v", ent.TS)
	}
}

func TestCorelightOutOfRange(t *testing.T) {
	c, err := NewCorelight(CorelightConfig{Prefix: `zeek`}, &testTagger{})
	if err != nil {
		t.Fatal(err)
	}
	ent := &entry.Entry{TS: origTS, Data: []byte(`{"_path":"conn","ts":"9999-12-31T23:59:59Z","uid":"foo"}`)}
	if _, err := c.Process([]*entry.Entry{ent}); err != nil {
		t.Fatal(err)
	} else if ent.TS != origTS {
		t.Fatalf("out of range timestamp was applied: %v", ent.TS)
	}
}

func TestVpcStartOverflow(t *testing.T) {
	p, err := NewVpcProcessor(VpcConfig{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	tests := []struct {
		start int64
		valid bool
	}{
		{math.MaxInt64, false},
		{math.MinInt64, false},
		{-1, false},
		{now + 48*3600, false},
		{now - 2*365*24*3600, false},
		{now - 3600, true},
	}
	for _, tst := range tests {
		data := []byte(fmt.Sprintf(`{"logEvents":[{"message":"foo","extractedFields":{"start":"%d"}}]}`, tst.start))
		ents, err := p.Process([]*entry.Entry{{TS: origTS, Data: data}})
		if err != nil {
			t.Fatal(err)
		} else if len(ents) != 1 {
			t.Fatalf("bad entry count %d", len(ents))
		}
		if tst.valid && ents[0].TS != entry.UnixTime(tst.start, 0) {
			t.Fatalf("start %d was not applied: %v", tst.start, ents[0].TS)
		} else if !tst.valid && ents[0].TS != origTS {
			t.Fatalf("start %d was applied: %v", tst.start, ents[0].TS)
		}
	}
}

func TestCiscoISEOutOfRange(t *testing.T) {
	const hdr = `2020-11-23 12:50:16.963 -05:00`
	body := strayMerged[len(hdr):]
	good := time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond)
	tests := []struct {
		ts    string
		valid bool
	}{
		{`9999-12-31 23:59:59 +00:00`, false},
		{`0000-01-01 00:00:00 +00:00`, false},
		{`1969-12-31 23:59:59 +00:00`, false},
		{time.Now().Add(48 * time.Hour).UTC().Format(iseTimestampFormat), false},
		{time.Now().Add(-2 * 365 * 24 * time.Hour).UTC().Format(iseTimestampFormat), false},
		{good.Format(iseTimestampFormat), true},
	}
	for _, format := range []string{outputCEF, outputJSON} {
		p, err := NewCiscoISEProcessor(CiscoISEConfig{Output_Format: format, Drop_Misses: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, tst := range tests {
			ent := &entry.Entry{TS: origTS, Data: []byte(tst.ts + body)}
			if ents, err := p.Process([]*entry.Entry{ent}); err != nil {
				t.Fatal(err)
			} else if len(ents) != 1 {
				t.Fatalf("%s %s: bad entry count %d", format, tst.ts, len(ents))
			}
			if ent.TS.Sec < 0 {
				t.Fatalf("%s %s produced negative TS %d", format, tst.ts, ent.TS.Sec)
			} else if tst.valid && !ent.TS.StandardTime().Equal(good) {
				t.Fatalf("%s %s was not applied: %v", format, tst.ts, ent.TS)
			} else if !tst.valid && ent.TS != origTS {
				t.Fatalf("%s %s was applied: %v", format, tst.ts, ent.TS)
			}
		}
	}

	// multipart reassembly goes through the same bounds, the 2020 test data is outside the default window
	for _, tst := range []struct {
		maxPast string
		valid   bool
	}{{``, false}, {`175200h`, true}} {
		p, err := NewCiscoISEProcessor(CiscoISEConfig{
			Output_Format:               outputJSON,
			Enable_Multipart_Reassembly: true,
			Drop_Misses:                 true,
			Timestamp_Max_Past_Delta:    tst.maxPast,
		})
		if err != nil {
			t.Fatal(err)
		}
		var out []*entry.Entry
		for _, d := range testdata {
			ents, err := p.Process([]*entry.Entry{{TS: origTS, Data: []byte(d)}})
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, ents...)
		}
		if len(out) != 1 {
			t.Fatalf("bad reassembled entry count %d", len(out))
		} else if tst.valid && out[0].TS == origTS {
			t.Fatalf("in window multipart timestamp was not applied with Timestamp-Max-Past-Delta %q", tst.maxPast)
		} else if !tst.valid && out[0].TS != origTS {
			t.Fatalf("out of window multipart timestamp was applied: %v", out[0].TS)
		}
	}

	// bad deltas are config errors on load and on Config
	if _, err := NewCiscoISEProcessor(CiscoISEConfig{Timestamp_Max_Future_Delta: `bad`}); err == nil {
		t.Fatal("failed to catch bad cisco_ise delta")
	}
	p, err := NewCiscoISEProcessor(CiscoISEConfig{Output_Format: outputCEF})
	if err != nil {
		t.Fatal(err)
	} else if err = p.Config(CiscoISEConfig{Output_Format: outputCEF, Timestamp_Max_Past_Delta: `0`}); err == nil {
		t.Fatal("failed to catch bad cisco_ise delta on Config")
	} else if err = p.Config(CiscoISEConfig{Output_Format: outputCEF, Timestamp_Max_Past_Delta: `48h`}); err != nil {
		t.Fatal(err)
	} else if p.bounds.window.MaxPastDelta != 48*time.Hour {
		t.Fatalf("Config did not rebuild bounds: %+v", p.bounds.window)
	}
}
