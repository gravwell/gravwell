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
	"net"
	"sort"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

type TimeConfig struct {
	Ignore_Timestamps         bool //Just apply the current timestamp to lines as we get them
	Assume_Local_Timezone     bool
	Timezone_Override         string
	Timestamp_Format_Override string //override the timestamp format
}

type bucket struct {
	TimeConfig
	AuthConfig
	Reader          string //defaults to line
	Tag_Name        string
	Source_Override string
	File_Filters    []string
	Preprocessor    []string
	Max_Line_Size   int
}

type sqsS3 struct {
	TimeConfig
	Reader          string //defaults to line
	Tag_Name        string
	Queue_URL       string
	Region          string
	AKID            string
	Secret          string `json:"-"` // DO NOT send this when marshalling
	Preprocessor    []string
	Max_Line_Size   int
	Source_Override string
}

type global struct {
	config.IngestConfig
	State_Store_Location string
}

type cfgReadType struct {
	Global          global
	Attach          attach.AttachConfig
	Bucket          map[string]*bucket
	SQS_S3_Listener map[string]*sqsS3
	Preprocessor    processors.ProcessorConfig
	TimeFormat      config.CustomTimeFormat
}

type cfgType struct {
	config.IngestConfig
	Attach               attach.AttachConfig
	State_Store_Location string
	Bucket               map[string]*bucket
	SQS_S3_Listener      map[string]*sqsS3
	Preprocessor         processors.ProcessorConfig
	TimeFormat           config.CustomTimeFormat
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
		IngestConfig:         cr.Global.IngestConfig,
		Attach:               cr.Attach,
		State_Store_Location: cr.Global.State_Store_Location,
		Bucket:               cr.Bucket,
		SQS_S3_Listener:      cr.SQS_S3_Listener,
		Preprocessor:         cr.Preprocessor,
		TimeFormat:           cr.TimeFormat,
	}

	// Verify and set UUID
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
	}

	if c.State_Store_Location == `` {
		return errors.New("Missing State-Store-Location")
	}

	if len(c.Bucket) == 0 && len(c.SQS_S3_Listener) == 0 {
		return errors.New("No listeners specified")
	}
	if c.State_Store_Location == `` {
		c.State_Store_Location = defaultStateLoc
	}

	if err := c.Preprocessor.Validate(); err != nil {
		return err
	} else if err = c.TimeFormat.Validate(); err != nil {
		return err
	}

	for k, v := range c.Bucket {
		if len(v.Tag_Name) == 0 {
			v.Tag_Name = entry.DefaultTagName
		}
		if ingest.CheckTag(v.Tag_Name) != nil {
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
		if v.Source_Override != `` {
			if net.ParseIP(v.Source_Override) == nil {
				return fmt.Errorf("Source-Override %s is not a valid IP address", v.Source_Override)
			}
		}

		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Listener %s preprocessor invalid: %v", k, err)
		}
		if err := v.AuthConfig.validate(); err != nil {
			return err
		}
		if _, err := parseReader(v.Reader); err != nil {
			return fmt.Errorf("Invalid Reader %q - %v", v.Reader, err)
		}
	}

	for k, v := range c.SQS_S3_Listener {
		if len(v.Tag_Name) == 0 {
			v.Tag_Name = entry.DefaultTagName
		}
		if ingest.CheckTag(v.Tag_Name) != nil {
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
		if v.Source_Override != `` {
			if net.ParseIP(v.Source_Override) == nil {
				return fmt.Errorf("Source-Override %s is not a valid IP address", v.Source_Override)
			}
		}

		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Listener %s preprocessor invalid: %v", k, err)
		}
		if _, err := parseReader(v.Reader); err != nil {
			return fmt.Errorf("Invalid Reader %q - %v", v.Reader, err)
		}
	}

	return nil
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)

	for _, v := range c.Bucket {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
	}

	for _, v := range c.SQS_S3_Listener {
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

func (c *cfgType) newTimeGrinder(tc TimeConfig) (tg *timegrinder.TimeGrinder, err error) {
	tcfg := timegrinder.Config{
		EnableLeftMostSeed: true,
	}
	if tg, err = timegrinder.NewTimeGrinder(tcfg); err != nil {
		err = fmt.Errorf("failed to get a handle on the timegrinder %w", err)
		return
	} else if err = c.TimeFormat.LoadFormats(tg); err != nil {
		err = fmt.Errorf("failed to load custom time formats %w", err)
		return
	}
	if tc.Assume_Local_Timezone {
		tg.SetLocalTime()
	}
	if tc.Timezone_Override != `` {
		if err = tg.SetTimezone(tc.Timezone_Override); err != nil {
			err = fmt.Errorf("failed to set timezone to %v: %v", tc.Timezone_Override, err)
			return
		}
	}
	if tc.Timestamp_Format_Override != `` {
		if err = tg.SetFormatOverride(tc.Timestamp_Format_Override); err != nil {
			return
		}
	}
	return
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

func (g *global) verifyStateStore() (err error) {
	if g.State_Store_Location == `` {
		g.State_Store_Location = defaultStateLoc
	}
	return
}

func (tc TimeConfig) validate() (err error) {
	if tc.Timezone_Override != `` {
		if _, err = time.LoadLocation(tc.Timezone_Override); err != nil {
			return
		}
	}
	if tc.Timestamp_Format_Override != `` {
		//check that we have a valid timestamp
		var zero = time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC)
		var ts time.Time
		if ts, err = time.Parse(tc.Timestamp_Format_Override, tc.Timestamp_Format_Override); err != nil {
			return
		} else if ts.IsZero() || ts.Equal(zero) {
			err = fmt.Errorf("Timestamp-Format-Override %s is not a valid timestamp format", tc.Timestamp_Format_Override)
			return
		}
	}
	return
}
