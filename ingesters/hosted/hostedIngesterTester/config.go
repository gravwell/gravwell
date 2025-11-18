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
	config.IngestConfig
	Attach attach.AttachConfig
	State  hosted.StateConfig
	Okta   map[string]okta.Config
}

func (c cfgType) Verify() (err error) {
	if err = c.IngestConfig.Verify(); err != nil {
		return
	} else if err = c.Attach.Verify(); err != nil {
		return
	} else if err = c.State.Verify(); err != nil {
		return
	}

	for k, v := range c.Okta {
		if err = v.Verify(); err != nil {
			err = fmt.Errorf("Okta config %q failed validation %w", k, err)
			return
		}
	}
	return
}

func (c cfgType) IngesterCount() (r int) {
	r = len(c.Okta)
	return
}
