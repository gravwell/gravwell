/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	TagSrcRouterProcessor = "tagrouter"
)

var (
	ErrNoRoutesDefined = errors.New("no routes defined")
)

type tagRoute struct {
	dstTag       entry.EntryTag
	isIPFiltered bool
	addr         net.IP
	isIPSubnet   bool
	subnet       net.IPNet
}

type TagSrcRouterConfig struct {
	Route []string
}

type TagSrcRouter struct {
	nocloser
	TagSrcRouterConfig
	tg     Tagger
	routes map[string]tagRoute
}

func TagSrcRouterLoadConfig(vc *config.VariableConfig) (c TagSrcRouterConfig, err error) {
	if err = vc.MapTo(&c); err != nil {
		return
	}
	if len(c.Route) == 0 {
		err = ErrNoRoutesDefined
	}
	return
}

// parseRoutes parses out the Route rules in string form into native tag routes
func (tsr TagSrcRouterConfig) parseRoutes(tgr Tagger) (map[string]tagRoute, error) {
	routes := make(map[string]tagRoute, len(tsr.Route))

	for _, rule := range tsr.Route {
		parts := strings.Split(rule, ":")
		ruleLen := len(parts)
		// 2 entries srcTag:dstTag
		// 3 entries srcTag:dstTag:ipFilter
		if ruleLen != 2 && ruleLen != 3 {
			return nil, fmt.Errorf("Invalid route definition: %s", rule)
		}

		srcTagName := parts[0]
		if err := ingest.CheckTag(srcTagName); err != nil {
			return nil, fmt.Errorf("invalid source tag %q %w", srcTagName, err)
		}
		if err := ingest.CheckTag(parts[1]); err != nil {
			return nil, fmt.Errorf("invalid destination tag %q %w", parts[1], err)
		}

		dstTag, err := tgr.NegotiateTag(parts[1])
		if err != nil {
			return nil, fmt.Errorf("Failed to Negotiate tag for %s: %w", parts[1], err)
		}

		rt := tagRoute{dstTag: dstTag}

		if ruleLen == 3 {
			rt.isIPFiltered = true
			ipFilter := parts[2]

			if ip := net.ParseIP(ipFilter); ip != nil {
				rt.addr = ip
			} else if ip, subnet, err := net.ParseCIDR(ipFilter); err == nil {
				rt.isIPSubnet = true
				rt.addr = ip
				rt.subnet = *subnet
			} else {
				return nil, fmt.Errorf("Invalid IP filter in route definition: %s", rule)
			}
		}

		routes[srcTagName] = rt
	}

	return routes, nil
}

func NewTagSrcRouter(cfg TagSrcRouterConfig, tagger Tagger) (*TagSrcRouter, error) {
	tsr := &TagSrcRouter{}
	if err := tsr.init(cfg, tagger); err != nil {
		return nil, err
	}
	return tsr, nil
}

func (tsr *TagSrcRouter) init(cfg TagSrcRouterConfig, tagger Tagger) error {
	if len(cfg.Route) == 0 {
		return ErrNoRoutesDefined
	} else if tagger == nil {
		return errors.New("nil tagger")
	}
	routes, err := cfg.parseRoutes(tagger)
	if err != nil {
		return err
	}
	tsr.tg = tagger
	tsr.routes = routes
	return nil
}

func (tsr *TagSrcRouter) Config(v interface{}, tagger Tagger) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(TagSrcRouterConfig); ok {
		err = tsr.init(cfg, tagger)
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (tsr *TagSrcRouter) Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	for _, ent := range ents {
		// Check if gravwell tag and let it pass through
		if ent.Tag == entry.GravwellTagId {
			continue
		}

		tagName, ok := tsr.tg.LookupTag(ent.Tag)
		if !ok {
			continue // pass unknown tags through
		}

		rt, ok := tsr.routes[tagName]
		if !ok {
			continue
		}

		if !rt.isIPFiltered {
			ent.Tag = rt.dstTag
			break
		} else {
			srcIP := ent.SRC
			if (!rt.isIPSubnet && rt.addr.Equal(srcIP)) || (rt.isIPSubnet && rt.subnet.Contains(srcIP)) {
				ent.Tag = rt.dstTag
			}
			break
		}
	}
	return ents, nil
}
