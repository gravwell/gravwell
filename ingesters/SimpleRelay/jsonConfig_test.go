/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"testing"
)

const (
	te1 string = `xxx:yyy`
	te2 string = `xxx : yyy`
	te3 string = `xxx123: yyy`
	te4 string = `ABCD:yyy`
	te5 string = `"A.b.c.d" : yyy`
	te6 string = `"A::b::CC":"yyy"`

	bte1 string = ``
	bte2 string = `A`
	bte3 string = `A:`
	bte4 string = `:C`
	bte5 string = `A:"  "`
	bte6 string = `A:abc123--`
)

var (
	goodTags = []good{
		good{te1, "xxx", "yyy"},
		good{te2, "xxx", "yyy"},
		good{te3, "xxx123", "yyy"},
		good{te4, "ABCD", "yyy"},
		good{te5, "A.b.c.d", "yyy"},
		good{te6, "A::b::CC", "yyy"},
	}
	badTags = []string{bte1, bte2, bte3, bte4, bte5}
)

type good struct {
	v   string
	m   string
	tag string
}

func TestExtractElement(t *testing.T) {
	for _, v := range goodTags {
		if m, tag, err := extractElementTag(v.v); err != nil {
			t.Fatal(err)
		} else if m != v.m {
			t.Fatal(fmt.Errorf(`Bad match extraction(%s): "%v" != "%v"`, v.v, m, v.m))
		} else if tag != v.tag {
			t.Fatal(fmt.Errorf(`Bad tag extraction(%s): "%v" != "%v"`, v, tag, v.tag))
		}
	}

	for _, v := range badTags {
		if _, _, err := extractElementTag(v); err == nil {
			t.Fatal("Failed to catch bad tag " + v)
		}
	}
}

type checker struct {
	orig   string
	fields []string
}

func TestExtractFields(t *testing.T) {
	toCheck := []checker{
		checker{`a.b.c.d`, []string{`a`, `b`, `c`, `d`}},
		checker{`a.b.c."d.e.f"`, []string{`a`, `b`, `c`, `d.e.f`}},
	}
	for _, tc := range toCheck {
		flds, err := getJsonFields(tc.orig)
		if err != nil {
			t.Fatal(err)
		} else if len(flds) != len(tc.fields) {
			t.Fatal(fmt.Errorf("Field count mismatch: %d != %d", len(flds), len(tc.fields)))
		}
		for i := range flds {
			if flds[i] != tc.fields[i] {
				t.Fatal(fmt.Errorf("Field %d mismatch: %s != %s", i, flds[i], tc.fields[i]))
			}
		}
	}
}

func TestLoadConfig(t *testing.T) {
	fout, err := ioutil.TempFile(tmpDir, `cfg`)
	if err != nil {
		t.Fatal()
	}
	defer fout.Close()
	if n, err := io.WriteString(fout, jsonbaseConfig); err != nil {
		t.Fatal(err)
	} else if n != len(jsonbaseConfig) {
		t.Fatal(fmt.Sprintf("Failed to write full file: %d != %d", n, len(jsonbaseConfig)))
	}
	cfg, err := GetConfig(fout.Name())
	if err != nil {
		t.Fatal(err)
	}
	if tgts, err := cfg.Targets(); err != nil {
		t.Fatal(err)
	} else if len(tgts) != 4 {
		t.Fatal("Invalid target count")
	}
	if cfg.Secret() != `IngestSecrets` {
		t.Fatal("invalid secret")
	}
	if cfg.Max_Ingest_Cache != 1024*1024*1024 {
		t.Fatal("invalid cache size")
	}
	if cfg.Ingest_Cache_Path != `/opt/gravwell/cache/simple_relay.cache` {
		t.Fatal("invalid cache path")
	}
	if len(cfg.JSONListener) != 1 {
		t.Fatal("Invalid listeners")
	}
	l, ok := cfg.JSONListener[`default`]
	if !ok {
		t.Fatal("missing default listener")
	}
	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}
	if l.Extractor != `A.B.C.D` {
		t.Fatal("invalid extractor")
	}
	if l.Default_Tag != `testing` {
		t.Fatal("invalid default tag")
	}
	if len(l.Tag_Match) != 2 {
		t.Fatal("invalid tag matches")
	}
	tm, err := l.TagMatchers()
	if err != nil {
		t.Fatal(err)
	}
	if len(tm) != 2 {
		t.Fatal("invalid tag matchers")
	}
	for _, m := range tm {
		switch m.Value {
		case `XXX`:
			if m.Tag != `Xtag` {
				t.Fatal("Invalid tag match ", m)
			}
		case `YYY`:
			if m.Tag != `Ytag` {
				t.Fatal("Invalid tag match ", m)
			}
		default:
			t.Fatal("Invalid tag ", m)
		}
	}
}

const (
	jsonbaseConfig string = `
[Global]
Ingest-Secret = IngestSecrets
Connection-Timeout = 0
Insecure-Skip-TLS-Verify=false
Cleartext-Backend-target=127.0.0.1:4023 #example of adding a cleartext connection
Cleartext-Backend-target=127.1.0.1:4023 #example of adding another cleartext connection
Encrypted-Backend-target=127.1.1.1:4024 #example of adding an encrypted connection
Pipe-Backend-Target=/opt/gravwell/comms/pipe #a named pipe connection, this should be used when ingester is on the same machine as a backend
Ingest-Cache-Path=/opt/gravwell/cache/simple_relay.cache #adding an ingest cache for local storage when uplinks fail
Max-Ingest-Cache=1024 #Number of MB to store, localcache will only store 1GB before stopping.  This is a safety net
Log-Level=INFO
Log-File=/opt/gravwell/log/simple_relay.log

#basic default logger, all entries will go to the default tag
# this is useful for sending generic line-delimited
# data to Gravwell, for example if you have an old log file sitting around:
#	cat logfile | nc gravwell-host 7777
#no Tag-Name means use the default tag
[JSONListener "default"]
	Bind-String="0.0.0.0:7777" #we are binding to all interfaces, with TCP implied
	Assume-Local-Timezone=false #Default for assume localtime is false
	Source-Override="DEAD::BEEF" #override the source for just this listener
	Extractor=A.B.C.D
	Default-Tag=testing
	Tag-Match=XXX:Xtag
	Tag-Match=YYY:Ytag
	`
)
