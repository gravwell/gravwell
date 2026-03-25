/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

var (
	ErrMissingModule error = errors.New("extraction module name is missing")
	ErrMissingParams error = errors.New("extraction parameters missing")
	ErrMissingTag    error = errors.New("extraction tag assignment missing")
)

// AX object, when setting an AutoExtractor, only Name, Module,
// Params, and Tag must be set.
type AX struct {
	CommonFields

	Module string   `toml:"module"`
	Params string   `toml:"params" json:",omitempty"`
	Args   string   `toml:"args,omitempty" json:",omitempty"`
	Tags   []string `toml:"tags"` // AXs can support multiple tags. For backwards compatibility, we leave Tag and add Tags
}

func (dc AX) JSONMetadata() (ro json.RawMessage, err error) {
	x := &struct {
		Name   string   `json:"name,omitempty"`
		Desc   string   `json:"desc,omitempty"`
		Module string   `json:"module"`
		Tags   []string `json:"tags"`
	}{
		Name:   dc.Name,
		Desc:   dc.Description,
		Module: dc.Module,
		Tags:   dc.Tags,
	}
	if x.Desc == `` {
		x.Desc = fmt.Sprintf("%s extractor for tags %v", x.Module, dc.Tags)
	}
	b, err := json.Marshal(x)
	return json.RawMessage(b), err
}

func (dc AX) Equal(v AX) bool {
	if dc.Name != v.Name || dc.Description != v.Description || dc.Module != v.Module || dc.ID != v.ID {
		return false
	}
	if dc.Params != v.Params || dc.Args != v.Args {
		return false
	}

	t1 := dc.Tags
	t2 := v.Tags
	sort.Strings(t1)
	sort.Strings(t2)
	if len(t1) != len(t2) {
		return false
	}
	for i, t := range t1 {
		if t != t2[i] {
			return false
		}
	}

	if dc.OwnerID != v.OwnerID || dc.Readers.Global != v.Readers.Global || dc.Writers.Global != v.Writers.Global {
		return false
	}
	if len(dc.Labels) != len(v.Labels) || len(dc.Readers.GIDs) != len(v.Readers.GIDs) || len(dc.Writers.GIDs) != len(v.Writers.GIDs) {
		return false
	}
	for i, l := range dc.Labels {
		if v.Labels[i] != l {
			return false
		}
	}
	for i, g := range dc.Readers.GIDs {
		if v.Readers.GIDs[i] != g {
			return false
		}
	}
	for i, g := range dc.Writers.GIDs {
		if v.Writers.GIDs[i] != g {
			return false
		}
	}

	return true
}

// AXListResponse is what gets returned when you query a list of
// autoextractors.
type AXListResponse struct {
	BaseListResponse
	Results []AX `json:"results"`
}
