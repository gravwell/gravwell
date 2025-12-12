/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"fmt"
	"regexp"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	RegexReplaceProcessor string = `regexreplace`
)

type RegexReplaceConfig struct {
	Regex         string
	Replacement   string
	CaseSensitive bool
}

func RegexReplaceLoadConfig(vc *config.VariableConfig) (c RegexReplaceConfig, err error) {
	if err = vc.MapTo(&c); err != nil {
		return
	}
	return c, c.validate()
}

func (rerc *RegexReplaceConfig) validate() (err error) {
	if len(rerc.Regex) == 0 {
		err = fmt.Errorf("regex cannot be empty")
		return
	}

	if _, err = regexp.Compile(rerc.Regex); err != nil {
		err = fmt.Errorf("invalid regex: %w", err)
		return
	}

	return
}

type RegexReplacer struct {
	nocloser
	RegexReplaceConfig
	re *regexp.Regexp
}

func NewRegexReplacer(cfg RegexReplaceConfig) (*RegexReplacer, error) {
	re, err := regexp.Compile(cfg.Regex)
	if err != nil {
		return nil, err
	}

	// If case insensitive, compile with case insensitive flag
	if !cfg.CaseSensitive {
		re = regexp.MustCompile("(?i)" + cfg.Regex)
	}

	return &RegexReplacer{
		RegexReplaceConfig: cfg,
		re:                 re,
	}, nil
}

func (rr *RegexReplacer) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(RegexReplaceConfig); ok {
		rr.RegexReplaceConfig = cfg
		if rr.re, err = regexp.Compile(cfg.Regex); err != nil {
			return err
		}
		if !cfg.CaseSensitive {
			rr.re = regexp.MustCompile("(?i)" + cfg.Regex)
		}
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type %T", v)
	}
	return
}

func (rr *RegexReplacer) Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if len(ents) == 0 {
		return nil, nil
	}

	var r []*entry.Entry
	for _, ent := range ents {
		if ent == nil {
			continue
		}
		if set, err := rr.processItem(ent); err != nil {
			continue
		} else if len(set) > 0 {
			r = append(r, set...)
		}
	}
	return r, nil
}

func (rr *RegexReplacer) processItem(ent *entry.Entry) (rset []*entry.Entry, err error) {
	if ent == nil {
		return
	}

	// Create a copy of the entry
	newEnt := &entry.Entry{
		Tag:  ent.Tag,
		SRC:  ent.SRC,
		TS:   ent.TS,
		Data: make([]byte, len(ent.Data)),
	}
	copy(newEnt.Data, ent.Data)

	// Apply regex replacement to the entry data
	if len(newEnt.Data) > 0 {
		// Use ReplaceAllString to replace all matches
		replacedData := rr.re.ReplaceAll(newEnt.Data, []byte(rr.Replacement))
		newEnt.Data = replacedData
	}

	rset = append(rset, newEnt)
	return
}
