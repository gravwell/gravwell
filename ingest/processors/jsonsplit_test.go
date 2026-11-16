/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var (
	testArrayInputJson        = []byte(`{"foo":{"bar":["a", "b", 1.4, {"stuff":"things"}]}, "foobar": "barbaz", "barbaz": 99}`)
	testArrayInputJsonQuoted  = []byte(`{"foo.bar":{"bar":["a", "b", 1.4, {"stuff":"things"}]}, "foo.bar": "barbaz", "barbaz": 99}`)
	testArrayExtraction       = `foo.bar`
	testArrayExtractionQuoted = "`\"foo.bar\".bar`"

	testJSONArrayValues = []string{
		`{"bar":"a"}`,
		`{"bar":"b"}`,
		`{"bar":1.4}`,
		`{"bar":{"stuff":"things"}}`,
	}
	testJSONArrayValuesExtra = []string{
		`{"bar":"a","foobar":"barbaz","barbaz":99}`,
		`{"bar":"b","foobar":"barbaz","barbaz":99}`,
		`{"bar":1.4,"foobar":"barbaz","barbaz":99}`,
		`{"bar":{"stuff":"things"},"foobar":"barbaz","barbaz":99}`,
	}

	testArrayValues = []string{`a`, `b`, `1.4`, `{"stuff":"things"}`}

	bareArrayInput  = []byte(`[{"foo":"bar"}, {"bar":"foo"}]`)
	bareArrayValues = []string{
		`{"foo":"bar"}`,
		`{"bar":"foo"}`,
	}
)

