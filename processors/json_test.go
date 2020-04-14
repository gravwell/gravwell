/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"net"
	"testing"

	"github.com/gravwell/ingest/v3/config"
	"github.com/gravwell/ingest/v3/entry"
)

var (
	testSrc = net.ParseIP("192.168.1.1")

	testInputJson   = []byte(`{"foo": 99, "bar": "hello", "foobar": {"baz": 4.12}}`)
	testOutputJson  = `{"foo":99,"bar":"hello","baz":4.12}`
	testExtractions = `foo,bar,foobar.baz`

	testArrayInputJson  = []byte(`{"foo":{"bar":["a", "b", 1.4, {"stuff":"things"}]}}`)
	testArrayExtraction = `foo.bar`
	testJSONArrayValues = []string{
		`{"bar":"a"}`,
		`{"bar":"b"}`,
		`{"bar":1.4}`,
		`{"bar":{"stuff":"things"}}`,
	}
	testArrayValues = []string{`a`, `b`, `1.4`, `{"stuff":"things"}`}
)

func TestJsonConfig(t *testing.T) {
	b := []byte(`
	[global]
	foo = "bar"
	bar = 1337
	baz = 1.337
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "j1"]
		type = jsonextract
		Strict-Extraction=false
		Passthrough-Misses=false
		Extractions="` + testExtractions + `"
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
	p, err := tc.Preprocessor.getProcessor(`j1`, &tt)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("no processor back")
	}
	rset, err := p.Process(makeEntry(testInputJson, 0))
	if err != nil {
		t.Fatal(err)
	} else if len(rset) != 1 {
		t.Fatalf("Invalid return count %v != 1", len(rset))
	} else if string(rset[0].Data) != testOutputJson {
		t.Fatal("bad result", string(rset[0].Data))
	}
}

func TestBzipJson(t *testing.T) {
	var err error
	data := testInputJson
	var tw testWriter
	ps := NewProcessorSet(&tw)
	ent := entry.Entry{
		TS:   entry.Now(),
		SRC:  net.ParseIP("192.168.1.1"),
		Tag:  0,
		Data: data,
	}

	//compress it 100 times and add 100 decompressors
	for i := 0; i < 128; i++ {
		if ent.Data, err = gzipCompress(ent.Data); err != nil {
			t.Fatal(err)
		}
		cfg := GzipDecompressorConfig{
			Passthrough_Non_Gzip: false,
		}
		p, err := NewGzipDecompressor(cfg)
		if err != nil {
			t.Fatal(err)
		}
		ps.AddProcessor(p)
	}
	//add our json extractor
	cfg := JsonExtractConfig{
		Extractions: testExtractions,
	}
	p, err := NewJsonExtractor(cfg)
	if err != nil {
		t.Fatal(p)
	}
	ps.AddProcessor(p)

	if !ps.Enabled() {
		t.Fatal("Failed to catch enabled processor")
	}
	if err := ps.Process(&ent); err != nil {
		t.Fatal(err)
	}
	if len(tw.ents) != 1 {
		t.Fatal("process failure")
	}
	ent.Data = []byte(testOutputJson)
	if !entryEqual(tw.ents[0], &ent) {
		t.Fatal("resulting ent is bad", string(ent.Data))
	}

	return
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
		Passthrough-Misses=false
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
}

func TestBzipJsonExtractArraySplit(t *testing.T) {
	var err error
	var tw testWriter
	ps := NewProcessorSet(&tw)
	ent := entry.Entry{
		TS:  entry.Now(),
		SRC: net.ParseIP("192.168.1.1"),
		Tag: 0,
	}

	//Run a chain that is a compressed json with an extraction and split
	if ent.Data, err = gzipCompress(testArrayInputJson); err != nil {
		t.Fatal(err)
	}

	zcfg := GzipDecompressorConfig{
		Passthrough_Non_Gzip: false,
	}
	if p, err := NewGzipDecompressor(zcfg); err != nil {
		t.Fatal(err)
	} else {
		ps.AddProcessor(p)
	}

	//add our json extractor
	ecfg := JsonExtractConfig{
		Force_JSON_Object: true,
		Extractions:       `foo.bar`,
	}
	if p, err := NewJsonExtractor(ecfg); err != nil {
		t.Fatal(p)
	} else {
		ps.AddProcessor(p)
	}

	//add our splitter
	scfg := JsonArraySplitConfig{
		Extraction: `bar`,
	}
	if p, err := NewJsonArraySplitter(scfg); err != nil {
		t.Fatal(p)
	} else {
		ps.AddProcessor(p)
	}

	if err := ps.Process(&ent); err != nil {
		t.Fatal(err)
	}
	if len(tw.ents) != len(testArrayValues) {
		t.Fatalf("return count mismatch: %d != %d", len(tw.ents), len(testJSONArrayValues))
	}
	for i := range tw.ents {
		ent.Data = []byte(testArrayValues[i])
		if !entryEqual(tw.ents[i], &ent) {
			t.Fatal(i, "resulting ent is bad", string(ent.Data))
		}
	}
}

func makeEntry(v []byte, tag entry.EntryTag) *entry.Entry {
	return &entry.Entry{
		Tag:  tag,
		SRC:  testSrc,
		TS:   entry.Now(),
		Data: v,
	}
}
