/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	RegexExtractProcessor = `regexextract`
)

var (
	st  = []byte(`${`)
	end = []byte(`}`)
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
	if c.Drop_Misses == false {
		if c.Passthrough_Misses == false {
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
	return
}

type replaceNode interface {
	Bytes(*entry.Entry, [][]byte) []byte
}

type formatter struct {
	nodes []replaceNode
	bb    *bytes.Buffer
}

func newFormatter(s string) (f *formatter, err error) {
	var nodes []replaceNode
	v := []byte(s)
	for len(v) > 0 {
		var n replaceNode
		if n, v, err = consumeNode(v); err != nil {
			return
		}
		nodes = append(nodes, n)

	}
	f = &formatter{
		nodes: nodes,
		bb:    bytes.NewBuffer(nil),
	}
	return
}

func (f *formatter) setReplaceNames(names []string) (err error) {
	for i := range f.nodes {
		if lu, ok := f.nodes[i].(*lookupNode); ok {
			if lu.idx = getStringIndex(lu.name, names); lu.idx == -1 {
				err = fmt.Errorf("Replacement name %s not found in regular expression list", lu.name)
				break
			}
		}
	}
	return
}

func (f *formatter) render(ent *entry.Entry, vals [][]byte) (data []byte) {
	f.bb.Reset()
	for i := range f.nodes {
		f.bb.Write(f.nodes[i].Bytes(ent, vals))
	}
	data = append([]byte{}, f.bb.Bytes()...)
	return
}

func getStringIndex(needle string, haystack []string) int {
	for i, n := range haystack {
		if needle == n {
			return i
		}
	}
	return -1
}

type constNode struct {
	val []byte
}

func (c constNode) Bytes(ent *entry.Entry, lo [][]byte) []byte {
	return c.val
}

type lookupNode struct {
	name string
	idx  int
}

func (l lookupNode) Bytes(ent *entry.Entry, lo [][]byte) []byte {
	if l.idx <= len(lo) && l.idx >= 0 {
		return lo[l.idx]
	}
	return nil
}

type srcNode struct {
}

func (s srcNode) Bytes(ent *entry.Entry, lo [][]byte) []byte {
	return []byte(fmt.Sprintf("%v", ent.SRC))
}

type dataNode struct {
}

func (s dataNode) Bytes(ent *entry.Entry, lo [][]byte) []byte {
	return []byte(fmt.Sprintf("%v", string(ent.Data)))
}

type tsNode struct {
}

func (s tsNode) Bytes(ent *entry.Entry, lo [][]byte) []byte {
	return []byte(fmt.Sprintf("%v", ent.TS))
}

func consumeNode(v []byte) (n replaceNode, r []byte, err error) {
	if len(v) == 0 {
		return
	}
	stidx := bytes.Index(v, st)
	switch stidx {
	case 0: // start of lookup node
		v = v[len(st):]
		//start of replacement node, find the end
		eidx := bytes.Index(v, end)
		if eidx == -1 {
			err = errors.New("Closing curly bracket } missing missing on field")
			return
		}
		r = v[eidx+1:]
		name := string(v[:eidx])
		switch name {
		case "_SRC_":
			n = &srcNode{}
		case "_DATA_":
			n = &dataNode{}
		case "_TS_":
			n = &tsNode{}
		default:
			n = &lookupNode{
				name: name,
			}
		}
	case -1: //completely missed
		//end of string, consume as a const node
		n = &constNode{v}
	default: //eat a constant first
		n = &constNode{v[:stidx]}
		r = v[stidx:]
	}
	return
}
