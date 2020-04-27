/*************************************************************************
 * Copyright 2020 Gravwell, Inc. All rights reserved.
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
	"github.com/gravwell/ingest/v3"
	"github.com/gravwell/ingest/v3/config"
	"github.com/gravwell/ingest/v3/processors"
)

const (
	MAX_CONFIG_SIZE int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
)

type stenographer struct {
	base
	Tag_Name     string
	URL          string
	CA_Cert      string
	Client_Cert  string
	Client_Key   string
	Preprocessor []string
}

type base struct {
	Ignore_Timestamps         bool //Just apply the current timestamp to lines as we get them
	Assume_Local_Timezone     bool
	Timezone_Override         string
	Source_Override           string
	Timestamp_Format_Override string //override the timestamp format
}

type global struct {
	config.IngestConfig
	Listen_Address string
	Server_Cert    string
	Server_Key     string
}

type cfgType struct {
	Global       global
	Stenographer map[string]*stenographer
	Preprocessor processors.ProcessorConfig
}

func GetConfig(path string) (*cfgType, error) {
	var c cfgType
	if err := config.LoadConfigFile(&c, path); err != nil {
		return nil, err
	}

	if err := verifyConfig(&c); err != nil {
		return nil, err
	}

	// Verify and set UUID
	if _, ok := c.Global.IngesterUUID(); !ok {
		id := uuid.New()
		if err := c.Global.SetIngesterUUID(id, path); err != nil {
			return nil, err
		}
		if id2, ok := c.Global.IngesterUUID(); !ok || id != id2 {
			return nil, errors.New("Failed to set a new ingester UUID")
		}
	}
	return &c, nil
}

func verifyConfig(c *cfgType) error {
	//verify the global parameters
	if err := c.Global.Verify(); err != nil {
		return err
	}

	if len(c.Stenographer) == 0 {
		return errors.New("No stenographer connections specified")
	}

	if err := c.Preprocessor.Validate(); err != nil {
		return err
	}

	if c.Global.Listen_Address == "" {
		return fmt.Errorf("config must provide Listen-Address")
	}

	for k, v := range c.Stenographer {
		if len(v.Tag_Name) == 0 {
			v.Tag_Name = `default`
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

		if v.URL == "" {
			return fmt.Errorf("%s must provide URL", k)
		}
		if v.CA_Cert == "" {
			return fmt.Errorf("%s must provide CA-Cert", k)
		}
		if v.Client_Cert == "" {
			return fmt.Errorf("%s must provide Client-Cert", k)
		}
		if v.Client_Key == "" {
			return fmt.Errorf("%s must provide Client-Key", k)
		}
	}

	return nil
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)

	for _, v := range c.Stenographer {
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
