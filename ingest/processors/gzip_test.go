/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestGzipLoadConfig(t *testing.T) {
	b := []byte(`
	[global]
	foo = "bar"
	bar = 1337
	baz = 1.337
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "gz1"]
		type = gzip
		Passthrough-Non-Gzip=false
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
	p, err := tc.Preprocessor.getProcessor(`gz1`, &tt)
	if err != nil {
		t.Fatal(err)
	}
	if set, err := p.Process(makeEntry([]byte("hello"), 0)); err != nil || len(set) != 0 {
		t.Fatalf("Failed to catch bad gzip data")
	}
	val := `testing this test`
	x, err := gzipCompressVal(val)
	if err != nil {
		t.Fatal(err)
	}
	if rset, err := p.Process(makeEntry(x, entry.EntryTag(99))); err != nil {
		t.Fatal(err)
	} else if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	} else if string(rset[0].Data) != val {
		t.Fatalf("Bad results: %v != %v", string(rset[0].Data), val)
	} else if rset[0].Tag != entry.EntryTag(99) {
		t.Fatalf("Bad result tag: %d != 99", rset[0].Tag)
	}
}

func TestGzipProcessor(t *testing.T) {
	cfg := GzipDecompressorConfig{
		Passthrough_Non_Gzip: false,
	}
	p, err := NewGzipDecompressor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	//ensure we get an error about nongzip
	if set, err := p.Process(makeEntry([]byte("hello"), 0)); err != nil || len(set) != 0 {
		t.Fatalf("Failed to catch bad gzip data")
	}
	if set, err := p.Process(makeEntry(nil, 0)); err != nil || len(set) != 0 {
		t.Fatalf("Failed to catch bad gzip data")
	}
	if set, err := p.Process(makeEntry([]byte("X"), 0)); err != nil || len(set) != 0 {
		t.Fatalf("Failed to catch bad gzip data")
	}

	//try a few items
	toCheck := []string{
		`this is my string, there are many like it, but this string is mine`,
		`x`,
		``,
	}
	for i, v := range toCheck {
		x, err := gzipCompressVal(v)
		if err != nil {
			t.Fatal(err)
		}
		if rset, err := p.Process(makeEntry(x, entry.EntryTag(i))); err != nil {
			t.Fatal(err)
		} else if len(rset) != 1 {
			t.Fatalf("Invalid result count: %d", len(rset))
		} else if string(rset[0].Data) != v {
			t.Fatalf("Bad results: %v != %v", string(rset[0].Data), v)
		} else if rset[0].Tag != entry.EntryTag(i) {
			t.Fatalf("Bad result tag: %d != %d", rset[0].Tag, i)
		}
	}

	//change the config to allow pass through
	cfg.Passthrough_Non_Gzip = true
	if err = p.Config(cfg); err != nil {
		t.Fatal(err)
	}
	if rset, err := p.Process(makeEntry([]byte("hello"), 0)); err != nil {
		t.Fatal(err)
	} else if string(rset[0].Data) != `hello` {
		t.Fatalf("Failed to pass through nongzip: %v", string(rset[0].Data))
	}
	if rset, err := p.Process(makeEntry(nil, 0)); err != nil {
		t.Fatal(err)
	} else if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	} else if rset[0].Data != nil {
		t.Fatal("Failed to pass through nongzip")
	}
	if rset, err := p.Process(makeEntry([]byte("X"), 0)); err != nil {
		t.Fatal(err)
	} else if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	} else if string(rset[0].Data) != "X" {
		t.Fatal("Failed to pass through nongzip")
	}

}
