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

	"github.com/gravwell/gravwell/v4/ingest/config"
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
		if v == ',' {
			return true
		}
		return false
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
		if v == ',' {
			return true
		}
		return false
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
		if v == ',' {
			return true
		}
		return false
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
		if v == '.' {
			return true
		}
		return false
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
		if v == '.' {
			return true
		}
		return false
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
