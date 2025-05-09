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
	"os"
	"testing"
)

var (
	tmpDir string
)

func TestMain(m *testing.M) {
	var err error
	if tmpDir, err = os.MkdirTemp(os.TempDir(), `sr`); err != nil {
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
	cfgPath, err := dropConfig(testConfig)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := GetConfig(cfgPath, ``)
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
	if cfg.Max_Ingest_Cache != 1024 {
		t.Fatalf("invalid cache size: %d != %d", cfg.Max_Ingest_Cache, 1024)
	}
	if cfg.Ingest_Cache_Path != `/tmp/cache/simple_relay.cache` {
		t.Fatal("invalid cache path")
	}
	if len(cfg.Listener) != 9 {
		t.Fatalf("invalid listener counts: %d != 9", len(cfg.Listener))
	}
}

func TestBadConfig(t *testing.T) {
	cfgs := []string{
		badConfigNoListener,
		badConfigWrongListener,
		badConfigDropPriority,
		badConfigReaderBind,
	}

	for _, v := range cfgs {
		cfgPath, err := dropConfig(v)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := GetConfig(cfgPath, ``); err == nil {
			t.Fatalf("failed to catch bad config:\n%s\n", v)
		}
	}
}

func dropConfig(cfg string) (pth string, err error) {
	var fout *os.File
	var n int
	if fout, err = os.CreateTemp(tmpDir, `cfg`); err != nil {
		return
	}
	defer fout.Close()
	if n, err = io.WriteString(fout, cfg); err != nil {
		return
	} else if n != len(cfg) {
		err = fmt.Errorf("Failed to write full file: %d != %d", n, len(cfg))
	} else {
		pth = fout.Name()
	}
	return
}

const (
	testConfig string = `
[Global]
Ingest-Secret = IngestSecrets
Connection-Timeout = 0
Insecure-Skip-TLS-Verify=false
Cleartext-Backend-target=127.0.0.1:4023 #example of adding a cleartext connection
Cleartext-Backend-target=127.1.0.1:4023 #example of adding another cleartext connection
Encrypted-Backend-target=127.1.1.1:4024 #example of adding an encrypted connection
Pipe-Backend-Target=/tmp/pipe #a named pipe connection, this should be used when ingester is on the same machine as a backend
Ingest-Cache-Path=/tmp/cache/simple_relay.cache #adding an ingest cache for local storage when uplinks fail
Max-Ingest-Cache=1024 #Number of MB to store, localcache will only store 1GB before stopping.  This is a safety net
Log-Level=INFO
Log-File=/tmp/simple_relay.log

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
	Drop-Priority=true

[Listener "strange UDP line reader"]
	#NOTICE! Lines CANNOT span multiple UDP packets, if they do, they will be treated
	#as seperate entries
	Bind-String = udp://127.0.0.1:9999 #bind ONLY to localhost on UDP
	Tag-Name = udpliner
	Reader-Type=line

[Listener "fortinet udp"]
	#NOTICE! Lines CANNOT span multiple UDP packets, if they do, they will be treated
	#as seperate entries
	Bind-String = udp://127.0.0.1:9998 #bind ONLY to localhost on UDP
	Tag-Name = udpfortinet
	Reader-Type=rfc5424
	Drop-Priority=true

[Listener "fortinet tcp"]
	Bind-String = tcp://127.0.0.1:9998
	Tag-Name = tcpfortinet
	Reader-Type=rfc6587
	Drop-Priority=true

[Listener "GenericEvents"]
	#example generic event handler, it takes lines, and attaches current timestamp
	Bind-String = 127.0.0.1:8888
	Tag-Name = generic
	Ignore-Timestamps = true`

	badConfigNoListener string = `
[Global]
Ingest-Secret = IngestSecrets
Cleartext-Backend-target=127.0.0.1:4023 #example of adding a cleartext connection
Log-Level=INFO
Log-File=/tmp/simple_relay.log
`

	badConfigWrongListener string = `
[Global]
Ingest-Secret = IngestSecrets
Cleartext-Backend-target=127.0.0.1:4023 #example of adding a cleartext connection
Log-Level=INFO
Log-File=/tmp/simple_relay.log

[Listener "GenericEvents"]
	Bind-String="udp://0.0.0.0:8888"
	Reader-Type=foobar
`

	badConfigDropPriority string = `
[Global]
Ingest-Secret = IngestSecrets
Cleartext-Backend-target=127.0.0.1:4023 #example of adding a cleartext connection
Log-Level=INFO
Log-File=/tmp/simple_relay.log

[Listener "GenericEvents"]
	Bind-String="udp://0.0.0.0:8888"
	Drop-Priority=true
`

	badConfigReaderBind string = `
[Global]
Ingest-Secret = IngestSecrets
Cleartext-Backend-target=127.0.0.1:4023 #example of adding a cleartext connection
Log-Level=INFO
Log-File=/tmp/simple_relay.log

[Listener "GenericEvents"]
	Bind-String="udp://0.0.0.0:8888"
	Drop-Priority=true
	Reader-Type=rfc6587
`
)
