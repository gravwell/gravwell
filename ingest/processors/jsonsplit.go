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
	"fmt"
	"strconv"
	"strings"

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
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
	if jasc.Drop_Misses == false {
		if jasc.Passthrough_Misses == false {
			jasc.Drop_Misses = true
		}
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
	JsonArraySplitConfig
	key        []string
	keyname    string
	useBuilder bool
	bldr       builder
}

func NewJsonArraySplitter(cfg JsonArraySplitConfig) (*JsonArraySplitter, error) {
	key, keyname, err := cfg.getKeyData()
	if err != nil {
		return nil, err
	}
	var bldr builder
	var useBuilder bool
	if cfg.Additional_Fields != `` {
		flds := splitRespectQuotes(cfg.Additional_Fields, commaSplitter)
		var additional [][]string
		var additionalNames []string
		for _, fld := range flds {
			var keys []string
			var name string
			if keys, name, err = getKeys(fld); err != nil {
				return nil, err
			}
			additional = append(additional, keys)
			additionalNames = append(additionalNames, name)
		}
		bldr = builder{
			forceJson: cfg.Force_JSON_Object,
			bb:        bytes.NewBuffer(nil),
			keys:      additional,
			keynames:  additionalNames,
		}
		useBuilder = true
	}
	return &JsonArraySplitter{
		JsonArraySplitConfig: cfg,
		key:                  key,
		keyname:              keyname,
		bldr:                 bldr,
		useBuilder:           useBuilder,
	}, nil
}

func (j *JsonArraySplitter) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(JsonArraySplitConfig); ok {
		j.JsonArraySplitConfig = cfg
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (je *JsonArraySplitter) Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if len(ents) == 0 {
		return nil, nil
	}
	var r []*entry.Entry
	for _, ent := range ents {
		if ent == nil {
			continue
		}
		if set, err := je.processItem(ent); err != nil {
			continue
		} else if len(set) > 0 {
			r = append(r, set...)
		}
	}
	return r, nil
}

func (je *JsonArraySplitter) processItem(ent *entry.Entry) (rset []*entry.Entry, err error) {
	cb := func(v []byte, dt jsonparser.ValueType, off int, lerr error) {
		if len(v) == 0 || lerr != nil {
			return
		}
		if je.useBuilder {
			//manually add our array value
			je.bldr.add(je.keyname, dt, v)
			if err = je.bldr.extract(ent.Data); err != nil {
				return
			}
			if data, cnt := je.bldr.render(); cnt > 0 {
				tent := &entry.Entry{
					Tag:  ent.Tag,
					SRC:  ent.SRC,
					TS:   ent.TS,
					Data: data,
				}
				tent.CopyEnumeratedBlock(ent)
				rset = append(rset, tent)
			}
		} else if r, ok := je.genEntry(dt, ent, v); ok {
			r.CopyEnumeratedBlock(ent)
			rset = append(rset, r)
		}
		return
	}
	if ent == nil {
		return
	}
	if _, err = jsonparser.ArrayEach(ent.Data, cb, je.key...); err != nil {
		if err == jsonparser.KeyPathNotFoundError {
			if je.Drop_Misses == false && rset == nil {
				rset = []*entry.Entry{ent}
			}
			err = nil
		}
	}
	return
}

func (je *JsonArraySplitter) genEntry(dt jsonparser.ValueType, ent *entry.Entry, v []byte) (r *entry.Entry, ok bool) {
	if ent == nil || v == nil {
		return
	}
	if len(v) != 0 {
		ok = true
		r = &entry.Entry{
			Tag: ent.Tag,
			SRC: ent.SRC,
			TS:  ent.TS,
		}
		if !je.Force_JSON_Object {
			r.Data = v
		} else if dt == jsonparser.String {
			r.Data = []byte(fmt.Sprintf(`{"%s":"%s"}`, je.keyname, string(v)))
		} else {
			r.Data = []byte(fmt.Sprintf(`{"%s":%s}`, je.keyname, string(v)))
		}
	}
	return
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
	if v == ',' {
		return true
	}
	return false
}

func dotSplitter(v rune) bool {
	if v == '.' {
		return true
	}
	return false
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
