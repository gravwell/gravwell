/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
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
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
)

var (
	ErrMissingModule error = errors.New("extraction module name is missing")
	ErrMissingParams error = errors.New("extraction parameters missing")
	ErrMissingTag    error = errors.New("extraction tag assignment missing")
)

// AutoExtractor object. When setting an AutoExtractor, only Name, Module,
// Params, and Tag must be set.
type AXDefinition struct {
	Name        string    `toml:"name,omitempty" json:",omitempty"`
	Desc        string    `toml:"desc,omitempty" json:",omitempty"`
	Module      string    `toml:"module"`
	Params      string    `toml:"params" json:",omitempty"`
	Args        string    `toml:"args,omitempty" json:",omitempty"`
	Tag         string    `toml:"tag"`
	Labels      []string  `toml:"-"`
	UID         int32     `toml:"-"`
	GIDs        []int32   `toml:"-"`
	Global      bool      `toml:"-"`
	UUID        uuid.UUID `toml:"-"`
	Synced      bool      `toml:"-" json:"-"`
	LastUpdated time.Time `toml:"-"`
}

// Verify all required fields in an AXDefinition object are valid.
func (dc *AXDefinition) Validate() error {
	if dc.Module == `` {
		return ErrMissingModule
	}
	if dc.Tag == `` {
		return ErrMissingTag
	} else if err := ingest.CheckTag(dc.Tag); err != nil {
		return err
	}
	dc.Name = sanitizeValue(dc.Name)
	dc.Desc = sanitizeValue(dc.Desc)
	dc.Module = sanitizeValue(dc.Module)
	dc.Params = sanitizeValue(dc.Params)
	dc.Tag = sanitizeValue(dc.Tag)
	dc.Args = sanitizeValue(dc.Args)
	return nil
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
func (dc AXDefinition) Encode(fout io.Writer, hdr string) (err error) {
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
	if err = GenLine(fout, `tag`, dc.Tag); err != nil {
		return
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
	if err = GenLine(fout, `desc`, dc.Desc); err != nil {
		return
	}
	return
}

func (dc AXDefinition) JSONMetadata() (ro json.RawMessage, err error) {
	x := &struct {
		Name   string `json:"name,omitempty"`
		Desc   string `json:"desc,omitempty"`
		Module string `json:"module"`
		Tag    string `json:"tag"`
	}{
		Name:   dc.Name,
		Desc:   dc.Desc,
		Module: dc.Module,
		Tag:    dc.Tag,
	}
	if x.Desc == `` {
		x.Desc = fmt.Sprintf("%s extractor for tag %s", x.Module, x.Tag)
	}
	b, err := json.Marshal(x)
	return json.RawMessage(b), err
}

func GenLine(wtr io.Writer, name, line string) (err error) {
	if len(line) == 0 {
		return
	}
	_, err = fmt.Fprintf(wtr, "  %s = '%s'\n", name, line)
	return
}

func (axd AXDefinition) Equal(v AXDefinition) bool {
	if axd.Name != v.Name || axd.Desc != v.Desc || axd.Module != v.Module || axd.UUID != v.UUID {
		return false
	}
	if axd.Params != v.Params || axd.Args != v.Args || axd.Tag != v.Tag {
		return false
	}
	if axd.UID != v.UID || axd.Global != v.Global {
		return false
	}
	if len(axd.Labels) != len(v.Labels) || len(axd.GIDs) != len(v.GIDs) {
		return false
	}
	for i, l := range axd.Labels {
		if v.Labels[i] != l {
			return false
		}
	}
	for i, g := range axd.GIDs {
		if v.GIDs[i] != g {
			return false
		}
	}
	return true
}
