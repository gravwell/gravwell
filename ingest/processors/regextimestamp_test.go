/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/config"
)

func TestRegexTimestamp(t *testing.T) {
	b := []byte(`
	[global]
	foo = "bar"
	bar = 1337
	baz = 1.337
	foo-bar-baz="foo bar baz"

	[item "A"]
	name = "test A"
	value = 0xA

	[preprocessor "re1"]
		type = regextimestamp
		TS-Match-Name=ts
		Timestamp-Format-Override="rfc3339"
		Regex=` + "`foobar=(?P<ts>\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\S+) BAR`")
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
	p, err := tc.Preprocessor.getProcessor(`re1`, &tt)
	if err != nil {
		t.Fatal(err)
	}
	set, err := p.Process(makeEntry([]byte("foobar=2009-11-10T23:00:00Z BAR"), 0))
	if err != nil {
		t.Fatal(err)
	} else if len(set) != 1 {
		t.Fatalf("Invalid set count: %d", len(set))
	}
	ts, _ := time.Parse(time.RFC3339, `2009-11-10T23:00:00Z`)
	if !set[0].TS.StandardTime().Equal(ts) {
		t.Fatalf("invalid timestamp: %v != %v", set[0].TS, ts)
	}

}
