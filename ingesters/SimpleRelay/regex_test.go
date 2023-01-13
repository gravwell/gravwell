/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"sync"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

func makeConfig() regexHandlerConfig {
	cfg := regexHandlerConfig{

		wg:  &sync.WaitGroup{},
		ctx: context.Background(),
	}
	return cfg
}

func TestRegexTrimWhitespace(t *testing.T) {
	lg = log.New(os.Stderr)
	cfg := makeConfig()
	cfg.trimWhitespace = true
	cfg.regex = "X"
	out := make(chan *entry.Entry)
	input := bytes.NewBuffer([]byte(" foo X bar "))
	go regexLoop(input, cfg, net.IP{}, out)
	var ents []*entry.Entry
	for e := range out {
		ents = append(ents, e)
	}
	if len(ents) != 2 {
		for i, e := range ents {
			t.Logf("entry %d: %s\n", i, e.Data)
		}
		t.Fatalf("Expected 2 entries, got %d", len(ents))
	}
	expected := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
	}
	for i := range ents {
		if !bytes.Equal(ents[i].Data, expected[i]) {
			t.Fatalf("Invalid entry %d, expected %s (%v) got %s (%v)", i, ents[i].Data, ents[i].Data, expected[i], expected[i])
		}
	}
}

func TestRegexMaxBuffer(t *testing.T) {
	lg = log.New(os.Stderr)
	cfg := makeConfig()
	cfg.maxBuffer = 3
	cfg.regex = "X"
	out := make(chan *entry.Entry)
	input := bytes.NewBuffer([]byte("foobar"))
	go regexLoop(input, cfg, net.IP{}, out)
	var ents []*entry.Entry
	for e := range out {
		ents = append(ents, e)
	}
	if len(ents) != 2 {
		for i, e := range ents {
			t.Logf("entry %d: %s\n", i, e.Data)
		}
		t.Fatalf("Expected 2 entries, got %d", len(ents))
	}
	expected := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
	}
	for i := range ents {
		if !bytes.Equal(ents[i].Data, expected[i]) {
			t.Fatalf("Invalid entry %d, expected %s (%v) got %s (%v)", i, ents[i].Data, ents[i].Data, expected[i], expected[i])
		}
	}
}
