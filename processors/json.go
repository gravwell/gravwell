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
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/buger/jsonparser"
	"github.com/gravwell/ingest/v3/config"
	"github.com/gravwell/ingest/v3/entry"
)

const (
	JsonExtractProcessor    string = `jsonextract`
	JsonArraySplitProcessor string = `jsonarraysplit`
)

var (
	ErrMissStrictConflict   = errors.New("Strict-Extraction and Passthrough-Misses are mutually exclusive")
	ErrMissingExtractions   = errors.New("Extractions specifications missing")
	ErrInvalidExtractions   = errors.New("Invalid Extractions")
	ErrInvalidKeyname       = errors.New("Invalid keyname")
	ErrDuplicateKey         = errors.New("Duplicate extraction key")
	ErrDuplicateKeyname     = errors.New("Duplicate keys")
	ErrSingleArraySplitOnly = errors.New("jsonarraysplit only supports a single extraction")
)

type JsonExtractConfig struct {
	Passthrough_Misses bool
	Strict_Extraction  bool
	Force_JSON_Object  bool
	Extractions        string
}

func JsonExtractLoadConfig(vc *config.VariableConfig) (c JsonExtractConfig, err error) {
	err = vc.MapTo(&c)
	return
}

// JsonExtractor
type JsonExtractor struct {
	JsonExtractConfig
	bldr builder
}

func NewJsonExtractor(cfg JsonExtractConfig) (*JsonExtractor, error) {
	if cfg.Passthrough_Misses && cfg.Strict_Extraction {
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

func (je *JsonExtractor) Process(ent *entry.Entry) (rset []*entry.Entry, err error) {
	if ent == nil {
		return
	}
	if err = je.bldr.extract(ent.Data); err != nil {
		je.bldr.reset()
		return
	}
	data, cnt := je.bldr.render()
	if je.Strict_Extraction && cnt != len(je.bldr.keynames) {
		return nil, nil //just dropping the entry
	} else if cnt == 0 && je.Passthrough_Misses {
		rset = []*entry.Entry{ent}
	} else if len(data) > 0 {
		ent.Data = data
		rset = []*entry.Entry{ent}
	}
	return
}

func (jec JsonExtractConfig) getKeyData() (keys [][]string, keynames []string, err error) {
	if len(jec.Extractions) == 0 {
		err = ErrMissingExtractions
		return
	}
	r := csv.NewReader(strings.NewReader(jec.Extractions))
	r.Comma = ' ' // space
	r.TrimLeadingSpace = true
	var flds []string
	if flds, err = r.Read(); err != nil {
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
			if !b.comma {
				io.WriteString(b.bb, "{")
			} else {
				io.WriteString(b.bb, ",")
			}
			addData(b.keynames[i], dt, v, b.bb)
			b.cnt++
			b.comma = true
		}
	}
	return nil
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

type JsonArraySplitConfig struct {
	Passthrough_Misses bool
	Extraction         string
	Force_JSON_Object  bool
}

func JsonArraySplitLoadConfig(vc *config.VariableConfig) (c JsonArraySplitConfig, err error) {
	err = vc.MapTo(&c)
	return
}

func (jasc JsonArraySplitConfig) getKeyData() (key []string, keyname string, err error) {
	if len(jasc.Extraction) == 0 {
		err = ErrMissingExtractions
		return
	}
	r := csv.NewReader(strings.NewReader(jasc.Extraction))
	r.Comma = ' ' // space
	r.TrimLeadingSpace = true
	var flds []string
	if flds, err = r.Read(); err != nil {
		return
	}
	if len(flds) != 1 {
		err = ErrSingleArraySplitOnly
		return
	}
	if key = strings.Split(flds[0], `.`); len(key) == 0 {
		err = ErrMissingExtractions
		return
	}
	keyname = key[len(key)-1]
	return
}

type JsonArraySplitter struct {
	JsonArraySplitConfig
	key     []string
	keyname string
}

func NewJsonArraySplitter(cfg JsonArraySplitConfig) (*JsonArraySplitter, error) {
	key, keyname, err := cfg.getKeyData()
	if err != nil {
		return nil, err
	}
	return &JsonArraySplitter{
		JsonArraySplitConfig: cfg,
		key:                  key,
		keyname:              keyname,
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

func (je *JsonArraySplitter) Process(ent *entry.Entry) (rset []*entry.Entry, err error) {
	cb := func(v []byte, dt jsonparser.ValueType, off int, lerr error) {
		if len(v) == 0 {
			return
		}
		if r, ok := je.genEntry(dt, ent, v); ok {
			rset = append(rset, r)
		}
		return
	}
	if ent == nil {
		return
	}
	if _, err = jsonparser.ArrayEach(ent.Data, cb, je.key...); err != nil {
		if err == jsonparser.KeyPathNotFoundError {
			if je.Passthrough_Misses && rset == nil {
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
