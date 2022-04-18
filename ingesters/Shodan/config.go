/*************************************************************************
 *
 * Gravwell - "Consume all the things!"
 *
 * ________________________________________________________________________
 *
 * Copyright 2019 - All Rights Reserved
 * Gravwell Inc <legal@gravwell.io>
 * ________________________________________________________________________
 *
 * NOTICE:  This code is part of the Gravwell project and may not be shared,
 * published, sold, or otherwise distributed in any from without the express
 * written consent of its owners.
 *
 **************************************************************************/

package main

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest/config"
)

const (
	MAX_CONFIG_SIZE int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
)

type bindType int
type readerType int

type cfgType struct {
	Global struct {
		config.IngestConfig
		State_Store_Location string
		Batching             bool
		Label                string
	}
	ShodanAccount map[string]*struct {
		API_Key             string `json:"-"`
		Tag_Name            string
		Module_Tags_Prefix  string
		Extracted_Modules   []string
		Extract_All_Modules bool
		Full_Firehose       bool
	}
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var c cfgType
	if err := config.LoadConfigFile(&c, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&c, overlayPath); err != nil {
		return nil, err
	}
	if err := verifyConfig(c); err != nil {
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

func verifyConfig(c cfgType) error {
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
	if len(c.ShodanAccount) == 0 {
		return errors.New("At least one Shodan account required.")
	}

	for _, acct := range c.ShodanAccount {
		if acct.Tag_Name == `` && acct.Module_Tags_Prefix == `` {
			return errors.New("Shodan accounts must specify either a Tag-Name or a Module-Tags-Prefix")
		}
		if acct.Tag_Name == `` && !acct.Extract_All_Modules {
			return errors.New("Shodan accounts must specify a default Tag-Name if Extract-All-Modules is not true")
		}
	}
	return nil
}

func (c *cfgType) Targets() ([]string, error) {
	var conns []string
	for _, v := range c.Global.Cleartext_Backend_Target {
		conns = append(conns, "tcp://"+v)
	}
	for _, v := range c.Global.Encrypted_Backend_Target {
		conns = append(conns, "tls://"+v)
	}
	for _, v := range c.Global.Pipe_Backend_Target {
		conns = append(conns, "pipe://"+v)
	}
	if len(conns) == 0 {
		return nil, errors.New("no connections specified")
	}
	return conns, nil
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)
	for _, v := range c.ShodanAccount {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
	}
	return tags, nil
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
