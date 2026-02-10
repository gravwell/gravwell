/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/jsonparser"
)

const (
	JsonRouterProcessor = `jsonrouter`
)

var (
	ErrMissingRouteKey   = errors.New("Missing route extraction key")
	ErrMissingJsonRoutes = errors.New("Missing JSON route specifications")
)

type JsonRouteConfig struct {
	Route_Key   string
	Route       []string
	Drop_Misses bool
}

type JsonRouter struct {
	nocloser
	JsonRouteConfig
	routes   map[string]entry.EntryTag
	drops    map[string]struct{}
	routeKey []string
}

func JsonRouteLoadConfig(vc *config.VariableConfig) (c JsonRouteConfig, err error) {
	err = vc.MapTo(&c)
	return
}

func NewJsonRouter(cfg JsonRouteConfig, tagger Tagger) (*JsonRouter, error) {
	jr := &JsonRouter{}
	if err := jr.init(cfg, tagger); err != nil {
		return nil, err
	}
	return jr, nil
}

func (jr *JsonRouter) Config(v interface{}, tagger Tagger) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(JsonRouteConfig); ok {
		err = jr.init(cfg, tagger)
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (jr *JsonRouter) init(cfg JsonRouteConfig, tagger Tagger) (err error) {
	var rts []route
	if jr.routeKey, rts, err = cfg.validate(); err != nil {
		return
	}
	jr.JsonRouteConfig = cfg
	jr.routes = make(map[string]entry.EntryTag)
	jr.drops = make(map[string]struct{}) // routes that are defined but do not have tags are dropped as a one off filter
	// rip through the routes and look for routes that have good source paths and no target tag, those are explicitely dropped
	for _, r := range rts {
		// check for duplicate route values
		if _, ok := jr.routes[r.val]; ok {
			err = fmt.Errorf("Duplicate route value %s", r.val)
			return
		} else if _, ok = jr.drops[r.val]; ok {
			err = fmt.Errorf("Duplicate route value %s", r.val)
			return
		}
		if r.drop {
			jr.drops[r.val] = empty
		} else {
			var tg entry.EntryTag
			if tg, err = tagger.NegotiateTag(r.tag); err != nil {
				err = fmt.Errorf("Failed to get tag %s for %s: %v", r.tag, r.val, err)
				return
			}
			jr.routes[r.val] = tg
		}
	}
	return
}

func (jr *JsonRouter) Process(ents []*entry.Entry) (rset []*entry.Entry, err error) {
	if len(ents) == 0 {
		return
	}
	rset = ents[:0]
	for _, ent := range ents {
		if ent == nil {
			continue
		} else if ent = jr.processItem(ent); ent != nil {
			rset = append(rset, ent)
		}
	}
	return
}

func (jr *JsonRouter) processItem(ent *entry.Entry) *entry.Entry {
	v, _, _, err := jsonparser.Get(ent.Data, jr.routeKey...)
	if err != nil {
		if jr.Drop_Misses {
			return nil
		}
		return ent
	}

	if tag, drop, ok := jr.handleExtract(v); drop {
		// an explicit drop, this happens when we have an explicit route with no tag specification
		return nil
	} else if ok {
		ent.Tag = tag
	} else if jr.Drop_Misses {
		return nil
	}
	return ent
}

// handleEtxract is just a little wrapper to help do the lookup on the two maps
func (jr *JsonRouter) handleExtract(v []byte) (tag entry.EntryTag, drop, ok bool) {
	//check if we have a tag
	if tag, ok = jr.routes[string(v)]; !ok {
		//check if it should be dropped
		if _, drop = jr.drops[string(v)]; !drop {
			if jr.Drop_Misses {
				drop = true // return an explicit drop with a safety check on the config
			}
		}
	}

	return
}

func (jrc JsonRouteConfig) validate() (routeKey []string, rts []route, err error) {
	if jrc.Route_Key == `` {
		err = ErrMissingRouteKey
		return
	} else if len(jrc.Route) == 0 {
		err = ErrMissingJsonRoutes
		return
	}

	// Parse the route key into a path for jsonparser
	routeKey = unquoteFields(splitRespectQuotes(jrc.Route_Key, dotSplitter))

	for _, v := range jrc.Route {
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
