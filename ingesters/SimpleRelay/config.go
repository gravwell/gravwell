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
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

const (
	MAX_CONFIG_SIZE int64    = (1024 * 1024 * 2) //2MB, even this is crazy large
	tcp             bindType = iota
	udp             bindType = iota
	tcp6            bindType = iota
	udp6            bindType = iota
	TLS             bindType = iota

	lineReader    readerType = iota
	rfc5424Reader readerType = iota
	rfc6587Reader readerType = iota
)

var ()

type bindType int
type readerType int

type listener struct {
	base
	Reader_Type   string
	Drop_Priority bool // remove the <nnn> priority value at the start of the log message, useful for things like fortinet
	Keep_Priority bool `json:"-"` //NOTE DEPRECATED AND UNUSED.  Left so that config parsing doesn't break
}

type base struct {
	Tag_Name                  string
	Bind_String               string //IP port pair 127.0.0.1:1234
	Ignore_Timestamps         bool   //Just apply the current timestamp to lines as we get them
	Assume_Local_Timezone     bool
	Timezone_Override         string
	Source_Override           string
	Timestamp_Format_Override string //override the timestamp format
	Cert_File                 string
	Key_File                  string
	Preprocessor              []string
}

type cfgReadType struct {
	Global        config.IngestConfig
	Listener      map[string]*listener
	JSONListener  map[string]*jsonListener
	RegexListener map[string]*regexListener
	Preprocessor  processors.ProcessorConfig
	TimeFormat    config.CustomTimeFormat
}

type cfgType struct {
	config.IngestConfig
	Listener      map[string]*listener
	JSONListener  map[string]*jsonListener
	RegexListener map[string]*regexListener
	Preprocessor  processors.ProcessorConfig
	TimeFormat    config.CustomTimeFormat
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
		IngestConfig:  cr.Global,
		Listener:      cr.Listener,
		RegexListener: cr.RegexListener,
		JSONListener:  cr.JSONListener,
		Preprocessor:  cr.Preprocessor,
		TimeFormat:    cr.TimeFormat,
	}

	if err := verifyConfig(c); err != nil {
		return nil, err
	}
	// Verify and set UUID
	if _, ok := c.IngesterUUID(); !ok {
		id := uuid.New()
		if err := c.SetIngesterUUID(id, path); err != nil {
			return nil, err
		}
		if id2, ok := c.IngesterUUID(); !ok || id != id2 {
			return nil, errors.New("Failed to set a new ingester UUID")
		}
	}
	return c, nil
}

func verifyConfig(c *cfgType) error {
	//verify the global parameters
	if err := c.Verify(); err != nil {
		return err
	}
	if len(c.Listener) == 0 && len(c.RegexListener) == 0 && len(c.JSONListener) == 0 {
		return errors.New("No listeners specified")
	}
	if err := c.Preprocessor.Validate(); err != nil {
		return err
	} else if err = c.TimeFormat.Validate(); err != nil {
		return err
	}
	bindMp := make(map[string]string, 1)
	for k, v := range c.Listener {
		if err := v.base.Validate(); err != nil {
			return fmt.Errorf("Listener %s configuration error: %v", k, err)
		}
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
		if err := checkListenerSettings(v); err != nil {
			return fmt.Errorf("Listener %q is invalid: %v", k, err)
		}
		if n, ok := bindMp[v.Bind_String]; ok {
			return errors.New("Bind-String for " + k + " already in use by " + n)
		}
		bindMp[v.Bind_String] = k
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Listener %s preprocessor invalid: %v", k, err)
		}
	}
	for k, v := range c.RegexListener {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("RegexListener %s configuration error: %v", k, err)
		}
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
		if n, ok := bindMp[v.Bind_String]; ok {
			return errors.New("Bind-String for " + k + " already in use by " + n)
		}
		bindMp[v.Bind_String] = k
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Listener %s preprocessor invalid: %v", k, err)
		}
	}
	for k, v := range c.JSONListener {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("Listener %s configuration error: %v", k, err)
		}
		if ingest.CheckTag(v.Default_Tag) != nil {
			return errors.New("Invalid characters in the Default-Tag for " + k)
		}
		tms, err := v.TagMatchers()
		if err != nil {
			return err
		}
		for _, t := range tms {
			if len(t.Tag) == 0 || len(t.Value) == 0 {
				return errors.New("Empty tag-match pair " + k + " not allowed in JSON listener " + k)
			}
			if ingest.CheckTag(t.Tag) != nil {
				return errors.New("Invalid characters in Tag-Match tag " + t.Tag + " for " + k)
			}
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
		if n, ok := bindMp[v.Bind_String]; ok {
			return errors.New("Bind-String for " + k + " already in use by " + n)
		}
		bindMp[v.Bind_String] = k
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Listener %s preprocessor invalid: %v", k, err)
		}
	}
	if err := checkJsonConfigs(c.JSONListener); err != nil {
		return err
	}
	return nil
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)
	//iterate over simple listeners
	for _, v := range c.Listener {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
	}

	for _, v := range c.RegexListener {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
	}

	//iterate over json listeners
	for _, v := range c.JSONListener {
		tgs, err := v.Tags()
		if err != nil {
			return nil, err
		}
		for _, tg := range tgs {
			if _, ok := tagMp[tg]; !ok {
				tags = append(tags, tg)
				tagMp[tg] = true
			}
		}
	}

	if len(tags) == 0 {
		return nil, errors.New("No tags specified")
	}
	sort.Strings(tags)
	return tags, nil
}

