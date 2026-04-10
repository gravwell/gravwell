/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package plugins

import (
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/plugins/mimecast"
	"github.com/gravwell/gravwell/v3/hosted/plugins/okta"
	"github.com/gravwell/gravwell/v3/hosted/plugins/tester"
)

type BuilderConfig interface {
	UUID() uuid.UUID
}

// Builder is provided as a generic way to implement the IngesterBuilder interface.
// This can't be truly generic to every config as IngesterBuilder.Build does break standards and returns an interface.
// To use this a new struct can embed Builder with the same type used in Configs.
// The Build method will need to be implemented manually. This is done to pivot the types to the interface.
// And a NewThingBuilder method should be created as well.
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
