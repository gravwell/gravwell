/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	CSVRouterProcessor = `csvrouter`
)

var (
	ErrInvalidColumnIndex = errors.New("Invalid column index")
)

type CSVRouteConfig struct {
	Route_Extraction int
	Route            []string
	Drop_Misses      bool
}

type CSVRouter struct {
	nocloser
	CSVRouteConfig
	routes map[string]entry.EntryTag
	drops  map[string]struct{}
}

func CSVRouteLoadConfig(vc *config.VariableConfig) (c CSVRouteConfig, err error) {
	err = vc.MapTo(&c)
	return
}

func NewCSVRouter(cfg CSVRouteConfig, tagger Tagger) (*CSVRouter, error) {
	cr := &CSVRouter{}
	if err := cr.init(cfg, tagger); err != nil {
		return nil, err
	}
	return cr, nil
}

func (cr *CSVRouter) Config(v interface{}, tagger Tagger) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(CSVRouteConfig); ok {
		err = cr.init(cfg, tagger)
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (cr *CSVRouter) init(cfg CSVRouteConfig, tagger Tagger) (err error) {
	var rts []route
	if rts, err = cfg.validate(); err != nil {
		return
	}
	cr.CSVRouteConfig = cfg
	cr.routes = make(map[string]entry.EntryTag)
	cr.drops = make(map[string]struct{})
	for _, r := range rts {
		if r.drop {
			cr.drops[r.val] = empty
		} else {
			var tg entry.EntryTag
			if tg, err = tagger.NegotiateTag(r.tag); err != nil {
				err = fmt.Errorf("Failed to get tag %s for %s: %v", r.tag, r.val, err)
				return
			}
			cr.routes[r.val] = tg
		}
	}
	return
}

func (cr *CSVRouter) Process(ents []*entry.Entry) (rset []*entry.Entry, err error) {
	if len(ents) == 0 {
		return
	}
	rset = ents[:0]
	for _, ent := range ents {
		if ent == nil {
			continue
		} else if ent = cr.processItem(ent); ent != nil {
			rset = append(rset, ent)
		}
	}
	return
}

func (cr *CSVRouter) processItem(ent *entry.Entry) *entry.Entry {
	r := csv.NewReader(bytes.NewReader(ent.Data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = false
	r.TrimLeadingSpace = true

	if fields, err := r.Read(); err == nil && cr.CSVRouteConfig.Route_Extraction < len(fields) {
		if tag, drop, ok := cr.handleExtract(fields[cr.CSVRouteConfig.Route_Extraction]); drop {
			return nil
		} else if ok {
			ent.Tag = tag
		}
	} else if cr.Drop_Misses {
		return nil
	}
	return ent
}

func (cr *CSVRouter) handleExtract(v string) (tag entry.EntryTag, drop, ok bool) {
	//check if we have a tag
	if tag, ok = cr.routes[string(v)]; !ok {
		//check if it should be dropped
		if _, drop = cr.drops[string(v)]; !drop {
			if cr.Drop_Misses {
				drop = true
			}
		}
	}

	return
}

func (rrc CSVRouteConfig) validate() (rts []route, err error) {
	if len(rrc.Route) == 0 {
		err = ErrMissingRoutes
		return
	} else if rrc.Route_Extraction < 0 {
		err = ErrInvalidColumnIndex
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
