package microsoft

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

type Builder struct {
	config *Config
	name   string
	now    func() time.Time
}

func NewBuilder(name string, config *Config) *Builder {
	return &Builder{name: name, config: config}
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
	SyncContext(context.Context, time.Duration) error
}

func (b *Builder) Build(negotiator hosted.TagNegotiator, syncFn func() error) (hosted.Ingester, error) {
	writer, ok := negotiator.(processorWriter)
	if !ok {
		return nil, fmt.Errorf("Microsoft API ingest writer is incompatible with the Gravwell ingest muxer")
	}
	processorSet := processors.NewProcessorSet(writer)
	job := New(b.config, processorSet)
	if b.now != nil {
		job.now = b.now
	}
	job.syncDelivery = writer.SyncContext
	job.syncState = syncFn
	return hosted.WrapJobWithSync(job, syncFn), nil
}
