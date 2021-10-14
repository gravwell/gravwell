// +build !386,!arm,!mips,!mipsle,!s390x,!go1.17
//go:build !386,!arm,!mips,!mipsle,!s390x,!go1.17

/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestBasicPlugin(t *testing.T) {
	pp, err := NewPluginProgram([]byte(basicPlugin))
	if err != nil {
		t.Fatal(err)
	} else if err := pp.Run(time.Second); err != nil {
		t.Fatal(err)
	} else if err = pp.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNoRegister(t *testing.T) {
	pp, err := NewPluginProgram([]byte(basicBadPlugin))
	if err != nil {
		t.Fatal(err)
	} else if err := pp.Run(time.Second); err == nil {
		t.Fatalf("Failed to catch bad plugin with early exit")
	} else if err = pp.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNoRegisterNoExit(t *testing.T) {
	pp, err := NewPluginProgram([]byte(badIdlePlugin))
	if err != nil {
		t.Fatal(err)
	} else if err := pp.Run(time.Second); err == nil {
		t.Fatalf("Failed to catch bad plugin with early exit")
	} else if err = pp.Close(); !errors.Is(err, context.Canceled) {
		t.Fatalf("invalid exit error on bad no exit: %v", err)
	}
}

func TestBad(t *testing.T) {
	bad := []string{badPackage, empty, broken, noMain, badCall}
	for i, b := range bad {
		if _, err := NewPluginProgram([]byte(b)); err == nil {
			t.Fatalf("Failed to catch bad program[%d]", i)
		}
	}
}

func TestCalls(t *testing.T) {
	tgr := newTestTagger()
	b := []byte(`
	[Config]
		Upper=true
	`)
	tc := struct {
		Config config.VariableConfig
	}{}
	if err := config.LoadConfigBytes(&tc, b); err != nil {
		t.Fatalf("Failed to build config: %v", err)
	}

	//build up som entries and pass them in
	ents := makeEnts(16)

	if pp, err := NewPluginProgram([]byte(recase)); err != nil {
		t.Fatal(err)
	} else if err := pp.Run(time.Second); err != nil {
		t.Fatal(err)
	} else if err = pp.Config(&tc.Config, tgr); err != nil {
		t.Fatalf("Failed config: %v", err)
	} else if pp.Flush() != nil {
		t.Fatalf("should not have gotten entries back on a flush")
	} else if rents, err := pp.Process(ents); err != nil {
		t.Fatalf("failed to process: %v", err)
	} else if len(rents) != len(ents) {
		t.Fatalf("invlid count: %d != %d", len(rents), len(ents))
	} else if err := checkEntsCase(ents, rents, true); err != nil {
		t.Fatalf("returned entries are bad: %v", err)
	} else if err = pp.Close(); err != nil {
		t.Fatal(err)
	}
}

type testTagger struct {
	mp map[string]entry.EntryTag
}

func newTestTagger() *testTagger {
	return &testTagger{
		mp: map[string]entry.EntryTag{`default`: 0},
	}
}

func (tt *testTagger) KnownTags() (r []string) {
	r = make([]string, 0, len(tt.mp))
	for k := range tt.mp {
		r = append(r, k)
	}
	return
}

func (tt *testTagger) LookupTag(t entry.EntryTag) (string, bool) {
	for k, v := range tt.mp {
		if v == t {
			return k, true
		}
	}
	return ``, false
}

func (tt *testTagger) NegotiateTag(name string) (entry.EntryTag, error) {
	if v, ok := tt.mp[name]; ok {
		return v, nil
	}
	t := entry.EntryTag(len(tt.mp))
	tt.mp[name] = t
	return t, nil
}

var src = net.ParseIP("192.168.1.1")

func makeEnts(cnt int) (r []*entry.Entry) {
	r = make([]*entry.Entry, cnt)
	for i := range r {
		ts := entry.Now()
		r[i] = &entry.Entry{
			TS:   ts,
			SRC:  src,
			Tag:  1,
			Data: []byte(fmt.Sprintf("%v\t%d\ttHiS SoME CaSe GaRbAgE", ts, i)),
		}
	}
	return
}

func checkEntsCase(orig, output []*entry.Entry, isUpper bool) (err error) {
	if len(orig) != len(output) {
		return fmt.Errorf("invalid counts: %d != %d", len(orig), len(output))
	}
	for i, v := range orig {
		var tgt []byte
		if v == nil {
			return fmt.Errorf("Entry %d is nil", i)
		} else if isUpper {
			tgt = bytes.ToUpper(v.Data)
		} else {
			tgt = bytes.ToLower(v.Data)
		}
		if !bytes.Equal(tgt, output[i].Data) {
			return fmt.Errorf("Case does not match (%v) on %d:\nWANTED: %s\nGOT:    %s", isUpper, i, tgt, output[i].Data)
		}
	}
	return nil
}
