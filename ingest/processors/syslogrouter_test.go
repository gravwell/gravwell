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

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var (
	srConfig = SyslogRouterConfig{
		Drop_Misses: false,
		Template:    `${Hostname}-${Appname}`,
	}
)

func TestSyslogRouterConfig(t *testing.T) {
	var src SyslogRouterConfig
	if err := src.validate(); err == nil {
		t.Fatal("Failed to catch empty SyslogRouterConfig")
	}
	//check with a good set
	src.Template = srConfig.Template
	if err := src.validate(); err != nil {
		t.Fatal(err)
	}

	// check with a broken template
	src.Template = `${`
	if err := src.validate(); err == nil {
		t.Fatal("Failed to catch bad SyslogRouterConfig")
	}
	//check with a bad constant value
	src.Template = `${foobar}-!@`
	if err := src.validate(); err == nil {
		t.Fatal("Failed to catch forbidden constant")
	}
}

func TestSyslogRouterSimpleConfig(t *testing.T) {
	b := `
	[preprocessor "rtr"]
		type = syslogrouter
		Template="${_SRC_}-${Hostname}-${Appname}"
	`
	p, err := testLoadPreprocessor(b, `rtr`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the syslogrouter preprocess or
	if cp, ok := p.(*SyslogRouter); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *SyslogRouter", p)
	} else {
		if cp.Drop_Misses {
			t.Fatalf("invalid miss config: %v", cp.Drop_Misses)
		}
	}
}

func TestSyslogRouterSimpleConfigBadSpec(t *testing.T) {
	b := `
	[preprocessor "rtr"]
		type = syslogrouter
		Template="${_SRC_}:${Hostname}/${Appname}"
	`
	if _, err := testLoadPreprocessor(b, `rtr`); err == nil {
		t.Fatal("failed to catch spec with bad characters")
	}
}

func TestSyslogRouterRoute(t *testing.T) {
	var tagger testTagger
	//build out the default tag
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		t.Fatal(err)
	}

	b := `
	[preprocessor "rtr"]
		type = syslogrouter
		Template="${Hostname}-${Appname}"
	`
	p, err := testLoadPreprocessor(b, `rtr`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the syslogrouter preprocess or
	sr, ok := p.(*SyslogRouter)
	if !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *SyslogRouter", p)
	} else {
		if sr.Drop_Misses {
			t.Fatalf("invalid miss config: %v", sr.Drop_Misses)
		}
	}
	sr.tagger = &tagger

	testSet := []testTagSet{
		testTagSet{tag: `foobar-su`, data: "<34>1 2003-10-11T22:14:15.003Z foobar su - ID47 - 'su root' failed for lonvick on /dev/pts/8"},
		testTagSet{tag: `barbaz-`, data: `<34>1 2003-10-11T22:14:15.003Z barbaz - - ID47 - my message`},
		testTagSet{tag: `-su`, data: `<34>1 2003-10-11T22:14:15.003Z - su - ID47 - my message`},
		testTagSet{tag: `box-kernel`, data: `<6>1 2023-04-03T16:24:41.390796-06:00 box kernel - - - [849085.223282] restoring control 00000000-0000-0000-0000-000000000101/10/5`},
		testTagSet{tag: `box-`, data: `<6>1 2023-04-03T16:24:41.390796-06:00 box - - - - [849085.223282] restoring control 00000000-0000-0000-0000-000000000101/10/5`},
		//some rfc3164 entries
		testTagSet{tag: `box-very_large_syslog_message_tag`, data: `<34>Oct 11 22:14:15 box very.large.syslog.message.tag: 'su root' failed for lonvick on /dev/pts/8`},
		testTagSet{tag: `box-foo_bar_baz`, data: `<34>Oct 11 22:14:15 box foo!bar#baz: 'su root' failed for lonvick on /dev/pts/8`},
		testTagSet{tag: ``, data: `not syslog`},
	}

	for _, v := range testSet {
		ent := makeRawTestEntry(v.data)
		ogTag := ent.Tag
		if set, err := sr.Process([]*entry.Entry{ent}); err != nil {
			t.Fatal(err)
		} else if v.drop && len(set) != 0 {
			t.Fatalf("invalid drop status on %+v: %d", v, len(set))
		} else if !v.drop && len(set) != 1 {
			t.Fatalf("invalid drop status: %d", len(set))
		} else {
			if v.tag != `` {
				if tg, ok := tagger.mp[v.tag]; !ok && !v.drop {
					t.Fatalf("tagger didn't create tag %v", v.tag)
				} else if tg != ent.Tag && !v.drop {
					t.Fatalf("Invalid tag results: %v != %v", tg, ent.Tag)
				} else if set[0].Tag != tg {
					t.Fatalf("Invalid tag results: %v != %v", tg, set[0].Tag)
				}
			} else if ogTag != set[0].Tag {
				t.Fatalf("Tag was modified when it should not have been %s", v.data)
			}
		}
	}
}

