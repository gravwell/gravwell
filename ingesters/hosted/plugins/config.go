/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package plugins

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingesters/hosted"

	// include all the native hosted ingesters
	"github.com/gravwell/gravwell/v3/ingesters/hosted/plugins/okta"
	"github.com/gravwell/gravwell/v3/ingesters/hosted/plugins/tester"
)

type Configs struct {
	Okta   map[string]*okta.Config
	Tester map[string]*tester.Config
}

// Verify ensures that the plugin configs are valid
func (c Configs) Verify() (err error) {
	// verify Okta configs
	for k, v := range c.Okta {
		if v != nil {
			if err = v.Verify(); err != nil {
				err = fmt.Errorf("Okta config %q failed validation %w", k, err)
				return
			}
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
	return
}

// IngesterCount returns the number of ingesters configured
func (c Configs) IngesterCount() (count int) {
	count += len(c.Okta) + len(c.Tester)
	return
}

type NewIngesterCallback func(id, name string, runner hosted.Runner) error
type NewRuntimeCallback func(id, name string, ingesterUUID uuid.UUID) (hosted.Runtime, error)

func (c Configs) ForEachIngester(tn hosted.TagNegotiator, nrt NewRuntimeCallback, cb NewIngesterCallback) (err error) {
	// native tester
	for k, v := range c.Tester {
		// this shouldn't happen, but scream about it anyway
		if v == nil {
			err = fmt.Errorf("%v ingester %q has a nil config", tester.ID, k)
			return
		}
		// get a new ingester
		var ig *tester.TesterIngester
		if ig, err = tester.NewTesterIngester(*v, tn); err != nil {
			err = fmt.Errorf("failed to create new %q ingester %q: %w", tester.ID, k, err)
			continue
		}
		if err = c.buildIngester(k, tester.ID, tester.Name, tester.Version, v.UUID(), ig, nrt, cb); err != nil {
			return
		}
	}
	// native Okta
	for k, v := range c.Okta {
		// this shouldn't happen, but scream about it anyway
		if v == nil {
			err = fmt.Errorf("%v ingester %q has a nil config", okta.ID, k)
			return
		}
		// get a new ingester
		var ig *okta.OktaIngester
		if ig, err = okta.NewOktaIngester(*v, tn); err != nil {
			err = fmt.Errorf("failed to create new %q ingester %q: %w", okta.ID, k, err)
			continue
		}
		if err = c.buildIngester(k, okta.ID, okta.Name, okta.Version, v.UUID(), ig, nrt, cb); err != nil {
			return
		}
	}
	return
}

func (c Configs) buildIngester(name, id, kind, ver string, ingesterUUID uuid.UUID, ig hosted.Ingester, nrt NewRuntimeCallback, cb NewIngesterCallback) (err error) {
	//create a new runtime for this ingester
	var rt hosted.Runtime
	if rt, err = nrt(kind, name, ingesterUUID); err != nil {
		err = fmt.Errorf("failed to create new runtime for %s ingester %q: %w", kind, name, err)
		return
	}
	// create a new hosted native runner
	var runner *hosted.NativeRunner
	if runner, err = hosted.NewNativeRunner(id, name, ver, ingesterUUID, ig, rt); err != nil {
		err = fmt.Errorf("failed to create new native %s runner %w", kind, err)
		runner = nil
		return
	}

	// create the ingester and ask for for the runtime associated with this ingester uuid
	err = cb(kind, name, runner)
	return
}
