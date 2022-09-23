/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"testing"
	"time"

	"github.com/gobwas/glob"
	"github.com/gravwell/gravwell/v3/ingest/config"
)

func TestCiscoISEEmptyConfig(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = cisco_ise
		Enable-MultiPart-Reassembly=true
		Output-format=json
	`
	p, err := testLoadPreprocessor(b, `ise`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the cisco preprocess or
	if cp, ok := p.(*CiscoISE); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *CiscoISE", p)
	} else {
		if cp.Drop_Misses || !cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestCiscoISEBasicConfig(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = cisco_ise
		Enable-MultiPart-Reassembly=true
		Output-format=json
		Passthrough-Misses=false
	`
	p, err := testLoadPreprocessor(b, `ise`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the cisco preprocess or
	if cp, ok := p.(*CiscoISE); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *CiscoISE", p)
	} else {
		if !cp.Drop_Misses || cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestCiscoISEConflictingConfig(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = cisco_ise
		Enable-MultiPart-Reassembly=true
		Output-format=json
		Passthrough-Misses=false
		Drop-Misses=true
	`
	p, err := testLoadPreprocessor(b, `ise`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the cisco preprocess or
	if cp, ok := p.(*CiscoISE); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *CiscoISE", p)
	} else {
		if !cp.Drop_Misses || cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestCiscoISEDropConfig(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = cisco_ise
		Enable-MultiPart-Reassembly=true
		Output-format=json
		Drop-Misses=true
	`
	p, err := testLoadPreprocessor(b, `ise`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the cisco preprocess or
	if cp, ok := p.(*CiscoISE); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *CiscoISE", p)
	} else {
		if !cp.Drop_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestParseRemoteHeader(t *testing.T) {
	//just test that we can parse each
	for _, tv := range testdata {
		var rih remoteISE
		if err := rih.Parse(tv); err != nil {
			t.Fatal(err)
		}
		if rih.id != 983328 {
			t.Fatalf("bad message ID: %d", rih.id)
		}
	}
	var rih remoteISE
	if err := rih.Parse(strayData); err != nil {
		t.Fatal(err)
	}
	if rih.id != 983331 {
		t.Fatalf("Bad message ID: %d", rih.id)
	}
}

func TestRemoteAssembler(t *testing.T) {
	total := append(testdata, strayData)
	ejectOn := len(testdata) - 1

	mpa := newMultipartAssembler(1024*1024, time.Second)
	for i, v := range total {
		var rih remoteISE
		if err := rih.Parse(v); err != nil {
			t.Fatal(err)
		}
		res, ejected, bad := mpa.add(rih, float64(32.0))
		if bad {
			t.Fatal("Bad value", v)
		} else if ejected {
			//make sure its the right eject ID
			if i != ejectOn {
				t.Fatal("Invalid eject sequence", i, ejectOn)
			}
			//check that we got the right thing out
			if res.output != mergedData {
				t.Fatalf("Merged data is invalid:\n\t%s\n\t%s\n", res.output, mergedData)
			} else if vf, ok := res.meta.(float64); !ok || vf != 32.0 {
				t.Fatal("Metadata object is bad")
			}
		} else if res.output != `` {
			t.Fatal("got output when we didn't want any")
		} else if res.meta != nil {
			t.Fatal("Metadata object is bad")
		}
	}

	//check that there is exactly one item left in the reassembler
	if len(mpa.tracker) != 1 {
		t.Fatal("invalid residual items")
	}

	//check that purging isn't set
	if mpa.shouldFlush() {
		t.Fatal("Flush is set when it should not be")
	}

	purgeSet := mpa.flush(false) //do not force a flush
	if len(purgeSet) != 0 {
		t.Fatal("invalid result on a flush")
	}

	//lets artificially force a purge condition and then check on the purges
	mpa.oldest = time.Now().Add(-1 * time.Minute)
	if !mpa.shouldFlush() {
		t.Fatal("Flush condition isn't set")
	}

	//should still miss
	if purgeSet = mpa.flush(false); len(purgeSet) != 0 {
		t.Fatalf("invalid number of flushed values: %d != 0", len(purgeSet))
	}

	//manually force all existing to an old value (should only be one)
	//this is a hack
	for _, v := range mpa.tracker {
		v.last = v.last.Add(-10 * time.Minute)
	}
	if purgeSet = mpa.flush(false); len(purgeSet) != 1 {
		t.Fatalf("invalid number of flushed values: %d != 1", len(purgeSet))
	}

	//check that what we got out matches the stray
	if purgeSet[0].output != strayMerged {
		t.Fatalf("Merged data is invalid:\n\t%s\n\t%s\n", purgeSet[0], strayMerged)
	} else if purgeSet[0].meta == nil {
		t.Fatalf("Merged meta is invalid")
	} else if vf, ok := purgeSet[0].meta.(float64); !ok || vf != 32.0 {
		t.Fatal("Metadata object is bad")
	}

	//force a purge
	if purgeSet = mpa.flush(true); purgeSet != nil {
		t.Fatal("got values out after a forced purge on empty")
	}
}

type testoffset struct {
	value  []byte
	offset int
}

func TestEscapedCommaReader(t *testing.T) {
	set := []testoffset{
		testoffset{offset: -1, value: []byte(``)}, //empty
		testoffset{offset: 0, value: []byte(`, this is a test,`)},
		testoffset{offset: 1, value: []byte(`h, ello`)},
		testoffset{offset: 2, value: []byte(`\,,`)},                        //skip and escaped
		testoffset{offset: -1, value: []byte(`this is a test\, no comma`)}, //
		testoffset{offset: 13, value: []byte(`hello\, world, `)},
		testoffset{offset: 23, value: []byte(`hello\, world\, testing, `)},
	}

	for _, s := range set {
		if r := indexOfNonEscaped(s.value, ','); r != s.offset {
			t.Fatalf("Missed offset on %q: %d != %d", string(s.value), r, s.offset)
		}
	}
}

func TestParseISEMessage(t *testing.T) {
	ts1, err := time.Parse(iseTimestampFormat, `2020-11-23 12:50:16.963 -05:00`)
	if err != nil {
		t.Fatal(err)
	}
	ts2, err := time.Parse(iseTimestampFormat, `2020-11-23 12:50:01.926 -05:00`)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []iseMessage{
		iseMessage{
			ts:    ts1,
			seq:   1706721103,
			ode:   `5205`,
			sev:   `NOTICE`,
			class: `Dynamic-Authorization`,
			text:  `Dynamic Authorization succeeded`,
			attrs: strayMergedValues,
		},
		iseMessage{
			ts:    ts2,
			seq:   1706719405,
			ode:   `5200`,
			sev:   `NOTICE`,
			class: `Passed-Authentication`,
			text:  `Authentication succeeded`,
			attrs: mergedDataValues,
		},
	}
	inputs := []string{strayMerged, mergedData}

	for i, inp := range inputs {
		var m iseMessage
		if err := m.Parse(inp, nil, false); err != nil {
			t.Fatalf("Failed to parse %q: %v", inp, err)
		} else if !m.equal(&outputs[i]) {
			t.Fatalf("input %d does not match output\n%+v\n%+v", i, m, outputs[i])
		}
	}
}

func TestParseISEMessageWithFiltering(t *testing.T) {
	ts, err := time.Parse(iseTimestampFormat, `2020-11-23 12:50:01.926 -05:00`)
	if err != nil {
		t.Fatal(err)
	}
	output := iseMessage{
		ts:    ts,
		seq:   1706719405,
		ode:   `5200`,
		sev:   `NOTICE`,
		class: `Passed-Authentication`,
		text:  `Authentication succeeded`,
		attrs: mergedDataValuesFiltered,
	}
	input := mergedData

	filterStrings := []string{`Step*`, `NAS-*`}
	var filters []glob.Glob
	for _, s := range filterStrings {
		filters = append(filters, glob.MustCompile(s))
	}

	var m iseMessage
	if err := m.Parse(input, filters, false); err != nil {
		t.Fatalf("Failed to parse %q: %v", input, err)
	} else if !m.equal(&output) {
		t.Fatalf("input does not match output\n%+v\n%+v", m, output)
	}
}

func TestParseISEMessageWithStripping(t *testing.T) {
	ts, err := time.Parse(iseTimestampFormat, `2020-11-23 12:50:01.926 -05:00`)
	if err != nil {
		t.Fatal(err)
	}
	output := iseMessage{
		ts:    ts,
		seq:   1706719405,
		ode:   `5200`,
		sev:   `NOTICE`,
		class: `Passed-Authentication`,
		text:  `Authentication succeeded`,
		attrs: mergedDataValuesFilteredStripped,
	}
	input := mergedData

	filterStrings := []string{`Step*`, `NAS-*`, `Net*`, `Ext*`}
	var filters []glob.Glob
	for _, s := range filterStrings {
		filters = append(filters, glob.MustCompile(s))
	}

	var m iseMessage
	if err := m.Parse(input, filters, true); err != nil {
		t.Fatalf("Failed to parse %q: %v", input, err)
	} else if !m.equal(&output) {
		t.Fatalf("input does not match output\n%+v\n%+v", m, output)
	}
}

func TestCiscoISEProcess(t *testing.T) {
	b := []byte(`
	[global]
	foo = "bar"
	bar = 1337
	baz = 1.337
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "ise"]
		type = cisco_ise
		Enable-MultiPart-Reassembly=true
		Output-format=json
		Passthrough-Misses=false
	`)
	tc := struct {
		Global struct {
			Foo         string
			Bar         uint16
			Baz         float32
			Foo_Bar_Baz string
		}
		Item map[string]*struct {
			Name  string
			Value int
		}
		Preprocessor ProcessorConfig
	}{}
	if err := config.LoadConfigBytes(&tc, b); err != nil {
		t.Fatal(err)
	}
	var tt testTagger
	p, err := tc.Preprocessor.getProcessor(`ise`, &tt)
	if err != nil {
		t.Fatal(err)
	}

	//cast to the cisco preprocess or
	if cp, ok := p.(*CiscoISE); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *CiscoISE", p)
	} else {
		if !cp.Drop_Misses || cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}

	for i, d := range testdata {
		set, err := p.Process(makeEntry([]byte(d), 0))
		if err != nil {
			t.Fatal(err)
		}
		if i == (len(testdata) - 1) {
			if len(set) != 1 {
				t.Fatal("Failed to dump at end of test data")
			}
		} else {
			if len(set) != 0 {
				t.Fatal("Premature dump", i)
			}
		}
	}
}

func BenchmarkJSONMarshal(b *testing.B) {
	var m iseMessage
	if err := m.Parse(mergedData, nil, false); err != nil {
		b.Fatalf("Failed to parse: %v", err)
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := m.MarshalJSON(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCEFMarshal(b *testing.B) {
	var m iseMessage
	if err := m.Parse(mergedData, nil, false); err != nil {
		b.Fatalf("Failed to parse: %v", err)
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if x := m.formatAsCEF(); x == nil {
			b.Fatal("format failed")
		}
	}
}
