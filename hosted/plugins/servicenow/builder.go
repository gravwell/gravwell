package servicenow

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

type Builder struct {
	config        *Config
	name          string
	preprocessors processors.ProcessorConfig
}

func NewBuilder(name string, config *Config, p processors.ProcessorConfig) *Builder {
	return &Builder{config, name, p}
}
func (b *Builder) Config() any     { return b.config }
func (b *Builder) Name() string    { return b.name }
func (b *Builder) Kind() string    { return Name }
func (b *Builder) ID() string      { return ID }
func (b *Builder) Version() string { return Version }
func (b *Builder) UUID() uuid.UUID { return b.config.UUID() }

type processorWriter interface {
	hosted.TagNegotiator
	LookupTag(entry.EntryTag) (string, bool)
	KnownTags() []string
	WriteEntry(*entry.Entry) error
	WriteEntryContext(context.Context, *entry.Entry) error
	WriteBatch([]*entry.Entry) error
	WriteBatchContext(context.Context, []*entry.Entry) error
}

func (b *Builder) Build(n hosted.TagNegotiator, syncFn func() error) (hosted.Ingester, error) {
	w, ok := n.(processorWriter)
	if !ok {
		return nil, fmt.Errorf("ServiceNow preprocessor writer is incompatible with the ingest muxer")
	}
	p, err := b.preprocessors.ProcessorSet(w, b.config.Preprocessor)
	if err != nil {
		return nil, err
	}
	return hosted.WrapJobWithSync(New(b.config, p), syncFn), nil
}
