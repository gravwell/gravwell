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
	"encoding/gob"
	"errors"
	"fmt"

	"github.com/golang/snappy"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/traetox/buffer"
)

const PersistentBufferProcessor = `persistent-buffer`

type PersistentBufferConfig struct {
	Filename   string
	BufferSize string
}

func PersistentBufferLoadConfig(vc *config.VariableConfig) (c PersistentBufferConfig, err error) {
	if err = vc.MapTo(&c); err == nil {
		err = c.validate()
	}
	return
}

func (c PersistentBufferConfig) validate() (err error) {
	if c.Filename == `` {
		err = errors.New("Missing filename")
	} else if c.BufferSize == `` {
		err = errors.New("Missing buffersize")
	} else {
		_, err = parseDataSize(c.BufferSize)
	}
	return
}

func (c PersistentBufferConfig) capacity() (v int, err error) {
	v, err = parseDataSize(c.BufferSize)
	return
}

func NewPersistentBuffer(cfg PersistentBufferConfig) (*PersistentBuffer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	capacity, err := cfg.capacity()
	if err != nil {
		return nil, err
	}
	b, err := buffer.New(cfg.Filename, capacity)
	if err != nil {
		return nil, err
	}
	return &PersistentBuffer{
		PersistentBufferConfig: cfg,
		b:                      b,
		bb:                     bytes.NewBuffer(nil),
	}, nil
}

// PersistentBuffer does not have any state, and doesn't do much
type PersistentBuffer struct {
	PersistentBufferConfig
	b  *buffer.Buffer
	bb *bytes.Buffer
}

func (gd *PersistentBuffer) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(PersistentBufferConfig); ok {
		gd.PersistentBufferConfig = cfg
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (gd *PersistentBuffer) Process(ent []*entry.Entry) (rset []*entry.Entry, err error) {
	gd.bb.Reset()
	if err = gob.NewEncoder(gd.bb).Encode(ent); err == nil {
		if buff := snappy.Encode(nil, gd.bb.Bytes()); buff != nil {
			gd.b.Insert(buff)
		}
	}
	rset = ent
	return
}

func (gd *PersistentBuffer) Flush() []*entry.Entry {
	gd.b.Sync()
	return nil
}

func (gd *PersistentBuffer) Close() (err error) {
	err = gd.b.Close()
	return
}
