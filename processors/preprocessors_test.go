/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	"github.com/gravwell/ingest/v3/config"
	"github.com/gravwell/ingest/v3/entry"
)

// TestCheckPreprocessors just ensures we actually clean and trigger properly on preprocessor IDs
func TestCheckPreprocessors(t *testing.T) {
	//do some generic tests
	if err := CheckPreprocessor(`gzip`); err != nil {
		t.Fatal(err)
	}
	if err := CheckPreprocessor(` gzip `); err != nil {
		t.Fatal(err)
	}
	if err := CheckPreprocessor(` GzIp	`); err != nil {
		t.Fatal(err)
	}
}

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
		Preprocessor PreprocessorConfig
	}{}
	if err := config.LoadConfigBytes(&tc, b); err != nil {
		t.Fatal(err)
	}
	p, err := tc.Preprocessor.GetPreprocessor(`gz1`)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Process([]byte("hello"), 0); err != ErrNotGzipped {
		t.Fatalf("Failed to catch bad gzip data")
	}
	val := `testing this test`
	x, err := gzipCompressVal(val)
	if err != nil {
		t.Fatal(err)
	}
	if rtag, rbuf, err := p.Process(x, entry.EntryTag(99)); err != nil {
		t.Fatal(err)
	} else if string(rbuf) != val {
		t.Fatalf("Bad results: %v != %v", string(rbuf), val)
	} else if rtag != entry.EntryTag(99) {
		t.Fatalf("Bad result tag: %d != 99", rtag)
	}
}

func TestGzipPreprocessor(t *testing.T) {
	cfg := GzipDecompressorConfig{
		Passthrough_Non_Gzip: false,
	}
	p, err := NewGzipDecompressor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	//ensure we get an error about nongzip
	if _, _, err := p.Process([]byte("hello"), 0); err != ErrNotGzipped {
		t.Fatalf("Failed to catch bad gzip data")
	}
	if _, _, err := p.Process(nil, 0); err != ErrNotGzipped {
		t.Fatalf("Failed to catch bad gzip data")
	}
	if _, _, err := p.Process([]byte("X"), 0); err != ErrNotGzipped {
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
		if rtag, rbuf, err := p.Process(x, entry.EntryTag(i)); err != nil {
			t.Fatal(err)
		} else if string(rbuf) != v {
			t.Fatalf("Bad results: %v != %v", string(rbuf), v)
		} else if rtag != entry.EntryTag(i) {
			t.Fatalf("Bad result tag: %d != %d", rtag, i)
		}
	}

	//change the config to allow pass through
	cfg.Passthrough_Non_Gzip = true
	if err = p.Config(cfg); err != nil {
		t.Fatal(err)
	}
	if _, rbuf, err := p.Process([]byte("hello"), 0); err != nil {
		t.Fatal(err)
	} else if string(rbuf) != `hello` {
		t.Fatalf("Failed to pass through nongzip: %v", string(rbuf))
	}
	if _, rbuf, err := p.Process(nil, 0); err != nil {
		t.Fatal(err)
	} else if rbuf != nil {
		t.Fatal("Failed to pass through nongzip")
	}
	if _, rbuf, err := p.Process([]byte("X"), 0); err != nil {
		t.Fatal(err)
	} else if string(rbuf) != "X" {
		t.Fatal("Failed to pass through nongzip")
	}

}

func gzipCompressVal(x string) (r []byte, err error) {
	bwtr := bytes.NewBuffer(nil)
	gzw := gzip.NewWriter(bwtr)
	if _, err = io.WriteString(gzw, x); err == nil {
		if err = gzw.Close(); err == nil {
			r = bwtr.Bytes()
		}
	}
	return
}
