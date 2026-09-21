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
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	JsonNormalizeProcessor string = `jsonnormalize`

	defaultJsonNormalizeMaxDepth uint = 8

	// maxStructuralDepth caps how many levels of plain (already-decoded)
	// object/array nesting normalizeValue will walk into for a single
	// value, independent of Max_Depth. Max_Depth budgets escaping
	// *layers* -- how many times a string is re-parsed as embedded JSON
	// -- not the structural nesting of an already-valid document, which
	// costs nothing against that budget (see normalizeValue). This
	// separate, generous cap exists purely to bound recursion against
	// pathological inputs (e.g. megabytes of "[[[[...]]]]") and avoid a
	// stack overflow; it is not user-configurable.
	maxStructuralDepth int = 1000
)

var (
	// ErrNotJSON is returned when an entry cannot be coerced into valid JSON,
	// even after attempting to repair common escaping problems, or when it
	// decodes but contains invalid UTF-8.
	ErrNotJSON = errors.New("Input does not appear to be JSON, even after unescaping")
)

// JsonNormalizeConfig controls the behavior of the JsonNormalize preprocessor.
//
// The processor handles two distinct flavors of "heavily escaped" JSON:
//
//  1. Whole-record escaping, where an entire JSON document has been
//     serialized as a string one or more times, e.g. an entry whose data is
//     literally `"{\"a\":1}"` instead of `{"a":1}`, or, in the case where an
//     upstream system stripped the enclosing quotes, the invalid fragment
//     `{\"a\":1}`.
//  2. Field-level escaping, where an otherwise well-formed JSON document
//     contains a field whose value is itself a JSON-encoded string, e.g.
//     `{"user":"alice","payload":"{\"a\":1}"}`. This is common when a
//     logging pipeline serializes a sub-object independently before
//     embedding it in a parent document (nested marshal/unmarshal, or
//     copy-pasting a JSON blob into a string field).
//
// JsonNormalize repairs both cases and re-emits a single, clean JSON
// document with all string-encoded JSON inlined as real objects/arrays.
type JsonNormalizeConfig struct {
	// Max_Depth bounds how many layers of string-escaping the processor
	// will unwind: both when repairing a malformed, over-escaped document
	// (Step 1) and when recursively inlining JSON-encoded strings found as
	// field values (Step 2). A value of 0 means "use the default" (8).
	//
	// This budgets escaping layers, not structural nesting -- walking down
	// through an already-valid document's plain objects/arrays costs
	// nothing against it. A CloudTrail record nested 20 objects deep with a
	// single escaped field at the bottom is handled the same as one nested
	// 2 objects deep, as long as that field is only escaped once.
	Max_Depth uint

	// Passthrough_Non_JSON controls what happens to entries that are not
	// valid JSON and cannot be repaired into valid JSON by unescaping, or
	// that decode but contain invalid UTF-8. If true, the entry is passed
	// through unmodified. If false (the default), the entry is dropped,
	// mirroring the Passthrough_Non_Gzip behavior of the gzip processor.
	Passthrough_Non_JSON bool

	// Pretty, if true, indents the normalized JSON output for readability.
	// By default the output is compact, single-line JSON.
	Pretty bool
}

// JsonNormalizeLoadConfig decodes a JsonNormalizeConfig out of a VariableConfig.
func JsonNormalizeLoadConfig(vc *config.VariableConfig) (c JsonNormalizeConfig, err error) {
	err = vc.MapTo(&c)
	return
}

// JsonNormalize is a Processor that repairs over-escaped JSON entries and
// inlines JSON-encoded string fields, producing a single normalized JSON
// document per entry. It carries no per-entry state.
type JsonNormalize struct {
	nocloser
	JsonNormalizeConfig
	maxDepth uint
}

// NewJsonNormalize instantiates a JsonNormalize preprocessor.
func NewJsonNormalize(cfg JsonNormalizeConfig) (*JsonNormalize, error) {
	jn := &JsonNormalize{
		JsonNormalizeConfig: cfg,
	}
	jn.setDepth()
	return jn, nil
}

func (jn *JsonNormalize) setDepth() {
	if jn.Max_Depth == 0 {
		jn.maxDepth = defaultJsonNormalizeMaxDepth
	} else {
		jn.maxDepth = jn.Max_Depth
	}
}

