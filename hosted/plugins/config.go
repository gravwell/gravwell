/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package plugins
// This contains the necessary config wiring and validation to limit the scope of adding new plugins.
package plugins

import (
	"fmt"
	"iter"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/hosted"

	// include all the native hosted ingesters
	"github.com/gravwell/gravwell/v4/hosted/plugins/mimecast"
	"github.com/gravwell/gravwell/v4/hosted/plugins/okta"
	"github.com/gravwell/gravwell/v4/hosted/plugins/tester"
)

type Configs struct {
	Okta     map[string]*okta.Config
	Mimecast map[string]*mimecast.Config
	Tester   map[string]*tester.Config
}

// Verify ensures that the plugin configs are valid
func (c Configs) Verify() (err error) {
	// verify Okta configs
	for k, v := range c.Okta {
		if v == nil {
			err = fmt.Errorf("Okta config %q is nil", k)
			return
		}
		if err = v.Verify(); err != nil {
			err = fmt.Errorf("Okta config %q failed validation %w", k, err)
			return
		}
	}
	for k, v := range c.Tester {
		if v == nil {
			err = fmt.Errorf("Tester config %q is nil", k)
			return
		}
	}
	for k, v := range c.Mimecast {
		if v == nil {
			err = fmt.Errorf("Mimecast config %q is nil", k)
			return
		}
		if err = v.Verify(); err != nil {
			err = fmt.Errorf("Mimecast config %q failed validation %w", k, err)
			return
		}
	}
	return
}

// Tags implements the required interface for base.cfgHelper which is used during startup
func (c Configs) Tags() (tags []string, err error) {
	if len(c.Okta) > 0 {
		tags = append(tags, okta.Tags...)
	}
	if len(c.Tester) > 0 {
		tags = append(tags, tester.Tag)
	}
	for _, v := range c.Mimecast {
		tags = append(tags, v.Tags()...)
	}
	return
}

// IngesterCount returns the number of ingesters configured
func (c Configs) IngesterCount() (count int) {
	count += len(c.Okta) + len(c.Tester) + len(c.Mimecast)
	return
}

type IngesterBuilder interface {
	UUID() uuid.UUID
	Kind() string
	ID() string
	Version() string
	Build(hosted.TagNegotiator) (hosted.Ingester, error)
	Config() any
}

// Builders returns an iter.Seq2 for use in iterating over each of configured plugins generically.
// The intention is to not couple the plugins to directly to the runtime or runner.
// Any new plugins MUST add another loop here returning an IngesterBuilder for each config entry.
func (c Configs) Builders() iter.Seq2[string, IngesterBuilder] {
	return func(yield func(string, IngesterBuilder) bool) {
		for name, config := range c.Tester {
			if !yield(name, NewTesterBuilder(config, tester.Name, tester.ID, tester.Version)) {
				return
			}
		}
		for name, config := range c.Okta {
			if !yield(name, NewOktaBuilder(config, okta.Name, okta.ID, okta.Version)) {
				return
			}
		}
		for name, config := range c.Mimecast {
			if !yield(name, NewMimecastBuilder(config, mimecast.Name, mimecast.ID, mimecast.Version)) {
				return
			}
		}
	}
}
