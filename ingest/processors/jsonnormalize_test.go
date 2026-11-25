/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestJsonNormalizeMaxDepthLimitsEscapingLayers(t *testing.T) {
	// Max_Depth budgets escaping *layers*, not structural nesting. A single
	// escaped field costs exactly one unit of that budget regardless of how
	// deeply it is structurally nested, so Max_Depth=1 is enough to fully
	// unwrap one escaped field...
	p, err := NewJsonNormalize(JsonNormalizeConfig{Max_Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	in := `{"payload":"{\"a\":1}"}`
	want := map[string]interface{}{
		"payload": map[string]interface{}{"a": float64(1)},
	}
	rset, err := p.Process(makeEntry([]byte(in), 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	}
	checkNormalized(t, rset[0].Data, want)

	// ...but is NOT enough to unwrap a doubly-escaped field: the outer
	// layer consumes the entire depth=1 budget, leaving the inner escaped
	// string untouched.
	in2 := `{"payload":"{\"inner\":\"{\\\"a\\\":1}\"}"}`
	rset2, err := p.Process(makeEntry([]byte(in2), 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rset2) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset2))
	}
	var got2 map[string]interface{}
	if err := json.Unmarshal(rset2[0].Data, &got2); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	payload, ok := got2["payload"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected payload to be unwrapped one layer, got %T: %v", got2["payload"], got2["payload"])
	}
	if _, ok := payload["inner"].(string); !ok {
		t.Fatalf("expected inner to remain an unparsed string at depth 1, got %T: %v", payload["inner"], payload["inner"])
	}
}

func TestJsonNormalizeStructuralNestingIsFree(t *testing.T) {
	// Regression test: structural nesting of an already-valid document must
	// NOT consume the Max_Depth (escaping-layer) budget. Build a document
	// nested well past the default Max_Depth (8) with a single escaped
	// field at the bottom, and confirm it still gets unwrapped.
	p, err := NewJsonNormalize(JsonNormalizeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	nestLevels := 20
	in := `{"a":1}`
	for i := 0; i < nestLevels; i++ {
		in = fmt.Sprintf(`{"level%d":%s}`, i, in)
	}
	// bury one escaped field at the very bottom, next to the structural nesting
	in = fmt.Sprintf(`{"outer":%s,"escaped":"{\"x\":1}"}`, in)

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
	escaped, ok := got["escaped"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected deeply-nested document to still unwrap its escaped field, got %T: %v", got["escaped"], got["escaped"])
	}
	if escaped["x"] != float64(1) {
		t.Fatalf("unexpected escaped field contents: %v", escaped)
	}
	// and confirm the structural nesting itself survived untouched
	cur := got["outer"]
	for i := nestLevels - 1; i >= 0; i-- {
		m, ok := cur.(map[string]interface{})
		if !ok {
			t.Fatalf("structural nesting was corrupted at level%d: %T %v", i, cur, cur)
		}
		cur = m[fmt.Sprintf("level%d", i)]
	}
	if m, ok := cur.(map[string]interface{}); !ok || m["a"] != float64(1) {
		t.Fatalf("innermost structural value was corrupted: %v", cur)
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

func TestJsonNormalizePreservesNumberPrecision(t *testing.T) {
	// float64 only carries ~15-17 significant decimal digits; naively
	// decoding into interface{} corrupts 19-digit IDs like Snowflake IDs,
	// EventRecordIDs, or epoch-nanosecond timestamps. Confirm the exact
	// digit sequence survives round-trip, both for a top-level number and
	// for one buried inside an escaped/nested field.
	p, err := NewJsonNormalize(JsonNormalizeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	in := `{"id":1234567890123456789,"nested":"{\"eventRecordID\":9223372036854775807}"}`
	rset, err := p.Process(makeEntry([]byte(in), 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	}
	out := string(rset[0].Data)
	if !bytes.Contains(rset[0].Data, []byte(`"id":1234567890123456789`)) {
		t.Fatalf("top-level 19-digit id was corrupted: %s", out)
	}
	if !bytes.Contains(rset[0].Data, []byte(`"eventRecordID":9223372036854775807`)) {
		t.Fatalf("nested 19-digit id was corrupted: %s", out)
	}
}

func TestJsonNormalizeDoesNotHTMLEscape(t *testing.T) {
	// json.Marshal HTML-escapes <, >, and & by default (e.g. "&" becomes
	// "\u0026"), which breaks downstream regex/grok extraction expecting
	// the literal characters. Confirm they come through untouched.
	p, err := NewJsonNormalize(JsonNormalizeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	in := `{"query":"?a=1&b=2","html":"<b>hi</b>"}`
	rset, err := p.Process(makeEntry([]byte(in), 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	}
	out := string(rset[0].Data)
	if bytes.Contains(rset[0].Data, []byte(`\u0026`)) || bytes.Contains(rset[0].Data, []byte(`\u003c`)) {
		t.Fatalf("output was HTML-escaped: %s", out)
	}
	if !bytes.Contains(rset[0].Data, []byte(`?a=1&b=2`)) || !bytes.Contains(rset[0].Data, []byte(`<b>hi</b>`)) {
		t.Fatalf("expected literal characters to survive, got: %s", out)
	}
}

func TestJsonNormalizeRejectsInvalidUTF8(t *testing.T) {
	// A raw 0xFF byte inside a string value is structurally valid JSON
	// (json.Valid doesn't check UTF-8 validity) but is not valid UTF-8.
	// Decoding it would silently substitute U+FFFD; instead this should be
	// treated like any other unrecoverable entry, governed by
	// Passthrough_Non_JSON.
	bad := append([]byte(`{"a":"`), 0xff, '"', '}')

	// default: dropped
	p, err := NewJsonNormalize(JsonNormalizeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if rset, err := p.Process(makeEntry(bad, 0)); err != nil {
		t.Fatal(err)
	} else if len(rset) != 0 {
		t.Fatalf("expected invalid UTF-8 entry to be dropped, got %d results: %v", len(rset), rset)
	}

	// with passthrough enabled: passed through unmodified, not corrupted
	p2, err := NewJsonNormalize(JsonNormalizeConfig{Passthrough_Non_JSON: true})
	if err != nil {
		t.Fatal(err)
	}
	rset, err := p2.Process(makeEntry(bad, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	}
	if !bytes.Equal(rset[0].Data, bad) {
		t.Fatalf("expected passthrough to preserve original bytes exactly, got %v != %v", rset[0].Data, bad)
	}
}

func TestJsonNormalizePreservesEnumeratedValues(t *testing.T) {
	// Enumerated values attached by an upstream processor (e.g. attach)
	// must survive onto the new entry this processor builds, not be
	// silently dropped.
	p, err := NewJsonNormalize(JsonNormalizeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ents := makeEntry([]byte(`{"a":1}`), 0)
	if err := ents[0].AddEnumeratedValueEx(`custom`, `customvalue`); err != nil {
		t.Fatal(err)
	}
	rset, err := p.Process(ents)
	if err != nil {
		t.Fatal(err)
	}
	if len(rset) != 1 {
		t.Fatalf("Invalid result count: %d", len(rset))
	}
	if val, ok := rset[0].GetEnumeratedValue(`custom`); !ok {
		t.Fatalf("expected enumerated value 'custom' to survive normalization")
	} else if val != `customvalue` {
		t.Fatalf("enumerated value corrupted: %v", val)
	}
	// the EV attached by makeEntry itself should also have survived
	if val, ok := rset[0].GetEnumeratedValue(`testing`); !ok {
		t.Fatalf("expected enumerated value 'testing' to survive normalization")
	} else if val != `testvalue` {
		t.Fatalf("enumerated value corrupted: %v", val)
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