func (jn *JsonNormalize) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(JsonNormalizeConfig); ok {
		jn.JsonNormalizeConfig = cfg
		jn.setDepth()
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type %T", v)
	}
	return
}

func (jn *JsonNormalize) Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if len(ents) == 0 {
		return nil, nil
	}
	rset := ents[:0]
	for _, ent := range ents {
		if ent == nil {
			continue
		}
		out, err := jn.processItem(ent)
		if err != nil {
			if err == ErrNotJSON && jn.Passthrough_Non_JSON {
				rset = append(rset, ent)
			}
			// otherwise drop the entry
			continue
		}
		if out != nil {
			rset = append(rset, out)
		}
	}
	return rset, nil
}

func (jn *JsonNormalize) processItem(ent *entry.Entry) (rset *entry.Entry, err error) {
	if ent == nil {
		return
	}
	data := bytes.TrimSpace(ent.Data)
	if len(data) == 0 {
		rset = ent
		return
	}

	// Step 1: repair documents that are not valid JSON as-is because a
	// layer of string-escaping was applied without (or in addition to) the
	// enclosing quotes that would make them valid JSON string literals.
	// We progressively strip one layer of escaping at a time until the
	// result parses as valid JSON, or we exhaust our depth budget. depth
	// is declared outside the loop so the layers consumed here can be
	// deducted from the budget passed to Step 2 below -- Max_Depth bounds
	// escaping layers unwound across the whole record, not per step.
	var depth uint
	for ; !json.Valid(data) && depth < jn.maxDepth; depth++ {
		unescaped, ok := unescapeOnce(data)
		if !ok {
			break
		}
		data = unescaped
	}
	if !json.Valid(data) {
		err = ErrNotJSON
		return
	}

	// json.Valid only checks structural well-formedness; it does not
	// guarantee the string content is valid UTF-8. Decoding a string value
	// containing invalid UTF-8 silently replaces the bad bytes with U+FFFD
	// rather than erroring, which would corrupt latin-1/cp1252 payloads
	// on the happy path. Reject explicitly instead, so the existing
	// Passthrough_Non_JSON/drop behavior governs this case too.
	if !utf8.Valid(data) {
		err = ErrNotJSON
		return
	}

	// Step 2: decode generically and recursively inline any string value
	// that is itself JSON-encoded (an object, array, or another encoded
	// string), up to maxDepth escaping layers of nesting. This handles the
	// common case of a well-formed document with one or more fields whose
	// values are themselves JSON serialized as strings.
	v, derr := decodeJSON(data)
	if derr != nil {
		err = ErrNotJSON
		return
	}
	v = normalizeValue(v, jn.maxDepth-depth)

	out, merr := marshalJSON(v, jn.Pretty)
	if merr != nil {
		err = ErrNotJSON
		return
	}

	rset = &entry.Entry{
		Tag:  ent.Tag,
		SRC:  ent.SRC,
		TS:   ent.TS,
		Data: out,
	}
	// Preserve any enumerated values (e.g. attached upstream by the attach
	// processor) rather than silently dropping them on the new entry.
	rset.CopyEnumeratedBlock(ent)
	return
}

// decodeJSON decodes data into a generic interface{} tree, preserving the
// exact textual representation of numbers via json.Number rather than
// coercing everything to float64. float64 only has ~15-17 significant
// decimal digits of precision, so a naive json.Unmarshal into interface{}
// silently corrupts 19-digit values like Snowflake IDs, Windows
// EventRecordIDs, or epoch-nanosecond timestamps
// (1234567890123456789 -> 1234567890123456800).
func decodeJSON(data []byte) (v interface{}, err error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	err = dec.Decode(&v)
	return
}

