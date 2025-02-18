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

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/timegrinder"
)

const (
	defaultHECUrl       string = `/services/collector`
	defaultLineBreaker  string = "\n"
	defaultHECTokenName string = `Splunk`
)

type hecCompatible struct {
	URL                       string   //override the base URL, defaults to "/services/collector/event"
	Raw_Line_Breaker          string   // character(s) to use as line breakers on the raw endpoint. Default "\n"
	TokenValue                string   `json:"-"` //DO NOT SEND THIS when marshalling
	Token_Value               string   `json:"-"` //DO NOT SEND THIS when marshalling
	Token_Name                string   `json:"-"` // DO NOT SEND THIS, used when overriding the auth token prefix away from "Splunk"
	Routed_Token_Value        []string `json:"-"` // DO NOT SEND THIS when marshalling, used for tag routing based on token
	Tag_Name                  string   //the tag to assign to the request
	Ignore_Timestamps         bool
	Timestamp_Format_Override string //override the timestamp format (only used for raw)
	Ack                       bool
	Max_Size                  int
	Debug_Posts               bool // whether we are going to log on the gravwell tag about posts
	Attach_URL_Parameter      []string
	Tag_Match                 []string
	Preprocessor              []string
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
	if ingest.CheckTag(v.Tag_Name) != nil {
		return ``, errors.New("Invalid characters in the \"" + v.Tag_Name + "\"Tag-Name for " + name)
	}
	// deal with the bad format of TokenValue vs Token-Value
	if len(v.TokenValue) == 0 && len(v.Token_Value) != 0 {
		v.TokenValue = v.Token_Value
	}
	if len(v.TokenValue) == 0 && len(v.Routed_Token_Value) == 0 {
		return ``, errors.New("No tokens specified, missing TokenValue and Routed-Token-Value")
	}

	//check the Tag_Match member
	if _, err = v.sourcetypeTagMatchers(); err != nil {
		return ``, fmt.Errorf("HEC-Compatible-Listener %s has invalid Tag-Match %w", name, err)
	}

	if _, err = v.tokenTagMatchers(); err != nil {
		return ``, fmt.Errorf("HEC-Compatible-Listener %s has an invalid tag in Routed-Token-Value: %w", name, err)
	}

	//normalize the path
	v.URL = pth
	return pth, nil
}

type tagMatcher struct {
	Value string
	Tag   string
}

func (h *hecCompatible) sourcetypeTagMatchers() (tags []tagMatcher, err error) {
	if len(h.Tag_Match) == 0 {
		return
	}
	var tm tagMatcher
	//process each of the tag matches
	for i := range h.Tag_Match {
		if tm.Value, tm.Tag, err = extractElementTag(h.Tag_Match[i]); err != nil {
			break
		} else if err = ingest.CheckTag(tm.Tag); err != nil {
			break
		}
		tags = append(tags, tm)
	}
	return
}

func (h *hecCompatible) tokenTagMatchers() (tags []tagMatcher, err error) {
	if len(h.Routed_Token_Value) == 0 {
		return
	}
	var tm tagMatcher
	//process each of the tag matches
	for _, v := range h.Routed_Token_Value {
		if tm.Value, tm.Tag, err = extractElementTag(v); err != nil {
			break
		} else if err = ingest.CheckTag(tm.Tag); err != nil {
			break
		}
		tags = append(tags, tm)
	}
	return
}

func (h *hecCompatible) tags() (tags []string, err error) {
	var stms, ttms []tagMatcher
	if stms, err = h.sourcetypeTagMatchers(); err != nil {
		return
	} else if ttms, err = h.tokenTagMatchers(); err != nil {
		return
	} else if len(stms) == 0 && len(ttms) == 0 && h.Tag_Name == `` {
		//no tags anywhere, just bail
		return
	}
	mp := map[string]bool{}
	//if there is a default tag name, add it, there does not HAVE to be
	if h.Tag_Name != `` {
		tags = []string{h.Tag_Name}
		mp[h.Tag_Name] = true
	}
	//load up the sourcetype overrides
	for _, tm := range stms {
		if _, ok := mp[tm.Tag]; !ok {
			mp[tm.Tag] = true
			tags = append(tags, tm.Tag)
		}
	}

	//load up the token type overrides
	for _, tm := range ttms {
		if _, ok := mp[tm.Tag]; !ok {
			mp[tm.Tag] = true
			tags = append(tags, tm.Tag)
		}
	}

	return
}

func (h *hecCompatible) loadSourcetypeTagRouter(igst *ingest.IngestMuxer) (mp map[string]entry.EntryTag) {
	if igst == nil || len(h.Tag_Match) == 0 {
		return
	}
	if tm, err := h.sourcetypeTagMatchers(); err == nil && len(tm) > 0 {
		mp = make(map[string]entry.EntryTag, len(tm))
		for _, v := range tm {
			if tag, err := igst.NegotiateTag(v.Tag); err == nil {
				mp[v.Value] = tag
			}
		}
	}
	return
}

func (h *hecCompatible) loadTokenTagRouter(igst *ingest.IngestMuxer) (mp map[string]entry.EntryTag) {
	if igst == nil || len(h.Routed_Token_Value) == 0 {
		return
	}
	if tm, err := h.tokenTagMatchers(); err == nil && len(tm) > 0 {
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
			ackIds: map[string]uint64{},
			hecHealth: hecHealth{
				igst:  hnd.igst,
				token: v.TokenValue,
			},
			rawLineBreaker: v.Raw_Line_Breaker,
			name:           k,
			maxSize:        fixupMaxSize(v.Max_Size),
			debugPosts:     v.Debug_Posts,
			tagRouter:      v.loadSourcetypeTagRouter(igst),
			tokenRouter:    v.loadTokenTagRouter(igst),
		}
		if hh.auth, err = newHecAuth(v, igst); err != nil {
			lg.Error("HEC authentication error", log.KVErr(err))
		}
		hcfg := routeHandler{
			handler:       hh.handle,
			paramAttacher: getAttacher(v.Attach_URL_Parameter),
			auth:          hh.auth,
		}

		if hcfg.tag, err = igst.NegotiateTag(v.Tag_Name); err != nil {
			lg.Error("failed to pull tag", log.KV("tag", v.Tag_Name), log.KVErr(err))
			return
		}
		if v.Ignore_Timestamps {
			hcfg.ignoreTs = true
		} else {
			var window timegrinder.TimestampWindow
			window, err = cfg.GlobalTimestampWindow()
			if err != nil {
				lg.Error("Failed to get global timestamp window", log.KVErr(err))
				return
			}
			if hcfg.tg, err = timegrinder.New(timegrinder.Config{TSWindow: window}); err != nil {
				lg.Error("Failed to create timegrinder", log.KVErr(err))
				return
			} else if err = cfg.TimeFormat.LoadFormats(hcfg.tg); err != nil {
				lg.Error("failed to load custom time formats", log.KVErr(err))
				return
			}
			if v.Timestamp_Format_Override != `` {
				if err = hcfg.tg.SetFormatOverride(v.Timestamp_Format_Override); err != nil {
					lg.Fatal("Failed to set override timestamp", log.KVErr(err))
				}
			}
		}

		if hcfg.pproc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Error("preprocessor construction error", log.KVErr(err))
			return
		}
		bp := v.URL
		// detect if you're specifying `URL=/services/collector/event` in the old way and handle it sneakily
		if path.Base(bp) == "event" {
			bp = path.Dir(bp)
		}
		//had the main handler for events
		if err = hnd.addHandler(http.MethodPost, bp, hcfg); err != nil {
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
