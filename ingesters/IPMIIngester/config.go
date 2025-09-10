/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
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

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/attach"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/processors"
)

type ipmi struct {
	Tag_Name          string
	Target            []string
	Username          string
	Password          string
	Preprocessor      []string
	Source_Override   string
	Rate              int
	Ignore_Timestamps bool //Just apply the current timestamp to lines as we get them
}

type cfgType struct {
	Global       config.IngestConfig
	Attach       attach.AttachConfig
	IPMI         map[string]*ipmi
	Preprocessor processors.ProcessorConfig
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var c cfgType
	if err := config.LoadConfigFile(&c, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&c, overlayPath); err != nil {
		return nil, err
	}

	if err := c.Verify(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *cfgType) Verify() error {
	//verify the global parameters
	if err := c.Global.Verify(); err != nil {
		return err
	} else if err = c.Attach.Verify(); err != nil {
		return err
	}

	if len(c.IPMI) == 0 {
		return errors.New("No IPMI targets specified")
	}

	if err := c.Preprocessor.Validate(); err != nil {
		return err
	}

	for k, v := range c.IPMI {
		if v.Rate == 0 {
			v.Rate = 60
		}

		if len(v.Tag_Name) == 0 {
			v.Tag_Name = entry.DefaultTagName
		}
		if ingest.CheckTag(v.Tag_Name) != nil {
			return errors.New("Invalid characters in the Tag-Name for " + k)
		}

		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Listener %s preprocessor invalid: %v", k, err)
		}
	}

	return nil
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)

	for _, v := range c.IPMI {
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

func (c *cfgType) IngestBaseConfig() config.IngestConfig {
	return c.Global
}

func (c *cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
}