// marshalJSON serializes v back to JSON without HTML-escaping. The standard
// json.Marshal escapes <, >, and & (e.g. "?a=1&b=2" becomes
// "?a=1\u0026b=2"), which breaks downstream regex/grok extraction that
// expects the literal characters. json.Encoder.Encode always appends a
// trailing newline, which is trimmed to match json.Marshal's output shape.
func marshalJSON(v interface{}, pretty bool) ([]byte, error) {
	bb := bytes.NewBuffer(nil)
	enc := json.NewEncoder(bb)
	enc.SetEscapeHTML(false)
	if pretty {
		enc.SetIndent(``, `  `)
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(bb.Bytes(), []byte("\n")), nil
}

// unescapeOnce attempts to remove a single layer of backslash-escaping from
// data by stripping \" and \\ sequences wherever they occur in the raw
// bytes. This repairs the common case where a JSON object was serialized
// to a string and the enclosing quotes were lost somewhere upstream,
// leaving invalid JSON like {\"a\":1} instead of either a valid {"a":1} or
// a valid quoted literal "{\"a\":1}".
//
// It returns ok=false if the input contains no such escape sequences,
// signaling that no further progress can be made.
//
// Note: this is only ever called on data that has already failed
// json.Valid (see the Step 1 loop in processItem). A properly quoted JSON
// string literal is itself valid JSON and would never reach this function;
// that case is instead handled naturally in Step 2, where normalizeValue
// re-parses decoded string values that look like embedded JSON -- so no
// separate "strict" unquoting path is needed here.
func unescapeOnce(data []byte) ([]byte, bool) {
	if !bytes.Contains(data, []byte(`\"`)) && !bytes.Contains(data, []byte(`\\`)) {
		return nil, false
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if data[i] == '\\' && i+1 < len(data) && (data[i+1] == '"' || data[i+1] == '\\') {
			out = append(out, data[i+1])
			i++
			continue
		}
		out = append(out, data[i])
	}
	if bytes.Equal(out, data) {
		return nil, false
	}
	return out, true
}

// normalizeValue walks a decoded JSON value looking for strings that are
// themselves JSON-encoded objects or arrays, and inlines them in place.
// This is what turns a field like:
//
//	"payload": "{\"user\":\"alice\",\"count\":3}"
//
// into:
//
//	"payload": {"user":"alice","count":3}
//
// escapeDepth bounds how many layers of string-escaping will be unwound;
// it is only consumed when a string value is successfully re-parsed as
// embedded JSON (an actual escaping layer). Recursing into the children of
// an already-decoded map or array is plain structural traversal, not an
// escaping layer, and does not consume escapeDepth -- otherwise a document
// that is simply deeply *nested* (not deeply *escaped*), such as CloudTrail,
// Kubernetes audit, or Windows event JSON routinely is, would exhaust the
// budget just walking down to an escaped field and come back byte-identical
// with no error or indication that nothing happened. Structural recursion
// is instead bounded by the separate, generous maxStructuralDepth constant
// purely to guard against stack overflow on pathological input.
func normalizeValue(v interface{}, escapeDepth uint) interface{} {
	return normalizeValueDepth(v, escapeDepth, maxStructuralDepth)
}

func normalizeValueDepth(v interface{}, escapeDepth uint, structDepth int) interface{} {
	if structDepth <= 0 {
		return v
	}
	switch t := v.(type) {
	case string:
		if escapeDepth == 0 {
			return t
		}
		trimmed := bytes.TrimSpace([]byte(t))
		if len(trimmed) < 2 {
			return t
		}
		// Only attempt to re-parse strings that look like they could be a
		// JSON object or array. This avoids wasting cycles trying to parse
		// ordinary text values, and avoids surprising conversions -- a
		// literal string that merely starts and ends with a quote (e.g. a
		// quoted phrase in a log message) is indistinguishable from a
		// doubly-encoded JSON string, so it is left alone rather than
		// having its quotes silently stripped.
		switch trimmed[0] {
		case '{', '[':
		default:
			return t
		}
		nested, err := decodeJSON(trimmed)
		if err != nil {
			return t
		}
		// Unwrapping a layer of string-encoding consumes one unit of the
		// escaping budget. The freshly unwrapped value gets a fresh
		// structural-depth allowance, since maxStructuralDepth bounds a
		// single value's nesting, not the cumulative walk across unwraps.
		return normalizeValueDepth(nested, escapeDepth-1, maxStructuralDepth)
	case map[string]interface{}:
		for k, val := range t {
			t[k] = normalizeValueDepth(val, escapeDepth, structDepth-1)
		}
		return t
	case []interface{}:
		for i, val := range t {
			t[i] = normalizeValueDepth(val, escapeDepth, structDepth-1)
		}
		return t
	default:
		return v
	}
}

