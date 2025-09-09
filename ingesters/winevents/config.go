//go:build windows
// +build windows

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
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/attach"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/winevent"
)

const (
	defaultBookmarkName       = `bookmark`
	maxConfigSize       int64 = (1024 * 1024 * 2) //2MB, even this is crazy large

	defaultReachback = 168 * time.Hour //1 week
)

const (
	mb = 1024 * 1024

	defaultBuffSize = 2 * mb  //2MB  Sure... why not
	minBuffSize     = 1 * mb  //1MB is kindo of a lower bound
	maxBuffSize     = 32 * mb //a 32MB message is stupid

	defaultHandleRequest = 128
	maxHandleRequest     = 1024
)

var (
	defaultLevels = []string{`verbose`, `information`, `warning`, `error`, `critical`}

	ErrInvalidName              = errors.New("Event channel name is invalid")
	ErrInvalidReachbackDuration = errors.New("Invalid event reachback duration")
	ErrInvalidLevel             = errors.New("Invalid level")
	ErrInvalidEventIds          = errors.New("Invalid Event IDs, must be of the form 100 or -100 or 100-200")

	evRangeRegex = regexp.MustCompile(`\A([0-9]+)\s*-\s*([0-9]+)\z`)
)

type EventStreamConfig struct {
	Tag_Name       string   //which tag are we applying to this event channel
	Channel        string   //Names like: System, Application, Security...
	Max_Reachback  string   //duration like: 72 hours, or 6 weeks, etc..
	Level          []string //levels include: verbose,information,warning,error,critical
	Provider       []string //list of providers to filter on
	EventID        []string //list of eventID filters: 1000-2000 or -1000
	Request_Size   int      //number of entries to request per cycle
	Request_Buffer int      //number request buffer
	Preprocessor   []string
}

type CfgType struct {
	Global struct {
		config.IngestConfig
		Bookmark_Location string
		Ignore_Timestamps bool
	}
	Attach       attach.AttachConfig
	EventChannel map[string]*EventStreamConfig
	Preprocessor processors.ProcessorConfig
}