func TestJsonArraySplitterEmptyConfig(t *testing.T) {
	b := `
	[preprocessor "j2"]
		type = jsonarraysplit
		Extraction="` + testArrayExtraction + `"
		Force-JSON-Object=true
	`
	p, err := testLoadPreprocessor(b, `j2`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the cisco preprocess or
	if cp, ok := p.(*JsonArraySplitter); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *JsonArraySplitter", p)
	} else {
		if cp.Drop_Misses || !cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestJsonArraySplitterBasicConfig(t *testing.T) {
	b := `
	[preprocessor "j2"]
		type = jsonarraysplit
		Passthrough-Misses=false
		Extraction="` + testArrayExtraction + `"
		Force-JSON-Object=true
	`
	p, err := testLoadPreprocessor(b, `j2`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the cisco preprocess or
	if cp, ok := p.(*JsonArraySplitter); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *JsonArraySplitter", p)
	} else {
		if !cp.Drop_Misses || cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestJsonArraySplitterConflictingConfig(t *testing.T) {
	b := `
	[preprocessor "j2"]
		type = jsonarraysplit
		Passthrough-Misses=false
		Drop-Misses=true
		Extraction="` + testArrayExtraction + `"
		Force-JSON-Object=true
	`
	p, err := testLoadPreprocessor(b, `j2`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the cisco preprocess or
	if cp, ok := p.(*JsonArraySplitter); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *JsonArraySplitter", p)
	} else {
		if !cp.Drop_Misses || cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestJsonArraySplit(t *testing.T) {
	b := []byte(`
	[global]
	foo = "bar"
	bar = 1337
	baz = 1.337
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "j2"]
		type = jsonarraysplit
		Drop-Misses=true
		Extraction="` + testArrayExtraction + `"
		Force-JSON-Object=true
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
	if _, err := tc.Preprocessor.getProcessor(`j1`, &tt); err == nil {
		t.Fatal("Failed to pickup missing processor")
	}
	p, err := tc.Preprocessor.getProcessor(`j2`, &tt)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("no processor back")
	}
	rset, err := p.Process(makeEntry(testArrayInputJson, 123))
	if err != nil {
		t.Fatal(err)
	} else if len(rset) != len(testJSONArrayValues) {
		t.Fatalf("return count mismatch: %d != %d", len(rset), len(testJSONArrayValues))
	}

	for i := range rset {
		if rset[i].Tag != 123 {
			t.Fatalf("%d invalid return tag", rset[i].Tag)
		}
		if string(rset[i].Data) != testJSONArrayValues[i] {
			t.Fatalf("%d invalid return value: %s != %s", i,
				string(rset[i].Data), testJSONArrayValues[i])
		}
	}
	if err := checkEntryEVs(rset); err != nil {
		t.Fatal(err)
	}
}

func TestJsonArraySplitData(t *testing.T) {
	b := []byte(`
	[global]
	foo = "bar"
	bar = 1337
	baz = 1.337
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "j2"]
		type = jsonarraysplit
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
	p, err := tc.Preprocessor.getProcessor(`j2`, &tt)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("no processor back")
	}
	rset, err := p.Process(makeEntry(bareArrayInput, 123))
	if err != nil {
		t.Fatal(err)
	} else if len(rset) != len(bareArrayValues) {
		t.Fatalf("return count mismatch: %d != %d", len(rset), len(bareArrayValues))
	}

	for i := range rset {
		if rset[i].Tag != 123 {
			t.Fatalf("%d invalid return tag", rset[i].Tag)
		}
		if string(rset[i].Data) != bareArrayValues[i] {
			t.Fatalf("%d invalid return value: %s != %s", i,
				string(rset[i].Data), bareArrayValues[i])
		}
	}
}

func TestJsonArraySplitAdditional(t *testing.T) {
	b := []byte(`
	[global]
	foo = "bar"
	bar = 1337
	baz = 1.337
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "j2"]
		type = jsonarraysplit
		Passthrough-Misses=false
		Extraction="` + testArrayExtraction + `"
		Additional-Fields="foobar,barbaz"
		Force-JSON-Object=true
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
	if _, err := tc.Preprocessor.getProcessor(`j1`, &tt); err == nil {
		t.Fatal("Failed to pickup missing processor")
	}
	p, err := tc.Preprocessor.getProcessor(`j2`, &tt)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("no processor back")
	}
	rset, err := p.Process(makeEntry(testArrayInputJson, 123))
	if err != nil {
		t.Fatal(err)
	} else if len(rset) != len(testJSONArrayValues) {
		t.Fatalf("return count mismatch: %d != %d", len(rset), len(testJSONArrayValues))
	}

	for i := range rset {
		if rset[i].Tag != 123 {
			t.Fatalf("%d invalid return tag", rset[i].Tag)
		}
		if string(rset[i].Data) != testJSONArrayValuesExtra[i] {
			t.Fatalf("%d invalid return value: %s != %s", i,
				string(rset[i].Data), testJSONArrayValuesExtra[i])
		}
	}
}

func TestJsonSplitQuotedCSV(t *testing.T) {
	in := `foo,bar.baz,"foo.bar","foo,bar"`
	expected := []string{`foo`, `bar.baz`, `"foo.bar"`, `"foo,bar"`}

	f := func(v rune) bool {
		return v == ','
	}

	fields := splitRespectQuotes(in, f)
	for i, v := range expected {
		if fields[i] != v {
			t.Fatal("mismatched extraction", v, fields[i])
		}
	}

}

func TestJsonSplitQuotedCSV2(t *testing.T) {
	in := `"foo",bar.baz,foo.bar`
	expected := []string{`"foo"`, `bar.baz`, `foo.bar`}

	f := func(v rune) bool {
		return v == ','
	}

	fields := splitRespectQuotes(in, f)
	for i, v := range expected {
		if fields[i] != v {
			t.Fatal("mismatched extraction", v, fields[i])
		}
	}

}

func TestJsonSplitQuotedCSV3(t *testing.T) {
	in := `"foo",bar.baz,"foo.bar".baz`
	expected := []string{`"foo"`, `bar.baz`, `"foo.bar".baz`}

	f := func(v rune) bool {
		return v == ','
	}

	fields := splitRespectQuotes(in, f)
	for i, v := range expected {
		if fields[i] != v {
			t.Fatal("mismatched extraction", v, fields[i])
		}
	}

}

func TestJsonSplitQuotedDot(t *testing.T) {
	in := `"foo.bar"`
	expected := []string{`"foo.bar"`}

	f := func(v rune) bool {
		return v == '.'
	}

	fields := splitRespectQuotes(in, f)
	for i, v := range expected {
		if fields[i] != v {
			t.Fatal("mismatched extraction", v, fields[i])
		}
	}

}

func TestJsonSplitQuotedDot2(t *testing.T) {
	in := `foo."bar"`
	expected := []string{`foo`, `"bar"`}

	f := func(v rune) bool {
		return v == '.'
	}

	fields := splitRespectQuotes(in, f)
	for i, v := range expected {
		if fields[i] != v {
			t.Fatal("mismatched extraction", v, fields[i])
		}
	}

}

func TestJsonArraySplitQuoted(t *testing.T) {
	b := []byte(`
	[global]
	foo = "bar"
	bar = 1337
	baz = 1.337
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "j2"]
		type = jsonarraysplit
		Drop-Misses=true
		Extraction=` + testArrayExtractionQuoted + `
		Force-JSON-Object=true
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
	if _, err := tc.Preprocessor.getProcessor(`j1`, &tt); err == nil {
		t.Fatal("Failed to pickup missing processor")
	}
	p, err := tc.Preprocessor.getProcessor(`j2`, &tt)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("no processor back")
	}
	rset, err := p.Process(makeEntry(testArrayInputJsonQuoted, 123))
	if err != nil {
		t.Fatal(err)
	} else if len(rset) != len(testJSONArrayValues) {
		t.Fatalf("return count mismatch: %d != %d", len(rset), len(testJSONArrayValues))
	}

	for i := range rset {
		if rset[i].Tag != 123 {
			t.Fatalf("%d invalid return tag", rset[i].Tag)
		}
		if string(rset[i].Data) != testJSONArrayValues[i] {
			t.Fatalf("%d invalid return value: %s != %s", i,
				string(rset[i].Data), testJSONArrayValues[i])
		}
	}
	if err := checkEntryEVs(rset); err != nil {
		t.Fatal(err)
	}
}

// splitData runs one batch through p and returns the output data.
func splitData(t *testing.T, p *JsonArraySplitter, in ...string) (out []string) {
	t.Helper()
	var ents []*entry.Entry
	for _, v := range in {
		ents = append(ents, &entry.Entry{Tag: 123, Data: []byte(v)})
	}
	rset, err := p.Process(ents)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range rset {
		out = append(out, string(ent.Data))
	}
	return
}

func newTestSplitter(t *testing.T, cfg JsonArraySplitConfig) *JsonArraySplitter {
	t.Helper()
	p, err := NewJsonArraySplitter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A failed extraction must not leak into later entries.
func TestJsonArraySplitNoStateLeak(t *testing.T) {
	cfg := JsonArraySplitConfig{Extraction: `events`, Additional_Fields: `host`, Drop_Misses: true}
	bad := `{"events":[{"id":1},{"id":2}],"host":tru}`
	good := `{"events":[{"id":5},{"id":6}],"host":"x"}`
	want := []string{`{"events":{"id":5},"host":"x"}`, `{"events":{"id":6},"host":"x"}`}

	t.Run("same batch", func(t *testing.T) {
		if got := splitData(t, newTestSplitter(t, cfg), bad, good); !slices.Equal(got, want) {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("separate calls", func(t *testing.T) {
		p := newTestSplitter(t, cfg)
		for range 3 {
			if got := splitData(t, p, bad); len(got) != 0 {
				t.Fatalf("malformed entry emitted %q", got)
			}
		}
		if got := splitData(t, p, good); !slices.Equal(got, want) {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

// Entries that fail to split follow Drop-Misses and are never partially emitted.
func TestJsonArraySplitMalformed(t *testing.T) {
	for _, tc := range []struct{ name, additional, in string }{
		{`bad additional field`, `host`, `{"events":[{"id":1},{"id":2}],"host":tru}`},
		{`truncated array`, `host`, `{"host":"x","events":[{"id":1},{"id":2`},
		{`truncated array, no additional`, ``, `{"events":[{"id":1},{"id":2`},
		{`not an array`, `host`, `{"host":"x","events":"none"}`},
	} {
		for _, drop := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/drop=%v", tc.name, drop), func(t *testing.T) {
				cfg := JsonArraySplitConfig{Extraction: `events`, Additional_Fields: tc.additional, Drop_Misses: drop}
				var want []string
				if !drop {
					want = []string{tc.in}
				}
				if got := splitData(t, newTestSplitter(t, cfg), tc.in); !slices.Equal(got, want) {
					t.Fatalf("got %q, want %q", got, want)
				}
			})
		}
	}
}

// Additional field names must not collide with the extraction name or each other.
func TestJsonArraySplitKeyCollision(t *testing.T) {
	for _, additional := range []string{`meta.events`, `a.host,b.host`, `host,host`} {
		cfg := JsonArraySplitConfig{Extraction: `events`, Additional_Fields: additional}
		if _, err := NewJsonArraySplitter(cfg); err == nil {
			t.Errorf("Additional-Fields=%q was accepted", additional)
		}
	}
}

// Additional-Fields always produces objects, so the loaded config must report Force-JSON-Object.
func TestJsonArraySplitAdditionalForcesObject(t *testing.T) {
	p, err := testLoadPreprocessor(`
	[preprocessor "j2"]
		type = jsonarraysplit
		Extraction=events
		Additional-Fields=host
	`, `j2`)
	if err != nil {
		t.Fatal(err)
	}
	if !p.(*JsonArraySplitter).Force_JSON_Object {
		t.Fatal("Force-JSON-Object not set with Additional-Fields")
	}
}

// Cost must not depend on whether the additional field sits before or after the array.
func TestJsonArraySplitAdditionalCost(t *testing.T) {
	const count = 16 << 10
	elems := strings.Repeat(`0,`, count-1) + `0`
	run := func(doc string) (best time.Duration) {
		p := newTestSplitter(t, JsonArraySplitConfig{Extraction: `a`, Additional_Fields: `z`})
		for i := range 3 {
			start := time.Now()
			if n := len(splitData(t, p, doc)); n != count {
				t.Fatalf("got %d entries, want %d", n, count)
			}
			if d := time.Since(start); i == 0 || d < best {
				best = d
			}
		}
		return
	}
	before := run(`{"z":1,"a":[` + elems + `]}`)
	after := run(`{"a":[` + elems + `],"z":1}`)
	if after > 10*before {
		t.Fatalf("field after array took %v, before array took %v", after, before)
	}
}
