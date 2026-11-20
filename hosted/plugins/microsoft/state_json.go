/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package microsoft

import (
	"bytes"
	"encoding/json"
	"errors"
	"unicode"
	"unicode/utf8"
)

// These limits apply only to the structural duplicate-member pass over state
// documents. Plugin-written state is far shallower and has far fewer members;
// the limits keep a corrupt document from growing the scanner's stack or a
// per-object name set as a function of the state-store contents.
const (
	maximumStoredJSONDepth         = 64
	maximumStoredJSONNameCache     = 8
	maximumStoredJSONObjectMembers = 1024
)

type storedJSONName struct {
	start, end int
	decoded    string
	normalized bool
}

type storedJSONScanner struct {
	encoded []byte
	offset  int
}

// rejectDuplicateStateMembers rejects repeated names in every JSON object in
// one plugin-owned persisted state document. encoding/json otherwise accepts
// repeated scalar members with last-member-wins semantics, which can make a
// corrupted checkpoint silently choose an ambiguous window or watermark.
//
// The pass walks the encoded bytes without decoding values or materializing
// arrays. Each active object retains its first few member spans in a fixed
// array; only an object wider than that allocates a name set, and both nesting
// and per-object names have explicit ceilings. Escaped and unescaped spellings
// of the same JSON name compare equal. Callers continue to apply their existing
// document, array, element, and aggregate byte bounds before decoding the state
// into production structs.
func rejectDuplicateStateMembers(encoded []byte) error {
	if !json.Valid(encoded) {
		return errors.New("stored Microsoft state is not valid JSON")
	}
	scanner := storedJSONScanner{encoded: encoded}
	if err := scanner.value(0); err != nil {
		return err
	}
	scanner.space()
	if scanner.offset != len(encoded) {
		return errors.New("stored Microsoft state contains trailing data")
	}
	return nil
}

func (scanner *storedJSONScanner) value(depth int) error {
	scanner.space()
	if scanner.offset >= len(scanner.encoded) {
		return errors.New("stored Microsoft state is incomplete")
	}
	switch scanner.encoded[scanner.offset] {
	case '{':
		if depth >= maximumStoredJSONDepth {
			return errors.New("stored Microsoft state exceeds the JSON nesting bound")
		}
		return scanner.object(depth + 1)
	case '[':
		if depth >= maximumStoredJSONDepth {
			return errors.New("stored Microsoft state exceeds the JSON nesting bound")
		}
		return scanner.array(depth + 1)
	case '"':
		_, _, err := scanner.string()
		return err
	default:
		// json.Valid already proved the primitive's grammar. Stop at the first
		// structural delimiter or whitespace; the surrounding parser consumes it.
		for scanner.offset < len(scanner.encoded) {
			switch scanner.encoded[scanner.offset] {
			case ',', ']', '}', ' ', '\t', '\r', '\n':
				return nil
			default:
				scanner.offset++
			}
		}
		return nil
	}
}

func (scanner *storedJSONScanner) object(depth int) error {
	scanner.offset++
	scanner.space()
	if scanner.take('}') {
		return nil
	}

	var names [maximumStoredJSONNameCache]storedJSONName
	nameCount := 0
	var overflow map[string]struct{}
	for {
		if nameCount >= maximumStoredJSONObjectMembers {
			return errors.New("stored Microsoft state exceeds the object-member bound")
		}
		start, end, err := scanner.string()
		if err != nil {
			return err
		}
		candidate := storedJSONName{start: start, end: end}
		if overflow == nil {
			for index := 0; index < nameCount; index++ {
				equal, err := scanner.sameName(&names[index], &candidate)
				if err != nil {
					return err
				}
				if equal {
					return errors.New("stored Microsoft state repeats an object member")
				}
			}
			if nameCount < len(names) {
				names[nameCount] = candidate
			} else {
				overflow = make(map[string]struct{}, maximumStoredJSONNameCache*2)
				for index := range names {
					name, err := scanner.normalizedName(&names[index])
					if err != nil {
						return err
					}
					overflow[foldStoredJSONName(name)] = struct{}{}
				}
				name, err := scanner.normalizedName(&candidate)
				if err != nil {
					return err
				}
				overflow[foldStoredJSONName(name)] = struct{}{}
			}
		} else {
			name, err := scanner.normalizedName(&candidate)
			if err != nil {
				return err
			}
			folded := foldStoredJSONName(name)
			if _, repeated := overflow[folded]; repeated {
				return errors.New("stored Microsoft state repeats an object member")
			}
			overflow[folded] = struct{}{}
		}
		nameCount++

		scanner.space()
		if !scanner.take(':') {
			return errors.New("stored Microsoft state contains an invalid object")
		}
		if err := scanner.value(depth); err != nil {
			return err
		}
		scanner.space()
		if scanner.take('}') {
			return nil
		}
		if !scanner.take(',') {
			return errors.New("stored Microsoft state contains an invalid object")
		}
		scanner.space()
	}
}

