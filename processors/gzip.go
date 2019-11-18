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

	"github.com/gravwell/ingest/v3/config"
	"github.com/gravwell/ingest/v3/entry"
	"github.com/klauspost/compress/gzip"
)

const (
	GzipProcessor string = `gzip`

	gzipMagic uint16 = 0x8B1F
)

var (
	ErrNotGzipped = errors.New("Input is not a gzipped stream")
)

type GzipDecompressorConfig struct {
	Passthrough_Non_Gzip bool
}

func GzipLoadConfig(vc *config.VariableConfig) (c GzipDecompressorConfig, err error) {
	err = vc.MapTo(&c)
	return
}

func NewGzipDecompressor(cfg GzipDecompressorConfig) (*GzipDecompressor, error) {
	return &GzipDecompressor{
		GzipDecompressorConfig: cfg,
	}, nil
}

// GzipDecompressor does not have any state
type GzipDecompressor struct {
	GzipDecompressorConfig
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

func (gd *GzipDecompressor) Process(ent *entry.Entry) (rset []*entry.Entry, err error) {
	var gzok bool
	var gzr *gzip.Reader
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
			rset = []*entry.Entry{ent}
		} else {
			err = ErrNotGzipped
		}
		return
	}

	//ok we we have gzip, go ahead and do the things
	if gzr, err = gzip.NewReader(bytes.NewBuffer(ent.Data)); err == nil {
		bwtr := bytes.NewBuffer(nil)
		if _, err = io.Copy(bwtr, gzr); err == nil {
			if err = gzr.Close(); err == nil {
				ent.Data = bwtr.Bytes()
				rset = []*entry.Entry{ent}
			}
		}
	}
	return
}
