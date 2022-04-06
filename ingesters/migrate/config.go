/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

const (
	MAX_CONFIG_SIZE int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
)

var (
	ErrInvalidStateStoreLocation         = errors.New("Empty state storage location")
	ErrTimestampDelimiterMissingOverride = errors.New("Timestamp delimiting requires a defined timestamp override")
)

type bindType int
type readerType int

type cfgReadType struct {
	Global       global
	Files        map[string]*files
	Preprocessor processors.ProcessorConfig
	TimeFormat   config.CustomTimeFormat
}

type global struct {
	config.IngestConfig
	State_Store_Location string
}

type cfgType struct {
	global
	Files        map[string]*files
	Preprocessor processors.ProcessorConfig
	TimeFormat   config.CustomTimeFormat
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var cr cfgReadType
	if err := config.LoadConfigFile(&cr, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&cr, overlayPath); err != nil {
		return nil, err
	}
	c := &cfgType{
		global:       cr.Global,
		Files:        cr.Files,
		Preprocessor: cr.Preprocessor,
		TimeFormat:   cr.TimeFormat,
	}
	if err := verifyConfig(c); err != nil {
		return nil, err
	}
	// Verify and set UUID
	if _, ok := c.IngesterUUID(); !ok {
		id := uuid.New()
		if err := c.SetIngesterUUID(id, path); err != nil {
			return nil, err
		}
		if id2, ok := c.IngesterUUID(); !ok || id != id2 {
			return nil, errors.New("Failed to set a new ingester UUID")
		}
	}
	return c, nil
}

func verifyConfig(c *cfgType) error {
	//verify the global parameters
	if err := c.Verify(); err != nil {
		return err
	}
	if len(c.Files) == 0 {
		return errors.New("No Filess specified")
	}
	if err := c.Preprocessor.Validate(); err != nil {
		return err
	} else if err = c.TimeFormat.Validate(); err != nil {
		return err
	}
	for k, v := range c.Files {
		if err := v.Validate(c.Preprocessor); err != nil {
			return fmt.Errorf("Files config %s failed %w", k, err)
		}
	}
	return nil
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)
	for _, v := range c.Files {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
	}
	if len(tags) == 0 {
		return nil, errors.New("No tags specified")
	}
	sort.Strings(tags)
	return tags, nil
}

func (g *global) Verify() (err error) {
	if err = g.IngestConfig.Verify(); err != nil {
		return
	}
	return
}

func (g *global) StatePath() string {
	return g.State_Store_Location
}
