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
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/buger/jsonparser"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
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
	if c.Drop_Misses == false {
		if c.Passthrough_Misses == false {
			c.Drop_Misses = true
		}
	}
	_, _, err = c.getKeyData()
	return
}

// JsonExtractor
type JsonExtractor struct {
	nocloser
	JsonExtractConfig
	bldr builder
}

func NewJsonExtractor(cfg JsonExtractConfig) (*JsonExtractor, error) {
	if cfg.Drop_Misses == false && cfg.Strict_Extraction {
		return nil, ErrMissStrictConflict
	}
	keys, keynames, err := cfg.getKeyData()
	if err != nil {
		return nil, err
	}
	return &JsonExtractor{
		JsonExtractConfig: cfg,
		bldr: builder{
			forceJson: cfg.Force_JSON_Object,
			bb:        bytes.NewBuffer(nil),
			keys:      keys,
			keynames:  keynames,
		},
	}, nil
}

func (j *JsonExtractor) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(JsonExtractConfig); ok {
		j.JsonExtractConfig = cfg
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (je *JsonExtractor) Process(ents []*entry.Entry) (rset []*entry.Entry, err error) {
	if len(ents) == 0 {
		return
	}
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
	if err := je.bldr.extract(ent.Data); err != nil {
		je.bldr.reset()
		if je.Drop_Misses == false {
			return ent
		}
		return nil
	}
	data, cnt := je.bldr.render()
	if je.Strict_Extraction && cnt != len(je.bldr.keynames) {
		return nil //just dropping the entry
	} else if cnt == 0 && je.Drop_Misses == false {
		return ent
	} else if len(data) > 0 {
		ent.Data = data
	}
	return ent
}

func (jec JsonExtractConfig) getKeyData() (keys [][]string, keynames []string, err error) {
	if len(jec.Extractions) == 0 {
		err = ErrMissingExtractions
		return
	}
	var flds []string
	if flds, err = splitField(jec.Extractions); err != nil {
		return
	}
	for _, key := range flds {
		var keyname string
		if len(key) == 0 {
			continue
		}
		bits := strings.Split(key, `.`)
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

func (b *builder) extract(data []byte) error {
	if len(b.keys) > 1 || b.forceJson {
		return b.extractJson(data)
	}
	return b.extractObject(data)
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
	addData(key, dt, v, b.bb)
	b.cnt++
	b.comma = true
}

func (b *builder) render() (r []byte, cnt int) {
	if b.cnt == 0 {
		return
	}
	if len(b.keys) > 1 || b.forceJson {
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

func addData(key string, dt jsonparser.ValueType, v []byte, bb *bytes.Buffer) {
	if dt == jsonparser.String {
		fmt.Fprintf(bb, `"%s":"%s"`, key, string(v))
	} else {
		fmt.Fprintf(bb, `"%s":`, key)
		bb.Write(v)
	}
}
