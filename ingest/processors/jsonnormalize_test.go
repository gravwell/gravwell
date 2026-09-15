/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestJsonNormalizeLoadConfig(t *testing.T) {
	b := []byte(`
	[global]
	foo = "bar"

	[preprocessor "jn1"]
		type = jsonnormalize
		Max-Depth=4
		Passthrough-Non-JSON=true
	`)
	tc := struct {
		Global struct {
			Foo string
		}
		Preprocessor ProcessorConfig
	}{}
	if err := config.LoadConfigBytes(&tc, b); err != nil {
		t.Fatal(err)
	}
	var tt testTagger
	p, err := tc.Preprocessor.getProcessor(`jn1`, &tt)
	if err != nil {
		t.Fatal(err)
	}
	// non-JSON should pass through because Passthrough-Non-JSON=true
	if rset, err := p.Process(makeEntry([]byte(`not json at all`), 0)); err != nil {
		t.Fatal(err)
	} else if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	} else if string(rset[0].Data) != `not json at all` {
		t.Fatalf("Failed to pass through non-JSON: %v", string(rset[0].Data))
	}
}

func TestJsonNormalizeDefaultDrop(t *testing.T) {
	// Passthrough_Non_JSON defaults to false, so non-JSON entries are dropped
	p, err := NewJsonNormalize(JsonNormalizeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if rset, err := p.Process(makeEntry([]byte(`not json at all`), 0)); err != nil {
		t.Fatal(err)
	} else if len(rset) != 0 {
		t.Fatalf("Expected non-JSON entry to be dropped, got %d results", len(rset))
	}
}

func TestJsonNormalizeEmpty(t *testing.T) {
	p, err := NewJsonNormalize(JsonNormalizeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if rset, err := p.Process(nil); err != nil || len(rset) != 0 {
		t.Fatalf("Expected empty input to yield no results, got %d results, err %v", len(rset), err)
	}
	// empty entry data should just pass through unmodified
	if rset, err := p.Process(makeEntry([]byte(``), 0)); err != nil {
		t.Fatal(err)
	} else if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	} else if len(rset[0].Data) != 0 {
		t.Fatalf("Expected empty data to remain empty, got %v", string(rset[0].Data))
	}
}

func TestJsonNormalizeNestedField(t *testing.T) {
	p, err := NewJsonNormalize(JsonNormalizeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	in := `{"user":"alice","payload":"{\"a\":1,\"b\":[1,2,3]}"}`
	want := map[string]interface{}{
		"user": "alice",
		"payload": map[string]interface{}{
			"a": float64(1),
			"b": []interface{}{float64(1), float64(2), float64(3)},
		},
	}
	rset, err := p.Process(makeEntry([]byte(in), entry.EntryTag(7)))
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	}
	if rset[0].Tag != entry.EntryTag(7) {
		t.Fatalf("Bad result tag: %d != 7", rset[0].Tag)
	}
	checkNormalized(t, rset[0].Data, want)
}

func TestJsonNormalizeWholeEntryQuoted(t *testing.T) {
	p, err := NewJsonNormalize(JsonNormalizeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	// the entire entry is a quoted, escaped JSON document, itself containing
	// another escaped JSON document as a field value
	in := `"{\"a\":1,\"nested\":\"{\\\"c\\\":true}\"}"`
	want := map[string]interface{}{
		"a": float64(1),
		"nested": map[string]interface{}{
			"c": true,
		},
	}
	rset, err := p.Process(makeEntry([]byte(in), 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	}
	checkNormalized(t, rset[0].Data, want)
}

func TestJsonNormalizeMalformedNoOuterQuotes(t *testing.T) {
	p, err := NewJsonNormalize(JsonNormalizeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	// invalid JSON as raw bytes: backslash-escaping was applied but the
	// enclosing quotes were stripped upstream
	in := `{\"a\":\"b\",\"c\":{\"d\":1}}`
	want := map[string]interface{}{
		"a": "b",
		"c": map[string]interface{}{
			"d": float64(1),
		},
	}
	rset, err := p.Process(makeEntry([]byte(in), 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	}
	checkNormalized(t, rset[0].Data, want)
}

func TestJsonNormalizeArrayOfEscapedStrings(t *testing.T) {
	p, err := NewJsonNormalize(JsonNormalizeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	in := `["{\"x\":1}", "{\"y\":2}"]`
	want := []interface{}{
		map[string]interface{}{"x": float64(1)},
		map[string]interface{}{"y": float64(2)},
	}
	rset, err := p.Process(makeEntry([]byte(in), 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	}
	checkNormalized(t, rset[0].Data, want)
}

func TestJsonNormalizeAlreadyNormal(t *testing.T) {
	p, err := NewJsonNormalize(JsonNormalizeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	in := `{"a":1,"b":"c"}`
	want := map[string]interface{}{"a": float64(1), "b": "c"}
	rset, err := p.Process(makeEntry([]byte(in), 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	}
	checkNormalized(t, rset[0].Data, want)
}

func TestJsonNormalizeMaxDepthLimitsRecursion(t *testing.T) {
	// with Max_Depth=1, only the outer document is parsed; the nested
	// escaped string field should be left untouched as a string
	p, err := NewJsonNormalize(JsonNormalizeConfig{Max_Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	in := `{"payload":"{\"a\":1}"}`
	rset, err := p.Process(makeEntry([]byte(in), 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	}
	var got map[string]interface{}
	if err := json.Unmarshal(rset[0].Data, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if _, ok := got["payload"].(string); !ok {
		t.Fatalf("expected payload to remain an unparsed string at depth 1, got %T: %v", got["payload"], got["payload"])
	}
}

func TestJsonNormalizePretty(t *testing.T) {
	p, err := NewJsonNormalize(JsonNormalizeConfig{Pretty: true})
	if err != nil {
		t.Fatal(err)
	}
	rset, err := p.Process(makeEntry([]byte(`{"a":1}`), 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	}
	if string(rset[0].Data) == `{"a":1}` {
		t.Fatalf("expected pretty-printed output, got compact: %v", string(rset[0].Data))
	}
	var got map[string]interface{}
	if err := json.Unmarshal(rset[0].Data, &got); err != nil {
		t.Fatalf("pretty output is not valid JSON: %v", err)
	}
}

// checkNormalized unmarshals got and compares it against want using
// reflect.DeepEqual, so callers can express expectations as Go values
// instead of exact-byte-string JSON (map key order is not guaranteed).
func checkNormalized(t *testing.T, got []byte, want interface{}) {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("output is not valid JSON: %v (data: %s)", err, string(got))
	}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("normalized output mismatch:\n got:  %#v\n want: %#v", v, want)
	}
}
