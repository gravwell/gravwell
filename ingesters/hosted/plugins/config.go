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
)

type Configs struct {
	Okta map[string]*okta.Config
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
	return
}

// IngesterCount returns the number of ingesters configured
func (c Configs) IngesterCount() (count int) {
	count += len(c.Okta)
	return
}

type NewIngesterCallback func(id, name string, runner hosted.Runner) error
type NewRuntimeCallback func(id, name string, ingesterUUID uuid.UUID) (hosted.Runtime, error)

func (c Configs) ForEachIngester(tn hosted.TagNegotiator, nrt NewRuntimeCallback, cb NewIngesterCallback) (err error) {
	// do native Okta first
	for k, v := range c.Okta {
		// this shouldn't happen, but scream about it anyway
		if v == nil {
			err = fmt.Errorf("okta ingester %q has a nil config", k)
			return
		}
		// get a new ingester
		var ig *okta.OktaIngester
		if ig, err = okta.NewOktaIngester(*v, tn); err != nil {
			err = fmt.Errorf("failed to create new okta ingester %w", err)
			continue
		}

		//create a new runtime for this ingester
		var rt hosted.Runtime
		if rt, err = nrt(`okta`, k, v.UUID()); err != nil {
			err = fmt.Errorf("failed to create new runtime for okta ingester %q: %w", k, err)
			return
		}

		// create a new hosted native runner
		var runner *hosted.NativeRunner
		if runner, err = hosted.NewNativeRunner(okta.ID, k, okta.Version, v.UUID(), ig, rt); err != nil {
			err = fmt.Errorf("failed to create new native %s runner %w", `okta`, err)
			runner = nil
			return
		}

		// create the okta ingester
		// ask for for the runtime associated with this ingester uuid
		// create new native runner because Okta is native
		if err = cb(okta.Name, k, runner); err != nil {
			return err
		}
	}
	return
}
