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

	"github.com/asergeyev/nradix"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
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
	tree *nradix.Tree
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
	sr.tree = nradix.NewTree(32) // it will grow, but make a decent starting point
	for _, r := range rts {
		//check if the item already exists
		if r.drop {
			if err = sr.tree.AddCIDR(r.val, false); err != nil {
				err = fmt.Errorf("Failed to add %q: %v", r.val, err)
				return
			}
		} else {
			var tg entry.EntryTag
			if tg, err = tagger.NegotiateTag(r.tag); err != nil {
				err = fmt.Errorf("Failed to get tag %s for %s: %v", r.tag, r.val, err)
				return
			} else if err = sr.tree.AddCIDR(r.val, tg); err != nil {
				err = fmt.Errorf("Failed to add %q: %v", r.val, err)
				return
			}
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
	}
	return ent
}

func (sr *SrcRouter) handleExtract(v net.IP) (tag entry.EntryTag, drop, ok bool) {
	//check if we have a tag
	var r interface{}
	if v != nil {
		r, _ = sr.tree.FindCIDR(v.String())
	}
	if r == nil {
		drop = sr.Drop_Misses //straight not found
	} else if _, ok = r.(bool); ok {
		drop = true
	} else if tag, ok = r.(entry.EntryTag); !ok {
		//found, but we can't convert it, this REALLY should never happen
		drop = sr.Drop_Misses
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
		//attempt to parse as a CIDER
		if _, _, err = net.ParseCIDR(t); err != nil {
			//try to parse as an IP
			if ip := net.ParseIP(t); ip == nil {
				err = fmt.Errorf("Invalid IP specification: %v", t)
				return
			} else if ip.To4() != nil {
				t = ip.String() + "/32"
				err = nil
			} else {
				t = ip.String() + "/128"
				err = nil
			}
		}
		//its valid, just hand back the string
		a = t
	}
	return
}
