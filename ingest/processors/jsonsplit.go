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
	"strconv"
	"strings"
	"sync"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/jsonparser"
)

const (
	JsonArraySplitProcessor string = `jsonarraysplit`
)

type JsonArraySplitConfig struct {
	Passthrough_Misses bool //deprecated DO NOT USE
	Drop_Misses        bool
	Extraction         string
	Force_JSON_Object  bool
	Additional_Fields  string
}

func JsonArraySplitLoadConfig(vc *config.VariableConfig) (c JsonArraySplitConfig, err error) {
	//to support legacy config, set Passthrough_Misses to true so that we can catch them setting it to false
	//default is now to send them through
	c.Passthrough_Misses = true
	if err = vc.MapTo(&c); err == nil {
		err = c.validate()
	}
	return
}

func (jasc *JsonArraySplitConfig) validate() (err error) {
	//handle the legacy config items and potential overrides
	// if Drop-Misses is set, that overrides everything
	if !jasc.Drop_Misses {
		if !jasc.Passthrough_Misses {
			jasc.Drop_Misses = true
		}
	}
	var keyname string
	if _, keyname, err = jasc.getKeyData(); err == nil {
		_, _, err = jasc.getAdditionalData(keyname)
	}
	return
}

// getAdditionalData parses Additional-Fields into key paths and output names.
// A name that collides with keyname or an earlier field is skipped so output objects never repeat a key;
// configs with collisions predate this check, so they still load.
func (jasc *JsonArraySplitConfig) getAdditionalData(keyname string) (keys [][]string, names []string, err error) {
	if jasc.Additional_Fields == `` {
		return
	}
	for _, fld := range splitRespectQuotes(jasc.Additional_Fields, commaSplitter) {
		var k []string
		var name string
		if k, name, err = getKeys(fld); err != nil {
			return
		} else if name == keyname || inStringSet(names, name) {
			continue
		}
		keys = append(keys, k)
		names = append(names, name)
	}
	return
}

func (jasc *JsonArraySplitConfig) getKeyData() (key []string, keyname string, err error) {
	if len(jasc.Extraction) == 0 {
		// This is allowed *as long as they don't specify an additional fields*
		if len(jasc.Additional_Fields) == 0 {
			// Set this to ensure we don't mess with the contents
			jasc.Force_JSON_Object = false
			return
		} else {
			err = ErrNoAdditionalFields
			return
		}
	}

	flds := splitRespectQuotes(jasc.Extraction, commaSplitter)
	if len(flds) != 1 {
		err = ErrSingleArraySplitOnly
		return
	}
	key, keyname, err = getKeys(flds[0])
	return
}

type JsonArraySplitter struct {
	nocloser
	mu sync.Mutex
	jsonArraySplitState
}

// jsonArraySplitState is everything derived from the config, so Config() can swap it as one unit.
type jsonArraySplitState struct {
	JsonArraySplitConfig
	key             []string
	keyname         string
	additional      [][]string
	additionalNames []string
}

func NewJsonArraySplitter(cfg JsonArraySplitConfig) (*JsonArraySplitter, error) {
	key, keyname, err := cfg.getKeyData()
	if err != nil {
		return nil, err
	}
	additional, additionalNames, err := cfg.getAdditionalData(keyname)
	if err != nil {
		return nil, err
	} else if cfg.Additional_Fields != `` {
		// additional fields share an object with the array element, even if every one was a skipped collision
		cfg.Force_JSON_Object = true
	}
	return &JsonArraySplitter{jsonArraySplitState: jsonArraySplitState{
		JsonArraySplitConfig: cfg,
		key:                  key,
		keyname:              keyname,
		additional:           additional,
		additionalNames:      additionalNames,
	}}, nil
}

func (je *JsonArraySplitter) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(JsonArraySplitConfig); ok {
		// rebuild so derived state (keys, additional fields, forced objects) tracks the new config
		var nje *JsonArraySplitter
		if nje, err = NewJsonArraySplitter(cfg); err == nil {
			je.mu.Lock()
			je.jsonArraySplitState = nje.jsonArraySplitState
			je.mu.Unlock()
		}
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (je *JsonArraySplitter) Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if len(ents) == 0 {
		return nil, nil
	}
	je.mu.Lock()
	defer je.mu.Unlock()
	var r []*entry.Entry
	for _, ent := range ents {
		if ent == nil {
			continue
		}
		r = append(r, je.processItem(ent)...)
	}
	return r, nil
}