func BenchmarkSyslogRouter(b *testing.B) {
	var tagger testTagger
	//build out the default tag
	if _, err := tagger.NegotiateTag(`default`); err != nil {
		b.Fatal(err)
	}

	buf := `
	[preprocessor "rtr"]
		type = syslogrouter
		Template="${Hostname}-${Appname}"
	`
	p, err := testLoadPreprocessor(buf, `rtr`)
	if err != nil {
		b.Fatal(err)
	}
	//cast to the syslogrouter preprocess or
	sr, ok := p.(*SyslogRouter)
	if !ok {
		b.Fatalf("preprocessor is the wrong type: %T != *SyslogRouter", p)
	} else {
		if sr.Drop_Misses {
			b.Fatalf("invalid miss config: %v", sr.Drop_Misses)
		}
	}
	sr.tagger = &tagger

	testSet := []testTagSet{
		testTagSet{tag: `foobar-su`, data: "<34>1 2003-10-11T22:14:15.003Z foobar su - ID47 - 'su root' failed for lonvick on /dev/pts/8"},
		testTagSet{tag: `barbaz-`, data: `<34>1 2003-10-11T22:14:15.003Z barbaz - - ID47 - my message`},
		testTagSet{tag: `-su`, data: `<34>1 2003-10-11T22:14:15.003Z - su - ID47 - my message`},
		testTagSet{tag: `box-kernel`, data: `<6>1 2023-04-03T16:24:41.390796-06:00 box kernel - - - [849085.223282] restoring control 00000000-0000-0000-0000-000000000101/10/5`},
		testTagSet{tag: `box-`, data: `<6>1 2023-04-03T16:24:41.390796-06:00 box - - - - [849085.223282] restoring control 00000000-0000-0000-0000-000000000101/10/5`},
		//some rfc3164 entries
		testTagSet{tag: `box-very_large_syslog_message_tag`, data: `<34>Oct 11 22:14:15 box very.large.syslog.message.tag: 'su root' failed for lonvick on /dev/pts/8`},
		testTagSet{tag: `box-foo_bar_baz`, data: `<34>Oct 11 22:14:15 box foo!bar#baz: 'su root' failed for lonvick on /dev/pts/8`},
		//one that is just bad and should not be remapped at all
		testTagSet{tag: ``, data: `not syslog`},
	}
	ents := make([]*entry.Entry, 0, len(testSet))
	for _, v := range testSet {
		ents = append(ents, makeRawTestEntry(v.data))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if set, err := sr.Process(ents); err != nil {
			b.Fatal(err)
		} else if len(set) != len(ents) {
			b.Fatalf("invalid return size: %d != %d", len(set), len(ents))
		}
	}
}

func makeRawTestEntry(df string) *entry.Entry {
	return &entry.Entry{
		Tag:  0, //doesn't matter
		SRC:  testIP,
		TS:   testTime,
		Data: []byte(df),
	}
}
