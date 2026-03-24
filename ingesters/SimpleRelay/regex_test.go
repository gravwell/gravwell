/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
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
	"regexp"
	"sync"
	"testing"

	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/timegrinder"
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
	input := bytes.NewBuffer([]byte(" foo X bar "))

	trk := &tracker{}
	cfg.proc = processors.NewProcessorSet(&nilWriter{})
	cfg.proc.AddProcessor(trk)
	rs := regexState{
		rx:          regexp.MustCompile(cfg.regex),
		prefixIndex: -1,
		suffixIndex: -1,
	}
	tg, _ := timegrinder.New(timegrinder.Config{})

	regexLoop(input, cfg, net.IP{}, rs, tg)
	if len(trk.ents) != 2 {
		for i, e := range trk.ents {
			t.Logf("entry %d: %s\n", i, e.Data)
		}
		t.Fatalf("Expected 2 entries, got %d", len(trk.ents))
	}
	expected := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
	}
	for i := range trk.ents {
		if !bytes.Equal(trk.ents[i].Data, expected[i]) {
			t.Fatalf("Invalid entry %d, expected %s (%v) got %s (%v)", i, trk.ents[i].Data, trk.ents[i].Data, expected[i], expected[i])
		}
	}
}

func TestRegexMaxBuffer(t *testing.T) {
	lg = log.New(os.Stderr)
	cfg := makeConfig()
	cfg.maxBuffer = 3
	cfg.regex = "X"
	input := bytes.NewBuffer([]byte("foobar"))

	trk := &tracker{}
	cfg.proc = processors.NewProcessorSet(&nilWriter{})
	cfg.proc.AddProcessor(trk)
	rs := regexState{
		rx:          regexp.MustCompile(cfg.regex),
		prefixIndex: -1,
		suffixIndex: -1,
	}
	tg, _ := timegrinder.New(timegrinder.Config{})

	regexLoop(input, cfg, net.IP{}, rs, tg)
	if len(trk.ents) != 2 {
		for i, e := range trk.ents {
			t.Logf("entry %d: %s\n", i, e.Data)
		}
		t.Fatalf("Expected 2 entries, got %d", len(trk.ents))
	}
	expected := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
	}
	for i := range trk.ents {
		if !bytes.Equal(trk.ents[i].Data, expected[i]) {
			t.Fatalf("Invalid entry %d, expected %s (%v) got %s (%v)", i, trk.ents[i].Data, trk.ents[i].Data, expected[i], expected[i])
		}
	}
}

type tracker struct {
	ents []*entry.Entry
}

func (t *tracker) Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	t.ents = append(t.ents, ents...)
	return ents, nil
}

func (t *tracker) Flush() []*entry.Entry {
	return nil
}

func (t *tracker) Close() error {
	return nil
}

type nilWriter struct{}

func (n *nilWriter) WriteEntry(*entry.Entry) error                           { return nil }
func (n *nilWriter) WriteEntryContext(context.Context, *entry.Entry) error   { return nil }
func (n *nilWriter) WriteBatch([]*entry.Entry) error                         { return nil }
func (n *nilWriter) WriteBatchContext(context.Context, []*entry.Entry) error { return nil }
