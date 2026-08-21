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
	"io"
	"sort"
	"strings"

	"github.com/gravwell/gravwell/v4/ingest"
)

var (
	ErrMissingModule error = errors.New("extraction module name is missing")
	ErrMissingParams error = errors.New("extraction parameters missing")
	ErrMissingTag    error = errors.New("extraction tag assignment missing")
	ErrMissingName   error = errors.New("extraction name missing")
)

// AX object, when setting an AutoExtractor, only Name, Module,
// Params, and Tag must be set.
type AX struct {
	CommonFields

	Module string   `toml:"module"`
	Params string   `toml:"params" json:",omitempty"`
	Args   string   `toml:"args,omitempty" json:",omitempty"`
	Tags   []string `toml:"tags"`
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (dc AX) MarshalJSON() ([]byte, error) {
	type dummyAX AX
	dc.CommonFields = dc.CommonFields.MakeNilSlices()
	dc.Tags = nonNilSlice(dc.Tags)
	return json.Marshal(dummyAX(dc))
}

// Validate verifies all required fields in an AXDefinition object are valid.
func (dc *AX) Validate() error {
	if dc.Name == `` {
		return ErrMissingName
	}
	if dc.Module == `` {
		return ErrMissingModule
	}
	if len(dc.GetTags()) == 0 {
		return ErrMissingTag
	}
	for _, t := range dc.GetTags() {
		if err := ingest.CheckTag(t); err != nil {
			return err
		}
	}

	dc.Name = sanitizeValue(dc.Name)
	dc.Description = sanitizeValue(dc.Description)
	dc.Module = sanitizeValue(dc.Module)
	dc.Params = sanitizeValue(dc.Params)
	for i, t := range dc.Tags {
		dc.Tags[i] = sanitizeValue(t)
	}
	dc.Args = sanitizeValue(dc.Args)

	collisions := make(map[string]bool)

	for _, t := range dc.GetTags() {
		if _, ok := collisions[t]; ok {
			return fmt.Errorf("Tag %v already defined", t)
		}
		collisions[t] = true
	}

	return nil
}

func (dc *AX) GetTags() []string {
	return dc.Tags
}

func sanitizeValue(v string) string {
	trim := func(r rune) rune {
		switch r {
		case '\n':
			return ' '
		case '\r':
			return ' '
		}
		return r
	}
	return strings.Map(trim, v)
}

// Encode the "config file" styled AX definition to the given io.Writer. hdr is
// an optional header comment.
func (dc AX) Encode(fout io.Writer, hdr string) (err error) {
	if err = dc.Validate(); err != nil {
		return
	}
	//write header comment if it exists
	if hdr != `` {
		if _, err = fmt.Fprintf(fout, "# %s\n", hdr); err != nil {
			return
		}
	}
	//write actual header
	if _, err = fmt.Fprintf(fout, "[[extraction]]\n"); err != nil {
		return
	}
	//write required parameters
	for _, t := range dc.GetTags() {
		if err = GenLine(fout, `tag`, t); err != nil {
			return
		}
	}

	if err = GenLine(fout, `module`, dc.Module); err != nil {
		return
	}
	if err = GenLine(fout, `params`, dc.Params); err != nil {
		return
	}
	//write optional parameters
	if err = GenLine(fout, `args`, dc.Args); err != nil {
		return
	}
	if err = GenLine(fout, `name`, dc.Name); err != nil {
		return
	}
	if err = GenLine(fout, `desc`, dc.Description); err != nil {
		return
	}
	return
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
	Results []AX
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (a AXListResponse) MarshalJSON() ([]byte, error) {
	type dummyAXListResponse AXListResponse
	a.Results = nonNilSlice(a.Results)
	a.BaseListResponse = a.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummyAXListResponse(a))
}

func GenLine(wtr io.Writer, name, line string) (err error) {
	if len(line) == 0 {
		return
	}
	_, err = fmt.Fprintf(wtr, "  %s = '%s'\n", name, line)
	return
}
