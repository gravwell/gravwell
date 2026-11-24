/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
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
	"io"
	"sync"

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/jsonparser"
)

const (
	JsonExtractProcessor string = `jsonextract`
)

var (
	ErrMissStrictConflict   = errors.New("Strict-Extraction requires Drop-Misses=true")
	ErrMissingExtractions   = errors.New("Extractions specifications missing")
	ErrNoAdditionalFields   = errors.New("Additional-Fields cannot be set if Extractions parameter is unset")
	ErrInvalidExtractions   = errors.New("Invalid Extractions")
	ErrInvalidKeyname       = errors.New("Invalid keyname")
	ErrDuplicateKey         = errors.New("Duplicate extraction key")
	ErrDuplicateKeyname     = errors.New("Duplicate keys")
	ErrSingleArraySplitOnly = errors.New("jsonarraysplit only supports a single extraction")
)

type JsonExtractConfig struct {
	Passthrough_Misses bool //deprecated DO NOT USE
	Drop_Misses        bool
	Strict_Extraction  bool
	Force_JSON_Object  bool
	Extractions        string
}

func JsonExtractLoadConfig(vc *config.VariableConfig) (c JsonExtractConfig, err error) {
	//to support legacy config, set Passthrough_Misses to true so that we can catch them setting it to false
	//default is now to send them through
	c.Passthrough_Misses = true
	if err = vc.MapTo(&c); err == nil {
		err = c.validate()
	}
	return
}

func (c *JsonExtractConfig) validate() (err error) {
	//handle the legacy config items and potential overrides
	// if Drop-Misses is set, that overrides everything
	if !c.Drop_Misses {
		if !c.Passthrough_Misses {
			c.Drop_Misses = true
		}
	}
	_, _, err = c.getKeyData()
	return
}

type JsonExtractor struct {
	nocloser
	mu sync.Mutex
	jsonExtractState
}

// jsonExtractState is everything derived from the config, so Config() can swap it as one unit.
type jsonExtractState struct {
	JsonExtractConfig
	bldr builder
}

func NewJsonExtractor(cfg JsonExtractConfig) (*JsonExtractor, error) {
	if !cfg.Drop_Misses && cfg.Strict_Extraction {
		return nil, ErrMissStrictConflict
	}
	keys, keynames, err := cfg.getKeyData()
	if err != nil {
		return nil, err
	}
	return &JsonExtractor{jsonExtractState: jsonExtractState{
		JsonExtractConfig: cfg,
		bldr: builder{
			forceJson: cfg.Force_JSON_Object,
			bb:        bytes.NewBuffer(nil),
			keys:      keys,
			keynames:  keynames,
		},
	}}, nil
}

