/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"testing"
)

var (
	tmpDir string
)

func TestMain(m *testing.M) {
	var err error
	if tmpDir, err = ioutil.TempDir(os.TempDir(), `sr`); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create tempdir %v\n", err)
		os.Exit(-1)
	}
	r := m.Run()
	if r == 0 {
		if err = os.RemoveAll(tmpDir); err != nil {
			r = -2
		}
	} else {
		os.RemoveAll(tmpDir)
	}
	os.Exit(r)
}

func TestBasicConfig(t *testing.T) {
	fout, err := ioutil.TempFile(tmpDir, `cfg`)
	if err != nil {
		t.Fatal()
	}
	defer fout.Close()
	if n, err := io.WriteString(fout, baseConfig); err != nil {
		t.Fatal(err)
	} else if n != len(baseConfig) {
		t.Fatal(fmt.Sprintf("Failed to write full file: %d != %d", n, len(baseConfig)))
	}
	cfg, err := GetConfig(fout.Name())
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
	if cfg.MaxCachedData() != 1024*1024*1024 {
		t.Fatal("invalid cache size")
	}
	if cfg.LocalFileCachePath() != `/opt/gravwell/cache/simple_relay.cache` {
		t.Fatal("invalid cache path")
	}
	if len(cfg.Listener) != 7 {
		t.Fatal(fmt.Sprintf("invalid listener counts: %d != 7", len(cfg.Listener)))
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
Ingest-Cache-Path=/opt/gravwell/cache/simple_relay.cache #adding an ingest cache for local storage when uplinks fail
Max-Ingest-Cache=1024 #Number of MB to store, localcache will only store 1GB before stopping.  This is a safety net
Log-Level=INFO
Log-File=/opt/gravwell/log/simple_relay.log

#basic default logger, all entries will go to the default tag
# this is useful for sending generic line-delimited
# data to Gravwell, for example if you have an old log file sitting around:
#	cat logfile | nc gravwell-host 7777
#no Tag-Name means use the default tag
[Listener "default"]
	Bind-String="0.0.0.0:7777" #we are binding to all interfaces, with TCP implied
	#Lack of "Reader-Type" implines line break delimited logs
	#Lack of "Tag-Name" implies the "default" tag
	#Assume-Local-Timezone=false #Default for assume localtime is false
	#Source-Override="DEAD::BEEF" #override the source for just this listener

[Listener "syslogtcp"]
	Bind-String="tcp://0.0.0.0:601" #standard RFC5424 reliable syslog
	Reader-Type=rfc5424
	Tag-Name=syslog
	Assume-Local-Timezone=true #if a time format does not have a timezone, assume local time

[Listener "syslogudp"]
	Bind-String="udp://0.0.0.0:514" #standard UDP based RFC5424 syslog
	Reader-Type=rfc5424
	Tag-Name=syslog
	Assume-Local-Timezone=true #if a time format does not have a timezone, assume local time

[Listener "new hotness syslog "]
	#use reliable syslog, which is syslog over TCP on port 601
	Bind-String = 127.0.0.1:601 #bind ONLY to localhost with no proto specifier we default to tcp
	Tag-Name = syslog

[Listener "crappy old syslog"]
	#use regular old UDP syslog using the RFC5424 format
	#RFC5424 lexer also eats RFC3164 logs from legacy syslog and BSD-syslog
	Bind-String = udp://127.0.0.1:514 #bind ONLY to localhost on UDP
	Tag-Name = syslog2
	Reader-Type=rfc5424

[Listener "strange UDP line reader"]
	#NOTICE! Lines CANNOT span multiple UDP packets, if they do, they will be treated
	#as seperate entries
	Bind-String = udp://127.0.0.1:9999 #bind ONLY to localhost on UDP
	Tag-Name = udpliner
	Reader-Type=line

[Listener "GenericEvents"]
	#example generic event handler, it takes lines, and attaches current timestamp
	Bind-String = 127.0.0.1:8888
	Tag-Name = generic
	Ignore-Timestamps = true
	`
)
