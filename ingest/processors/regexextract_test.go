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
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
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
	if _, _, _, err := rec.validate(); err == nil {
		t.Fatal("Failed to catch empty RegexExtractConfig")
	}
	//check with a good set
	rec.Template = testTemplate
	rec.Regex = testRegex
	if tmp, rx, _, err := rec.validate(); err != nil {
		t.Fatal(err)
	} else if tmp == nil || rx == nil {
		t.Fatal("bad params")
	}
	//check with a bad regex
	rec.Regex = `\d+(\x`
	if _, _, _, err := rec.validate(); err == nil {
		t.Fatal("Failed to catch bad regex")
	}
	//test with a bad template
	rec.Regex = testRegex
	rec.Template = `TEST ${things`
	if _, _, _, err := rec.validate(); err == nil {
		t.Fatal("Failed to catch bad template")
	}
	//test with missing regex values
	rec.Template = `TEST ${things} ${nope, Chuck Testa!}`
	if _, _, _, err := rec.validate(); err == nil {
		t.Fatal("Failed to catch missing template values")
	}
}

func TestRegexExtractorEmptyConfig(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = regexextract
		Template="${_SRC_} ${stuff}"
		Regex=` + "`" + testRegex + "`"
	p, err := testLoadPreprocessor(b, `ise`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the regexextract preprocess or
	if cp, ok := p.(*RegexExtractor); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *RegexExtractor", p)
	} else {
		if cp.Drop_Misses || !cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestRegexExtractorSingleAttachConfig(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = regexextract
		Template="${_SRC_} ${stuff}"
		Regex=` + "`" + testRegex + "`" + `
		Attach=things
		`
	p, err := testLoadPreprocessor(b, `ise`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the regexextract preprocess or
	if cp, ok := p.(*RegexExtractor); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *RegexExtractor", p)
	} else {
		if cp.Drop_Misses || !cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		} else if len(cp.Attach) != 1 || cp.Attach[0] != `things` {
			t.Fatalf("invalid attach directive")
		} else if len(cp.attachSet) != 1 {
			t.Fatalf("did not create attach set: %v", len(cp.attachSet))
		}
	}
}

func TestRegexExtractorBasicConfig(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = regexextract
		Passthrough-Misses=false
		Template="${_SRC_} ${stuff}"
		Regex=` + "`" + testRegex + "`"
	p, err := testLoadPreprocessor(b, `ise`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the regexextract preprocess or
	if cp, ok := p.(*RegexExtractor); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *RegexExtractor", p)
	} else {
		if !cp.Drop_Misses || cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestRegexExtractorConflictingConfig(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = regexextract
		Passthrough-Misses=false
		Drop-Misses=true
		Template="${_SRC_} ${stuff}"
		Regex=` + "`" + testRegex + "`"
	p, err := testLoadPreprocessor(b, `ise`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the regexextract preprocess or
	if cp, ok := p.(*RegexExtractor); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *RegexExtractor", p)
	} else {
		if !cp.Drop_Misses || cp.Passthrough_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestRegexExtractorDropConfig(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = regexextract
		Drop-Misses=true
		Template="${_SRC_} ${stuff}"
		Regex=` + "`" + testRegex + "`"
	p, err := testLoadPreprocessor(b, `ise`)
	if err != nil {
		t.Fatal(err)
	}
	//cast to the regexextract preprocess or
	if cp, ok := p.(*RegexExtractor); !ok {
		t.Fatalf("preprocessor is the wrong type: %T != *RegexExtractor", p)
	} else {
		if !cp.Drop_Misses {
			t.Fatalf("invalid miss config: %v %v", cp.Drop_Misses, cp.Passthrough_Misses)
		}
	}
}

func TestRegexExtractorInvalidAttach(t *testing.T) {
	b := `
	[preprocessor "ise"]
		type = regexextract
		Drop-Misses=true
		Template="${_SRC_} ${stuff}"
		Regex=` + "`" + testRegex + "`" + `
		Attach=things
		Attach=foobar
		`
	if _, err := testLoadPreprocessor(b, `ise`); err == nil {
		t.Fatal("failed to catch bad extract")
	} else if !strings.Contains(err.Error(), `foobar`) {
		t.Fatalf("probably failed to catch bad attach: %v", err)
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
	ents, err := re.Process([]*entry.Entry{ent})
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
	ents, err := re.Process([]*entry.Entry{ent})
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

func TestRegexExtractProcess(t *testing.T) {
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
		type = regexextract
		Passthrough-Misses=false
		Template="${_SRC_} ${stuff}"
		Regex=` + "`" + testRegex + "`")
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
	ent := &entry.Entry{
		Tag:  testTag,
		SRC:  testIP,
		TS:   testTime,
		Data: []byte(`101 THINGS STUFF`),
	}
	if ents, err := p.Process([]*entry.Entry{ent}); err != nil {
		t.Fatal(err)
	} else if len(ents) != 1 {
		t.Fatal("bad count", len(ents))
	} else {
		tent := ents[0]
		if tent.Tag != ent.Tag || !tent.SRC.Equal(ent.SRC) || tent.TS != ent.TS {
			t.Fatal("bad entry header data:", tent)
		}
		if string(tent.Data) != fmt.Sprintf("%v STUFF", testIP) {
			t.Fatalf("Bad result: %v", string(tent.Data))
		}
		if evs := tent.EnumeratedValues(); len(evs) != 0 {
			t.Fatalf("errant attach: %v", evs)
		}
	}
}

func TestRegexExtractAttach(t *testing.T) {
	cfg := testRegexConfig
	cfg.Attach = []string{
		`stuff`,
		`things`,
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
	ents, err := re.Process([]*entry.Entry{ent})
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
	if evs := tent.EnumeratedValues(); len(evs) != 2 {
		t.Fatal("did not attach", len(evs))
	} else {
		if evs[0].Name != `stuff` || evs[0].Value.String() != `STUFF` {
			t.Fatal("bad extract", evs[0].String())
		}
		if evs[1].Name != `things` || evs[1].Value.String() != `THINGS` {
			t.Fatal("bad extract", evs[1].String())
		}
	}
}
