package plugins

import (
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingesters/hosted"
	"github.com/gravwell/gravwell/v3/ingesters/hosted/plugins/mimecast"
	"github.com/gravwell/gravwell/v3/ingesters/hosted/plugins/okta"
	"github.com/gravwell/gravwell/v3/ingesters/hosted/plugins/tester"
)

type BuilderConfig interface {
	UUID() uuid.UUID
}

type Builder[T BuilderConfig] struct {
	config  T
	kind    string
	id      string
	version string
}

func (b *Builder[T]) Config() any {
	return b.config
}

func (b *Builder[T]) Kind() string {
	return b.kind
}

func (b *Builder[T]) ID() string {
	return b.id
}

func (b *Builder[T]) Version() string {
	return b.version
}

func (b *Builder[T]) UUID() uuid.UUID {
	return b.config.UUID()
}

type TesterBuilder struct {
	Builder[*tester.Config]
}

func (tb *TesterBuilder) Build(tn hosted.TagNegotiator) (hosted.Ingester, error) {
	return tester.NewTesterIngester(*(tb.config), tn)
}

func NewTesterBuilder(config *tester.Config, kind, id, version string) *TesterBuilder {
	return &TesterBuilder{
		Builder[*tester.Config]{
			config:  config,
			kind:    kind,
			id:      id,
			version: version,
		},
	}
}

type OktaBuilder struct {
	Builder[*okta.Config]
}

func (ob *OktaBuilder) Build(tn hosted.TagNegotiator) (hosted.Ingester, error) {
	return okta.NewOktaIngester(*(ob.config), tn)
}

func NewOktaBuilder(config *okta.Config, kind, id, version string) *OktaBuilder {
	return &OktaBuilder{
		Builder[*okta.Config]{
			config:  config,
			kind:    kind,
			id:      id,
			version: version,
		},
	}
}

type MimecastBuilder struct {
	Builder[*mimecast.Config]
}

func (mb *MimecastBuilder) Build(tn hosted.TagNegotiator) (hosted.Ingester, error) {
	return mimecast.New(mb.config), nil
}

func NewMimecastBuilder(config *mimecast.Config, kind, id, version string) *MimecastBuilder {
	return &MimecastBuilder{
		Builder[*mimecast.Config]{
			config:  config,
			kind:    kind,
			id:      id,
			version: version,
		},
	}
}
