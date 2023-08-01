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
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	MAX_CONFIG_SIZE int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
	nfv5Type              = iota
	ipfixType             = iota

	nfv5Name  string = `netflowv5`
	ipfixName string = `ipfix`
)

var ()

type flowType int

type collector struct {
	Bind_String           string //IP port pair 127.0.0.1:1234
	Tag_Name              string
	Assume_Local_Timezone bool
	Ignore_Timestamps     bool
	Flow_Type             string
	Session_Dump_Enabled  bool
}

type cfgReadType struct {
	Global    config.IngestConfig
	Attach    attach.AttachConfig
	Collector map[string]*collector
}

type cfgType struct {
	config.IngestConfig
	Attach    attach.AttachConfig
	Collector map[string]*collector
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
		Attach:       cr.Attach,
		Collector:    cr.Collector,
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
	} else if c.Attach.Verify(); err != nil {
		return err
	}
	if len(c.Collector) == 0 {
		return errors.New("No collectors specified")
	}
	bindMp := make(map[string]string, 1)
	for k, v := range c.Collector {
		if len(v.Bind_String) == 0 {
			return errors.New("No Bind-String provided for " + k)
		}
		if len(v.Tag_Name) == 0 {
			v.Tag_Name = entry.DefaultTagName
		}
		if ingest.CheckTag(v.Tag_Name) != nil {
			return errors.New("Invalid characters in the Tag-Name for " + k)
		}
		if n, ok := bindMp[v.Bind_String]; ok {
			return errors.New("Bind-String for " + k + " already in use by " + n)
		}
		bindMp[v.Bind_String] = k
	}
	return nil
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)
	for _, v := range c.Collector {
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
	return c.IngestConfig
}

func (c *cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
}

func (ft flowType) String() string {
	switch ft {
	case nfv5Type:
		return "Netflow V5"
	case ipfixType:
		return "IPFIX"
	}
	return "unknown"
}

func translateFlowType(s string) (flowType, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case ``:
		fallthrough //default is netflow v5
	case `nfv5`: //nfv5Name shortcut
		fallthrough
	case nfv5Name:
		return nfv5Type, nil
	case ipfixName:
		return ipfixType, nil
	}
	return -1, errors.New("invalid reader type")
}
