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
)
