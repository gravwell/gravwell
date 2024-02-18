/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"bufio"
	"crypto/rand"
	"errors"
	"fmt"
	"os"

	"github.com/buger/jsonparser"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/minio/highwayhash"
)

const (
	JsonFilterProcessor string = `jsonfilter`
)

var (
	ErrMatchAction = errors.New("Match-Action must be either 'pass' or 'drop' (default pass)")
	ErrMatchLogic  = errors.New("Match-Logic must be either 'and' or 'or' (default and)")
)

type JsonFilterConfig struct {
	// what to do when an entry matches: "pass" or "drop"
	Match_Action string

	// "and" or "or", specifying that either *all* fields must match or that *any* field will be sufficient
	Match_Logic string

	// each Field-Filter consists of the field to match, a comma, and the path to the file containing possible values, e.g. "foo.bar,/tmp/values"
	Field_Filter []string
}

func JsonFilterLoadConfig(vc *config.VariableConfig) (c JsonFilterConfig, err error) {
	err = vc.MapTo(&c)
	return
}

type hsh [highwayhash.Size128]byte

type JsonFilter struct {
	nocloser
	JsonFilterConfig
	key       []byte
	matchPass bool
	matchAnd  bool
	fields    map[string][]string
	filters   map[string]map[hsh]struct{}
}

// NewJsonFilter instantiates a JsonFilter preprocessor. It will attempt
// to open and read the files specified in the configuration; nonexistent
// files or permissions problems will return an error.
func NewJsonFilter(cfg JsonFilterConfig) (*JsonFilter, error) {
	var x struct{}
	fields := make(map[string][]string)
	filters := make(map[string]map[hsh]struct{})

	// generate a hashing key
	key := make([]byte, 32)
	rand.Read(key)

	// Load the filter files
	for _, ff := range cfg.Field_Filter {
		r := splitRespectQuotes(ff, commaSplitter)

		if len(r) != 2 {
			return nil, errors.New("Field-Filter must consist of fieldname,filepath")
		}
		fieldname := r[0]
		pth := r[1]

		// split the keys
		fields[fieldname] = unquoteFields(splitRespectQuotes(fieldname, dotSplitter))

		// now populate the map with the contents of the field
		filters[fieldname] = make(map[hsh]struct{})
		f, err := os.Open(pth)
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			filters[fieldname][highwayhash.Sum128(scanner.Bytes(), key)] = x
		}
	}

	// Check the options
	matchPass := true
	switch cfg.Match_Action {
	case "":
		// default = pass
	case "pass":
	case "drop":
		matchPass = false
	default:
		return nil, ErrMatchAction
	}

	matchAnd := true
	switch cfg.Match_Logic {
	case "":
		// unspecified = AND
	case "and":
	case "or":
		matchAnd = false
	default:
		return nil, ErrMatchLogic
	}
	return &JsonFilter{
		JsonFilterConfig: cfg,
		filters:          filters,
		fields:           fields,
		key:              key,
		matchPass:        matchPass,
		matchAnd:         matchAnd,
	}, nil
}

func (j *JsonFilter) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(JsonFilterConfig); ok {
		j.JsonFilterConfig = cfg
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (j *JsonFilter) Process(ents []*entry.Entry) (rset []*entry.Entry, err error) {
	if len(ents) == 0 {
		return
	}
	rset = ents[:0]
	for _, ent := range ents {
		if ent == nil {
			continue
		}
		if ent = j.processItem(ent); ent != nil {
			rset = append(rset, ent)
		}
	}
	return
}

func (j *JsonFilter) processItem(ent *entry.Entry) *entry.Entry {
	var ok bool
	var v []byte
	var err error //errors are ignored
	for fieldname, keys := range j.fields {
		ok = false
		if v, _, _, err = jsonparser.Get(ent.Data, keys...); err == nil {
			// This way if there was a problem extracting, we just leave ok = false
			_, ok = j.filters[fieldname][highwayhash.Sum128(v, j.key)]
		}
		if ok && !j.matchAnd {
			// !j.matchAnd means they specified OR logic, and we have a match, so we return
			if j.matchPass {
				return ent
			} else {
				return nil //drop it
			}
		} else if !ok && j.matchAnd {
			// they specified AND but we didn't match, return
			if !j.matchPass {
				return ent
			} else {
				return nil
			}
		}
	}
	// if we got here, we had all match with AND, or *nothing* matched with OR
	if (j.matchAnd && j.matchPass) || (!j.matchAnd && !j.matchPass) || len(j.fields) == 0 {
		return ent
	}
	return nil //missed
}
