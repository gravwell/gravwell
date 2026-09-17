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

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const ()

var (
	crc = CSVRouteConfig{
		Route_Extraction: 1,
		Route:            []string{`foo:footag`, `bar:bartag`, `baz:`},
	}
)

func TestCSVRouteConfig(t *testing.T) {
	if rts, err := crc.validate(); err != nil {
		t.Fatal(err)
	} else if len(rts) != 3 {
		t.Fatal("bad route count")
	}
}

func TestNewCSVRouter(t *testing.T) {
	var tg testTagger
	//build out the default tag
	if _, err := tg.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}
	cc := crc
	//make a new one
	cr, err := NewCSVRouter(crc, &tg)
	if err != nil {
		t.Fatal(err)
	}
	//reconfigure the existing
	cc.Route = append(cc.Route, `testA:A`)
	if err = cr.Config(cc, &tg); err != nil {
		t.Fatal(err)
	}
	if tg.mp[`footag`] != 1 || tg.mp[`bartag`] != 2 || tg.mp[`A`] != 3 {
		t.Fatalf("bad tag negotiations: %+v", tg.mp)
	}
}

func TestCSVRouterProcess(t *testing.T) {
	var tagger testTagger
	//build out the default tag
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}
	cc := crc
	cc.Route = append(cc.Route, `dropme:`)
	cr, err := NewCSVRouter(cc, &tagger)
	if err != nil {
		t.Fatal(err)
	}
	testSet := []testTagSet{
		testTagSet{data: `a,foo,c`, tag: `footag`, drop: false},
		testTagSet{data: `a,bar,c`, tag: `bartag`, drop: false},
		testTagSet{data: `a,baz,c`, tag: ``, drop: true},
		testTagSet{data: `a,dropme`, tag: ``, drop: true},
		testTagSet{data: `UncleEddy`, tag: `default`, drop: false},
		testTagSet{data: `internal,quotes,foo"bar`, tag: `default`, drop: false},
		testTagSet{data: `a,     foo,c`, tag: `footag`, drop: false},
	}

	for _, v := range testSet {
		ent := makeCSVEntry(v.data)
		if set, err := cr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatal(err)
		} else if v.drop && len(set) != 0 {
			t.Fatalf("invalid drop status on %+v: %d", v, len(set))
		} else if !v.drop && len(set) != 1 {
			t.Fatalf("invalid drop status: %d", len(set))
		} else if tg, ok := tagger.mp[v.tag]; !ok && !v.drop {
			t.Fatalf("tagger didn't create tag %v", v.tag)
		} else if tg != ent.Tag && !v.drop {
			t.Fatalf("Invalid tag results: %v != %v", tg, ent.Tag)
		}
	}

	//make an entry that completely fails
	ent := makeTestEntry(``)
	ent.Data = []byte("12343ablkjsdrlkjdslkrjdslkj")
	if set, err := cr.Process([]*entry.Entry{ent}); err != nil {
		t.Fatal(err)
	} else if len(set) != 1 {
		t.Fatal("Failed to hit default on count")
	} else if set[0].Tag != tagger.mp[`default`] {
		t.Fatal("Failed to hit default tag")
	}

	//try again with dropping on misses
	cc.Drop_Misses = true
	if cr, err = NewCSVRouter(cc, &tagger); err != nil {
		t.Fatal(err)
	}
	//check with the item that will completely miss
	if set, err := cr.Process([]*entry.Entry{ent}); err != nil {
		t.Fatal(err)
	} else if len(set) != 0 {
		t.Fatal("Failed to hit default on count")
	}
	//try with one that will hit the regex but not the routes or drops
	ent = makeCSVEntry(`a,b,c`)
	if set, err := cr.Process([]*entry.Entry{ent}); err != nil {
		t.Fatal(err)
	} else if len(set) != 0 {
		t.Fatal("Failed to hit default on count")
	}
}

func makeCSVEntry(df string) *entry.Entry {
	return &entry.Entry{
		Tag:  0, //doesn't matter
		SRC:  testIP,
		TS:   testTime,
		Data: []byte(df),
	}
}
