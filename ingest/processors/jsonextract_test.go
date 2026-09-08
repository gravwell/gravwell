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

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

var (
	testSrc = net.ParseIP("192.168.1.1")

	testInputJson         = []byte(`{"foo": 99, "bar": "hello", "foobar": {"baz": 4.12}}`)
	testInputJsonQuoted   = []byte(`{"foo": 99, "bar": "hello", "foo.bar": {"baz": 4.12}}`)
	testOutputJson        = `{"foo":99,"bar":"hello","baz":4.12}`
	testExtractions       = `foo,bar,foobar.baz`
	testExtractionsQuoted = "`foo,bar,\"foo.bar\".baz`"
)

func TestJsonExtractorEmptyConfig(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = jsonextract
		Strict-Extraction=false
		Extractions="` + testExtractions + `"
	`
	p, err := testLoadPreprocessor(b, `ise`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the cisco preprocess or
	if cp, ok := p.(*JsonExtractor); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *JsonExtractor", p)
	} else {
		if cp.Drop_Misses || !cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestJsonExtractorBasicConfig(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = jsonextract
		Passthrough-Misses=false
		Strict-Extraction=false
		Extractions="` + testExtractions + `"
	`
	p, err := testLoadPreprocessor(b, `ise`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the cisco preprocess or
	if cp, ok := p.(*JsonExtractor); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *JsonExtractor", p)
	} else {
		if !cp.Drop_Misses || cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestJsonExtractorConflictingConfig(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = jsonextract
		Passthrough-Misses=false
		Drop-Misses=true
		Strict-Extraction=false
		Extractions="` + testExtractions + `"
	`
	p, err := testLoadPreprocessor(b, `ise`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the cisco preprocess or
	if cp, ok := p.(*JsonExtractor); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *JsonExtractor", p)
	} else {
		if !cp.Drop_Misses || cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestJsonExtractorDropConfig(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = jsonextract
		Drop-Misses=true
		Strict-Extraction=false
		Extractions="` + testExtractions + `"
	`
	p, err := testLoadPreprocessor(b, `ise`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the cisco preprocess or
	if cp, ok := p.(*JsonExtractor); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *JsonExtractor", p)
	} else {
		if !cp.Drop_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

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
		Passthrough-Misses=false
		Strict-Extraction=false
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

func TestJsonConfigQuoted(t *testing.T) {
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
		Passthrough-Misses=false
		Strict-Extraction=false
		Extractions=` + testExtractionsQuoted + `
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
	rset, err := p.Process(makeEntry(testInputJsonQuoted, 0))
	if err != nil {
		t.Fatal(err)
	} else if len(rset) != 1 {
		t.Fatalf("Invalid return count %v != 1", len(rset))
	} else if string(rset[0].Data) != testOutputJson {
		t.Fatal("bad result", string(rset[0].Data))
	}
}