func (scanner *storedJSONScanner) array(depth int) error {
	scanner.offset++
	scanner.space()
	if scanner.take(']') {
		return nil
	}
	for {
		if err := scanner.value(depth); err != nil {
			return err
		}
		scanner.space()
		if scanner.take(']') {
			return nil
		}
		if !scanner.take(',') {
			return errors.New("stored Microsoft state contains an invalid array")
		}
		scanner.space()
	}
}

// string returns the content span of one already-validated JSON string.
func (scanner *storedJSONScanner) string() (int, int, error) {
	if !scanner.take('"') {
		return 0, 0, errors.New("stored Microsoft state contains an invalid object member name")
	}
	start := scanner.offset
	for scanner.offset < len(scanner.encoded) {
		switch scanner.encoded[scanner.offset] {
		case '\\':
			scanner.offset += 2
		case '"':
			end := scanner.offset
			scanner.offset++
			return start, end, nil
		default:
			scanner.offset++
		}
	}
	return 0, 0, errors.New("stored Microsoft state contains an unterminated string")
}

func (scanner *storedJSONScanner) sameName(left, right *storedJSONName) (bool, error) {
	leftRaw := scanner.encoded[left.start:left.end]
	rightRaw := scanner.encoded[right.start:right.end]
	if !bytes.ContainsRune(leftRaw, '\\') && !bytes.ContainsRune(rightRaw, '\\') &&
		utf8.Valid(leftRaw) && utf8.Valid(rightRaw) {
		return bytes.EqualFold(leftRaw, rightRaw), nil
	}
	leftName, err := scanner.normalizedName(left)
	if err != nil {
		return false, err
	}
	rightName, err := scanner.normalizedName(right)
	if err != nil {
		return false, err
	}
	return bytes.EqualFold([]byte(leftName), []byte(rightName)), nil
}

func (scanner *storedJSONScanner) normalizedName(name *storedJSONName) (string, error) {
	if name.normalized {
		return name.decoded, nil
	}
	raw := scanner.encoded[name.start:name.end]
	if bytes.ContainsRune(raw, '\\') || !utf8.Valid(raw) {
		var decoded string
		if err := json.Unmarshal(scanner.encoded[name.start-1:name.end+1], &decoded); err != nil {
			return "", errors.New("stored Microsoft state contains an invalid object member name")
		}
		name.decoded = decoded
	} else {
		name.decoded = string(raw)
	}
	name.normalized = true
	return name.decoded, nil
}

// foldStoredJSONName produces the same equivalence classes bytes.EqualFold
// compares and encoding/json v1 uses for its folded struct-field lookup. The
// overflow set needs one stable key for that equivalence; ASCII-only lower or
// upper casing would miss Unicode simple-fold classes such as K, k and the
// Kelvin sign.
func foldStoredJSONName(name string) string {
	folded := make([]byte, 0, len(name))
	for len(name) != 0 {
		r, size := utf8.DecodeRuneInString(name)
		folded = utf8.AppendRune(folded, foldStoredJSONRune(r))
		name = name[size:]
	}
	return string(folded)
}

func foldStoredJSONRune(r rune) rune {
	for {
		next := unicode.SimpleFold(r)
		if next <= r {
			return next
		}
		r = next
	}
}

func (scanner *storedJSONScanner) space() {
	for scanner.offset < len(scanner.encoded) {
		switch scanner.encoded[scanner.offset] {
		case ' ', '\t', '\r', '\n':
			scanner.offset++
		default:
			return
		}
	}
}

func (scanner *storedJSONScanner) take(want byte) bool {
	if scanner.offset >= len(scanner.encoded) || scanner.encoded[scanner.offset] != want {
		return false
	}
	scanner.offset++
	return true
}
