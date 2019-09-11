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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gravwell/ingest"

	"gopkg.in/gcfg.v1"
)

const (
	MAX_CONFIG_SIZE int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
)

type bindType int
type readerType int
type FollowType struct {
	Base_Directory        string // the base directory we will be watching
	File_Filter           string // the glob for pattern matching
	Tag_Name              string
	Ignore_Timestamps     bool //Just apply the current timestamp to lines as we get them
	Assume_Local_Timezone bool
}

type cfgType struct {
	Global struct {
		State_Store_Location       string //Location that we will drop our state object
		Ingest_Secret              string
		Connection_Timeout         string
		Verify_Remote_Certificates bool
		Cleartext_Backend_Target   []string
		Encrypted_Backend_Target   []string
		Pipe_Backend_Target        []string
		Log_Level                  string
		Ingest_Cache_Path          string
	}
	Follower map[string]*FollowType
}

func GetConfig(path string) (*cfgType, error) {
	var content []byte
	fin, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := fin.Stat()
	if err != nil {
		fin.Close()
		return nil, err
	}
	//This is just a sanity check
	if fi.Size() > MAX_CONFIG_SIZE {
		fin.Close()
		return nil, errors.New("Config File Far too large")
	}
	content = make([]byte, fi.Size())
	n, err := fin.Read(content)
	fin.Close()
	if int64(n) != fi.Size() {
		return nil, errors.New("Failed to read config file")
	}

	var c cfgType
	if err := gcfg.ReadStringInto(&c, string(content)); err != nil {
		return nil, err
	}
	if err := verifyConfig(c); err != nil {
		return nil, err
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
	if len(c.Follower) == 0 {
		return errors.New("No listeners specified")
	}
	for k, v := range c.Follower {
		if len(v.Base_Directory) == 0 {
			return errors.New("No Base-Directory provided for " + k)
		}
		if len(v.Tag_Name) == 0 {
			v.Tag_Name = `default`
		}
		if strings.ContainsAny(v.Tag_Name, ingest.FORBIDDEN_TAG_SET) {
			return errors.New("Invalid characters in the Tag-Name for " + k)
		}
		v.Base_Directory = filepath.Clean(v.Base_Directory)
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
	for _, v := range c.Follower {
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

func (c *cfgType) StatePath() string {
	return c.Global.State_Store_Location
}

func (c *cfgType) Followers() map[string]FollowType {
	mp := make(map[string]FollowType, len(c.Follower))
	for k, v := range c.Follower {
		mp[k] = *v
	}
	return mp
}
