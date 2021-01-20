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

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestDropLoadConfig(t *testing.T) {
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
		type = drop
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
	if set, err := p.Process(makeEntry([]byte("hello"), 0)); err != nil || len(set) != 0 {
		t.Fatalf("Failed to drop")
	}
}

func TestDropper(t *testing.T) {
	var dc DropConfig
	d, err := NewDrop(dc)
	if err != nil {
		t.Fatal(err)
	} else if d == nil {
		t.Fatalf("nil drop")
	}
	ents := makeEntry([]byte("this is a test"), 1)
	if set, err := d.Process(ents); err != nil {
		t.Fatal(err)
	} else if len(set) != 0 {
		t.Fatalf("Drop did not drop: %d != 0", len(set))
	}

	//do the same thing with a nil set
	if set, err := d.Process(nil); err != nil {
		t.Fatal(err)
	} else if len(set) != 0 {
		t.Fatalf("Drop did not drop: %d != 0", len(set))
	}
	// do it again with a set that has a nil
	if set, err := d.Process([]*entry.Entry{nil}); err != nil {
		t.Fatal(err)
	} else if len(set) != 0 {
		t.Fatalf("Drop did not drop: %d != 0", len(set))
	}
}
