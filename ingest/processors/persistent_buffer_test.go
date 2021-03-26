/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"path/filepath"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/config"
)

func TestPersistentBufferLoadConfig(t *testing.T) {
	b := []byte(`
	[global]
	foo = "bar"
	bar = 1337
	baz = 1.337
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "hole"]
		type = persistent-buffer
		filename = "/tmp/test"
		buffersize = "2MB"
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
	p, err := tc.Preprocessor.getProcessor(`hole`, &tt)
	if err != nil {
		t.Fatal(err)
	}
	if set, err := p.Process(makeEntry([]byte("hello"), 0)); err != nil || len(set) != 1 {
		t.Fatalf("Failed to pass through")
	}
}

func TestPersistentBuffer(t *testing.T) {
	fout := filepath.Join(t.TempDir(), `test`)
	dc := PersistentBufferConfig{
		BufferSize: `1MB`,
		Filename:   fout,
	}
	d, err := NewPersistentBuffer(dc)
	if err != nil {
		t.Fatal(err)
	} else if d == nil {
		t.Fatalf("nil drop")
	}
	ents := makeEntry([]byte("this is a test"), 1)
	if set, err := d.Process(ents); err != nil {
		t.Fatal(err)
	} else if len(set) != len(ents) {
		t.Fatalf("PersistentBuffer did not pass through: %d != %d", len(set), len(ents))
	}
}
