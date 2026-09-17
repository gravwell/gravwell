//go:build linux
// +build linux

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
	"path/filepath"
	"testing"

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

func TestPersistentBufferLoadConfig(t *testing.T) {
	b := []byte(fmt.Sprintf(`
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
		filename = "%s/temp"
		buffersize = "2MB"
	`, t.TempDir()))
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
	var tt testTagger
	d, err := NewPersistentBuffer(dc, &tt)
	if err != nil {
		t.Fatal(err)
	} else if d == nil {
		t.Fatalf("nil drop")
	}

	var origCnt int
	for i := 0; i < 16; i++ {
		ents := makeEntry([]byte("this is a test"), 4)
		origCnt += len(ents)
		if set, err := d.Process(ents); err != nil {
			t.Fatal(err)
		} else if len(set) != len(ents) {
			t.Fatalf("PersistentBuffer did not pass through: %d != %d", len(set), len(ents))
		}
	}

	if err = d.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	//open the buffer and make sure we can pop 2 items off
	pbc, err := OpenPersistentBuffer(fout)
	if err != nil {
		t.Fatalf("Failed to open buffer: %v", err)
	}

	var cnt int
	for {
		if strents, err := pbc.Pop(); err != nil {
			if err == ErrBufferEmpty {
				break
			}
			t.Fatalf("Failed to pop: %v", err)
		} else {
			cnt += len(strents)
		}
	}

	if err = pbc.Close(); err != nil {
		t.Fatalf("Failed to close persistent buffer: %v", err)
	}

	if cnt != origCnt {
		t.Fatalf("Failed to pop correct number of entries: %d != %d", cnt, origCnt)
	}
}

func TestPBRollover(t *testing.T) {
	fout := filepath.Join(t.TempDir(), `test2`)
	dc := PersistentBufferConfig{
		BufferSize: `1MB`,
		Filename:   fout,
	}
	var tt testTagger
	d, err := NewPersistentBuffer(dc, &tt)
	if err != nil {
		t.Fatal(err)
	} else if d == nil {
		t.Fatalf("nil drop")
	}

	tags := make([]entry.EntryTag, 4)
	for i := 0; i < 4; i++ {
		if tg, err := tt.NegotiateTag(fmt.Sprintf("TAG%d", i)); err != nil {
			t.Fatal(err)
		} else {
			tags[i] = tg
		}
	}

	// make enough entries that our buffer rolls
	for i := 0; i < 4096; i++ {
		for j := 0; j < 4; j++ {
			ents := makeEntry([]byte("this is a test"), tags[j])
			if set, err := d.Process(ents); err != nil {
				t.Fatal(err)
			} else if len(set) != len(ents) {
				t.Fatalf("PersistentBuffer did not pass through: %d != %d", len(set), len(ents))
			}
		}
	}

	if err = d.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}
	//open the buffer and make sure we can pop 2 items off
	pbc, err := OpenPersistentBuffer(fout)
	if err != nil {
		t.Fatalf("Failed to open buffer: %v", err)
	}

	var cnt int
	for {
		if strents, err := pbc.Pop(); err != nil {
			if err == ErrBufferEmpty {
				break
			}
			t.Fatalf("Failed to pop: %v", err)
		} else {
			cnt += len(strents)
		}
	}

	if err = pbc.Close(); err != nil {
		t.Fatalf("Failed to close persistent buffer: %v", err)
	}

	if cnt == 0 {
		t.Fatalf("Failed to pop entries: %d", cnt)
	}
}
