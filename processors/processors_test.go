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
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/gravwell/ingest/v3/config"
	"github.com/gravwell/ingest/v3/entry"
)

// TestCheckProcessors just ensures we actually clean and trigger properly on preprocessor IDs
func TestCheckProcessors(t *testing.T) {
	//do some generic tests
	if err := CheckProcessor(`gzip`); err != nil {
		t.Fatal(err)
	}
	if err := CheckProcessor(` gzip `); err != nil {
		t.Fatal(err)
	}
	if err := CheckProcessor(` GzIp	`); err != nil {
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
	if _, err := p.Process(makeEntry([]byte("hello"), 0)); err != ErrNotGzipped {
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
	if _, err := p.Process(makeEntry([]byte("hello"), 0)); err != ErrNotGzipped {
		t.Fatalf("Failed to catch bad gzip data")
	}
	if _, err := p.Process(makeEntry(nil, 0)); err != ErrNotGzipped {
		t.Fatalf("Failed to catch bad gzip data")
	}
	if _, err := p.Process(makeEntry([]byte("X"), 0)); err != ErrNotGzipped {
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

func TestEmptyProcessorSet(t *testing.T) {
	ps := NewProcessorSet(nil)
	ent := entry.Entry{
		TS:   entry.Now(),
		SRC:  net.ParseIP("192.168.1.1"),
		Tag:  0,
		Data: []byte("Hello"),
	}
	if err := ps.Process(&ent); err != ErrNotReady {
		t.Fatal("Failed to catch bad processor")
	}
	if ps.Enabled() {
		t.Fatal("Failed to catch not enabled processor")
	}

	var tw testWriter
	if ps = NewProcessorSet(&tw); ps.Enabled() {
		t.Fatal("set is enabled when it shouldn't be")
	}
	if err := ps.Process(&ent); err != nil {
		t.Fatal(err)
	}
	if len(tw.ents) != 1 {
		t.Fatal("process failure")
	}
	if !entryEqual(tw.ents[0], &ent) {
		t.Fatal("resulting ent is bad")
	}

	return
}

func TestSingleProcessorSet(t *testing.T) {
	var err error
	data := []byte("Hello")
	var tw testWriter
	ps := NewProcessorSet(&tw)
	ent := entry.Entry{
		TS:  entry.Now(),
		SRC: net.ParseIP("192.168.1.1"),
		Tag: 0,
	}
	//compress the entry
	if ent.Data, err = gzipCompress(data); err != nil {
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
	if !ps.Enabled() {
		t.Fatal("Failed to catch enabled processor")
	}
	if err := ps.Process(&ent); err != nil {
		t.Fatal(err)
	}
	if len(tw.ents) != 1 {
		t.Fatal("process failure")
	}
	ent.Data = data
	if !entryEqual(tw.ents[0], &ent) {
		t.Fatal("resulting ent is bad")
	}

	return
}

func TestMultiProcessorSet(t *testing.T) {
	var err error
	data := []byte("Hello")
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
	if !ps.Enabled() {
		t.Fatal("Failed to catch enabled processor")
	}
	if err := ps.Process(&ent); err != nil {
		t.Fatal(err)
	}
	if len(tw.ents) != 1 {
		t.Fatal("process failure")
	}
	ent.Data = data
	if !entryEqual(tw.ents[0], &ent) {
		t.Fatal("resulting ent is bad")
	}

	return
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

func gzipCompress(x []byte) (r []byte, err error) {
	bwtr := bytes.NewBuffer(nil)
	gzw := gzip.NewWriter(bwtr)
	if _, err = gzw.Write(x); err == nil {
		if err = gzw.Close(); err == nil {
			r = bwtr.Bytes()
		}
	}
	return
}

type testTagger struct {
	i  entry.EntryTag
	mp map[string]entry.EntryTag
}

func (tt *testTagger) NegotiateTag(name string) (tg entry.EntryTag, err error) {
	var ok bool
	if tt.mp == nil {
		tt.mp = map[string]entry.EntryTag{}
	}
	if tg, ok = tt.mp[name]; !ok {
		tg = tt.i
		tt.mp[name] = tg
		tt.i++
	}
	return
}

func (tt *testTagger) LookupTag(tg entry.EntryTag) (name string, ok bool) {
	for k, v := range tt.mp {
		if v == tg {
			name = k
			ok = true
			break
		}
	}
	return
}

type testWriter struct {
	ents []*entry.Entry
}

func (tw *testWriter) WriteEntry(ent *entry.Entry) error {
	if ent == nil {
		return errors.New("nil entry")
	}
	tw.ents = append(tw.ents, ent)
	return nil
}

func (tw *testWriter) WriteEntryContext(ctx context.Context, ent *entry.Entry) error {
	return tw.WriteEntry(ent)
}

func entryEqual(a, b *entry.Entry) bool {
	if a == nil {
		return b == nil
	}
	if a.TS != b.TS {
		return false
	}
	if a.SRC != nil {
		if !a.SRC.Equal(b.SRC) {
			return false
		}
	} else if b.SRC != nil {
		return false
	}
	if a.Tag != b.Tag {
		return false
	}
	return bytes.Equal(a.Data, b.Data)
}