func checkListenerSettings(l *listener) (err error) {
	var lt readerType
	var bt bindType
	if l == nil {
		return errors.New("nil listener")
	}
	if lt, err = translateReaderType(l.Reader_Type); err != nil {
		return
	}
	if bt, _, err = translateBindType(l.Bind_String); err != nil {
		return
	}
	if l.Drop_Priority && !(lt == rfc5424Reader || lt == rfc6587Reader) {
		err = fmt.Errorf("Drop-Priority is not compatible with reader type %s", lt)
		return
	}
	if lt == rfc6587Reader && bt.UDP() {
		err = fmt.Errorf("RFC6587 reader type is not compatible with a UDP bind string")
		return
	}
	return
}

func (l base) Validate() error {
	if len(l.Bind_String) == 0 {
		return errors.New("No Bind-String provided")
	}
	return nil
}

func translateBindType(bstr string) (bindType, string, error) {
	bits := strings.SplitN(bstr, "://", 2)
	//if nothing specified, just return the tcp type
	if len(bits) != 2 {
		return tcp, bstr, nil
	}
	id := strings.ToLower(bits[0])
	switch id {
	case "tcp":
		return tcp, bits[1], nil
	case "udp":
		return udp, bits[1], nil
	case "tcp6":
		return tcp6, bits[1], nil
	case "udp6":
		return udp6, bits[1], nil
	case "tls":
		return TLS, bits[1], nil
	default:
	}
	return -1, "", errors.New("invalid bind protocol specifier of " + id)
}

func (bt bindType) TCP() bool {
	if bt == tcp || bt == tcp6 {
		return true
	}
	return false
}

func (bt bindType) UDP() bool {
	if bt == udp || bt == udp6 {
		return true
	}
	return false
}

func (bt bindType) TLS() bool {
	return bt == TLS
}

func (bt bindType) String() string {
	switch bt {
	case tcp:
		return "tcp"
	case tcp6:
		return "tcp6"
	case udp:
		return "udp"
	case udp6:
		return "udp6"
	case TLS:
		return "tls"
	}
	return "unknown"
}

func translateReaderType(s string) (readerType, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case `line`:
		return lineReader, nil
	case `rfc5424`:
		return rfc5424Reader, nil
	case `rfc6587`:
		return rfc6587Reader, nil
	case ``:
		return lineReader, nil
	}
	return -1, errors.New("invalid reader type")
}

func (rt readerType) String() string {
	switch rt {
	case lineReader:
		return `LINE`
	case rfc5424Reader:
		return `RFC5424`
	case rfc6587Reader:
		return `RFC6587`
	}
	return "UNKNOWN"
}
