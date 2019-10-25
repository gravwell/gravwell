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

	"github.com/google/uuid"
	"github.com/gravwell/ingest/v3/config"
	"github.com/gravwell/ingest/v3/processors"
)

const (
	MAX_CONFIG_SIZE int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
)

type bindType int
type readerType int

type global struct {
	config.IngestConfig
	AWS_Access_Key_ID     string
	AWS_Secret_Access_Key string
}

type cfgType struct {
	Global        global
	KinesisStream map[string]*struct {
		Stream_Name           string
		Tag_Name              string
		Iterator_Type         string
		Region                string
		Assume_Local_Timezone bool
		Timezone_Override     string
		Parse_Time            bool
		Preprocessor          string
	}
	Preprocessor processors.PreprocessorConfig
}

func GetConfig(path string) (*cfgType, error) {
	var c cfgType
	if err := config.LoadConfigFile(&c, path); err != nil {
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
	if len(c.KinesisStream) == 0 {
		return errors.New("At least one Kinesis stream required.")
	}
	for k, v := range c.KinesisStream {
		if v == nil {
			return fmt.Errorf("Kinesis stream %v config is nil", k)
		}
		if v.Preprocessor != `` {
			if err := c.CheckPreprocessor(v.Preprocessor); err != nil {
				return fmt.Errorf("Kinesis stream %s preprocessor %s error: %v", k, v.Preprocessor, err)
			}
		}
	}
	return nil
}

func (c *cfgType) GetPreprocessor(name string) (p processors.Preprocessor, err error) {
	name = strings.TrimSpace(name)
	p, err = c.Preprocessor.GetPreprocessor(name)
	return
}

func (c *cfgType) CheckPreprocessor(name string) (err error) {
	name = strings.TrimSpace(name)
	err = c.Preprocessor.CheckConfig(name)
	return
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
	for _, v := range c.KinesisStream {
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

func (c *cfgType) CachePath() string {
	return c.Global.Ingest_Cache_Path
}

func (c *cfgType) CacheEnabled() bool {
	return c.Global.Ingest_Cache_Path != ``
}

func (c *cfgType) parseTimeout() (time.Duration, error) {
	tos := strings.TrimSpace(c.Global.Connection_Timeout)
	if len(tos) == 0 {
		return 0, nil
	}
	return time.ParseDuration(tos)
}
