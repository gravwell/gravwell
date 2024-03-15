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

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
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
	Global                   gbl
	Attach                   attach.AttachConfig
	Listener                 map[string]*lst
	HEC_Compatible_Listener  map[string]*hecCompatible
	Amazon_Firehose_Listener map[string]*afh
	Preprocessor             processors.ProcessorConfig
	TimeFormat               config.CustomTimeFormat
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
	Attach_URL_Parameter      []string
	Preprocessor              []string
}

type cfgType struct {
	gbl
	Attach       attach.AttachConfig
	Listener     map[string]*lst
	HECListener  map[string]*hecCompatible
	AFHListener  map[string]*afh
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
		gbl:          cr.Global,
		Attach:       cr.Attach,
		Listener:     cr.Listener,
		HECListener:  cr.HEC_Compatible_Listener,
		AFHListener:  cr.Amazon_Firehose_Listener,
		Preprocessor: cr.Preprocessor,
		TimeFormat:   cr.TimeFormat,
	}
	if err := c.Verify(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *cfgType) Verify() error {
	if err := c.IngestConfig.Verify(); err != nil {
		return err
	} else if err = c.Attach.Verify(); err != nil {
		return err
	}
	if c.Bind == `` {
		return fmt.Errorf("No bind string specified")
	}
	if err := c.ValidateTLS(); err != nil {
		return err
	}
	urls := map[route]string{}
	if len(c.Listener) == 0 && len(c.HECListener) == 0 && len(c.AFHListener) == 0 {
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

	for k, v := range c.AFHListener {
		pth, err := v.validate(k)
		if err != nil {
			return err
		}
		rt := newRoute(http.MethodPost, pth)
		if orig, ok := urls[rt]; ok {
			return fmt.Errorf("URL %s duplicated in %s (was in %s)", v.URL, k, orig)
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("HTTP Amazon Firehose %s preprocessor invalid: %v", k, err)
		}
		urls[rt] = k
		c.AFHListener[k] = v
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
	for k, v := range c.HECListener {
		var ltags []string
		if ltags, err = v.tags(); err != nil {
			err = fmt.Errorf("failed to get tags on Hec-Compatible-Listener %s %w", k, err)
			return
		}
		for _, lt := range ltags {
			if _, ok := tagMp[lt]; !ok {
				tags = append(tags, lt)
				tagMp[lt] = true
			}
		}
	}
	for _, v := range c.AFHListener {
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

func (c *cfgType) IngestBaseConfig() config.IngestConfig {
	return c.IngestConfig
}

func (c *cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
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
		v.Tag_Name = entry.DefaultTagName
	}
	if ingest.CheckTag(v.Tag_Name) != nil {
		return ``, errors.New("Invalid characters in the \"" + v.Tag_Name + "\"Tag-Name for " + name)
	}
	//normalize the path
	v.URL = pth
	if v.Method == `` {
		v.Method = defaultMethod
	}
	return pth, nil
}

type paramAttacher struct {
	active bool
	all    bool
	params []string
	exts   []entry.EnumeratedValue
}

func getAttacher(ap []string) paramAttacher {
	if len(ap) == 0 {
		return paramAttacher{} //return a disabled attacher
	} else if len(ap) == 1 && ap[0] == `*` {
		return paramAttacher{
			active: true,
			all:    true,
		}
	}
	r := make([]string, 0, len(ap))
	mp := map[string]bool{}
	for _, p := range ap {
		if len(p) > 0 {
			if _, ok := mp[p]; !ok {
				mp[p] = true
				r = append(r, p)
			}
		}
	}
	pa := paramAttacher{
		active: true,
		params: r,
	}
	return pa
}

func (pa *paramAttacher) process(req *http.Request) {
	if req == nil {
		pa.exts = nil
		return
	} else if pa.active == false {
		return
	}

	if len(pa.exts) > 0 {
		pa.exts = pa.exts[0:0] //keep the slice but truncate it
	}

	if v := req.URL.Query(); len(v) > 0 {
		if pa.all {
			// we are processing everything
			pa.processAll(v)
		} else {
			pa.processSet(v)
		}
	}
	return
}

func (pa *paramAttacher) processSet(vals url.Values) {
	for _, p := range pa.params {
		if val := vals.Get(p); val != `` {
			pa.exts = append(pa.exts, entry.EnumeratedValue{
				Name:  p,
				Value: entry.StringEnumData(val),
			})
		}
	}
}

func (pa *paramAttacher) processAll(vals url.Values) {
	// loop through URL paramters
	for k, v := range vals {
		// only process parameters with a valid key and at least something on the value
		if len(k) > 0 && len(v) > 0 {
			// ignore tag override parameter
			if k != parameterTag {
				// initialize our string value, it will be empty by default
				// then scan the values looking for something that is NOT empty
				// if we find something, set the string value and break
				// if not, then it's an empty string and we use the initialized zero value
				var sval string
				// loop over values and find one that has something, potentially overriding the zero value
				for _, vv := range v {
					if len(vv) > 0 {
						sval = vv
						break
					}
				}
				pa.exts = append(pa.exts, entry.EnumeratedValue{
					Name:  k,
					Value: entry.StringEnumData(sval),
				})
			}
		}
	}
}

func (pa *paramAttacher) attach(ent *entry.Entry) {
	if pa.active && len(pa.exts) > 0 {
		ent.AddEnumeratedValues(pa.exts)
	}
}
