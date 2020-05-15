/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/config"
)

type testEntry struct {
	body       string
	shouldPass bool
}

func TestJsonFilterConfig(t *testing.T) {
	//testInputJson := `{"f1": "foo", "f2": 17, "foobar": {"baz": 4.12}}`
	b := []byte(`
	[global]
	[preprocessor "j1"]
		type = jsonfilter
		Match-Action=pass
		Match-Logic=and
		Field-Filter=f1,test_data/filter1
		Field-Filter=f2,test_data/filter2
	`)
	tc := struct {
		Global struct {
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
	p, err := tc.Preprocessor.getProcessor(`j1`, &tt)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("no processor returned")
	}
}

// Test when we say PASS if *all* fields match something
func TestFilterPassAND(t *testing.T) {
	cfg := []byte(`
	[global]
	[preprocessor "jf"]
		type = jsonfilter
		Match-Action=pass
		Match-Logic=and
		Field-Filter=f1,test_data/filter1
		Field-Filter=f2.f3,test_data/filter2
	`)
	entries := []testEntry{
		testEntry{`{"f1":"foo", "f2":{"f3":"X"}}`, true}, // both fields match, pass
		testEntry{`{"f1":"foo", "f2":"zappa"}`, false},   // only one field will match, drop
	}
	if err := runFilterTest(cfg, "jf", entries); err != nil {
		t.Fatal(err)
	}
}

func TestFilterDropAND(t *testing.T) {
	cfg := []byte(`
	[global]
	[preprocessor "jf"]
		type = jsonfilter
		Match-Action=drop
		Match-Logic=and
		Field-Filter=f1,test_data/filter1
		Field-Filter=f2.f3,test_data/filter2
	`)
	entries := []testEntry{
		testEntry{`{"f1":"foo", "f2":{"f3":"X"}}`, false}, // both fields match, drop
		testEntry{`{"f1":"foo", "f2":"zappa"}`, true},     // only one field will match, pass
		testEntry{`"desperadoes"`, true},                  // matches nothing at all, should pass
	}
	if err := runFilterTest(cfg, "jf", entries); err != nil {
		t.Fatal(err)
	}
}

func TestFilterPassOR(t *testing.T) {
	cfg := []byte(`
	[global]
	[preprocessor "jf"]
		type = jsonfilter
		Match-Action=pass
		Match-Logic=or
		Field-Filter=f1,test_data/filter1
		Field-Filter=f2.f3,test_data/filter2
	`)
	entries := []testEntry{
		testEntry{`{"f1":"foo", "f2":{"f3":"X"}}`, true},        // both fields are in the files, pass
		testEntry{`{"f1":"foo", "f2":"zappa"}`, true},           // only one field will match, pass
		testEntry{`{"xyzz":"quux", "f2":{"f3":"paco"}}`, false}, // neither field matches, drop
	}
	if err := runFilterTest(cfg, "jf", entries); err != nil {
		t.Fatal(err)
	}
}

func TestFilterDropOR(t *testing.T) {
	cfg := []byte(`
	[global]
	[preprocessor "jf"]
		type = jsonfilter
		Match-Action=drop
		Match-Logic=or
		Field-Filter=f1,test_data/filter1
		Field-Filter=f2.f3,test_data/filter2
	`)
	entries := []testEntry{
		testEntry{`{"f1":"foo", "f2":{"f3":"X"}}`, false},      // both fields are in the files, drop
		testEntry{`{"f1":"foo", "f2":"zappa"}`, false},         // only one field will match, drop
		testEntry{`{"xyzz":"quux", "f2":{"f3":"paco"}}`, true}, // neither field matches, pass
	}
	if err := runFilterTest(cfg, "jf", entries); err != nil {
		t.Fatal(err)
	}
}

func TestFilterFileReuse(t *testing.T) {
	cfg := []byte(`
	[global]
	[preprocessor "jf"]
		type = jsonfilter
		Match-Action=pass
		Match-Logic=and
		Field-Filter=f1,test_data/filter1
		Field-Filter=f2,test_data/filter1
	`)
	entries := []testEntry{
		testEntry{`{"f1":"foo", "f2":"foo"`, true},              // both fields are in the files, drop
		testEntry{`{"f1":"foo", "f2":"zappa"}`, false},          // only one field will match, drop
		testEntry{`{"xyzz":"quux", "f2":{"f3":"paco"}}`, false}, // neither field matches, drop
	}
	if err := runFilterTest(cfg, "jf", entries); err != nil {
		t.Fatal(err)
	}
}

func runFilterTest(cfg []byte, processorName string, entries []testEntry) error {
	tc := struct {
		Global struct {
		}
		Item map[string]*struct {
			Name  string
			Value int
		}
		Preprocessor ProcessorConfig
	}{}
	if err := config.LoadConfigBytes(&tc, cfg); err != nil {
		return err
	}
	var tt testTagger
	p, err := tc.Preprocessor.getProcessor(processorName, &tt)
	if err != nil {
		return err
	}
	if p == nil {
		return errors.New("no processor back")
	}
	for i := range entries {
		rset, err := p.Process(makeEntry([]byte(entries[i].body), 0))
		if err != nil {
			return err
		}
		if entries[i].shouldPass && len(rset) == 0 {
			return fmt.Errorf("Improperly dropped entry %v", entries[i].body)
		} else if !entries[i].shouldPass && len(rset) == 1 {
			return fmt.Errorf("Improperly passed entry %v", entries[i].body)
		}
	}
	return nil
}
