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

	"github.com/gravwell/ingest/v3/config"
	"github.com/gravwell/ingest/v3/entry"
)

const (
	preProcSectName string = `preprocessor`
	preProcTypeName string = `type`
)

var (
	ErrUnknownProcessor = errors.New("Unknown preprocessor")
	ErrNilConfig        = errors.New("Nil configuration")
	ErrNotFound         = errors.New("Processor not found")
	ErrNotReady         = errors.New("ProcessorSet not ready")
	ErrInvalidEntry     = errors.New("ErrInvalidEntry")
)

type ProcessorSet struct {
	wtr entWriter
	set []Processor
}

type ProcessorConfig map[string]*config.VariableConfig

// Processor is an interface that acts as an inline decompressor
// the decompressor is used for doing an transparent decompression of data
type Processor interface {
	Process(*entry.Entry) ([]*entry.Entry, error) //process an data item potentially setting a tag
}

func CheckProcessor(id string) error {
	id = strings.TrimSpace(strings.ToLower(id))
	switch id {
	case GzipProcessor:
		return nil
	case JsonExtractProcessor:
		return nil
	case JsonArraySplitProcessor:
		return nil
	case RegexTimestampProcessor:
		return nil
	case RegexExtractProcessor:
		return nil
	}
	return ErrUnknownProcessor
}

type Tagger interface {
	NegotiateTag(name string) (entry.EntryTag, error)
}

type entWriter interface {
	WriteEntry(*entry.Entry) error
}

type preprocessorBase struct {
	Type string
}

func ProcessorLoadConfig(vc *config.VariableConfig) (cfg interface{}, err error) {
	var pb preprocessorBase
	if err = vc.MapTo(&pb); err != nil {
		return
	}
	switch strings.TrimSpace(strings.ToLower(pb.Type)) {
	case GzipProcessor:
		cfg, err = GzipLoadConfig(vc)
	case JsonExtractProcessor:
		cfg, err = JsonExtractLoadConfig(vc)
	case JsonArraySplitProcessor:
		cfg, err = JsonArraySplitLoadConfig(vc)
	case RegexTimestampProcessor:
		cfg, err = RegexTimestampLoadConfig(vc)
	case RegexExtractProcessor:
		cfg, err = RegexExtractLoadConfig(vc)
	default:
		err = ErrUnknownProcessor
	}
	return
}

func (pc ProcessorConfig) CheckConfig(name string) (err error) {
	if vc, ok := pc[name]; !ok || vc == nil {
		err = ErrNotFound
	} else {
		_, err = ProcessorLoadConfig(vc)
	}
	return
}

func (pc ProcessorConfig) GetProcessor(name string, tgr Tagger) (p Processor, err error) {
	if vc, ok := pc[name]; !ok || vc == nil {
		err = ErrNotFound
	} else {
		p, err = NewProcessor(vc, tgr)
	}
	return
}

func NewProcessor(vc *config.VariableConfig, tgr Tagger) (p Processor, err error) {
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
	case JsonExtractProcessor:
		var cfg JsonExtractConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewJsonExtractor(cfg)
	case JsonArraySplitProcessor:
		var cfg JsonArraySplitConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewJsonArraySplitter(cfg)
	case RegexTimestampProcessor:
		var cfg RegexTimestampConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewRegexTimestampProcessor(cfg)
	case RegexExtractProcessor:
		var cfg RegexExtractConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewRegexExtractor(cfg)

	default:
		err = ErrUnknownProcessor
	}
	return
}

func NewProcessorSet(wtr entWriter) *ProcessorSet {
	return &ProcessorSet{
		wtr: wtr,
	}
}

func (pr *ProcessorSet) Enabled() bool {
	return len(pr.set) > 0 && pr.wtr != nil
}

func (pr *ProcessorSet) AddProcessor(p Processor) {
	pr.set = append(pr.set, p)
}

func (pr *ProcessorSet) Process(ent *entry.Entry) error {
	if pr == nil || pr.wtr == nil {
		return ErrNotReady
	} else if ent == nil {
		return ErrInvalidEntry
	} else if len(pr.set) == 0 {
		return pr.wtr.WriteEntry(ent)
	}
	//we have processors, start recursing into them
	return pr.processItem(ent, 0)
}

// processItem recurses into each processor generating entries and writing them out
func (pr *ProcessorSet) processItem(ent *entry.Entry, i int) error {
	if i >= len(pr.set) {
		//we are at the end of the line, just write the entry
		return pr.wtr.WriteEntry(ent)
	}
	if set, err := pr.set[i].Process(ent); err != nil {
		return err
	} else {
		for _, v := range set {
			if err := pr.processItem(v, i+1); err != nil {
				return err
			}
		}
	}
	return nil
}

type tagWriter interface {
	entWriter
	Tagger
}

func (pc ProcessorConfig) ProcessorSet(t tagWriter, names []string) (pr *ProcessorSet, err error) {
	if pc == nil {
		err = errors.New("ProcessorConfig not ready")
		return
	}
	pr = NewProcessorSet(t)
	var p Processor
	for _, n := range names {
		if p, err = pc.GetProcessor(n, t); err != nil {
			err = fmt.Errorf("%s %v", n, err)
			return
		}
		pr.AddProcessor(p)
	}
	return
}

func (pc ProcessorConfig) Validate() (err error) {
	for k, v := range pc {
		if _, err = ProcessorLoadConfig(v); err != nil {
			err = fmt.Errorf("Preprocessor %s config invalid: %v", k, err)
			return
		}
	}
	return
}

func (pc ProcessorConfig) CheckProcessors(set []string) (err error) {
	for _, v := range set {
		if _, ok := pc[v]; !ok {
			err = fmt.Errorf("Preprocessor %s not defined", err)
			break
		}
	}
	return
}
