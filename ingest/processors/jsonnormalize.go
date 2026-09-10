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

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	JsonNormalizeProcessor string = `jsonnormalize`

	defaultJsonNormalizeMaxDepth uint = 8
)

var (
	// ErrNotJSON is returned when an entry cannot be coerced into valid JSON,
	// even after attempting to repair common escaping problems.
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
	// Max_Depth bounds how many layers of escaping/nesting the processor
	// will unwind, both when repairing a malformed, over-escaped document
	// and when recursively inlining JSON-encoded strings found as field
	// values. A value of 0 means "use the default" (8). This bound exists
	// primarily to protect against pathological or adversarial input that
	// nests indefinitely.
	Max_Depth uint

	// Passthrough_Non_JSON controls what happens to entries that are not
	// valid JSON and cannot be repaired into valid JSON by unescaping. If
	// true, the entry is passed through unmodified. If false (the default),
	// the entry is dropped, mirroring the Passthrough_Non_Gzip behavior of
	// the gzip processor.
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
	// result parses as valid JSON, or we exhaust our depth budget.
	for depth := uint(0); !json.Valid(data) && depth < jn.maxDepth; depth++ {
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

	// Step 2: decode generically and recursively inline any string value
	// that is itself JSON-encoded (an object, array, or another encoded
	// string), up to maxDepth levels of nesting. This handles the common
	// case of a well-formed document with one or more fields whose values
	// are themselves JSON serialized as strings.
	var v interface{}
	if err = json.Unmarshal(data, &v); err != nil {
		err = ErrNotJSON
		return
	}
	v = normalizeValue(v, jn.maxDepth)

	var out []byte
	if jn.Pretty {
		out, err = json.MarshalIndent(v, ``, `  `)
	} else {
		out, err = json.Marshal(v)
	}
	if err != nil {
		err = ErrNotJSON
		return
	}

	rset = &entry.Entry{
		Tag:  ent.Tag,
		SRC:  ent.SRC,
		TS:   ent.TS,
		Data: out,
	}
	return
}

// unescapeOnce attempts to remove a single layer of JSON string-escaping
// from data.
//
// It first tries the strict path: if data is a properly quoted JSON string
// literal (e.g. `"{\"a\":1}"`), json.Unmarshal is used to unquote it
// correctly, handling all valid JSON escape sequences (\n, \t, \uXXXX, etc).
//
// If that fails -- commonly because an upstream system stripped the
// enclosing quotes but left the internal backslash-escaping behind, e.g.
// `{\"a\":1}` -- it falls back to a byte-level unescape of \" and \\
// sequences.
//
// It returns ok=false if neither approach changes the input, signaling that
// no further progress can be made and the remaining bytes are not
// recoverable JSON.
func unescapeOnce(data []byte) ([]byte, bool) {
	// Strict path: data is a properly quoted JSON string literal.
	if len(data) >= 2 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err == nil {
			return []byte(s), true
		}
	}

	// Heuristic path: strip one layer of backslash-escaping from quotes and
	// backslashes wherever they occur in the raw bytes. This repairs
	// documents where a JSON object was serialized to a string and the
	// enclosing quotes were lost somewhere upstream.
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
// themselves JSON-encoded objects, arrays, or (possibly further-escaped)
// strings, and inlines them in place. This is what turns a field like:
//
//	"payload": "{\"user\":\"alice\",\"count\":3}"
//
// into:
//
//	"payload": {"user":"alice","count":3}
//
// Recursion is bounded by depth to avoid runaway work on adversarial or
// unexpectedly deep input; once the budget is exhausted, remaining values
// are left as-is.
func normalizeValue(v interface{}, depth uint) interface{} {
	if depth == 0 {
		return v
	}
	switch t := v.(type) {
	case string:
		trimmed := bytes.TrimSpace([]byte(t))
		if len(trimmed) < 2 {
			return t
		}
		// Only attempt to re-parse strings that look like they could be a
		// JSON object, array, or another quoted JSON string. This avoids
		// wasting cycles trying to parse ordinary text values, and avoids
		// surprising conversions of things like "123" or "true" into
		// numbers/booleans.
		switch trimmed[0] {
		case '{', '[', '"':
		default:
			return t
		}
		var nested interface{}
		if err := json.Unmarshal(trimmed, &nested); err != nil {
			return t
		}
		return normalizeValue(nested, depth-1)
	case map[string]interface{}:
		for k, val := range t {
			t[k] = normalizeValue(val, depth-1)
		}
		return t
	case []interface{}:
		for i, val := range t {
			t[i] = normalizeValue(val, depth-1)
		}
		return t
	default:
		return v
	}
}
