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
	"net"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const ()

var (
	src = SrcRouteConfig{
		Route: []string{`1.1.1.1:foo`, `2.2.2.2:bar`, `3.3.3.3:`},
	}
)

func TestSrcRouteConfig(t *testing.T) {
	if rts, err := src.validate(); err != nil {
		t.Fatal(err)
	} else if len(rts) != 3 {
		t.Fatal("bad route count")
	}
}

func TestNewSrcRouter(t *testing.T) {
	var tg testTagger
	//build out the default tag
	if _, err := tg.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}
	rc := src
	//make a new one
	rr, err := NewSrcRouter(src, &tg)
	if err != nil {
		t.Fatal(err)
	}
	//reconfigure the existing
	rc.Route = append(rc.Route, `4.4.4.4:A`)
	if err = rr.Config(rc, &tg); err != nil {
		t.Fatal(err)
	}
	if tg.mp[`foo`] != 1 || tg.mp[`bar`] != 2 || tg.mp[`A`] != 3 {
		t.Fatalf("bad tag negotiations: %+v", tg.mp)
	}
}

func TestSrcRouterFile(t *testing.T) {
	var tg testTagger
	//build out the default tag
	if _, err := tg.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}
	rc := SrcRouteConfig{Route_File: "test_data/src_routes"}
	//make a new one
	_, err := NewSrcRouter(rc, &tg)
	if err != nil {
		t.Fatal(err)
	}
	if tg.mp[`file1`] != 1 || tg.mp[`file2`] != 2 {
		t.Fatalf("bad tag negotiations: %+v", tg.mp)
	}
}

type testSrcTagSet struct {
	src  string
	tag  string
	drop bool
}

func TestSrcRouterProcess(t *testing.T) {
	var tagger testTagger
	//build out the default tag
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}
	rc := src
	rc.Route = append(rc.Route, `4.4.4.4:`)
	rr, err := NewSrcRouter(rc, &tagger)
	if err != nil {
		t.Fatal(err)
	}
	testSet := []testSrcTagSet{
		testSrcTagSet{src: `1.1.1.1`, tag: `foo`, drop: false},
		testSrcTagSet{src: `2.2.2.2`, tag: `bar`, drop: false},
		testSrcTagSet{src: `3.3.3.3`, tag: ``, drop: true},
		testSrcTagSet{src: `4.4.4.4`, tag: ``, drop: true},
		testSrcTagSet{src: `5.5.5.5`, tag: `default`, drop: false},
	}

	for _, v := range testSet {
		ent := makeSrcTestEntry(v.src)
		if set, err := rr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatal(err)
		} else if v.drop && len(set) != 0 {
			t.Fatalf("invalid drop status on %+v: %d", v, len(set))
		} else if !v.drop && len(set) != 1 {
			t.Fatalf("invalid drop status: %d", len(set))
		} else if tg, ok := tagger.mp[v.tag]; !ok && !v.drop {
			t.Fatalf("tagger didn't create tag %v", v.tag)
		} else if tg != ent.Tag && !v.drop {
			t.Fatalf("Invalid tag results for src %v: %v != %v", v.src, tg, ent.Tag)
		}
	}

	//try again with dropping on misses
	rc.Drop_Misses = true
	if rr, err = NewSrcRouter(rc, &tagger); err != nil {
		t.Fatal(err)
	}
	//check with the item with no match
	ent := makeSrcTestEntry(`5.5.5.5`)
	if set, err := rr.Process([]*entry.Entry{ent}); err != nil {
		t.Fatal(err)
	} else if len(set) != 0 {
		t.Fatal("Failed to drop non-matching item")
	}
}

func makeSrcTestEntry(ip string) *entry.Entry {
	return &entry.Entry{
		Tag:  0, //doesn't matter
		SRC:  net.ParseIP(ip),
		TS:   testTime,
		Data: []byte(fmt.Sprintf(`foo bar and some other things`)),
	}
}
