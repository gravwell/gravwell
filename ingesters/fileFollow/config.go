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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/filewatch"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/attach"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/timegrinder"
)

const (
	MAX_CONFIG_SIZE        int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
	defaultMaxWatchedFiles       = 1024
)

var (
	ErrInvalidStateStoreLocation         = errors.New("Empty state storage location")
	ErrTimestampDelimiterMissingOverride = errors.New("Timestamp delimiting requires a defined timestamp override")
)

type bindType int
type readerType int

type cfgReadType struct {
	Global       global
	Attach       attach.AttachConfig
	Follower     map[string]*follower
	Preprocessor processors.ProcessorConfig
	TimeFormat   config.CustomTimeFormat
}

type follower struct {
	Base_Directory            string // the base directory we will be watching
	File_Filter               string // the glob for pattern matching
	Tag_Name                  string
	Ignore_Timestamps         bool //Just apply the current timestamp to lines as we get them
	Assume_Local_Timezone     bool
	Recursive                 bool // Should we descend into child directories?
	Attach_Filename           bool // attach the full path of the file to each entry
	Ignore_Line_Prefix        []string
	Ignore_Glob               []string
	Trim                      bool
	Timestamp_Format_Override string //override the timestamp format
	Timestamp_Delimited       bool
	Timezone_Override         string
	Regex_Delimiter           string
	Preprocessor              []string
	// these two must be used together
	Timestamp_Regex         string
	Timestamp_Format_String string
	// so that we can initialize the timegrinder
	timeFormats config.CustomTimeFormat
}

type global struct {
	config.IngestConfig
	Max_Files_Watched    int
	State_Store_Location string
}

type cfgType struct {
	global
	Attach       attach.AttachConfig
	Follower     map[string]*follower
	Preprocessor processors.ProcessorConfig
	TimeFormat   config.CustomTimeFormat
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var cr cfgReadType
	if err := config.LoadConfigFile(&cr, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&cr, overlayPath); err != nil {
		return nil, err
	}
	c := &cfgType{
		global:       cr.Global,
		Attach:       cr.Attach,
		Follower:     cr.Follower,
		Preprocessor: cr.Preprocessor,
		TimeFormat:   cr.TimeFormat,
	}
	if err := c.Verify(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *cfgType) Verify() error {
	//verify the global parameters
	if err := c.IngestConfig.Verify(); err != nil {
		return err
	} else if err = c.Attach.Verify(); err != nil {
		return err
	} else if err = c.global.verifyStateStore(); err != nil {
		return err
	} else if c.global.Max_Files_Watched <= 0 {
		c.global.Max_Files_Watched = defaultMaxWatchedFiles
	}
	if len(c.Follower) == 0 {
		return errors.New("No Followers specified")
	}
	if err := c.Preprocessor.Validate(); err != nil {
		return err
	} else if err = c.TimeFormat.Validate(); err != nil {
		return err
	}
	for k, v := range c.Follower {
		if len(v.Base_Directory) == 0 {
			return errors.New("No Base-Directory provided for " + k)
		}
		if len(v.Tag_Name) == 0 {
			v.Tag_Name = entry.DefaultTagName
		}
		if v.Timestamp_Delimited && v.Timestamp_Format_Override == `` {
			return ErrTimestampDelimiterMissingOverride
		}
		if (v.Timestamp_Regex != `` && v.Timestamp_Format_String == ``) || (v.Timestamp_Regex == `` && v.Timestamp_Format_String != ``) {
			return errors.New("Timestamp-Regex and Timestamp-Format-String must both be specified, or both left unset")
		}
		if v.Timestamp_Regex != `` {
			// check that it is valid
			if _, err := timegrinder.NewUserProcessor("user", v.Timestamp_Regex, v.Timestamp_Format_String); err != nil {
				return fmt.Errorf("Failed to parse Timestamp-Regex and Timestamp-Format-String defs: %v", err)
			}
		}
		if ingest.CheckTag(v.Tag_Name) != nil {
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
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Follower %s preprocessor invalid: %v", k, err)
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

func (c *cfgType) IngestBaseConfig() config.IngestConfig {
	return c.IngestConfig
}

func (c *cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
}

func (cfg *cfgType) Followers() map[string]follower {
	mp := make(map[string]follower, len(cfg.Follower))
	for k, v := range cfg.Follower {
		if v != nil {
			f := *v
			f.timeFormats = cfg.TimeFormat
			mp[k] = f
		}
	}
	return mp
}
func (f follower) TimestampOverride() (v string, err error) {
	v = strings.TrimSpace(f.Timestamp_Format_Override)
	return
}

func (f follower) TimestampDelimited() (rex string, ok bool, err error) {
	if !f.Timestamp_Delimited {
		return
	}
	if f.Timestamp_Format_Override == `` {
		err = ErrTimestampDelimiterMissingOverride
		return
	}
	//fir eup a timegrinder, set the override, and extract the regex in use
	cfg := timegrinder.Config{
		FormatOverride: f.Timestamp_Format_Override,
	}
	var tg *timegrinder.TimeGrinder
	var proc timegrinder.Processor
	if tg, err = timegrinder.New(cfg); err != nil {
		return
	} else if err = f.timeFormats.LoadFormats(tg); err != nil {
		return
	}
	if proc, err = tg.OverrideProcessor(); err != nil {
		return
	}
	if rex = proc.ExtractionRegex(); rex == `` {
		err = errors.New("Missing timestamp processor extraction string")
		return
	}
	//fixup the regex string to fit on line breaks
	if strings.HasPrefix(rex, `\A`) {
		//remove the "at beginning of text" sequence and replace with newline
		rex = `\n` + strings.TrimPrefix(rex, `\A`)
	}
	if strings.HasPrefix(rex, `^`) {
		//remove the "at beginning regex and replace with newline
		rex = `\n` + strings.TrimPrefix(rex, `^`)
	}
	if _, err = regexp.Compile(rex); err != nil {
		return
	}
	ok = true // we are all good
	return
}

func (f follower) TimezoneOverride() string {
	return f.Timezone_Override
}

func (g *global) Verify() (err error) {
	if err = g.IngestConfig.Verify(); err != nil {
		return
	}
	err = g.verifyStateStore()
	return
}

func (g *global) StatePath() string {
	return g.State_Store_Location
}

func dumpStateFile(pth string) {
	states, err := filewatch.DecodeStateFile(pth)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load state file: %v\n", err)
		return
	}
	fmt.Printf("%-24s %-16s %s\n", "Listener Name", "File Offset", "File Path")
	for _, state := range states {
		fmt.Printf("%-24s %-16d %s\n", state.BaseName, state.State, state.FilePath)
	}
}
