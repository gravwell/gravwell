/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	SrcRouterProcessor = `srcrouter`
)

type SrcRouteConfig struct {
	Route_File  string
	Route       []string
	Drop_Misses bool
}

type srcroute struct {
	val  string
	tag  string
	drop bool
}

type SrcRouter struct {
	nocloser
	SrcRouteConfig
	routes map[string]entry.EntryTag
	drops  map[string]struct{}
}

func SrcRouteLoadConfig(vc *config.VariableConfig) (c SrcRouteConfig, err error) {
	err = vc.MapTo(&c)
	return
}

func NewSrcRouter(cfg SrcRouteConfig, tagger Tagger) (*SrcRouter, error) {
	rr := &SrcRouter{}
	if err := rr.init(cfg, tagger); err != nil {
		return nil, err
	}
	return rr, nil
}

func (sr *SrcRouter) Config(v interface{}, tagger Tagger) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(SrcRouteConfig); ok {
		err = sr.init(cfg, tagger)
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (sr *SrcRouter) init(cfg SrcRouteConfig, tagger Tagger) (err error) {
	var rts []srcroute
	if rts, err = cfg.validate(); err != nil {
		return
	}
	sr.SrcRouteConfig = cfg
	sr.routes = make(map[string]entry.EntryTag)
	sr.drops = make(map[string]struct{})
	for _, r := range rts {
		if r.drop {
			sr.drops[r.val] = empty
		} else {
			var tg entry.EntryTag
			if tg, err = tagger.NegotiateTag(r.tag); err != nil {
				err = fmt.Errorf("Failed to get tag %s for %s: %v", r.tag, r.val, err)
				return
			}
			sr.routes[r.val] = tg
		}
	}
	return
}

func (sr *SrcRouter) Process(ents []*entry.Entry) (rset []*entry.Entry, err error) {
	if len(ents) == 0 {
		return
	}
	rset = ents[:0]
	for _, ent := range ents {
		if ent == nil {
			continue
		} else if ent = sr.processItem(ent); ent != nil {
			rset = append(rset, ent)
		}
	}
	return
}

func (sr *SrcRouter) processItem(ent *entry.Entry) *entry.Entry {
	if tag, drop, ok := sr.handleExtract(ent.SRC); drop {
		// They set this src to be dropped
		return nil
	} else if ok {
		// We found a tag to send it to
		ent.Tag = tag
	} else if sr.Drop_Misses {
		// No route found and we're dropping misses
		return nil
	}
	return ent
}

func (sr *SrcRouter) handleExtract(v net.IP) (tag entry.EntryTag, drop, ok bool) {
	//check if we have a tag
	if tag, ok = sr.routes[v.String()]; !ok {
		//check if it should be dropped
		if _, drop = sr.drops[v.String()]; !drop {
			if sr.Drop_Misses {
				drop = true
			}
		}
	}

	return
}

func (src SrcRouteConfig) validate() (rts []srcroute, err error) {
	// If there is a file specified, read that in.
	if len(src.Route_File) > 0 {
		var file *os.File
		if file, err = os.Open(src.Route_File); err != nil {
			return
		}
		defer file.Close()
		sc := bufio.NewScanner(file)
		for sc.Scan() {
			src.Route = append(src.Route, sc.Text())
		}
	}

	if len(src.Route) == 0 {
		err = ErrMissingRoutes
		return
	}
	for _, v := range src.Route {
		var r srcroute
		if r.val, r.tag, err = getSrcRoute(v); err != nil {
			return
		}
		if r.tag == `` {
			r.drop = true
		} else if err = ingest.CheckTag(r.tag); err != nil {
			return
		}
		rts = append(rts, r)
	}
	return
}

func getSrcRoute(v string) (a string, b string, err error) {
	bits := strings.Split(v, splitChar)
	if len(bits) < 2 {
		err = fmt.Errorf("Malformed route specification: %s", v)
	} else {
		l := len(bits)
		b = strings.TrimSpace(bits[l-1])
		t := strings.TrimSpace(strings.Join(bits[:l-1], splitChar))
		ip := net.ParseIP(t)
		if ip == nil {
			// bad IP spec
			err = fmt.Errorf("Invalid IP specification: %v", t)
			return
		}
		a = ip.String()
	}
	return
}
