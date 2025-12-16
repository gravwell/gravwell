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

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	RegexDropProcessor string = `regexdrop`
)

type RegexDropConfig struct {
	Regex  string // Regex is the regular expression to match against
	Invert bool   // Invert negates the match, if true entries that match are kept
}

// RegexDropLoadConfig loads the configuration for the regexdrop processor
func RegexDropLoadConfig(vc *config.VariableConfig) (c RegexDropConfig, err error) {
	if err = vc.MapTo(&c); err != nil {
		return
	}
	_, err = c.validate()
	return
}

func (c *RegexDropConfig) validate() (rx *regexp.Regexp, err error) {
	if len(c.Regex) == 0 {
		return nil, errors.New("Regex is required")
	}
	if rx, err = regexp.Compile(c.Regex); err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}
	return
}

// NewRegexDropper creates a new regexdrop processor
func NewRegexDropper(cfg RegexDropConfig) (*RegexDropper, error) {
	rx, err := cfg.validate()
	if err != nil {
		return nil, err
	}
	return &RegexDropper{
		RegexDropConfig: cfg,
		rx:              rx,
	}, nil
}

type RegexDropper struct {
	nocloser
	RegexDropConfig
	rx *regexp.Regexp
}

// Config updates the configuration for the regexdrop processor
func (r *RegexDropper) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(RegexDropConfig); ok {
		if r.rx, err = cfg.validate(); err != nil {
			return
		}
		r.RegexDropConfig = cfg
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type %T", v)
	}
	return
}

// Process filters entries based on regex match
// If Invert is false: drops entries that match (keeps non-matches)
// If Invert is true: drops entries that don't match (keeps only matches)
func (r *RegexDropper) Process(ents []*entry.Entry) (rset []*entry.Entry, err error) {
	if len(ents) == 0 {
		return
	}
	rset = ents[:0]
	for _, ent := range ents {
		if ent == nil {
			continue
		}
		if r.rx.Match(ent.Data) == r.Invert {
			rset = append(rset, ent)
		}
	}
	return
}
