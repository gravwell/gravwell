/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
// +build windows

package winevent

import (
	"errors"
	"fmt"
	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/gcfg.v1"
)

const (
	defaultBookmarkName       = `bookmark`
	maxConfigSize       int64 = (1024 * 1024 * 2) //2MB, even this is crazy large

	defaultTag       = entry.DefaultTagName
	defaultReachback = 168 * time.Hour //1 week
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
	Tag_Name      string   //which tag are we applying to this event channel
	Channel       string   //Names like: System, Application, Security...
	Max_Reachback string   //duration like: 72 hours, or 6 weeks, etc..
	Level         []string //levels include: verbose,information,warning,error,critical
	Provider      []string //list of providers to filter on
	EventID       []string //list of eventID filters: 1000-2000 or -1000
}

type CfgType struct {
	Global struct {
		Ingest_Secret              string
		Connection_Timeout         string
		Verify_Remote_Certificates bool
		Cleartext_Backend_Target   []string
		Encrypted_Backend_Target   []string
		Bookmark_Location          string
		Ignore_Timestamps          bool
		Ingest_Cache_Path          string
		Log_Level                  string
	}
	EventChannel map[string]*EventStreamConfig
}

func GetConfig(path string) (*CfgType, error) {
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
	if fi.Size() > maxConfigSize {
		fin.Close()
		return nil, errors.New("Config File Far too large")
	}
	content = make([]byte, fi.Size())
	n, err := fin.Read(content)
	fin.Close()
	if int64(n) != fi.Size() {
		return nil, errors.New("Failed to read config file")
	}

	var c CfgType
	if err := gcfg.ReadStringInto(&c, string(content)); err != nil {
		return nil, err
	}
	if err := c.verify(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *CfgType) verify() error {
	if len(c.Global.Ingest_Secret) == 0 {
		return errors.New("Missing ingest secret")
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
	if c.Global.Bookmark_Location == "" {
		b, err := ServiceFilename(defaultBookmarkName)
		if err != nil {
			return err
		}
		c.Global.Bookmark_Location = b
	}
	//ensure there is at least one target
	connCount := len(c.Global.Cleartext_Backend_Target) +
		len(c.Global.Encrypted_Backend_Target)
	if connCount == 0 {
		return errors.New("No backend targets specified")
	}
	for _, v := range c.EventChannel {
		v.normalize()
		if err := v.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c *CfgType) Targets() ([]string, error) {
	var conns []string
	for _, v := range c.Global.Cleartext_Backend_Target {
		conns = append(conns, "tcp://"+v)
	}
	for _, v := range c.Global.Encrypted_Backend_Target {
		conns = append(conns, "tls://"+v)
	}
	if len(conns) == 0 {
		return nil, errors.New("no connections specified")
	}
	return conns, nil
}

func (c *CfgType) Tags() ([]string, error) {
	var tags []string
	var tag string
	tagMp := make(map[string]bool, 1)
	for _, v := range c.EventChannel {
		tag = v.Tag_Name
		if len(tag) == 0 {
			tag = defaultTag
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
	if strings.ContainsAny(ec.Tag_Name, ingest.FORBIDDEN_TAG_SET) {
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

	//try to parse it as a straight up int
	v, err := strconv.ParseInt(ev, 10, 16)
	if err != nil || v == 0 {
		return ErrInvalidEventIds
	}
	return nil
}

type EventStreamParams struct {
	Name      string
	TagName   string
	Channel   string
	Levels    string
	EventIDs  string
	Providers []string
	ReachBack time.Duration
}

//Validate SHOULD have already been called, we aren't going to check anything here
func (ec *EventStreamConfig) params(name string) (EventStreamParams, error) {
	var dur time.Duration
	if len(ec.Max_Reachback) == 0 {
		dur = defaultReachback
	} else {
		var err error
		dur, err = time.ParseDuration(ec.Max_Reachback)
		if err != nil {
			return EventStreamParams{}, err
		}
	}
	tag := ec.Tag_Name
	if len(tag) == 0 {
		tag = defaultTag
	}
	return EventStreamParams{
		Name:      name,
		TagName:   tag,
		Channel:   ec.Channel,
		Levels:    strings.Join(ec.Level, ","),
		EventIDs:  strings.Join(ec.EventID, ","),
		Providers: append([]string{}, ec.Provider...),
		ReachBack: dur,
	}, nil
}

func (c *CfgType) Streams() ([]EventStreamParams, error) {
	var params []EventStreamParams
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

func (c *CfgType) LogLevel() string {
	return c.Global.Log_Level
}
func ServiceFilename(name string) (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return ``, fmt.Errorf("Failed to get executable path: %v", err)
	}
	exeDir, err := filepath.Abs(filepath.Dir(exePath))
	if err != nil {
		return ``, fmt.Errorf("Failed to get location of executable: %v", err)
	}
	return filepath.Join(exeDir, name), nil
}
