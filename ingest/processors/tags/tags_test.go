/*************************************************************************
* Copyright 2019 Gravwell, Inc. All rights reserved.
* Contact: <legal@gravwell.io>
*
* This software may be modified and distributed under the terms of the
* BSD 2-clause license. See the LICENSE file for details.
**************************************************************************/

package tags

import (
	"testing"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestTagMask(t *testing.T) {
	var tm TagMask
	//ensure every tag is empty
	i := entry.EntryTag(0)
	for {
		if tm.IsSet(i) {
			t.Fatalf("Tag at position %d is not empty", i)
		} else if i == 0xffff {
			break
		}
		i++
	}

	i = entry.EntryTag(0)
	//set every tag, check it, clear it, check it
	for {
		tm.Set(i)
		if !tm.IsSet(i) {
			t.Fatalf("Tag at position %d is not", i)
		}
		tm.Clear(i)
		if tm.IsSet(i) {
			t.Fatalf("Tag at position %d is not empty", i)
		} else if i == 0xffff {
			break
		}
		i++
	}
}

func TestNewTaggerBad(t *testing.T) {
	tc := TaggerConfig{
		Tags: []string{
			`default`,
			`gravwell`,
			`)(##@`, //bad
		},
	}
	if _, err := NewTagger(tc, newTestTagNeg()); err == nil {
		t.Fatal("Failed to catch bad tagger config")
	}
}

func TestNewTagger(t *testing.T) {
	tc := TaggerConfig{
		Tags: []string{
			`default`,
			`gravwell`,
			`foo*bar`,
			`bar*baz`,
		},
	}
	tgr, err := NewTagger(tc, newTestTagNeg())
	if err != nil {
		t.Fatalf("Failed to build new tagger: %v", err)
	}

	var hitTags []entry.EntryTag
	var max entry.EntryTag
	hits := []string{`default`, `gravwell`, `foobar`, `foofatbar`, `barbaz`, `bar1234baz`}
	miss := []string{`default1`, `gravwell1`, `foobarbaz`, `fatbar`, `1234`, `things`}

	for _, h := range hits {
		var tg entry.EntryTag
		if !tgr.AllowedName(h) {
			t.Fatalf("Failed to allow %s", h)
		} else if tg, err = tgr.Negotiate(h); err != nil {
			t.Fatalf("Failed to Negotiate %s: %v", h, err)
		} else {
			hitTags = append(hitTags, tg)
			if tg >= max {
				max = tg
			}
		}
	}

	for _, m := range miss {
		var tg entry.EntryTag
		if tgr.AllowedName(m) {
			t.Fatalf("Allowed %s", m)
		} else if tg, err = tgr.Negotiate(m); err != nil {
			t.Fatalf("Failed to negotiate tag %s - %v", m, err)
		} else if tgr.Allowed(tg) {
			t.Fatalf("Failed to reject negotiated bad tag: %s %d", m, tg)
		}
	}

	//now check the internal state of the maska
	for i, tn := range hitTags {
		if !tgr.Allowed(tn) {
			t.Fatalf("did not allow index %d / %d", i, tn)
		}
	}

	for i := entry.EntryTag(0xffff); i > max; i-- {
		if tgr.Allowed(i) {
			t.Fatalf("Allowed un-negotiated tag %d", i)
		}
	}

}

type testTagNeg struct {
	mp map[string]entry.EntryTag
}

func newTestTagNeg() *testTagNeg {
	return &testTagNeg{
		mp: make(map[string]entry.EntryTag),
	}
}

func (ttn *testTagNeg) NegotiateTag(tn string) (tg entry.EntryTag, err error) {
	if err = ingest.CheckTag(tn); err == nil {
		tg = entry.EntryTag(len(ttn.mp))
		ttn.mp[tn] = tg
	}
	return
}
