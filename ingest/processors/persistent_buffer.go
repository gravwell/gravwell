//go:build linux
// +build linux

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

	"github.com/cloudflare/buffer" //waiting on PR from github.com/traetox/buffer
	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var (
	ErrBufferEmpty = errors.New("Buffer is empty")
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

// PersistentBuffer does not have any state, and doesn't do much
type PersistentBuffer struct {
	PersistentBufferConfig
	tgr  Tagger
	b    *buffer.Buffer
	bb   *bytes.Buffer
	tags map[entry.EntryTag]string
}

func NewPersistentBuffer(cfg PersistentBufferConfig, tagger Tagger) (*PersistentBuffer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	} else if tagger == nil {
		return nil, errors.New("Tagger is nil")
	}
	capacity, err := cfg.capacity()
	if err != nil {
		return nil, err
	}
	b, err := buffer.Open(cfg.Filename, capacity)
	if err != nil {
		return nil, err
	}
	return &PersistentBuffer{
		PersistentBufferConfig: cfg,
		b:                      b,
		bb:                     bytes.NewBuffer(nil),
		tgr:                    tagger,
		tags:                   map[entry.EntryTag]string{},
	}, nil
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

func (gd *PersistentBuffer) Process(ents []*entry.Entry) (rset []*entry.Entry, err error) {
	if len(ents) == 0 {
		return
	}
	gd.bb.Reset()
	strents := make([]types.StringTagEntry, 0, len(ents))
	for _, e := range ents {
		if e == nil {
			continue
		}
		strent := types.StringTagEntry{
			Data: e.Data,
			TS:   e.TS.StandardTime(),
			SRC:  e.SRC,
			Tag:  gd.getTag(e.Tag),
		}
		strents = append(strents, strent)
	}

	if err = gob.NewEncoder(gd.bb).Encode(strents); err == nil {
		gd.b.InsertWithOverwrite(gd.bb.Bytes())
	}
	rset = ents
	return
}

func (gd *PersistentBuffer) getTag(tag entry.EntryTag) (s string) {
	var ok bool
	if s, ok = gd.tags[tag]; !ok {
		if s, ok = gd.tgr.LookupTag(tag); ok {
			//after the expensive lookup populate our local map
			gd.tags[tag] = s
		}
	}
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

type PersistentBufferConsumer struct {
	b *buffer.Buffer
}

func OpenPersistentBuffer(pth string) (pbc *PersistentBufferConsumer, err error) {
	var b *buffer.Buffer
	if b, err = buffer.Open(pth, 0); err != nil {
		return
	}
	pbc = &PersistentBufferConsumer{
		b: b,
	}
	return
}

func (pbc *PersistentBufferConsumer) Close() (err error) {
	if pbc == nil || pbc.b == nil {
		err = errors.New("Not open")
	} else {
		err = pbc.b.Close()
	}
	return
}

func (pbc *PersistentBufferConsumer) Pop() ([]types.StringTagEntry, error) {
	var strents []types.StringTagEntry
	buff, err := pbc.b.Pop()
	if err != nil {
		return nil, err
	} else if buff == nil {
		return nil, ErrBufferEmpty
	}
	if err = gob.NewDecoder(bytes.NewBuffer(buff)).Decode(&strents); err != nil {
		return nil, err
	}
	return strents, nil
}
