/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package processors

import (
	"errors"
	"fmt"
	"github.com/gravwell/ingest/v3/config"
	"github.com/gravwell/ingest/v3/entry"
	"regexp"
	"strings"
)

const (
	RegexRouterProcessor = `regexrouter`
)

var (
	splitChar = ":"

	ErrMissingRegex           = errors.New("Missing regular expression")
	ErrMissingRouteExtraction = errors.New("Missing route extraction name")
	ErrMissingRoutes          = errors.New("Missing route specifications")
	ErrMissingExtractNames    = errors.New("Regular expression does not extract any names")
)
var empty struct{}

type RegexRouteConfig struct {
	Regex            string
	Route_Extraction string
	Route            []string
	Drop_Misses      bool
}

type route struct {
	val  string
	tag  string
	drop bool
}

type RegexRouter struct {
	RegexRouteConfig
	routes   map[string]entry.EntryTag
	drops    map[string]struct{}
	matchIdx int
	rxp      *regexp.Regexp
}

func RegexRouteLoadConfig(vc *config.VariableConfig) (c RegexRouteConfig, err error) {
	err = vc.MapTo(&c)
	return
}

func NewRegexRouter(cfg RegexRouteConfig, tagger Tagger) (*RegexRouter, error) {
	rr := &RegexRouter{}
	if err := rr.init(cfg, tagger); err != nil {
		return nil, err
	}
	return rr, nil
}

func (rr *RegexRouter) Config(v interface{}, tagger Tagger) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(RegexRouteConfig); ok {
		err = rr.init(cfg, tagger)
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (rr *RegexRouter) init(cfg RegexRouteConfig, tagger Tagger) (err error) {
	var rts []route
	if rr.rxp, rts, rr.matchIdx, err = cfg.validate(); err != nil {
		return
	}
	rr.RegexRouteConfig = cfg
	rr.routes = make(map[string]entry.EntryTag)
	rr.drops = make(map[string]struct{})
	for _, r := range rts {
		if r.drop {
			rr.drops[r.val] = empty
		} else {
			var tg entry.EntryTag
			if tg, err = tagger.NegotiateTag(r.tag); err != nil {
				err = fmt.Errorf("Failed to get tag %s for %s: %v", r.tag, r.val, err)
				return
			}
			rr.routes[r.val] = tg
		}
	}
	return
}

func (rr *RegexRouter) Process(ent *entry.Entry) (rset []*entry.Entry, err error) {
	if ent == nil {
		return
	}
	var tag entry.EntryTag
	var drop bool
	if mtchs := rr.rxp.FindSubmatch(ent.Data); rr.matchIdx < len(mtchs) {
		if tag, drop = rr.handleExtract(mtchs[rr.matchIdx]); drop {
			return
		}
		ent.Tag = tag
		rset = []*entry.Entry{ent}
	} else if !rr.Drop_Misses {
		rset = []*entry.Entry{ent}
	}
	return
}

func (rr *RegexRouter) handleExtract(v []byte) (tag entry.EntryTag, drop bool) {
	//check if we have a tag
	var ok bool
	if tag, ok = rr.routes[string(v)]; !ok {
		//check if it should be dropped
		if _, drop = rr.drops[string(v)]; !drop {
			if rr.Drop_Misses {
				drop = true
			}
		}
	}

	return
}

func (rrc RegexRouteConfig) validate() (rxp *regexp.Regexp, rts []route, idx int, err error) {
	if rrc.Regex == `` {
		err = ErrMissingRegex
		return
	} else if rrc.Route_Extraction == `` {
		err = ErrMissingRouteExtraction
		return
	} else if len(rrc.Route) == 0 {
		err = ErrMissingRoutes
		return
	} else if rxp, err = regexp.Compile(rrc.Regex); err != nil {
		return
	}
	names := rxp.SubexpNames()
	if len(names) == 0 {
		err = ErrMissingExtractNames
		return
	}
	//make sure the extract names contain our Route_Extraction
	idx = -1
	for i, n := range names {
		if rrc.Route_Extraction == n {
			idx = i
		}
	}
	if idx < 0 || idx >= len(names) {
		err = fmt.Errorf("Regular expression does not provide %s", rrc.Route_Extraction)
		return
	}
	for _, v := range rrc.Route {
		var r route
		if r.val, r.tag, err = getRoute(v); err != nil {
			return
		}
		if r.tag == `` {
			r.drop = true
		}
		rts = append(rts, r)
	}
	return
}

func getRoute(v string) (a, b string, err error) {
	bits := strings.Split(v, splitChar)
	if len(bits) < 2 {
		err = fmt.Errorf("Malformed route specification: %s", v)
	} else {
		l := len(bits)
		b = strings.TrimSpace(bits[l-1])
		a = strings.TrimSpace(strings.Join(bits[:l-1], splitChar))
	}
	return
}
