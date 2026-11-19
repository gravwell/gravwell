/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

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

// Additional fields are merged into each element's object, checked byte for byte.
func TestJsonArraySplitAdditionalFieldsObject(t *testing.T) {
	for _, tc := range []struct {
		name       string
		additional string
		forceJSON  bool
		wantFields string
	}{
		{name: "single string", additional: "host", wantFields: `,"host":"sensor-1"`},
		{name: "single object", additional: "meta", wantFields: `,"meta":{"sequence":7}`},
		{name: "nested number", additional: "meta.sequence", wantFields: `,"sequence":7`},
		{name: "missing field", additional: "missing"},
		{name: "multiple fields", additional: "host,meta.sequence", wantFields: `,"host":"sensor-1","sequence":7`},
		{name: "explicit object", additional: "host", forceJSON: true, wantFields: `,"host":"sensor-1"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := fmt.Sprintf(`[preprocessor "split"]
				Type=jsonarraysplit
				Extraction=events
				Additional-Fields=%q
				Force-JSON-Object=%t
			`, tc.additional, tc.forceJSON)
			p, err := testLoadPreprocessor(cfg, "split")
			if err != nil {
				t.Fatal(err)
			}
			ents := makeEntry([]byte(`{"events":[{"id":1},{"id":2}],"host":"sensor-1","meta":{"sequence":7}}`), 123)
			original := ents[0]
			out, err := p.Process(ents)
			if err != nil {
				t.Fatal(err)
			} else if len(out) != 2 {
				t.Fatalf("got %d entries, want 2", len(out))
			}
			for i, ent := range out {
				if want := fmt.Sprintf(`{"events":{"id":%d}%s}`, i+1, tc.wantFields); string(ent.Data) != want {
					t.Errorf("entry %d: got %s, want %s", i, ent.Data, want)
				}
				if ent.Tag != original.Tag || ent.TS != original.TS || !ent.SRC.Equal(original.SRC) {
					t.Errorf("entry %d did not preserve metadata", i)
				}
			}
			if err := checkEntryEVs(out); err != nil {
				t.Fatal(err)
			}
		})
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
		{`truncated after array`, `host`, `{"events":[{"id":1},{"id":2}],"host":"x"`},
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
		if err := cfg.validate(); !errors.Is(err, ErrDuplicateKeyname) {
			t.Errorf("Additional-Fields=%q passed validation: %v", additional, err)
		}
		if _, err := NewJsonArraySplitter(cfg); !errors.Is(err, ErrDuplicateKeyname) {
			t.Errorf("Additional-Fields=%q was accepted: %v", additional, err)
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

// Splitting must work after Config() swaps in a config, including Additional-Fields without Force-JSON-Object.
func TestJsonArraySplitReconfig(t *testing.T) {
	cfg := JsonArraySplitConfig{Extraction: `e`, Additional_Fields: `h`}
	p := newTestSplitter(t, cfg)
	if err := p.Config(cfg); err != nil {
		t.Fatal(err)
	}
	want := []string{`{"e":1,"h":2}`}
	if got := splitData(t, p, `{"e":[1],"h":2}`); !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if err := p.Config(JsonArraySplitConfig{Extraction: `a,b`}); err == nil {
		t.Fatal("invalid config accepted")
	}
}

// Key names containing JSON metacharacters must be escaped in the output.
func TestJsonArraySplitEscapedKeys(t *testing.T) {
	cfg := JsonArraySplitConfig{Extraction: `"a\"b"`, Additional_Fields: `"c\\d"`, Force_JSON_Object: true}
	got := splitData(t, newTestSplitter(t, cfg), `{"a\"b":[1],"c\\d":2}`)
	want := []string{`{"a\"b":1,"c\\d":2}`}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !json.Valid([]byte(got[0])) {
		t.Fatalf("invalid JSON %s", got[0])
	}
}

// Arrays that yield no entries emit nothing, and empty strings survive when wrapped in an object.
func TestJsonArraySplitEmpty(t *testing.T) {
	for _, tc := range []struct {
		name  string
		force bool
		drop  bool
		in    string
		want  []string
	}{
		{name: `empty array`, in: `{"e":[],"h":"x"}`},
		{name: `empty array dropped`, drop: true, in: `{"e":[],"h":"x"}`},
		{name: `only empty strings`, in: `{"e":["",""]}`},
		{name: `empty string raw`, in: `{"e":["","x"]}`, want: []string{`x`}},
		{name: `empty string object`, force: true, in: `{"e":["","x"]}`, want: []string{`{"e":""}`, `{"e":"x"}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := JsonArraySplitConfig{Extraction: `e`, Force_JSON_Object: tc.force, Drop_Misses: tc.drop}
			if got := splitData(t, newTestSplitter(t, cfg), tc.in); !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Cost must not depend on whether the additional field sits before or after the array;
// compare the two benchmarks rather than asserting on wall-clock time in a test.
func BenchmarkJsonArraySplitAdditionalBefore(b *testing.B) {
	benchmarkJsonArraySplitAdditional(b, `{"z":1,"a":[%s]}`)
}

func BenchmarkJsonArraySplitAdditionalAfter(b *testing.B) {
	benchmarkJsonArraySplitAdditional(b, `{"a":[%s],"z":1}`)
}

func benchmarkJsonArraySplitAdditional(b *testing.B, format string) {
	const count = 16 << 10
	doc := []byte(fmt.Sprintf(format, strings.Repeat(`0,`, count-1)+`0`))
	p, err := NewJsonArraySplitter(JsonArraySplitConfig{Extraction: `a`, Additional_Fields: `z`})
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(doc)))
	for b.Loop() {
		if rset, _ := p.Process([]*entry.Entry{{Data: doc}}); len(rset) != count {
			b.Fatalf("got %d entries, want %d", len(rset), count)
		}
	}
}
