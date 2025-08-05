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
	"strings"
	"time"

	eventhubs "github.com/Azure/azure-event-hubs-go/v3"
	"github.com/gravwell/gravwell/v4/ingest/attach"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/processors"
)

const (
	MAX_CONFIG_SIZE   int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
	defaultStateStore       = `/opt/gravwell/etc/azure_event_hubs.state`
	defaultLogFile          = `/opt/gravwell/log/azure_event_hubs.log`
	defaultCheckpoint       = `start`
)

type bindType int
type readerType int

type global struct {
	config.IngestConfig
	State_Store_Location string
}

type eventHubConf struct {
	Event_Hubs_Namespace  string
	Event_Hub             string
	Consumer_Group        string // defaults to "$Default"
	Token_Name            string
	Token_Key             string `json:"-"` // DO NOT send this when marshalling
	Initial_Checkpoint    string // "start" or "end" of stream, defaults to "start"
	Tag_Name              string
	Assume_Local_Timezone bool
	Timezone_Override     string
	Parse_Time            bool
	Preprocessor          []string
}

type cfgType struct {
	Global       global
	Attach       attach.AttachConfig
	EventHub     map[string]*eventHubConf
	Preprocessor processors.ProcessorConfig
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var c cfgType
	if err := config.LoadConfigFile(&c, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&c, overlayPath); err != nil {
		return nil, err
	}
	//initialize the state store location if its empty
	if c.Global.State_Store_Location == `` {
		c.Global.State_Store_Location = defaultStateStore
	}
	if c.Global.Log_File == `` {
		c.Global.Log_File = defaultLogFile
	}
	return &c, nil
}

func (c cfgType) Verify() error {
	if err := c.Global.IngestConfig.Verify(); err != nil {
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
	if len(c.EventHub) == 0 {
		return errors.New("at least one EventHub definition is required")
	}
	if err := c.Preprocessor.Validate(); err != nil {
		return err
	}
	for k, v := range c.EventHub {
		if v == nil {
			return fmt.Errorf("EventHub stream %v config is nil", k)
		}
		if v.Event_Hubs_Namespace == "" {
			return fmt.Errorf("EventHub config %v Event-Hubs-Namespace parameter is empty", k)
		}
		if v.Event_Hub == "" {
			return fmt.Errorf("EventHub config %v Event-Hub parameter is empty", k)
		}
		if v.Token_Name == "" {
			return fmt.Errorf("EventHub config %v Token-Name parameter is empty", k)
		}
		if v.Token_Key == "" {
			return fmt.Errorf("EventHub config %v Token-Key parameter is empty", k)
		}
		if v.Consumer_Group == "" {
			c.EventHub[k].Consumer_Group = eventhubs.DefaultConsumerGroup
		}
		if v.Initial_Checkpoint == "" {
			c.EventHub[k].Initial_Checkpoint = defaultCheckpoint
		}
		switch v.Initial_Checkpoint {
		case "start":
		case "end":
		default:
			return fmt.Errorf(`Invalid Starting-Checkpoint %s for stream %s, must be "start" or "end"`, v.Initial_Checkpoint, k)
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("EventHub stream %s preprocessor invalid: %v", k, err)
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
	for _, v := range c.EventHub {
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
