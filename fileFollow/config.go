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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/config"

	"gopkg.in/gcfg.v1"
)

const (
	MAX_CONFIG_SIZE           int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
	defaultStateStoreLocation       = `/opt/gravwell/etc/file_follow.state`
)

var (
	ErrInvalidStateStoreLocation = errors.New("Empty state storage location")
)

type bindType int
type readerType int

type cfgReadType struct {
	Global   global
	Follower map[string]*follower
}

type follower struct {
	Base_Directory            string // the base directory we will be watching
	File_Filter               string // the glob for pattern matching
	Tag_Name                  string
	Ignore_Timestamps         bool //Just apply the current timestamp to lines as we get them
	Assume_Local_Timezone     bool
	Recursive                 bool // Should we descend into child directories?
	Ignore_Line_Prefix        []string
	Timestamp_Format_Override string //override the timestamp format
	Timestamp_Delimited       bool
	Timezone_Override         string
}

type global struct {
	config.IngestConfig
	Max_Files_Watched    int
	State_Store_Location string
}

type cfgType struct {
	global
	Follower map[string]*follower
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
	//read into the intermediary type to maintain backwards compatibility with the old system
	var cr cfgReadType
	cr.Global.Init() //initialize all the global parameters
	if err := gcfg.ReadStringInto(&cr, string(content)); err != nil {
		return nil, err
	}
	c := &cfgType{
		global:   cr.Global,
		Follower: cr.Follower,
	}
	if err := verifyConfig(c); err != nil {
		return nil, err
	}
	return c, nil
}

func verifyConfig(c *cfgType) error {
	//verify the global parameters
	if err := c.Verify(); err != nil {
		return err
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
		if v.Timestamp_Delimited && !v.Timestamp_Format_Override {
			return errors.New("Timestamp delimiting requires a defined timestamp override")
		}
		if strings.ContainsAny(v.Tag_Name, ingest.FORBIDDEN_TAG_SET) {
			return errors.New("Invalid characters in the Tag-Name for " + k)
		}
		v.Base_Directory = filepath.Clean(v.Base_Directory)
		if v.Timezone_Override != "" {
			if v.Assume_Local_Timezone {
				// cannot do both
				return fmt.Errorf("Cannot specify Assume-Local-Timezone and Timezone-Override in the same follower %v", k)
			}
			if _, err := time.LoadLocation(v.Timezone_Override); err != nil {
				return fmt.Errorf("Invalid timezone override %v in follower %v: %v", v.Timezone_Override, k, err)
			}
		}
	}
	return nil
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
	sort.Strings(tags)
	return tags, nil
}

func (f follower) TimestampOverride() (v string, err error) {
	v = strings.TrimSpace(f.Timestamp_Format_Override)
	return
}

func (g *global) Init() {
	if g.State_Store_Location == `` {
		g.State_Store_Location = defaultStateStoreLocation
	}
}

func (g *global) Verify() (err error) {
	if err = g.IngestConfig.Verify(); err != nil {
		return
	}
	if g.State_Store_Location == `` {
		err = ErrInvalidStateStoreLocation
	}
	return
}

func (g *global) StatePath() string {
	return g.State_Store_Location
}
