/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingesters/hosted/storage"

	"github.com/gravwell/gravwell/v3/ingesters/hosted/plugins"
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
	Global config.IngestConfig
	Attach attach.AttachConfig
	// State is not as abstract as it should be, but making that change should have minimal impact on end users.
	// Given the size of storage.BoltConfig we only need to share a few keys on any new implementation.
	State           storage.BoltConfig
	plugins.Configs // embed the type so we can abstract the startup more easily
}

func (c cfgType) Verify() (err error) {
	if err = c.Global.Verify(); err != nil {
		return
	} else if err = c.Attach.Verify(); err != nil {
		return
	} else if err = c.State.Verify(); err != nil {
		return
	} else if err = c.Configs.Verify(); err != nil {
		return
	}

	return
}

// implement the required interface for ingest config
func (c cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
}

// IngesterBaseConfig implements the required interface for base.cfgHelper which is used during startup
func (c cfgType) IngestBaseConfig() config.IngestConfig {
	return c.Global
}
