/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

const (
	maxConfigSize  int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
	defaultMaxBody int   = 4 * 1024 * 1024   //4MB
	defaultLogLoc        = `/opt/gravwell/log/gravwell_http_ingester.log`

	defaultMethod = http.MethodPost
)

type gbl struct {
	config.IngestConfig
	Bind                 string
	Max_Body             int
	TLS_Certificate_File string
	TLS_Key_File         string
	Health_Check_URL     string
}

type cfgReadType struct {
	Global                           gbl
	Listener                         map[string]*lst
	HEC_Compatible_Listener          map[string]*hecCompatible
	Kinesis_Delivery_Stream_Listener map[string]*kds
	Preprocessor                     processors.ProcessorConfig
	TimeFormat                       config.CustomTimeFormat
}

type lst struct {
	auth                             //authentication information
	URL                       string //the URL we will listen to
	Method                    string //method the listener expects
	Tag_Name                  string //the tag to assign to the request
	Multiline                 bool   //each request may have many entries
	Ignore_Timestamps         bool   //Just apply the current timestamp to lines as we get them
	Assume_Local_Timezone     bool
	Timezone_Override         string
	Timestamp_Format_Override string //override the timestamp format
	Preprocessor              []string
}

type cfgType struct {
	gbl
	Listener     map[string]*lst
	HECListener  map[string]*hecCompatible
	KDSListener  map[string]*kds
	Preprocessor processors.ProcessorConfig
	TimeFormat   config.CustomTimeFormat
}

func GetConfig(path string) (*cfgType, error) {
	var cr cfgReadType
	if err := config.LoadConfigFile(&cr, path); err != nil {
		return nil, err
	}
	c := &cfgType{
		gbl:          cr.Global,
		Listener:     cr.Listener,
		HECListener:  cr.HEC_Compatible_Listener,
		KDSListener:  cr.Kinesis_Delivery_Stream_Listener,
		Preprocessor: cr.Preprocessor,
		TimeFormat:   cr.TimeFormat,
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
	if err := c.IngestConfig.Verify(); err != nil {
		return err
	}
	if c.Bind == `` {
		return fmt.Errorf("No bind string specified")
	}
	if err := c.ValidateTLS(); err != nil {
		return err
	}
	urls := map[route]string{}
	if len(c.Listener) == 0 && len(c.HECListener) == 0 && len(c.KDSListener) == 0 {
		return errors.New("No Listeners specified")
	}
	if err := c.Preprocessor.Validate(); err != nil {
		return err
	} else if err = c.TimeFormat.Validate(); err != nil {
		return err
	}
	if hc, ok := c.HealthCheck(); ok {
		urls[newRoute(http.MethodGet, hc)] = `health check`
	}
	for k, v := range c.Listener {
		pth, err := v.validate(k)
		if err != nil {
			return err
		}
		rt := newRoute(v.Method, pth)
		if orig, ok := urls[rt]; ok {
			return fmt.Errorf("%s %s duplicated in %s (was in %s)", v.Method, v.URL, k, orig)
		}
		//validate authentication
		if enabled, err := v.auth.Validate(); err != nil {
			return fmt.Errorf("Auth for %s is invalid: %v", k, err)
		} else if enabled && v.LoginURL != `` {
			//check the url
			if orig, ok := urls[newRoute(http.MethodPost, v.LoginURL)]; ok {
				return fmt.Errorf("%s %s duplicated in %s (was in %s)", v.Method, v.URL, k, orig)
			}
			urls[newRoute(http.MethodPost, v.LoginURL)] = k
		}

		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("HTTP Listener %s preprocessor invalid: %v", k, err)
		}
		urls[rt] = k
		c.Listener[k] = v
	}
	for k, v := range c.HECListener {
		pth, err := v.validate(k)
		if err != nil {
			return err
		}
		rt := newRoute(http.MethodPost, pth)
		if orig, ok := urls[rt]; ok {
			return fmt.Errorf("URL %s duplicated in %s (was in %s)", v.URL, k, orig)
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("HTTP HEC-Compatible-Listener %s preprocessor invalid: %v", k, err)
		}
		urls[rt] = k
		c.HECListener[k] = v
	}

	for k, v := range c.KDSListener {
		pth, err := v.validate(k)
		if err != nil {
			return err
		}
		rt := newRoute(http.MethodPost, pth)
		if orig, ok := urls[rt]; ok {
			return fmt.Errorf("URL %s duplicated in %s (was in %s)", v.URL, k, orig)
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("HTTP Kinesis-Delivery-Stream %s preprocessor invalid: %v", k, err)
		}
		urls[rt] = k
		c.KDSListener[k] = v
	}

	if len(urls) == 0 {
		return fmt.Errorf("No listeners specified")
	}
	return nil
}

// Generate a list of all tags used by this ingester
func (c *cfgType) Tags() (tags []string, err error) {
	tagMp := make(map[string]bool, 1)
	for _, v := range c.Listener {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
	}
	for _, v := range c.HECListener {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
	}
	for _, v := range c.KDSListener {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
	}

	if len(tags) == 0 {
		err = errors.New("No tags specified")
	} else {
		sort.Strings(tags)
	}
	return
}

func (c *cfgType) MaxBody() int {
	if c.Max_Body <= 0 {
		return defaultMaxBody
	}
	return c.Max_Body
}

func (g gbl) ValidateTLS() (err error) {
	if !g.TLSEnabled() {
		//not enabled
	} else if g.TLS_Certificate_File == `` {
		err = errors.New("TLS-Certificate-File argument is missing")
	} else if g.TLS_Key_File == `` {
		err = errors.New("TLS-Key-File argument is missing")
	} else {
		_, err = tls.LoadX509KeyPair(g.TLS_Certificate_File, g.TLS_Key_File)
	}
	return
}

func (g gbl) TLSEnabled() (r bool) {
	r = g.TLS_Certificate_File != `` && g.TLS_Key_File != ``
	return
}

func (g gbl) HealthCheck() (pth string, ok bool) {
	if g.Health_Check_URL != `` {
		if pth = path.Clean(g.Health_Check_URL); pth != `.` {
			ok = true
		}
	}
	return
}

func (v *lst) validate(name string) (string, error) {
	if len(v.URL) == 0 {
		return ``, errors.New("No URL provided for " + name)
	}
	p, err := url.Parse(v.URL)
	if err != nil {
		return ``, fmt.Errorf("URL structure is invalid: %v", err)
	}
	if p.Scheme != `` {
		return ``, errors.New("May not specify scheme in listening URL")
	} else if p.Host != `` {
		return ``, errors.New("May not specify host in listening URL")
	}
	pth := p.Path
	if len(v.Tag_Name) == 0 {
		v.Tag_Name = `default`
	}
	if strings.ContainsAny(v.Tag_Name, ingest.FORBIDDEN_TAG_SET) {
		return ``, errors.New("Invalid characters in the \"" + v.Tag_Name + "\"Tag-Name for " + name)
	}
	//normalize the path
	v.URL = pth
	if v.Method == `` {
		v.Method = defaultMethod
	}
	return pth, nil
}
