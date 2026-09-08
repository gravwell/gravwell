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
	"regexp"

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const (
	RegexExtractProcessor = `regexextract`
)

type RegexExtractConfig struct {
	Passthrough_Misses bool //deprecated DO NOT USE
	Drop_Misses        bool
	Regex              string
	Template           string
	Attach             []string // list of regular expression items to attach as intrinsic EVs
}

func RegexExtractLoadConfig(vc *config.VariableConfig) (c RegexExtractConfig, err error) {
	//to support legacy config, set Passthrough_Misses to true so that we can catch them setting it to false
	//default is now to send them through
	c.Passthrough_Misses = true
	if err = vc.MapTo(&c); err == nil {
		_, _, _, err = c.validate()
	}
	return
}

func stringInSet(v string, set []string) int {
	for i, s := range set {
		if s == v {
			return i
		}
	}
	return -1
}

func (c *RegexExtractConfig) genAttachFields(names []string) (af []attachFields, err error) {
	if len(c.Attach) == 0 {
		return
	}
	for _, name := range c.Attach {
		if len(name) == 0 {
			continue
		}
		idx := stringInSet(name, names)
		if idx == -1 {
			err = fmt.Errorf("%s is not extracted in the regular expression", name)
			return
		}
		af = append(af, attachFields{
			name:     name,
			matchIdx: idx,
		})
	}

	return
}

func (c *RegexExtractConfig) validate() (rx *regexp.Regexp, tmp *formatter, af []attachFields, err error) {
	if c.Regex == `` {
		err = errors.New("Missing regular expression")
		return
	} else if c.Template == `` {
		err = errors.New("Missing template")
		return
	} else if tmp, err = newFormatter(c.Template); err != nil {
		return
	} else if rx, err = regexp.Compile(c.Regex); err != nil {
		return
	}
	names := rx.SubexpNames()
	if len(names) == 0 {
		err = ErrMissingExtractNames
		return
	}
	//handle the legacy config items and potential overrides
	// if Drop-Misses is set, that overrides everything
	if !c.Drop_Misses {
		if !c.Passthrough_Misses {
			c.Drop_Misses = true
		}
	}

	if err = tmp.setReplaceNames(names); err != nil {
		return
	}
	af, err = c.genAttachFields(names)
	return
}

type RegexExtractor struct {
	nocloser
	RegexExtractConfig
	tmp       *formatter
	rx        *regexp.Regexp
	cnt       int
	attachSet []attachFields
}

type attachFields struct {
	name     string
	matchIdx int
}

func NewRegexExtractor(cfg RegexExtractConfig) (*RegexExtractor, error) {
	rx, tmp, atch, err := cfg.validate()
	if err != nil {
		return nil, err
	}

	return &RegexExtractor{
		RegexExtractConfig: cfg,
		tmp:                tmp,
		rx:                 rx,
		attachSet:          atch,
		cnt:                len(rx.SubexpNames()),
	}, nil
}

func (re *RegexExtractor) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(RegexExtractConfig); ok {
		if re.rx, re.tmp, re.attachSet, err = cfg.validate(); err == nil {
			re.RegexExtractConfig = cfg
		}
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (re *RegexExtractor) Process(ents []*entry.Entry) (rset []*entry.Entry, err error) {
	if len(ents) == 0 {
		return
	}
	rset = ents[:0]
	for _, ent := range ents {
		if ent, err = re.processEntry(ent); err != nil {
			return
		} else if ent != nil {
			rset = append(rset, ent)
		}
	}
	return
}

func (re *RegexExtractor) processEntry(ent *entry.Entry) (*entry.Entry, error) {
	if ent == nil {
		return nil, nil
	}
	if mtchs := re.rx.FindSubmatch(ent.Data); len(mtchs) == re.cnt {
		ent.Data = re.tmp.render(ent, mtchs)
		if len(re.attachSet) > 0 {
			re.performAttaches(ent, mtchs)
		}
	} else if re.Drop_Misses {
		//NOT passing through misses, so set ent to nil, this is a DROP
		ent = nil
	}
	return ent, nil
}

func (re *RegexExtractor) performAttaches(ent *entry.Entry, matches [][]byte) {
	for _, a := range re.attachSet {
		if a.matchIdx < len(matches) {
			if m := matches[a.matchIdx]; m != nil {
				ent.AddEnumeratedValue(
					entry.EnumeratedValue{
						Name:  a.name,
						Value: entry.StringEnumData(string(m)),
					},
				)
			}
		}
	}
}
