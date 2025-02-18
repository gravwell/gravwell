/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This softwasr may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/syslogparser"
	"github.com/gravwell/syslogparser/rfc3164"
	"github.com/gravwell/syslogparser/rfc5424"
)

const (
	SyslogRouterProcessor = `syslogrouter`
)

type SyslogRouterConfig struct {
	Drop_Misses bool
	Template    string
}

func (src *SyslogRouterConfig) validate() (err error) {
	var f *formatter
	if src.Template == `` {
		err = errors.New("missing Template")
		return
	} else if f, err = newFormatter(src.Template); err != nil {
		return
	}

	//swing through and make sure any constant nodes don't have invalid characters
	for _, n := range f.nodes {
		if cn, ok := n.(*constNode); ok && cn != nil && len(cn.val) > 0 {
			if err = ingest.CheckTag(string(cn.val)); err != nil {
				err = fmt.Errorf("constant value %q violates tag spec %w", string(cn.val), err)
				return
			}
		}
	}
	return
}

func SyslogRouterLoadConfig(vc *config.VariableConfig) (c SyslogRouterConfig, err error) {
	if err = vc.MapTo(&c); err == nil {
		err = c.validate()
	}
	return
}

type SyslogRouter struct {
	nocloser
	SyslogRouterConfig
	tagger Tagger
	routes map[string]entry.EntryTag
	tmp    *formatter
}

func NewSyslogRouter(cfg SyslogRouterConfig, tagger Tagger) (*SyslogRouter, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	tmp, err := newFormatter(cfg.Template)
	if err != nil {
		return nil, err
	}

	return &SyslogRouter{
		SyslogRouterConfig: cfg,
		routes:             make(map[string]entry.EntryTag),
		tagger:             tagger,
		tmp:                tmp,
	}, nil
}

func (sr *SyslogRouter) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(SyslogRouterConfig); ok {
		if err = cfg.validate(); err == nil {
			if sr.tmp, err = newFormatter(cfg.Template); err != nil {
				return
			}
			sr.SyslogRouterConfig = cfg
		}
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (sr *SyslogRouter) Process(ents []*entry.Entry) (rset []*entry.Entry, err error) {
	if len(ents) == 0 {
		return
	}
	rset = ents[:0]
	for _, ent := range ents {
		parts := crackData(ent.Data)
		if parts == nil {
			if !sr.Drop_Misses {
				rset = append(rset, ent)
			}
			continue
		}
		//got a good crack, process it
		if tag, err := sr.processEntry(ent, parts); err != nil {
			if sr.Drop_Misses {
				continue
			}
		} else {
			ent.Tag = tag
		}
		rset = append(rset, ent)
	}
	return
}

func crackData(data []byte) (parts syslogparser.LogParts) {
	tp, err := syslogparser.DetectRFC(data)
	if err != nil || !(tp == syslogparser.RFC_3164 || tp == syslogparser.RFC_5424) {
		return
	}
	// at this point there was no error and the type MUST be either 3164 or 5424
	if tp == syslogparser.RFC_3164 {
		if p := rfc3164.NewParser(data); p != nil {
			if err = p.Parse(); err != nil {
				return
			}
			parts = p.Dump()
		}
	} else if tp == syslogparser.RFC_5424 {
		if p := rfc5424.NewParser(data); p != nil {
			if err := p.Parse(); err != nil {
				return
			}
			parts = p.Dump()
		}
	}
	if len(parts) == 0 {
		parts = nil
	}
	return
}

type getter struct {
	parts syslogparser.LogParts
}

func (g getter) Get(v string) interface{} {
	if val, ok := g.parts[v]; ok && val != nil {
		// if its a string and empty, return nil
		if s, ok := val.(string); ok && s == `-` {
			return nil
		}
		return val
	}
	return nil
}

const subChar rune = '_'

func remapTagCharacters(orig string) (ret string, err error) {
	mf := func(r rune) rune {
		if strings.IndexRune(ingest.FORBIDDEN_TAG_SET, r) != -1 {
			return subChar
		}
		return r
	}
	ret = strings.Map(mf, orig)
	err = ingest.CheckTag(ret)
	return
}

func (sr *SyslogRouter) processEntry(ent *entry.Entry, parts syslogparser.LogParts) (tag entry.EntryTag, err error) {
	var tagname string
	var ok bool
	if ent == nil {
		return
	}
	if tagname = sr.tmp.renderWithAccessor(ent, getter{parts: parts}); tagname != `` {
		//check tag
		if err = ingest.CheckTag(tagname); err != nil {
			//tag has invalid stuff, remap it
			if tagname, err = ingest.RemapTag(tagname, subChar); err != nil {
				return
			}
		}
	}
	//tagname is good, try to resolve it
	if tag, ok = sr.routes[tagname]; !ok {
		//try to negotiate
		if tag, err = sr.tagger.NegotiateTag(tagname); err == nil {
			sr.routes[tagname] = tag
		}
	}
	return
}
