/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/timegrinder"
	"github.com/gravwell/jsonparser"
)

const (
	JsonTimestampProcessor string = `jsontimeextract`
)

type JsonTimestampConfig struct {
	// Optional timestamp override
	Timestamp_Override string

	// Optional setting of assume local timezone
	Assume_Local_Timezone bool

	// Required path used to go find the timesatmp in the JSON blob
	Path string

	// Optional bounds on extracted timestamps, timestamps outside the bounds are ignored
	Timestamp_Max_Past_Delta   string
	Timestamp_Max_Future_Delta string
}

func JsonTimestampLoadConfig(vc *config.VariableConfig) (c JsonTimestampConfig, err error) {
	if err = vc.MapTo(&c); err == nil {
		err = c.validate()
	}
	return
}

type JsonTimestamp struct {
	nocloser
	JsonTimestampConfig
	keys   []string
	tg     *timegrinder.TimeGrinder
	bounds tsBounds
}

// NewJsonTimestamp instantiates a JsonTimestamp preprocessor. It will attempt
// to open and read the files specified in the configuration; nonexistent
// files or permissions problems will return an error.
func NewJsonTimestamp(cfg JsonTimestampConfig) (*JsonTimestamp, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	tg, err := timegrinder.New(timegrinder.Config{FormatOverride: cfg.Timestamp_Override})
	if err != nil {
		return nil, err
	}
	if cfg.Assume_Local_Timezone {
		tg.SetLocalTime()
	}
	bounds, err := newTSBounds(cfg.Timestamp_Max_Past_Delta, cfg.Timestamp_Max_Future_Delta)
	if err != nil {
		return nil, err
	}
	return &JsonTimestamp{
		JsonTimestampConfig: cfg,
		keys:                cfg.keys(),
		tg:                  tg,
		bounds:              bounds,
	}, nil
}

func (j *JsonTimestamp) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(JsonTimestampConfig); ok {
		if j.bounds, err = newTSBounds(cfg.Timestamp_Max_Past_Delta, cfg.Timestamp_Max_Future_Delta); err == nil {
			j.JsonTimestampConfig = cfg
		}
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (j *JsonTimestamp) Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if len(ents) == 0 {
		return ents, nil
	}
	for i := range ents {
		if ents[i] == nil {
			continue
		}
		if v, _, _, err := jsonparser.Get(ents[i].Data, j.keys...); err == nil {
			if ts, ok, err := j.tg.Extract(v); err == nil && ok {
				j.bounds.apply(ents[i], ts)
			}
		}
	}
	return ents, nil
}

func (jtc JsonTimestampConfig) validate() (err error) {
	if jtc.Path == `` {
		err = errors.New("missing jsontimestamp extraction key")
	} else if ov := strings.TrimSpace(jtc.Timestamp_Override); ov != `` {
		err = timegrinder.ValidateFormatOverride(ov)
	}
	if err == nil {
		_, err = newTSBounds(jtc.Timestamp_Max_Past_Delta, jtc.Timestamp_Max_Future_Delta)
	}
	return
}

func (jtc JsonTimestampConfig) keys() []string {
	return unquoteFields(splitRespectQuotes(jtc.Path, dotSplitter))
}
