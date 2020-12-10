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
	"encoding/binary"
	"fmt"
	"io"
	"strconv"

	"github.com/buger/jsonparser"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/klauspost/compress/gzip"
)

const (
	VpcProcessor string = `vpc`
)

type VpcConfig struct {
	Min_Buff_MB  uint
	Max_Buff_MB  uint
	Extract_JSON bool
}

func VpcLoadConfig(vc *config.VariableConfig) (c VpcConfig, err error) {
	err = vc.MapTo(&c)
	return
}

func (gdc VpcConfig) BufferSizes() (base, max int) {
	if gdc.Min_Buff_MB == 0 {
		base = defaultBaseBuff
	} else {
		base = int(gdc.Min_Buff_MB) * mb
	}
	if gdc.Max_Buff_MB == 0 {
		if max = defaultMaxBuff; max < base {
			max = base * 2
		}
	} else {
		max = int(gdc.Max_Buff_MB) * mb
	}
	return
}

func NewVpcProcessor(cfg VpcConfig) (*Vpc, error) {
	base, max := cfg.BufferSizes()
	return &Vpc{
		VpcConfig: cfg,
		rdr:       bytes.NewReader(nil),
		zrdr:      new(gzip.Reader),
		bb:        bytes.NewBuffer(make([]byte, base)),
		baseBuff:  base,
		maxBuff:   max,
	}, nil
}

type Vpc struct {
	VpcConfig
	rdr      *bytes.Reader
	zrdr     *gzip.Reader
	bb       *bytes.Buffer
	baseBuff int
	maxBuff  int
}

func (p *Vpc) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(VpcConfig); ok {
		p.VpcConfig = cfg
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (p *Vpc) Process(ent *entry.Entry) (rset []*entry.Entry, err error) {
	if ent == nil {
		return
	}
	if len(ent.Data) > 2 {
		//check for the gzip header
		if binary.LittleEndian.Uint16(ent.Data) == gzipMagic {
			p.rdr.Reset(ent.Data)
			p.zrdr.Reset(p.rdr)
			p.bb.Reset()

			//ok we we have gzip, go ahead and do the things
			if _, err = io.Copy(p.bb, p.zrdr); err == nil {
				if err = p.zrdr.Close(); err == nil {
					ent.Data = append(nb, p.bb.Bytes()...)
				}
			}
			if p.bb.Cap() > p.maxBuff {
				p.bb = bytes.NewBuffer(make([]byte, p.baseBuff))
			}
		}
	}

	// first split the "logEvents" array
	var logEvents [][]byte
	cb := func(v []byte, dt jsonparser.ValueType, off int, lerr error) {
		if len(v) == 0 {
			return
		}
		logEvents = append(logEvents, v)
		return
	}
	if _, err = jsonparser.ArrayEach(ent.Data, cb, "logEvents"); err != nil {
		return
	}

	// Now extract from each one
	var r *entry.Entry
	var tsString string
	var ts int64
	var v []byte
	for i := range logEvents {
		if p.VpcConfig.Extract_JSON {
			v, _, _, err = jsonparser.Get(logEvents[i], "extractedFields")
			if err != nil {
				return
			}
		} else {
			v, _, _, err = jsonparser.Get(logEvents[i], "message")
			if err != nil {
				return
			}
		}
		// Attempt to get the timestamp
		tsString, err = jsonparser.GetString(logEvents[i], "extractedFields", "start")
		if err != nil {
			return
		}
		ts, err = strconv.ParseInt(tsString, 10, 64)
		if err != nil {
			return
		}

		// build up the entry
		r = &entry.Entry{
			Tag:  ent.Tag,
			SRC:  ent.SRC,
			Data: v,
			TS:   entry.UnixTime(ts, 0),
		}
		rset = append(rset, r)
	}
	return
}

func (p *Vpc) Close() error {
	return nil
}

func (p *Vpc) Flush() []*entry.Entry {
	return nil
}