// processItem splits one entry; an entry that cannot be split is a miss and is never partially emitted.
func (je *JsonArraySplitter) processItem(ent *entry.Entry) (rset []*entry.Entry) {
	if !validJson(ent.Data) {
		return je.miss(ent)
	}
	extra, err := je.additionalFields(ent.Data)
	if err != nil {
		return je.miss(ent)
	}
	cb := func(v []byte, dt jsonparser.ValueType, _ int, _ error) {
		// an empty element can only be represented inside an object
		if len(v) == 0 && !je.Force_JSON_Object {
			return
		}
		rset = append(rset, je.genEntry(dt, ent, v, extra))
	}
	if _, err = jsonparser.ArrayEach(ent.Data, cb, je.key...); err != nil {
		return je.miss(ent)
	}
	return
}

// miss applies Drop-Misses to an entry that could not be split.
func (je *JsonArraySplitter) miss(ent *entry.Entry) []*entry.Entry {
	if je.Drop_Misses {
		return nil
	}
	return []*entry.Entry{ent}
}

// additionalFields extracts the additional fields once per entry as `,"name":value` pairs.
func (je *JsonArraySplitter) additionalFields(data []byte) (r []byte, err error) {
	for i, keys := range je.additional {
		v, dt, _, lerr := jsonparser.Get(data, keys...)
		if lerr == jsonparser.KeyPathNotFoundError || (lerr == nil && dt == jsonparser.NotExist) {
			continue
		} else if lerr != nil {
			return nil, lerr
		}
		r = appendField(append(r, ','), je.additionalNames[i], dt, v)
	}
	return
}

func (je *JsonArraySplitter) genEntry(dt jsonparser.ValueType, ent *entry.Entry, v, extra []byte) *entry.Entry {
	r := &entry.Entry{
		Tag:  ent.Tag,
		SRC:  ent.SRC,
		TS:   ent.TS,
		Data: v,
	}
	if je.Force_JSON_Object {
		data := make([]byte, 0, len(je.keyname)+len(v)+len(extra)+7)
		data = appendField(append(data, '{'), je.keyname, dt, v)
		r.Data = append(append(data, extra...), '}')
	}
	r.CopyEnumeratedBlock(ent)
	return r
}

func getKeys(s string) (keys []string, name string, err error) {
	keys = unquoteFields(splitRespectQuotes(s, dotSplitter))
	if len(keys) == 0 {
		err = ErrMissingExtractions
	} else {
		name = keys[len(keys)-1]
	}
	return
}

type isSplitFunc func(v rune) bool

func commaSplitter(v rune) bool {
	return v == ','
}

func dotSplitter(v rune) bool {
	return v == '.'
}

func splitRespectQuotes(src string, isSplit isSplitFunc) []string {
	var ret []string

	for len(src) > 0 {
		var arg string
		var quoteState bool
		var quote rune
		var escape bool
		var haveData bool
		for i, v := range src {
			// we don't actually do escape/unescape stuff here -- we just
			// ignore escaped quotes by tracking escaping
			if escape {
				escape = false
				continue
			} else if !escape && v == '\\' {
				escape = true
				continue
			}

			// we're guaranteed not to be in an escaped state now

			// check quotes
			if quoteState && v == quote {
				// found an unescaped close quote that matches the open quote
				quoteState = false
			} else if quoteState {
				continue
			} else if v == '"' {
				quoteState = true
				quote = v
				continue
			}

			// we're guaranteed not to be in a quoted state now

			if !isSplit(v) {
				haveData = true
				continue
			}

			// it's a split
			if haveData {
				arg = src[:i]
				src = src[i+1:]
				break
			}
		}

		if arg == "" {
			arg = src
			src = ""
		}
		ret = append(ret, strings.TrimFunc(arg, isSplit))
	}

	return ret
}

// unquoteFields removes quotes from all strings in the given slice. Unquoted strings remain intact.
func unquoteFields(s []string) []string {
	var ret []string

	for _, v := range s {
		unquoted, err := strconv.Unquote(v)
		if err == nil {
			ret = append(ret, unquoted)
		} else {
			ret = append(ret, v)
		}
	}

	return ret
}
