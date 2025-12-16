/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"

	// include all the native hosted ingesters
	"github.com/gravwell/gravwell/v3/ingesters/hosted"
	"github.com/gravwell/gravwell/v3/ingesters/hosted/okta"
)

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var cr cfgType
	if err := config.LoadConfigFile(&cr, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&cr, overlayPath); err != nil {
		return nil, err
	}
	if err := cr.Verify(); err != nil {
		return nil, err
	}
	return &cr, nil
}

type cfgType struct {
	Global          config.IngestConfig
	Attach          attach.AttachConfig
	State           hosted.StateConfig
	ingesterConfigs // embed the type so we can abstract the startup more easily
}

type ingesterConfigs struct {
	Okta map[string]*okta.Config
}

func (c cfgType) Verify() (err error) {
	if err = c.Global.Verify(); err != nil {
		return
	} else if err = c.Attach.Verify(); err != nil {
		return
	} else if err = c.State.Verify(); err != nil {
		return
	}

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

// implement the required interface for ingest config
func (c cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
}

// Tags implements the required interface for base.cfgHelper which is used during startup
func (c cfgType) Tags() (tags []string, err error) {
	if len(c.Okta) > 0 {
		tags = append(tags, okta.Tags...)
	}
	return
}

// IngesterBaseConfig implements the required interface for base.cfgHelper which is used during startup
func (c cfgType) IngestBaseConfig() config.IngestConfig {
	return c.Global
}

func (ic ingesterConfigs) IngesterCount() (r int) {
	r = len(ic.Okta)
	return
}

type newIngesterCallback func(id, name string, runner hosted.Runner) error
type newRuntime func(id, name string, ingesterUUID uuid.UUID) (hosted.Runtime, error)

func (ic ingesterConfigs) forEachIngester(tn hosted.TagNegotiator, nrt newRuntime, cb newIngesterCallback) (err error) {
	if tn == nil {
		err = fmt.Errorf("nil tag negotiator")
		return
	} else if nrt == nil {
		err = fmt.Errorf("nil new runtime function")
		return
	} else if cb == nil {
		err = fmt.Errorf("nil new ingester callback")
		return
	}
	// do native Okta first
	for k, v := range ic.Okta {
		// this shouldn't happen, but scream about it anyway
		if v == nil {
			err = fmt.Errorf("okta ingester %q has a nil config", k)
			return
		}
		// get a new ingester
		var ig *okta.OktaIngester
		var runner *hosted.NativeRunner
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
		if runner, err = hosted.NewNativeRunner(okta.ID, k, okta.Version, v.UUID(), ig, rt); err != nil {
			err = fmt.Errorf("failed to create new native %s runner %w", `okta`, err)
			runner = nil
			return
		}

		// create the okta ingester
		// ask for for the runtime associated with this ingester uuid
		// create new native runner because Okta is native
		if err = cb(`okta`, k, runner); err != nil {
			return err
		}
	}
	return
}
