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

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

var (
	st  = []byte(`${`)
	end = []byte(`}`)
)

type accessor interface {
	Get(string) interface{}
}

type replaceNode interface {
	Bytes(*entry.Entry, [][]byte) []byte
	Accessor(*entry.Entry, accessor) []byte
}

type formatter struct {
	nodes []replaceNode
	bb    *bytes.Buffer
}

func newFormatter(s string) (f *formatter, err error) {
	if s == `` {
		err = errors.New("missing template")
		return
	}
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

func (f *formatter) renderWithAccessor(ent *entry.Entry, acc accessor) (data string) {
	f.bb.Reset()
	for i := range f.nodes {
		f.bb.Write(f.nodes[i].Accessor(ent, acc))
	}
	return f.bb.String()
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

func (c constNode) Accessor(ent *entry.Entry, acc accessor) []byte {
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

func (l lookupNode) Accessor(ent *entry.Entry, acc accessor) []byte {
	if val := acc.Get(l.name); val != nil {
		return []byte(fmt.Sprintf("%v", val))
	}
	return nil
}

type srcNode struct {
}

func (s srcNode) Bytes(ent *entry.Entry, lo [][]byte) []byte {
	return []byte(fmt.Sprintf("%v", ent.SRC))
}

func (s srcNode) Accessor(ent *entry.Entry, acc accessor) []byte {
	return []byte(fmt.Sprintf("%v", ent.SRC))
}

type dataNode struct {
}

func (s dataNode) Bytes(ent *entry.Entry, lo [][]byte) []byte {
	return []byte(fmt.Sprintf("%v", string(ent.Data)))
}

func (s dataNode) Accessor(ent *entry.Entry, acc accessor) []byte {
	return []byte(fmt.Sprintf("%v", string(ent.Data)))
}

type tsNode struct {
}

func (s tsNode) Bytes(ent *entry.Entry, lo [][]byte) []byte {
	return []byte(fmt.Sprintf("%v", ent.TS))
}

func (s tsNode) Accessor(ent *entry.Entry, acc accessor) []byte {
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
