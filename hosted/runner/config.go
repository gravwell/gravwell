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
	"sort"

	"github.com/gravwell/gravwell/v3/hosted/plugins"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
)

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var cr cfgReadType
	if err := config.LoadConfigFile(&cr, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&cr, overlayPath); err != nil {
		return nil, err
	}
	if err := cr.Verify(); err != nil {
		return nil, err
	}

	return &cfgType{
		IngestConfig: cr.Global,
		Attach:       cr.Attach,
		State:        cr.State,
		Configs:      cr.Configs,
	}, nil
}

type cfgReadType struct {
	Global config.IngestConfig
	Attach attach.AttachConfig
	// State is not as abstract as it should be, but making that change should have minimal impact on end users.
	// Given the size of storage.BoltConfig we only need to share a few keys on any new implementation.
	State           storage.BoltConfig
	plugins.Configs // embed the type so we can abstract the startup more easily
}

// cfgType is the main type that we hand around, this embeds the IngestConfig
// this ended up being a convention that the GUI and webservers rely on when reporting
// ingesters states, so we have a type that we can read and one that we actually use
type cfgType struct {
	config.IngestConfig
	Attach attach.AttachConfig
	// State is not as abstract as it should be, but making that change should have minimal impact on end users.
	// Given the size of storage.BoltConfig we only need to share a few keys on any new implementation.
	State           storage.BoltConfig
	plugins.Configs // embed the type so we can abstract the startup more easily
}

func (c cfgType) Verify() (err error) {
	if err = c.IngestConfig.Verify(); err != nil {
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
	return c.IngestConfig
}

// Tags implements the required interface for base.cfgHelper which is used during startup.
// Every tag is sourced from the configured plugins, so we just gather them up, validate,
// and de-duplicate them so that the muxer can negotiate each tag exactly once.
func (c cfgType) Tags() (tags []string, err error) {
	var ptags []string
	if ptags, err = c.Configs.Tags(); err != nil {
		return
	}
	tagMp := make(map[string]bool, len(ptags))
	for _, tag := range ptags {
		if err = ingest.CheckTag(tag); err != nil {
			err = fmt.Errorf("invalid tag %q: %w", tag, err)
			return
		} else if tagMp[tag] {
			continue
		}
		tagMp[tag] = true
		tags = append(tags, tag)
	}
	if len(tags) > 0 {
		sort.Strings(tags)
	}
	return
}
