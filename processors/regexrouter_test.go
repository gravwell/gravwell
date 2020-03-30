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
	"testing"

	"github.com/gravwell/ingest/v3/entry"
)

const ()

var (
	rrc = RegexRouteConfig{
		Regex:            "foo bar (?P<capture>\\S+)",
		Route_Extraction: `capture`,
		Route:            []string{`foo:footag`, `bar:bartag`, `baz:`},
	}
)

func TestRegexRouteConfig(t *testing.T) {
	if _, rts, idx, err := rrc.validate(); err != nil {
		t.Fatal(err)
	} else if len(rts) != 3 {
		t.Fatal("bad route count")
	} else if idx != 1 {
		t.Fatal("bad index")
	}
}

func TestNewRegexRouter(t *testing.T) {
	var tg testTagger
	//build out the default tag
	if _, err := tg.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}
	rc := rrc
	//make a new one
	rr, err := NewRegexRouter(rrc, &tg)
	if err != nil {
		t.Fatal(err)
	}
	//reconfigure the existing
	rc.Route = append(rc.Route, `testA:A`)
	if err = rr.Config(rc, &tg); err != nil {
		t.Fatal(err)
	}
	if tg.mp[`footag`] != 1 || tg.mp[`bartag`] != 2 || tg.mp[`A`] != 3 {
		t.Fatalf("bad tag negotiations: %+v", tg.mp)
	}
}

type testTagSet struct {
	data string
	tag  string
	drop bool
}

func TestRegexRouterProcess(t *testing.T) {
	var tagger testTagger
	//build out the default tag
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}
	rc := rrc
	rc.Route = append(rc.Route, `dropme:`)
	rr, err := NewRegexRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}
	testSet := []testTagSet{
		testTagSet{data: `foo`, tag: `footag`, drop: false},
		testTagSet{data: `bar`, tag: `bartag`, drop: false},
		testTagSet{data: `baz`, tag: ``, drop: true},
		testTagSet{data: `dropme`, tag: ``, drop: true},
		testTagSet{data: `UncleEddy`, tag: `default`, drop: false},
	}

	for _, v := range testSet {
		ent := makeTestEntry(v.data)
		if set, err := rr.Process(ent); err != nil {
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

	//make an entry that completely fails the regex
	ent := makeTestEntry(``)
	ent.Data = []byte("12343ablkjsdrlkjdslkrjdslkj")
	if set, err := rr.Process(ent); err != nil {
		t.Fatal(err)
	} else if len(set) != 1 {
		t.Fatal("Failed to hit default on count")
	} else if set[0].Tag != tagger.mp[`default`] {
		t.Fatal("Failed to hit default tag")
	}

	//try again with dropping on misses
	rc.Drop_Misses = true
	if rr, err = NewRegexRouter(rc, &tagger); err != nil {
		t.Fatal(err)
	}
	//check with the item that will completely miss the regex
	if set, err := rr.Process(ent); err != nil {
		t.Fatal(err)
	} else if len(set) != 0 {
		t.Fatal("Failed to hit default on count")
	}
	//try with one that will hit the regex but not the routes or drops
	ent = makeTestEntry(`testval`)
	if set, err := rr.Process(ent); err != nil {
		t.Fatal(err)
	} else if len(set) != 0 {
		t.Fatal("Failed to hit default on count")
	}
}

func makeTestEntry(df string) *entry.Entry {
	return &entry.Entry{
		Tag:  0, //doesn't matter
		SRC:  testIP,
		TS:   testTime,
		Data: []byte(fmt.Sprintf(`foo bar %s and some other things`, df)),
	}
}
