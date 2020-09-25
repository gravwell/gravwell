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
	"time"

	//"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	testTemplate string = "START ${stuff}\t${things} END"
	testRegex    string = `\d+\s+(?P<things>\S+)\s+(?P<stuff>\S+)`
)

var (
	testTag         entry.EntryTag = 2
	testIP                         = net.ParseIP("192.168.1.1")
	testTime                       = entry.FromStandard(time.Now())
	testRegexConfig                = RegexExtractConfig{
		Template: testTemplate,
		Regex:    testRegex,
	}
)

func TestRegexExtractConfig(t *testing.T) {
	var rec RegexExtractConfig
	if _, _, err := rec.validate(); err == nil {
		t.Fatal("Failed to catch empty RegexExtractConfig")
	}
	//check with a good set
	rec.Template = testTemplate
	rec.Regex = testRegex
	if tmp, rx, err := rec.validate(); err != nil {
		t.Fatal(err)
	} else if tmp == nil || rx == nil {
		t.Fatal("bad params")
	}
	//check with a bad regex
	rec.Regex = `\d+(\x`
	if _, _, err := rec.validate(); err == nil {
		t.Fatal("Failed to catch bad regex")
	}
	//test with a bad template
	rec.Regex = testRegex
	rec.Template = `TEST ${things`
	if _, _, err := rec.validate(); err == nil {
		t.Fatal("Failed to catch bad template")
	}
	//test with missing regex values
	rec.Template = `TEST ${things} ${nope, Chuck Testa!}`
	if _, _, err := rec.validate(); err == nil {
		t.Fatal("Failed to catch missing template values")
	}
}

func TestRegexExtract(t *testing.T) {
	re, err := NewRegexExtractor(testRegexConfig)
	if err != nil {
		t.Fatal(err)
	}
	ent := &entry.Entry{
		Tag:  testTag,
		SRC:  testIP,
		TS:   testTime,
		Data: []byte(`101 THINGS STUFF`),
	}
	ents, err := re.Process(ent)
	if err != nil {
		t.Fatal(err)
	} else if len(ents) != 1 {
		t.Fatal("bad count", len(ents))
	}
	tent := ents[0]
	if tent.Tag != ent.Tag || !tent.SRC.Equal(ent.SRC) || tent.TS != ent.TS {
		t.Fatal("bad entry header data:", tent)
	}
	if string(tent.Data) != `START STUFF	THINGS END` {
		t.Fatal("Bad result")
	}
}

func TestRegexExtractSRC(t *testing.T) {
	cfg := RegexExtractConfig{
		Template: "${_SRC_} ${stuff}",
		Regex:    testRegex,
	}
	re, err := NewRegexExtractor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ent := &entry.Entry{
		Tag:  testTag,
		SRC:  testIP,
		TS:   testTime,
		Data: []byte(`101 THINGS STUFF`),
	}
	ents, err := re.Process(ent)
	if err != nil {
		t.Fatal(err)
	} else if len(ents) != 1 {
		t.Fatal("bad count", len(ents))
	}
	tent := ents[0]
	if tent.Tag != ent.Tag || !tent.SRC.Equal(ent.SRC) || tent.TS != ent.TS {
		t.Fatal("bad entry header data:", tent)
	}
	if string(tent.Data) != fmt.Sprintf("%v STUFF", testIP) {
		t.Fatalf("Bad result: %v", string(tent.Data))
	}
}
