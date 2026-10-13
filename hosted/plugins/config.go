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
	"reflect"
	"uuid"

	"github.com/gravwell/gravwell/v4/hosted"

	// include all the native hosted ingesters
	"github.com/gravwell/gravwell/v4/hosted/plugins/jamf"
	"github.com/gravwell/gravwell/v4/hosted/plugins/mimecast"
	"github.com/gravwell/gravwell/v4/hosted/plugins/msgraph"
	"github.com/gravwell/gravwell/v4/hosted/plugins/okta"
	"github.com/gravwell/gravwell/v4/hosted/plugins/sqs"
	"github.com/gravwell/gravwell/v4/hosted/plugins/tester"
	"github.com/gravwell/gravwell/v4/hosted/plugins/wiz"
)

type Configs struct {
	Okta     map[string]*okta.Config
	Mimecast map[string]*mimecast.Config
	MSGraph  map[string]*msgraph.Config
	Tester   map[string]*tester.Config
	Jamf     map[string]*jamf.Config
	Wiz      map[string]*wiz.Config
	SQS      map[string]*sqs.Config
}

// Verify ensures that the plugin configs are valid
func (c Configs) Verify() (err error) {
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
	for k, v := range c.MSGraph {
		if v == nil {
			err = fmt.Errorf("ms graph config %q is nil", k)
			return
		}
		if err = v.Verify(); err != nil {
			err = fmt.Errorf("config %q failed validation: %w", k, err)
			return
		}
	}
	for k, v := range c.Tester {
		if v == nil {
			err = fmt.Errorf("Tester config %q is nil", k)
			return
		}
		if err = v.Verify(); err != nil {
			err = fmt.Errorf("Tester config %q failed validation %w", k, err)
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
	for k, v := range c.Jamf {
		if v == nil {
			err = fmt.Errorf("Jamf config %q is nil", k)
			return
		}
		if err = v.Verify(); err != nil {
			err = fmt.Errorf("Jamf config %q failed validation %w", k, err)
			return
		}
	}
	for k, v := range c.Wiz {
		if v == nil {
			err = fmt.Errorf("Wiz config %q is nil", k)
			return
		}
		if err = v.Verify(); err != nil {
			err = fmt.Errorf("Wiz config %q failed validation %w", k, err)
			return
		}
	}
	for k, v := range c.SQS {
		if v == nil {
			err = fmt.Errorf("SQS config %q is nil", k)
			return
		}
		if err = v.Verify(); err != nil {
			err = fmt.Errorf("SQS config %q failed validation: %w", k, err)
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
	for _, v := range c.Tester {
		tags = append(tags, v.Tags()...)
	}
	for _, v := range c.Mimecast {
		tags = append(tags, v.Tags()...)
	}
	for _, v := range c.Jamf {
		tags = append(tags, v.Tags()...)
	}
	for _, v := range c.Wiz {
		tags = append(tags, v.Tags()...)
	}
	for _, v := range c.MSGraph {
		tags = append(tags, v.Tags()...)
	}
	for _, v := range c.SQS {
		tags = append(tags, v.Tags()...)
	}
	return
}

// IngesterCount returns the number of ingesters configured.
//
// Derived from Configs by reflection for the same reason Kinds is: a plugin added to
// Configs is counted with no further work.  A hand written sum that forgets a plugin
// reports zero configured ingesters and the runner refuses to start.  A member that is
// not a map is skipped rather than reported, Kinds already rejects that shape and it runs
// before anything asks for a count.
func (c Configs) IngesterCount() (count int) {
	rv := reflect.ValueOf(c)
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		if f := rt.Field(i); f.IsExported() && f.Type.Kind() == reflect.Map {
			count += rv.Field(i).Len()
		}
	}
	return
}

type IngesterBuilder interface {
	UUID() uuid.UUID
	Kind() string
	ID() string
	Version() string
	Build(hosted.TagNegotiator, func() error) (hosted.Ingester, error)
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
		for name, config := range c.Jamf {
			if !yield(name, NewJamfBuilder(config, jamf.Name, jamf.ID, jamf.Version)) {
				return
			}
		}
		for name, config := range c.Wiz {
			if !yield(name, NewWizBuilder(config, wiz.Name, wiz.ID, wiz.Version)) {
				return
			}
		}
		for name, config := range c.MSGraph {
			if !yield(name, NewMSGraphBuilder(config, msgraph.Name, msgraph.ID, msgraph.Version)) {
				return
			}
		}
		for name, config := range c.SQS {
			if !yield(name, NewSQSBuilder(config, sqs.Name, sqs.ID, sqs.Version)) {
				return
			}
		}
	}
}

// PluginKind describes a plugin config type so that it can be advertised for dynamic
// configuration.  Kind is the INI section name, which is what ties a config generated by
// the dynamic system back to a member of Configs.  Config is a zero valued config of that
// plugin's type, it is enumerated as a type and never read as data.  Singleton is taken
// from the config type when it implements SingletonConfig and is false otherwise.
type PluginKind struct {
	Kind      string
	Singleton bool
	Config    any
}

// Kinds returns the config type of every plugin so that a dynamic config manager can
// advertise what this binary is able to run.
//
// The list is derived from Configs by reflection rather than written out by hand.  Configs
// is the one place a plugin has to be wired in for its config to be readable at all, so
// deriving from it means a new plugin is advertised dynamically with no further work and
// the two can never drift apart.  It also guarantees the Kind matches the member a
// generated config has to land in, they are the same name.
func Kinds() (ks []PluginKind, err error) {
	rt := reflect.TypeOf(Configs{})
	ks = make([]PluginKind, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue // not config, gcfg cannot reach it either
		}
		// every plugin is carried as map[string]*Config, a map is also what says that
		// more than one of a kind can be configured at a time
		if f.Type.Kind() != reflect.Map || f.Type.Key().Kind() != reflect.String {
			err = fmt.Errorf("Configs member %s is a %s, every plugin must be a map[string]*Config", f.Name, f.Type)
			return
		}
		et := f.Type.Elem()
		for et.Kind() == reflect.Pointer {
			et = et.Elem()
		}
		if et.Kind() != reflect.Struct {
			err = fmt.Errorf("Configs member %s holds a %s, every plugin config must be a struct", f.Name, et)
			return
		}
		cfg := reflect.New(et) // a zero valued config of that type
		pk := PluginKind{Kind: f.Name, Config: cfg.Elem().Interface()}
		// a plugin that may only run once says so on its config type, the way it
		// describes everything else about itself
		if sc, ok := cfg.Interface().(SingletonConfig); ok {
			pk.Singleton = sc.Singleton()
		}
		ks = append(ks, pk)
	}
	return
}

// SingletonConfig is an OPTIONAL interface a plugin config may implement to say that only
// one runner of its kind may be configured at a time.  Kinds asks for it on a pointer to
// the zero config, the way plugin configs implement everything else.
type SingletonConfig interface {
	Singleton() bool
}
