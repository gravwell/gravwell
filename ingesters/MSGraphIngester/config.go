/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

const (
	MAX_CONFIG_SIZE int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
)

type bindType int
type readerType int

type global struct {
	config.IngestConfig
	State_Store_Location string
	Client_ID            string
	Client_Secret        string `json:"-"` // DO NOT send this when marshalling
	Directory_ID         string
	Tenant_Domain        string
}

type contentType struct {
	Tag_Name     string
	Content_Type string

	Assume_Local_Timezone bool
	Timezone_Override     string
	Ignore_Timestamps     bool

	Preprocessor []string
}

type cfgType struct {
	Global       global
	Attach       attach.AttachConfig
	ContentType  map[string]*contentType
	Preprocessor processors.ProcessorConfig
	TimeFormat   config.CustomTimeFormat
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

func (c cfgType) Verify() error {
	if err := c.Global.Verify(); err != nil {
		return err
	} else if err = c.Attach.Verify(); err != nil {
		return err
	}

	if to, err := c.parseTimeout(); err != nil || to < 0 {
		if err != nil {
			return err
		}
		return errors.New("Invalid connection timeout")
	}
	if c.Global.Ingest_Secret == "" {
		return errors.New("Ingest-Secret not specified")
	}
	//ensure there is at least one target
	connCount := len(c.Global.Cleartext_Backend_Target) +
		len(c.Global.Encrypted_Backend_Target) +
		len(c.Global.Pipe_Backend_Target)
	if connCount == 0 {
		return errors.New("No backend targets specified")
	}
	if len(c.ContentType) == 0 {
		return errors.New("At least one content type required.")
	}
	if err := c.Preprocessor.Validate(); err != nil {
		return err
	} else if err = c.TimeFormat.Validate(); err != nil {
		return err
	}
	for k, v := range c.ContentType {
		if v == nil {
			return fmt.Errorf("Content Type %v config is nil", k)
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Content Type %s preprocessor %s error: %v", k, v.Preprocessor, err)
		}
	}
	return nil
}

func (c *cfgType) Targets() ([]string, error) {
	return c.Global.Targets()
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)
	for _, v := range c.ContentType {
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
	return tags, nil
}

func (c *cfgType) IngestBaseConfig() config.IngestConfig {
	return c.Global.IngestConfig
}

func (c *cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
}

func (c *cfgType) ContentTypes() (ret []string) {
	for _, v := range c.ContentType {
		ret = append(ret, v.Content_Type)
	}
	return
}

func (c *cfgType) VerifyRemote() bool {
	return c.Global.Verify_Remote_Certificates
}

func (c *cfgType) Timeout() time.Duration {
	if tos, _ := c.parseTimeout(); tos > 0 {
		return tos
	}
	return 0
}

func (c *cfgType) Secret() string {
	return c.Global.Ingest_Secret
}

func (c *cfgType) LogLevel() string {
	return c.Global.Log_Level
}

func (c *cfgType) parseTimeout() (time.Duration, error) {
	tos := strings.TrimSpace(c.Global.Connection_Timeout)
	if len(tos) == 0 {
		return 0, nil
	}
	return time.ParseDuration(tos)
}