func GetConfig(path string) (*CfgType, error) {
	var c CfgType
	if err := config.LoadConfigFile(&c, path); err != nil {
		return nil, err
	}

	if err := c.Verify(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *CfgType) Verify() error {
	//verify the global parameters
	if err := c.Global.Verify(); err != nil {
		return err
	} else if err = c.Attach.Verify(); err != nil {
		return err
	} else if err := c.Preprocessor.Validate(); err != nil {
		return err
	}

	if c.Global.Bookmark_Location == "" {
		b, err := winevent.ProgramDataFilename(filepath.Join(`gravwell\eventlog\`, defaultBookmarkName))
		if err != nil {
			return err
		}
		c.Global.Bookmark_Location = b
	}
	for k, v := range c.EventChannel {
		v.normalize()
		if err := v.Validate(); err != nil {
			return err
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Event Stream %s preprocessor invalid: %v", k, err)
		}
	}
	return nil
}

func (c *CfgType) Targets() ([]string, error) {
	return c.Global.Targets()
}

func (c *CfgType) Tags() ([]string, error) {
	var tags []string
	var tag string
	tagMp := make(map[string]bool, 1)
	for _, v := range c.EventChannel {
		tag = v.Tag_Name
		if len(tag) == 0 {
			tag = entry.DefaultTagName
		}
		if _, ok := tagMp[tag]; !ok {
			tags = append(tags, tag)
			tagMp[tag] = true
		}
	}
	if len(tags) == 0 {
		return nil, errors.New("No tags specified")
	}
	return tags, nil
}

func (c *CfgType) IngestBaseConfig() config.IngestConfig {
	return c.Global.IngestConfig
}

func (c *CfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
}

// RawConfig gets an object that is suitable for the ingestmuxer.SetRawConfiguration
func (c *CfgType) RawConfig() interface{} {
	if c == nil {
		return nil
	}
	return struct {
		config.IngestConfig
		Bookmark_Location string
		Ignore_Timestamps bool
		Attach            attach.AttachConfig
		EventChannel      map[string]*EventStreamConfig
		Preprocessor      processors.ProcessorConfig
	}{
		IngestConfig:      c.Global.IngestConfig,
		Bookmark_Location: c.Global.Bookmark_Location,
		Ignore_Timestamps: c.Global.Ignore_Timestamps,
		Attach:            c.Attach,
		EventChannel:      c.EventChannel,
		Preprocessor:      c.Preprocessor,
	}
}

func (c *CfgType) VerifyRemote() bool {
	return c.Global.Verify_Remote_Certificates
}

func (c *CfgType) Timeout() time.Duration {
	if tos, _ := c.parseTimeout(); tos > 0 {
		return tos
	}
	return 0
}

func (c *CfgType) Secret() string {
	return c.Global.Ingest_Secret
}

func (c *CfgType) BookmarkPath() string {
	return c.Global.Bookmark_Location
}

func (c *CfgType) IgnoreTimestamps() bool {
	return c.Global.Ignore_Timestamps
}

func (c *CfgType) parseTimeout() (time.Duration, error) {
	tos := strings.TrimSpace(c.Global.Connection_Timeout)
	if len(tos) == 0 {
		return 0, nil
	}
	return time.ParseDuration(tos)
}

func (ec *EventStreamConfig) normalize() {
	ec.Channel = strings.TrimSpace(ec.Channel)
	ec.Max_Reachback = strings.TrimSpace(ec.Max_Reachback)
	ec.Tag_Name = strings.TrimSpace(ec.Tag_Name)
	for i := range ec.Level {
		ec.Level[i] = strings.ToLower(strings.TrimSpace(ec.Level[i]))
	}
	for i := range ec.Provider {
		ec.Provider[i] = strings.TrimSpace(ec.Provider[i])
	}
	for i := range ec.EventID {
		ec.EventID[i] = strings.TrimSpace(ec.EventID[i])
	}
	if ec.Request_Size == 0 {
		ec.Request_Size = defaultHandleRequest
	} else if ec.Request_Size > maxHandleRequest {
		ec.Request_Size = maxHandleRequest
	} else if ec.Request_Size < winevent.MinHandleRequest {
		ec.Request_Size = winevent.MinHandleRequest
	}

	ec.Request_Buffer *= mb
	if ec.Request_Buffer == 0 {
		ec.Request_Buffer = defaultBuffSize
	} else if ec.Request_Buffer > maxBuffSize {
		ec.Request_Buffer = maxBuffSize
	} else if ec.Request_Buffer < minBuffSize {
		ec.Request_Buffer = minBuffSize
	}
}

func (ec *EventStreamConfig) Validate() error {
	if len(ec.Channel) == 0 {
		return ErrInvalidName
	}
	if len(ec.Max_Reachback) != 0 {
		dur, err := time.ParseDuration(ec.Max_Reachback)
		if err != nil {
			return err
		}
		if dur < 0 {
			return ErrInvalidReachbackDuration
		}
	}
	if len(ec.Level) == 0 {
		ec.Level = defaultLevels
	}
	if ingest.CheckTag(ec.Tag_Name) != nil {
		return errors.New("Invalid characters in the Tag-Name for " + ec.Tag_Name)
	}
	for i := range ec.Level {
		if !inStringSet(ec.Level[i], defaultLevels) {
			return ErrInvalidLevel
		}
	}
	for i := range ec.EventID {
		if err := validateEventIDs(ec.EventID[i]); err != nil {
			return err
		}
	}
	return nil
}

func inStringSet(needle string, haystack []string) bool {
	for i := range haystack {
		if needle == haystack[i] {
			return true
		}
	}
	return false
}

func validateEventIDs(ev string) error {
	ev = strings.TrimSpace(ev)
	//event IDs MUST be of the form (num, -num, or num-num)
	//test if it's a range
	subs := evRangeRegex.FindAllStringSubmatch(ev, -1)
	if len(subs) > 1 {
		return ErrInvalidEventIds
	}
	if len(subs) == 1 {
		s := subs[0]
		if len(s) != 3 {
			return ErrInvalidEventIds
		}
		//try to parse each piece
		v1, err := strconv.ParseInt(s[1], 10, 16)
		if err != nil {
			return ErrInvalidEventIds
		}
		v2, err := strconv.ParseInt(s[2], 10, 16)
		if err != nil {
			return ErrInvalidEventIds
		}
		if v1 >= v2 {
			return ErrInvalidEventIds
		}
		return nil
	}

	//try to parse as a CSV of ints
	if err := parseCSVInts(ev); err == nil {
		return nil
	}

	//try to parse it as a straight up int
	if _, err := strconv.ParseInt(ev, 10, 16); err != nil {
		return ErrInvalidEventIds
	}
	return nil
}

func parseCSVInts(val string) error {
	bits := strings.Split(val, ",")
	if len(bits) == 0 {
		return errors.New("empty list")
	}
	for _, ev := range bits {
		ev = strings.TrimSpace(ev)
		if _, err := strconv.ParseInt(ev, 10, 16); err != nil {
			return fmt.Errorf("%w %s is not a valid EventID", ErrInvalidEventIds, ev)
		}
	}
	return nil
}

// Validate SHOULD have already been called, we aren't going to check anything here
func (ec *EventStreamConfig) params(name string) (winevent.EventStreamParams, error) {
	var dur time.Duration
	if len(ec.Max_Reachback) == 0 {
		dur = defaultReachback
	} else {
		var err error
		dur, err = time.ParseDuration(ec.Max_Reachback)
		if err != nil {
			return winevent.EventStreamParams{}, err
		}
	}
	tag := ec.Tag_Name
	if len(tag) == 0 {
		tag = entry.DefaultTagName
	}
	return winevent.EventStreamParams{
		Name:         name,
		TagName:      tag,
		Channel:      ec.Channel,
		Levels:       strings.Join(ec.Level, ","),
		EventIDs:     strings.Join(ec.EventID, ","),
		Providers:    append([]string{}, ec.Provider...),
		ReachBack:    dur,
		Preprocessor: ec.Preprocessor,
		ReqSize:      ec.Request_Size,
		BuffSize:     ec.Request_Buffer,
	}, nil
}

func (c *CfgType) Streams() ([]winevent.EventStreamParams, error) {
	var params []winevent.EventStreamParams
	for k, v := range c.EventChannel {
		esp, err := v.params(k)
		if err != nil {
			return nil, err
		}
		params = append(params, esp)
	}
	return params, nil
}

func (c *CfgType) EnableCache() bool {
	return len(c.Global.Ingest_Cache_Path) != 0
}

func (c *CfgType) LocalFileCachePath() string {
	return c.Global.Ingest_Cache_Path
}

func (c *CfgType) CacheSize() int {
	return c.Global.Max_Ingest_Cache
}

func (c *CfgType) LogLevel() string {
	return c.Global.Log_Level
}
