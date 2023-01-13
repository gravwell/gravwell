/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
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
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

const (
	MAX_CONFIG_SIZE int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
)

type queue struct {
	base
	Tag_Name     string
	Queue_URL    string
	Region       string
	AKID         string
	Secret       string `json:"-"` // DO NOT send this when marshalling
	Preprocessor []string
}

type base struct {
	Ignore_Timestamps         bool //Just apply the current timestamp to lines as we get them
	Assume_Local_Timezone     bool
	Timezone_Override         string
	Source_Override           string
	Timestamp_Format_Override string //override the timestamp format
}

type cfgReadType struct {
	Global       config.IngestConfig
	Queue        map[string]*queue
	Preprocessor processors.ProcessorConfig
}

type cfgType struct {
	config.IngestConfig
	Queue        map[string]*queue
	Preprocessor processors.ProcessorConfig
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	//read into the intermediary type to maintain backwards compatibility with the old system
	var cr cfgReadType
	if err := config.LoadConfigFile(&cr, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&cr, overlayPath); err != nil {
		return nil, err
	}
	c := &cfgType{
		IngestConfig: cr.Global,
		Queue:        cr.Queue,
		Preprocessor: cr.Preprocessor,
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

	if len(c.Queue) == 0 {
		return errors.New("No queues specified")
	}

	if err := c.Preprocessor.Validate(); err != nil {
		return err
	}

	for k, v := range c.Queue {
		if len(v.Tag_Name) == 0 {
			v.Tag_Name = entry.DefaultTagName
		}
		if strings.ContainsAny(v.Tag_Name, ingest.FORBIDDEN_TAG_SET) {
			return errors.New("Invalid characters in the Tag-Name for " + k)
		}
		if v.Timezone_Override != "" {
			if v.Assume_Local_Timezone {
				// cannot do both
				return fmt.Errorf("Cannot specify Assume-Local-Timezone and Timezone-Override in the same listener %v", k)
			}
			if _, err := time.LoadLocation(v.Timezone_Override); err != nil {
				return fmt.Errorf("Invalid timezone override %v in listener %v: %v", v.Timezone_Override, k, err)
			}
		}

		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Listener %s preprocessor invalid: %v", k, err)
		}

		if v.Queue_URL == "" {
			return fmt.Errorf("Queue %s must provide Queue-URL", k)
		}
		if v.Region == "" {
			return fmt.Errorf("Queue %s must provide Region", k)
		}
		if v.AKID == "" {
			return fmt.Errorf("Queue %s must provide AKID", k)
		}
		if v.Secret == "" {
			return fmt.Errorf("Queue %s must provide Secret", k)
		}
	}

	return nil
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)

	for _, v := range c.Queue {
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
