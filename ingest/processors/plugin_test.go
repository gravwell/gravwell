//go:build !386 && !arm && !mips && !mipsle && !s390x && !go1.18
// +build !386,!arm,!mips,!mipsle,!s390x,!go1.18

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
	"fmt"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestPluginLoad(t *testing.T) {
	b := []byte(`
	[global]
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "p2"]
		type = plugin
		Plugin-Path = "test_data/plugins/case_adjust.go"
		Upper=true
	`)
	tc := struct {
		Global struct {
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
	p, err := tc.Preprocessor.getProcessor(`p2`, &tt)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("no processor back")
	}
}

func TestPluginBadConfig(t *testing.T) {
	b := []byte(`
	[global]
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "p2"]
		type = plugin
		Plugin-Path = "test_data/plugins/case_adjust.go"
	`)
	tc := struct {
		Global struct {
			Foo_Bar_Baz string
		}
		Item map[string]*struct {
			Name  string
			Value int
		}
		Preprocessor ProcessorConfig
	}{}
	//LoadConfig DOES NOT actually let the plugin test its config
	if err := config.LoadConfigBytes(&tc, b); err != nil {
		t.Fatalf("Failed to catch bad config")
	}
	var tt testTagger
	if _, err := tc.Preprocessor.getProcessor(`p2`, &tt); err == nil {
		t.Fatalf("failed to catch bad config")
	}
}

func TestPluginBad(t *testing.T) {
	b := []byte(`
	[global]
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "p2"]
		type = plugin
		Plugin-Path = "test_data/plugins/noregister.go"

	[preprocessor "p3"]
		type = plugin
		Plugin-Path = "test_data/plugins/panic.go"
	`)
	tc := struct {
		Global struct {
			Foo_Bar_Baz string
		}
		Item map[string]*struct {
			Name  string
			Value int
		}
		Preprocessor ProcessorConfig
	}{}
	//LoadConfig DOES NOT actually let the plugin test its config
	if err := config.LoadConfigBytes(&tc, b); err != nil {
		t.Fatalf("Failed to catch bad config")
	}
	var tt testTagger
	if _, err := tc.Preprocessor.getProcessor(`p2`, &tt); err == nil {
		t.Fatalf("failed to catch no register")
	} else if _, err := tc.Preprocessor.getProcessor(`p3`, &tt); err == nil {
		t.Fatalf("failed to catch panic")
	}
}

func TestPluginProcess(t *testing.T) {
	b := []byte(`
	[global]
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "p2"]
		type = plugin
		Plugin-Path = "test_data/plugins/case_adjust.go"
		Upper=true
	`)
	tc := struct {
		Global struct {
			Foo_Bar_Baz string
		}
		Item map[string]*struct {
			Name  string
			Value int
		}
		Preprocessor ProcessorConfig
	}{}
	//LoadConfig DOES NOT actually let the plugin test its config
	if err := config.LoadConfigBytes(&tc, b); err != nil {
		t.Fatalf("Failed to catch bad config")
	}
	var tt testTagger
	p, err := tc.Preprocessor.getProcessor(`p2`, &tt)
	if err != nil {
		t.Fatal(err)
	}
	set := makeEntrySet(testPluginCase, 123, 1024)
	rset, err := p.Process(set)
	if err != nil {
		t.Fatal(err)
	} else if len(rset) != len(set) {
		t.Fatalf("return count mismatch: %d != %d", len(rset), len(set))
	}

	for i := range rset {
		if rset[i].Tag != 123 {
			t.Fatalf("%d invalid return tag", rset[i].Tag)
		}
		if v := bytes.ToUpper(set[i].Data); !bytes.Equal(rset[i].Data, v) {
			t.Fatalf("%d invalid return value: %s != %s", i,
				string(rset[i].Data), string(v))
		}
	}
	if ret := p.Flush(); len(ret) != 0 {
		t.Fatalf("got invalid entry count from flush")
	} else if err = p.Close(); err != nil {
		t.Fatal(err)
	}
}

var (
	testPluginCase = []byte(`This Is A Test Case`)
)

func makeEntrySet(base []byte, tag entry.EntryTag, count int) (r []*entry.Entry) {
	r = make([]*entry.Entry, count)
	for i := range r {
		r[i] = &entry.Entry{
			Tag:  tag,
			SRC:  testSrc,
			TS:   entry.Now(),
			Data: append(base, []byte(fmt.Sprintf(" %d/%d", i, count))...),
		}
	}
	return
}
