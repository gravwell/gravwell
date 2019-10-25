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
	"strings"

	"github.com/gravwell/ingest/v3/config"
	"github.com/gravwell/ingest/v3/entry"
)

const (
	preProcSectName string = `preprocessor`
	preProcTypeName string = `type`
)

var (
	ErrUnknownPreprocessor = errors.New("Unknown preprocessor")
	ErrNotGzipped          = errors.New("Input is not a gzipped stream")
	ErrNilConfig           = errors.New("Nil configuration")
	ErrNotFound            = errors.New("Preprocessor not found")
)

type PreprocessorConfig map[string]*config.VariableConfig

// Preprocessor is an interface that acts as an inline decompressor
// the decompressor is used for doing an transparent decompression of data
type Preprocessor interface {
	Process([]byte, entry.EntryTag) (entry.EntryTag, []byte, error) //process an data item potentially setting a tag
}

func CheckPreprocessor(id string) error {
	id = strings.TrimSpace(strings.ToLower(id))
	switch id {
	case GzipProcessor:
		return nil
	}
	return ErrUnknownPreprocessor
}

type preprocessorBase struct {
	Type string
}

func PreprocessorLoadConfig(vc *config.VariableConfig) (cfg interface{}, err error) {
	var pb preprocessorBase
	if err = vc.MapTo(&pb); err != nil {
		return
	}
	switch strings.TrimSpace(strings.ToLower(pb.Type)) {
	case GzipProcessor:
		cfg, err = GzipLoadConfig(vc)
	default:
		err = ErrUnknownPreprocessor
	}
	return
}

func (pc PreprocessorConfig) CheckConfig(name string) (err error) {
	if vc, ok := pc[name]; !ok || vc == nil {
		err = ErrNotFound
	} else {
		_, err = PreprocessorLoadConfig(vc)
	}
	return
}

func (pc PreprocessorConfig) GetPreprocessor(name string) (p Preprocessor, err error) {
	if vc, ok := pc[name]; !ok || vc == nil {
		err = ErrNotFound
	} else {
		p, err = NewPreprocessor(vc)
	}
	return
}

func NewPreprocessor(vc *config.VariableConfig) (p Preprocessor, err error) {
	var pb preprocessorBase
	if err = vc.MapTo(&pb); err != nil {
		return
	}
	id := strings.TrimSpace(strings.ToLower(pb.Type))
	switch id {
	case GzipProcessor:
		var cfg GzipDecompressorConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewGzipDecompressor(cfg)
	default:
		err = ErrUnknownPreprocessor
	}
	return
}