func (je *JsonExtractor) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(JsonExtractConfig); ok {
		// rebuild so the builder tracks the new extractions
		var nje *JsonExtractor
		if nje, err = NewJsonExtractor(cfg); err == nil {
			je.mu.Lock()
			je.jsonExtractState = nje.jsonExtractState
			je.mu.Unlock()
		}
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (je *JsonExtractor) Process(ents []*entry.Entry) (rset []*entry.Entry, err error) {
	if len(ents) == 0 {
		return
	}
	je.mu.Lock()
	defer je.mu.Unlock()
	rset = ents[:0]
	for _, ent := range ents {
		if ent == nil {
			continue
		} else if ent = je.processItem(ent); ent != nil {
			rset = append(rset, ent)
		}
	}
	return
}

func (je *JsonExtractor) processItem(ent *entry.Entry) *entry.Entry {
	// if we have drop misses or strict extraction, validate the JSON first
	if (je.Drop_Misses || je.Strict_Extraction) && !validJson(ent.Data) {
		return nil
	}
	if err := je.bldr.extract(ent.Data); err != nil {
		if !je.Drop_Misses {
			return ent
		}
		return nil
	}
	data, cnt := je.bldr.render()
	if je.Strict_Extraction && cnt != len(je.bldr.keynames) {
		return nil //just dropping the entry
	} else if cnt == 0 && !je.Drop_Misses {
		return ent
	} else if len(data) > 0 {
		ent.Data = data
	}
	return ent
}

func (c JsonExtractConfig) getKeyData() (keys [][]string, keynames []string, err error) {
	if len(c.Extractions) == 0 {
		err = ErrMissingExtractions
		return
	}
	flds := splitRespectQuotes(c.Extractions, commaSplitter)
	for _, key := range flds {
		var keyname string
		if len(key) == 0 {
			continue
		}

		bits := unquoteFields(splitRespectQuotes(key, dotSplitter))

		if keyname = bits[len(bits)-1]; len(keyname) == 0 {
			err = ErrInvalidKeyname
			return
		}
		if inStringSliceSet(keys, bits) {
			err = ErrDuplicateKey
			return
		} else if inStringSet(keynames, keyname) {
			err = ErrDuplicateKeyname
			return
		}
		keys = append(keys, bits)
		keynames = append(keynames, keyname)
	}
	if len(keys) == 0 || len(keynames) == 0 || len(keys) != len(keynames) {
		err = ErrInvalidExtractions
	}
	return
}

func inStringSliceSet(set [][]string, key []string) bool {
	for _, x := range set {
		if len(x) != len(key) {
			continue
		}
		var miss bool
		for i := range x {
			if key[i] != x[i] {
				miss = true
				break
			}
		}
		if !miss {
			return true // it matched
		}
	}
	return false
}

func inStringSet(set []string, key string) bool {
	for _, x := range set {
		if x == key {
			return true
		}
	}
	return false
}

type builder struct {
	comma     bool
	forceJson bool
	cnt       int
	bb        *bytes.Buffer
	keys      [][]string
	keynames  []string
}

// objectMode is true once add() has opened an object, so it always gets closed.
func (b *builder) objectMode() bool {
	return len(b.keys) > 1 || b.forceJson || b.comma
}

// extract resets the builder on failure so no partial state survives.
func (b *builder) extract(data []byte) (err error) {
	if b.objectMode() {
		err = b.extractJson(data)
	} else {
		err = b.extractObject(data)
	}
	if err != nil {
		b.reset()
	}
	return
}

// Extracting a single field and not rewrapping in JSON with a key name
func (b *builder) extractObject(data []byte) error {
	v, _, _, err := jsonparser.Get(data, b.keys[0]...)
	if err != nil {
		if err == jsonparser.KeyPathNotFoundError {
			err = nil
		}
	} else {
		//got a successful extraction
		b.cnt++
		b.bb.Write(v)
	}
	return err
}

// extract and handle data as a JSON object
func (b *builder) extractJson(data []byte) error {
	for i, keys := range b.keys {
		v, dt, _, err := jsonparser.Get(data, keys...)
		if err != nil {
			if err == jsonparser.KeyPathNotFoundError {
				continue
			}
			return err
		}
		if dt != jsonparser.NotExist {
			b.add(b.keynames[i], dt, v)
		}
	}
	return nil
}

func (b *builder) add(key string, dt jsonparser.ValueType, v []byte) {
	if !b.comma {
		b.bb.WriteString("{")
	} else {
		b.bb.WriteString(",")
	}
	b.bb.Write(appendField(b.bb.AvailableBuffer(), key, dt, v))
	b.cnt++
	b.comma = true
}

func (b *builder) render() (r []byte, cnt int) {
	if b.cnt == 0 {
		return
	}
	if b.comma {
		io.WriteString(b.bb, "}")
	}
	r = append([]byte{}, b.bb.Bytes()...) //force allocation
	cnt = b.cnt
	b.reset()
	return
}

func (b *builder) reset() {
	b.bb.Reset()
	b.comma = false
	b.cnt = 0
}

// appendField appends `"key":value` to b, escaping the key and quoting string values.
func appendField(b []byte, key string, dt jsonparser.ValueType, v []byte) []byte {
	b = append(appendJSONString(b, key), ':')
	if dt == jsonparser.String {
		return append(append(append(b, '"'), v...), '"')
	}
	return append(b, v...)
}

// appendJSONString appends s to b as a quoted JSON string.
func appendJSONString(b []byte, s string) []byte {
	const hex = `0123456789abcdef`
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\\':
			b = append(b, '\\', c)
		case c < 0x20:
			b = append(b, `\u00`...)
			b = append(b, hex[c>>4], hex[c&0xf])
		default:
			b = append(b, c)
		}
	}
	return append(b, '"')
}

// validJson reports whether data is a single valid JSON document, ignoring trailing NUL padding.
func validJson(data []byte) bool {
	return json.Valid(bytes.TrimRight(data, "\x00"))
}
