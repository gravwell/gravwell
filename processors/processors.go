/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package processors implements preprocessors for ingesters. The intended
// usage is to create a ProcessorSet and call ProcessorSet.Process(). Calls to
// ProcessorSet.Process() are thread-safe while Process() calls on specific
// processors is not.
package processors

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

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
	sync.Mutex
	wtr entWriter
	set []Processor
}

type ProcessorConfig map[string]*config.VariableConfig

// Processor is an interface that acts as an inline decompressor
// the decompressor is used for doing an transparent decompression of data
type Processor interface {
	Process(*entry.Entry) ([]*entry.Entry, error) //process an data item potentially setting a tag
	Close() error                                 //give the processor a chance to tide up
}

func CheckProcessor(id string) error {
	id = strings.TrimSpace(strings.ToLower(id))
	switch id {
	case GzipProcessor:
	case JsonExtractProcessor:
	case JsonArraySplitProcessor:
	case JsonFilterProcessor:
	case RegexTimestampProcessor:
	case RegexExtractProcessor:
	case RegexRouterProcessor:
	case ForwarderProcessor:
	default:
		return ErrUnknownProcessor
	}
	return nil
}

type Tagger interface {
	NegotiateTag(name string) (entry.EntryTag, error)
	LookupTag(entry.EntryTag) (string, bool)
}

type entWriter interface {
	WriteEntry(*entry.Entry) error
	WriteEntryContext(context.Context, *entry.Entry) error
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
	case JsonFilterProcessor:
		cfg, err = JsonFilterLoadConfig(vc)
	case RegexTimestampProcessor:
		cfg, err = RegexTimestampLoadConfig(vc)
	case RegexExtractProcessor:
		cfg, err = RegexExtractLoadConfig(vc)
	case RegexRouterProcessor:
		cfg, err = RegexRouteLoadConfig(vc)
	case ForwarderProcessor:
		cfg, err = ForwarderLoadConfig(vc)
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

func (pc ProcessorConfig) getProcessor(name string, tgr Tagger) (p Processor, err error) {
	if vc, ok := pc[name]; !ok || vc == nil {
		err = ErrNotFound
	} else {
		p, err = newProcessor(vc, tgr)
	}
	return
}

func newProcessor(vc *config.VariableConfig, tgr Tagger) (p Processor, err error) {
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
	case JsonFilterProcessor:
		var cfg JsonFilterConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewJsonFilter(cfg)
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
	case RegexRouterProcessor:
		var cfg RegexRouteConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewRegexRouter(cfg, tgr)
	case ForwarderProcessor:
		var cfg ForwarderConfig
		if err = vc.MapTo(&cfg); err != nil {
			return
		}
		p, err = NewForwarder(cfg, tgr)
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
	pr.Lock()
	defer pr.Unlock()
	return len(pr.set) > 0 && pr.wtr != nil
}

func (pr *ProcessorSet) AddProcessor(p Processor) {
	pr.Lock()
	defer pr.Unlock()
	pr.set = append(pr.set, p)
}

func (pr *ProcessorSet) Process(ent *entry.Entry) error {
	pr.Lock()
	defer pr.Unlock()
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

func (pr *ProcessorSet) ProcessContext(ent *entry.Entry, ctx context.Context) error {
	pr.Lock()
	defer pr.Unlock()
	if pr == nil || pr.wtr == nil {
		return ErrNotReady
	} else if ent == nil {
		return ErrInvalidEntry
	} else if len(pr.set) == 0 {
		return pr.wtr.WriteEntryContext(ctx, ent)
	}
	//we have processors, start recursing into them
	return pr.processItemContext(ent, 0, ctx)
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

// processItemContext recurses into each processor generating entries and writing them out
func (pr *ProcessorSet) processItemContext(ent *entry.Entry, i int, ctx context.Context) error {
	if i >= len(pr.set) {
		//we are at the end of the line, just write the entry
		return pr.wtr.WriteEntryContext(ctx, ent)
	}
	if set, err := pr.set[i].Process(ent); err != nil {
		return err
	} else {
		for _, v := range set {
			if err := pr.processItemContext(v, i+1, ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// Close will close the underlying preprocessors within the set.
// This function DOES NOT close the ingest muxer handle.
// It is ONLY for shutting down preprocessors
func (pr *ProcessorSet) Close() (err error) {
	for _, v := range pr.set {
		if v != nil {
			if lerr := v.Close(); lerr != nil {
				err = addError(lerr, err)
			}
		}
	}
	return
}

func addError(nerr, err error) error {
	if nerr == nil {
		return err
	} else if err == nil {
		return nerr
	}
	return fmt.Errorf("%v : %v", nerr, err)
}

type tagWriter interface {
	entWriter
	Tagger
}

func (pc ProcessorConfig) ProcessorSet(t tagWriter, names []string) (pr *ProcessorSet, err error) {
	if pc == nil {
		pr = NewProcessorSet(t) //nothing defined
		return
	}
	pr = NewProcessorSet(t)
	var p Processor
	for _, n := range names {
		if p, err = pc.getProcessor(n, t); err != nil {
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
			err = fmt.Errorf("Preprocessor %v not defined", err)
			break
		}
	}
	return
}

type nocloser struct{}

func (n nocloser) Close() error {
	return nil
}

const (
	defaultSetAllocSize   int = 1024
	defaultSetReallocSize int = 16
)

var (
	sa, _ = NewSetAllocator(defaultSetAllocSize, defaultSetReallocSize)
)

type SetAllocator struct {
	sync.Mutex
	set         []*entry.Entry
	allocSize   int
	reallocSize int
}

func NewSetAllocator(allocSize, reallocSize int) (sa *SetAllocator, err error) {
	if allocSize <= 0 {
		allocSize = defaultSetAllocSize
	}
	if reallocSize <= 0 {
		reallocSize = defaultSetReallocSize
	}
	if reallocSize >= allocSize {
		err = errors.New("invalid alloc to realloc size")
		return
	}
	sa = &SetAllocator{
		set:         make([]*entry.Entry, allocSize),
		allocSize:   allocSize,
		reallocSize: reallocSize,
	}
	return
}

func (sa *SetAllocator) Get(cnt int) (r []*entry.Entry) {
	sa.Lock()
	if cnt > sa.reallocSize {
		r = make([]*entry.Entry, cnt)
	} else {
		if len(sa.set) < cnt {
			//reallocate
			sa.set = make([]*entry.Entry, sa.allocSize)
		}
		r = sa.set[0:cnt]
		if sa.set = sa.set[cnt:]; len(sa.set) == 0 {
			sa.set = nil //help out the GC
		}
	}
	sa.Unlock()
	return
}

func PopSet(cnt int) []*entry.Entry {
	return sa.Get(cnt)
}
