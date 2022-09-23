/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
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
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
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
	Splunk       map[string]*splunk
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
	Splunk       map[string]*splunk
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
		Splunk:       cr.Splunk,
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
	if len(c.Files) == 0 && len(c.Splunk) == 0 {
		return errors.New("No Files or Splunk stanzas specified")
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
	for k, v := range c.Splunk {
		if err := v.Validate(c.Preprocessor); err != nil {
			return fmt.Errorf("Splunk config %s failed %w", k, err)
		}
	}
	return nil
}

// Tags returns a list of tags specified in the config.
// It will always include the gravwell tag.
func (c *cfgType) Tags() ([]string, error) {
	tags := []string{entry.GravwellTagName}
	tagMp := make(map[string]bool, 1)
	tagMp[entry.GravwellTagName] = true
	for _, v := range c.Files {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
	}
	for _, v := range c.Splunk {
		if tgs, err := v.Tags(); err != nil {
			return tags, err
		} else {
			for _, tag := range tgs {
				tags = append(tags, tag)
				tagMp[tag] = true
			}
		}
	}
	sort.Strings(tags)
	return tags, nil
}

func (c *cfgType) getSplunkConfig(splunkName string) (s splunk, err error) {
	if sp, ok := c.Splunk[splunkName]; !ok || sp == nil {
		err = errors.New("Not found")
	} else {
		s = *sp
	}
	return
}

func (c *cfgType) getSplunkConn(splunkName string) (sc splunkConn, err error) {
	for k, vv := range c.Splunk {
		if k == splunkName {
			// Connect to Splunk server
			sc = newSplunkConn(vv.Server, vv.Token)
			return
		}
	}
	err = errors.New("Not found")
	return
}

func (c *cfgType) getSplunkPreprocessors(splunkName string, igst *ingest.IngestMuxer) (pproc *processors.ProcessorSet, err error) {
	for k, vv := range c.Splunk {
		if k == splunkName {
			// get the ingester up and rolling
			pproc, err = c.Preprocessor.ProcessorSet(igst, vv.Preprocessor)
			return
		}
	}
	err = errors.New("Not found")
	return
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
