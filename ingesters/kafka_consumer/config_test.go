/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package main

import (
	"io"
	"os"
	"testing"
)

func TestBasicConfig(t *testing.T) {
	fout, err := os.CreateTemp(t.TempDir(), `cfg`)
	if err != nil {
		t.Fatal()
	}
	defer fout.Close()
	if n, err := io.WriteString(fout, baseConfig); err != nil {
		t.Fatal(err)
	} else if n != len(baseConfig) {
		t.Fatalf("Failed to write full file: %d != %d", n, len(baseConfig))
	}
	cfg, err := GetConfig(fout.Name(), ``)
	if err != nil {
		t.Fatal(err)
	}
	if tgts, err := cfg.Targets(); err != nil {
		t.Fatal(err)
	} else if len(tgts) != 4 {
		t.Fatal("Invalid target count")
	}
	if cfg.Secret() != `IngestSecrets` {
		t.Fatal("invalid secret")
	}
	if len(cfg.Consumers) != 3 {
		t.Fatalf("invalid listener counts: %d != 7", len(cfg.Consumers))
	}
	// no fetch sizes specified, sarama defaults should be left alone
	for name, c := range cfg.Consumers {
		if c.fetchDefault != 0 || c.fetchMax != 0 {
			t.Fatalf("consumer %s has unexpected fetch sizes %d/%d", name, c.fetchDefault, c.fetchMax)
		}
	}
}

func TestFetchSizeConfig(t *testing.T) {
	fout, err := os.CreateTemp(t.TempDir(), `cfg`)
	if err != nil {
		t.Fatal(err)
	}
	defer fout.Close()
	if _, err := io.WriteString(fout, fetchConfig); err != nil {
		t.Fatal(err)
	}
	cfg, err := GetConfig(fout.Name(), ``)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := cfg.Consumers[`fetch`]
	if !ok {
		t.Fatal("missing fetch consumer")
	}
	if c.fetchDefault != 4*mb {
		t.Fatalf("bad fetch default: %d", c.fetchDefault)
	}
	if c.fetchMax != 16*mb {
		t.Fatalf("bad fetch max: %d", c.fetchMax)
	}
}

func TestFetchSizes(t *testing.T) {
	tests := []struct {
		def, max   string
		wDef, wMax int32
		shouldFail bool
	}{
		{``, ``, 0, 0, false},
		{`4MB`, ``, 4 * mb, 0, false},
		{``, `64MB`, 0, 64 * mb, false},
		{`2MB`, `8MB`, 2 * mb, 8 * mb, false},
		{`1048576`, `2097152`, 1 * mb, 2 * mb, false},
		{` 512KB `, `512kb`, 512 * kb, 512 * kb, false},
		{``, `256KB`, 256 * kb, 256 * kb, false}, // implicit default is brought down to max
		{`8MB`, `0`, 8 * mb, 0, false},           // explicit 0 max means unlimited
		{`8MB`, `4MB`, 0, 0, true},               // default larger than max
		{`0`, ``, 0, 0, true},
		{`-1`, ``, 0, 0, true},
		{``, `-1`, 0, 0, true},
		{`lots`, ``, 0, 0, true},
		{``, `4XB`, 0, 0, true},
		{`1GB`, ``, 0, 0, true}, // bigger than sarama.MaxResponseSize
		{`99999999999999`, ``, 0, 0, true},
	}
	for i, tt := range tests {
		cc := ConfigConsumer{Fetch_Default: tt.def, Fetch_Max: tt.max}
		def, max, err := cc.fetchSizes()
		if tt.shouldFail {
			if err == nil {
				t.Errorf("%d: expected failure for %q/%q, got %d/%d", i, tt.def, tt.max, def, max)
			}
			continue
		} else if err != nil {
			t.Errorf("%d: unexpected error for %q/%q: %v", i, tt.def, tt.max, err)
		} else if def != tt.wDef || max != tt.wMax {
			t.Errorf("%d: %q/%q => %d/%d, wanted %d/%d", i, tt.def, tt.max, def, max, tt.wDef, tt.wMax)
		}
	}
}

const (
	baseConfig string = `
[Global]
Ingest-Secret = IngestSecrets
Connection-Timeout = 0
Insecure-Skip-TLS-Verify=false
Cleartext-Backend-target=127.0.0.1:4023 #example of adding a cleartext connection
Cleartext-Backend-target=127.1.0.1:4023 #example of adding another cleartext connection
Encrypted-Backend-target=127.1.1.1:4024 #example of adding an encrypted connection
Pipe-Backend-Target=/opt/gravwell/comms/pipe #a named pipe connection, this should be used when ingester is on the same machine as a backend
Ingest-Cache-Path=/opt/gravwell/cache/kafka.cache #adding an ingest cache for local storage when uplinks fail
Max-Ingest-Cache=1024 #Number of MB to store, localcache will only store 1GB before stopping.  This is a safety net
Log-Level=INFO
Log-File=/tmp/kafka.log

[Consumer "default"]
	Leader="127.0.0.1"
	Topic="foo"
	Default-Tag=foo
	Tags=bar*
	Tags=*baz
	Tag-Header=TAG

[Consumer "test"]
	Leader="127.0.0.1:1234"
	Topic="test"
	Default-Tag=foo
	Tags=bar*
	Tags=*baz
	Source-Header=SRC

[Consumer "test2"]
	Leader="[dead::beef]:1234"
	Topic="test2"
	Default-Tag=foo
	Tags=bar*
	Tags=*baz
	Source-As-Binary=true
	Tag-Header=TAG
	Source-Header=SRC
`
	fetchConfig string = `
[Global]
Ingest-Secret = IngestSecrets
Pipe-Backend-Target=/opt/gravwell/comms/pipe
Log-Level=INFO

[Consumer "fetch"]
	Leader="127.0.0.1"
	Topic="foo"
	Default-Tag=foo
	Tags=bar*
	Fetch-Default=4MB
	Fetch-Max=16MB
`
)
