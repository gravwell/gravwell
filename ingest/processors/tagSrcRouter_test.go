/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"net"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const ()

var (
	goodTagSrcRoutes = []TagSrcRouterConfig{
		// 2 entries srcTag:dstTag
		TagSrcRouterConfig{
			Route: []string{`alpha:foxtrot`},
		},
		// 3 entries srcTag:dstTag:ipFilter
		TagSrcRouterConfig{
			Route: []string{`bravo:echo:2.2.2.2`},
		},
		// 3 entries srcTag:dstTag:ipFilter/netmask
		TagSrcRouterConfig{
			Route: []string{`charlie:delta:3.0.0.0/8`},
		},
	}

	badTagSrcRoutes = []TagSrcRouterConfig{
		// No Routes
		TagSrcRouterConfig{
			Route: []string{``},
		},
		//Incomplete definition
		TagSrcRouterConfig{
			Route: []string{`alpha`},
		},
		//Invalid IP addr
		TagSrcRouterConfig{
			Route: []string{`alpha:bravo:charlie`},
		},
		//Bad Src Tag
		TagSrcRouterConfig{
			Route: []string{`x!f_$$$:bravo`},
		},
		//Bad Dst Tag
		TagSrcRouterConfig{
			Route: []string{`alpha:x!f_$$$`},
		},
	}

	tags = []string{
		`default`,
		`alpha`,
		`bravo`,
		`charlie`,
		`delta`,
		`echo`,
		`foxtrot`,
	}
)

func TestTagSrcRouter(t *testing.T) {
	var tg testTagger
	//build out the default tag
	if _, err := tg.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}
	rc := goodTagSrcRoutes[0]
	//make a new one
	rr, err := NewTagSrcRouter(rc, &tg)
	if err != nil {
		t.Fatal(err)
	}

	//reconfigure the existing
	rc.Route = append(rc.Route, `charlie:delta`)
	if err = rr.Config(rc, &tg); err != nil {
		t.Fatal(err)
	}
	if tg.mp[`default`] != 0 || tg.mp[`delta`] != 2 || tg.mp[`foxtrot`] != 1 {
		t.Fatalf("bad tag negotiations: %+v", tg.mp)
	}
}

func TestGoodTagSrcRoutes(t *testing.T) {
	var tg testTagger

	for i, c := range goodTagSrcRoutes {
		if _, err := c.parseRoutes(&tg); err != nil {
			t.Fatalf("Failed good config %d (%+v)\n", i, c)
			t.Fatal(err)
		}
	}
}

func TestBadTagSrcRoutes(t *testing.T) {
	var tg testTagger

	for i, c := range badTagSrcRoutes {
		if _, err := c.parseRoutes(&tg); err == nil {
			t.Fatalf("Failed to catch bad config %d (%+v)\n", i, c)
		}
	}
}

func TestTagSrcProcess(t *testing.T) {
	var tg testTagger

	for _, tag := range tags {
		if _, err := tg.NegotiateTag(tag); err != nil {
			t.Fatal(err)
		}
	}

	// 0: default
	// 1: alpha
	// 2: bravo
	// 3: charlie
	// 4: delta
	// 5: echo
	// 6: foxtrot
	entArray := []*entry.Entry{}
	entArray = append(entArray, makeTagSrcEntry(entry.EntryTag(1), `1.1.1.1`))     // 1.1.1.1 1 (Alpha)
	entArray = append(entArray, makeTagSrcEntry(entry.EntryTag(2), `2.2.2.2`))     // 2.2.2.2 2 (Bravo)
	entArray = append(entArray, makeTagSrcEntry(entry.EntryTag(2), `22.22.22.22`)) // 22.22.22.22 2 (Bravo)
	entArray = append(entArray, makeTagSrcEntry(entry.EntryTag(3), `3.3.3.3`))     // 3.3.3.3 3 (Charlie)
	entArray = append(entArray, makeTagSrcEntry(entry.EntryTag(3), `33.33.33.33`)) // 33.33.33.33 3 (Charlie)

	strArray := []string{}
	strArray = append(strArray, `alpha:foxtrot`)           //Route 1
	strArray = append(strArray, `bravo:echo:2.2.2.2`)      //Route 2
	strArray = append(strArray, `charlie:delta:3.0.0.0/8`) // Route3
	// 1.1.1.1     6 (Foxtrot) //Route 1
	// 2.2.2.2     5 (Echo)    //Route 2
	// 22.22.22.22 2 (Bravo)   //No Route
	// 3.3.3.3     4 (Charlie) //Route3
	// 33.33.33.33 3 (Charlie) //No Route

	rr, err := NewTagSrcRouter(TagSrcRouterConfig{Route: strArray}, &tg)
	if err != nil {
		t.Fatal(err)
	}

	if modifiedEntArray, err := rr.Process(entArray); err != nil {
		t.Fatal(err)
	} else {
		if modifiedEntArray[0].Tag != 6 || modifiedEntArray[1].Tag != 5 || modifiedEntArray[2].Tag != 2 || modifiedEntArray[3].Tag != 4 || modifiedEntArray[4].Tag != 3 {
			t.Fatalf("tags not processed properly")
		}
	}
}

func makeTagSrcEntry(tag entry.EntryTag, ip string) *entry.Entry {
	ent := &entry.Entry{
		Tag:  tag, //doesn't matter
		SRC:  net.ParseIP(ip),
		TS:   testTime,
		Data: []byte(`foo bar and some other things`),
	}
	ent.AddEnumeratedValueEx(`testing`, uint64(42))
	return ent
}
