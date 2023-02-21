/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	defaultHECUrl      string = `/services/collector`
	defaultLineBreaker string = "\n"
)

type hecCompatible struct {
	URL               string //override the base URL, defaults to "/services/collector/event"
	Raw_Line_Breaker  string // character(s) to use as line breakers on the raw endpoint. Default "\n"
	TokenValue        string `json:"-"` //DO NOT SEND THIS when marshalling
	Tag_Name          string //the tag to assign to the request
	Ignore_Timestamps bool
	Ack               bool
	Max_Size          int
	Tag_Match         []string
	Preprocessor      []string
}

func (v *hecCompatible) validate(name string) (string, error) {
	if len(v.URL) == 0 {
		v.URL = defaultHECUrl
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
	if len(v.Raw_Line_Breaker) == 0 {
		v.Raw_Line_Breaker = defaultLineBreaker
	}
	if strings.ContainsAny(v.Tag_Name, ingest.FORBIDDEN_TAG_SET) {
		return ``, errors.New("Invalid characters in the \"" + v.Tag_Name + "\"Tag-Name for " + name)
	}

	//check the Tag_Match member
	if _, err = v.tagMatchers(); err != nil {
		return ``, fmt.Errorf("HEC-Compatible-Listener %s has invalid Tag-Match %w", name, err)
	}

	//normalize the path
	v.URL = pth
	return pth, nil
}

type tagMatcher struct {
	Value string
	Tag   string
}

func (h *hecCompatible) tagMatchers() (tags []tagMatcher, err error) {
	if len(h.Tag_Match) == 0 {
		return
	}
	var tm tagMatcher
	//process each of the tag matches
	for i := range h.Tag_Match {
		if tm.Value, tm.Tag, err = extractElementTag(h.Tag_Match[i]); err != nil {
			break
		}
		tags = append(tags, tm)
	}
	return
}

func (h *hecCompatible) tags() (tags []string, err error) {
	var tms []tagMatcher
	tags = []string{h.Tag_Name}
	if tms, err = h.tagMatchers(); err != nil || len(tms) == 0 {
		return
	}
	mp := map[string]bool{
		h.Tag_Name: true,
	}
	for _, tm := range tms {
		if _, ok := mp[tm.Tag]; !ok {
			mp[tm.Tag] = true
			tags = append(tags, tm.Tag)
		}
	}

	return
}

func (h *hecCompatible) loadTagRouter(igst *ingest.IngestMuxer) (mp map[string]entry.EntryTag) {
	if igst == nil || len(h.Tag_Match) == 0 {
		return
	}
	if tm, err := h.tagMatchers(); err == nil && len(tm) > 0 {
		mp = make(map[string]entry.EntryTag, len(tm))
		for _, v := range tm {
			if tag, err := igst.NegotiateTag(v.Tag); err == nil {
				mp[v.Value] = tag
			}
		}
	}
	return
}

func extractElementTag(v string) (match, tag string, err error) {
	cr := csv.NewReader(strings.NewReader(v))
	cr.Comma = ':'
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = 2
	var rec []string
	if rec, err = cr.Read(); err != nil {
		return
	} else if len(rec) != 2 {
		err = fmt.Errorf("Tag-Match specification of %q is invalid", v)
		return
	}
	match, tag = rec[0], rec[1]
	if match == `` {
		err = fmt.Errorf("Tag-Match value %s for %q specification is invalid", match, v)
	} else if tag == `` {
		err = fmt.Errorf("Tag-Match tag name %s for %q specification is invalid", tag, v)
	} else if err = ingest.CheckTag(tag); err != nil {
		err = fmt.Errorf("Tag-Match %q has an invalid tag name %q - %w", v, tag, err)
	}
	return
}

func includeHecListeners(hnd *handler, igst *ingest.IngestMuxer, cfg *cfgType, lgr *log.Logger) (err error) {
	for k, v := range cfg.HECListener {
		hh := &hecHandler{
			hecHealth: hecHealth{
				igst:  hnd.igst,
				token: v.TokenValue,
			},
			rawLineBreaker: v.Raw_Line_Breaker,
			name:           k,
			maxSize:        fixupMaxSize(v.Max_Size),
			tagRouter:      v.loadTagRouter(igst),
		}
		hcfg := routeHandler{
			handler: hh.handle,
		}
		if hcfg.tag, err = igst.GetTag(v.Tag_Name); err != nil {
			lg.Error("failed to pull tag", log.KV("tag", v.Tag_Name), log.KVErr(err))
			return
		}
		if v.Ignore_Timestamps {
			hcfg.ignoreTs = true
		} else {
			if hcfg.tg, err = timegrinder.New(timegrinder.Config{}); err != nil {
				lg.Error("Failed to create timegrinder", log.KVErr(err))
				return
			}
		}

		if hcfg.pproc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Error("preprocessor construction error", log.KVErr(err))
			return
		}
		if hcfg.auth, err = newPresharedTokenHandler(`Splunk`, v.TokenValue, lgr); err != nil {
			lg.Error("failed to generate HEC-Compatible-Listener auth", log.KVErr(err))
			return
		}
		bp := path.Dir(v.URL)
		//had the main handler for events
		if err = hnd.addHandler(http.MethodPost, v.URL, hcfg); err != nil {
			lg.Error("failed to add HEC-Compatible-Listener handler", log.KVErr(err))
			return
		}
		// the `/event` path just acts like the root
		if err = hnd.addHandler(http.MethodPost, path.Join(bp, `event`), hcfg); err != nil {
			lg.Error("failed to add HEC-Compatible-Listener handler", log.KVErr(err))
			return
		}
		// add the other handlers for health, ack, and raw mode
		if err = hnd.addCustomHandler(http.MethodPost, path.Join(bp, `ack`), hh); err != nil {
			lg.Error("failed to add HEC-Compatible-Listener ACK handler", log.KVErr(err))
			return
		}
		if err = hnd.addCustomHandler(http.MethodGet, path.Join(bp, `health`), &hh.hecHealth); err != nil {
			lg.Error("failed to add HEC-Compatible-Listener ACK health handler", log.KVErr(err))
			return
		}
		// add in the raw handler
		hcfg.handler = hh.handleRaw
		if err = hnd.addHandler(http.MethodPost, path.Join(bp, `raw`), hcfg); err != nil {
			lg.Error("failed to add HEC-Compatible-Listener handler", log.KVErr(err))
			return
		}

		debugout("HEC Handler URL %s handling %s\n", v.URL, v.Tag_Name)
	}
	return
}
