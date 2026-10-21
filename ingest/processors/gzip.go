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
	"errors"
	"fmt"
	"io"

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/klauspost/compress/gzip"
)

const (
	GzipProcessor string = `gzip`

	gzipMagic uint16 = 0x8B1F
	kb               = 1024
	mb               = 1024 * kb

	defaultBaseBuff int = 4 * mb  //4MB
	defaultMaxBuff  int = 32 * mb //32MB
)

var (
	ErrNotGzipped = errors.New("Input is not a gzipped stream")
	nb            []byte
)

type GzipDecompressorConfig struct {
	Passthrough_Non_Gzip bool
	Min_Buff_MB          uint
	Max_Buff_MB          uint
}

func GzipLoadConfig(vc *config.VariableConfig) (c GzipDecompressorConfig, err error) {
	err = vc.MapTo(&c)
	return
}

func (gdc GzipDecompressorConfig) BufferSizes() (base, max int) {
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

func NewGzipDecompressor(cfg GzipDecompressorConfig) (*GzipDecompressor, error) {
	base, max := cfg.BufferSizes()
	return &GzipDecompressor{
		GzipDecompressorConfig: cfg,
		rdr:                    bytes.NewReader(nil),
		zrdr:                   new(gzip.Reader),
		bb:                     bytes.NewBuffer(make([]byte, base)),
		baseBuff:               base,
		maxBuff:                max,
	}, nil
}

// GzipDecompressor does not have any state
type GzipDecompressor struct {
	nocloser
	GzipDecompressorConfig
	rdr      *bytes.Reader
	zrdr     *gzip.Reader
	bb       *bytes.Buffer
	baseBuff int
	maxBuff  int
}

func (gd *GzipDecompressor) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(GzipDecompressorConfig); ok {
		gd.GzipDecompressorConfig = cfg
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (gd *GzipDecompressor) Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if len(ents) == 0 {
		return nil, nil
	}
	rset := ents[:0]
	for _, v := range ents {
		if ent, err := gd.procEnt(v); err == nil && ent != nil {
			rset = append(rset, ent)
		}
	}
	return rset, nil
}

func (gd *GzipDecompressor) procEnt(ent *entry.Entry) (rset *entry.Entry, err error) {
	var gzok bool
	if ent == nil {
		return
	}
	if len(ent.Data) > 2 {
		//check for the gzip header
		gzok = binary.LittleEndian.Uint16(ent.Data) == gzipMagic
	}
	if !gzok {
		//check if we are passing through
		if gd.Passthrough_Non_Gzip {
			rset = ent
		} else {
			err = ErrNotGzipped
		}
		return
	}

	gd.rdr.Reset(ent.Data)
	gd.zrdr.Reset(gd.rdr)
	//bwtr := bytes.NewBuffer(nil)
	gd.bb.Reset()

	//ok we we have gzip, go ahead and do the things
	if _, err = io.Copy(gd.bb, gd.zrdr); err == nil {
		if err = gd.zrdr.Close(); err == nil {
			ent.Data = append(nb, gd.bb.Bytes()...)
			rset = ent
		}
	}
	if gd.bb.Cap() > gd.maxBuff {
		gd.bb = bytes.NewBuffer(make([]byte, gd.baseBuff))
	}
	return
}
