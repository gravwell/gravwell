/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// this program acts as a test plugin for Gravwell ingester plugins
// it takes every entry that goes by and duplicates it
// one entry is left alone, and entry has a ToUpper() function applied
// This test demonstrates a plugin duplicating entries and all the glue
package main

import (
	"bytes"
	"errors"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors/plugins"
	hclog "github.com/hashicorp/go-hclog"
)

type TestPlugin struct {
	lgr     hclog.Logger
	toUpper bool
	toLower bool
	debug   bool
}

func (t *TestPlugin) LoadConfig(vars map[string][]string) (err error) {
	t.lgr.Debug("Loading config", vars)
	if r, ok := vars["Error"]; ok && len(r) == 1 {
		return errors.New(r[0])
	} else if r, ok = vars["Upper"]; ok && len(r) == 1 && r[0] == "true" {
		t.toUpper = true
	} else if r, ok = vars["Lower"]; ok && len(r) == 1 && r[0] == "true" {
		t.toLower = true
	} else if r, ok = vars["Debug"]; ok && len(r) == 1 && r[0] == "true" {
		t.debug = true
	}
	return nil
}

//process an data item potentially setting a tag
func (t *TestPlugin) Process(ents []*entry.Entry) (ret []*entry.Entry, err error) {
	for _, v := range ents {
		if v != nil {
			ret = append(ret, v)

			//append the upper case
			if t.toUpper {
				ret = append(ret, &entry.Entry{
					TS:   v.TS,
					Tag:  v.Tag,
					SRC:  v.SRC,
					Data: bytes.ToUpper(v.Data),
				})
			}
			//append the lower case
			if t.toLower {
				ret = append(ret, &entry.Entry{
					TS:   v.TS,
					Tag:  v.Tag,
					SRC:  v.SRC,
					Data: bytes.ToLower(v.Data),
				})
			}
		}
	}
	t.log("Processing", len(ents), "entries")
	return ents, nil
}

func (t *TestPlugin) Flush() []*entry.Entry {
	t.log("flushing")
	return nil
}

func (t *TestPlugin) log(msg string, args ...interface{}) {
	if t.debug {
		t.lgr.Debug(msg, args...)
	}
}

func main() {
	lgr := hclog.Default()
	lgr.SetLevel(hclog.Debug)
	tp := &TestPlugin{
		lgr: lgr,
	}
	plugins.ServePlugin(tp)
}
