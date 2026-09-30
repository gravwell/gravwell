/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const testGlobal = `
[Global]
Ingest-Secret = IngestSecrets
Pipe-Backend-Target=/tmp/pipe
Log-Level=INFO
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "macos_unified_log.conf")
	if err := os.WriteFile(p, []byte(testGlobal+body), 0640); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGetConfig(t *testing.T) {
	p := writeConfig(t, `
[Stream "system"]

[Stream "auth"]
	Tag-Name=macos-auth
	Level=Info
	Predicate="subsystem == 'com.apple.Authorization' OR process == \"sudo\""
	Process=sudo
	Process=1234
	Type=log
	Type=Activity
	Include-Source=true
	Source-Override=10.0.0.1

[Stream "auth2"]
	Tag-Name=macos-auth
`)
	cfg, err := GetConfig(p, ``)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Stream) != 3 {
		t.Fatalf("got %d streams, want 3", len(cfg.Stream))
	}
	if tag := cfg.Stream["system"].Tag_Name; tag != entry.DefaultTagName {
		t.Errorf("default tag = %q, want %q", tag, entry.DefaultTagName)
	}
	if args := cfg.Stream["system"].args(); !slices.Equal(args, []string{"stream", "--style", "json"}) {
		t.Errorf("unexpected default args %q", args)
	}

	auth := cfg.Stream["auth"]
	if auth.src == nil || auth.src.String() != "10.0.0.1" {
		t.Errorf("source override not parsed, got %v", auth.src)
	}
	want := []string{
		"stream", "--style", "json",
		"--level", "info",
		"--predicate", `subsystem == 'com.apple.Authorization' OR process == "sudo"`,
		"--process", "sudo", "--process", "1234",
		"--type", "log", "--type", "activity",
		"--source",
	}
	if args := auth.args(); !slices.Equal(args, want) {
		t.Errorf("args mismatch\n got %q\nwant %q", args, want)
	}

	tags, err := cfg.Tags()
	if err != nil {
		t.Fatal(err)
	} else if !slices.Equal(tags, []string{entry.DefaultTagName, "macos-auth"}) {
		t.Errorf("unexpected tags %q", tags)
	}
}

func TestGetConfigNoStreams(t *testing.T) {
	if _, err := GetConfig(writeConfig(t, ``), ``); err == nil {
		t.Fatal("expected an error for a config without streams")
	}
}

func TestStreamValidateErrors(t *testing.T) {
	tests := []struct {
		name string
		sc   streamConfig
	}{
		{"bad-level", streamConfig{Level: "verbose"}},
		{"bad-type", streamConfig{Type: []string{"log", "signal"}}},
		{"empty-process", streamConfig{Process: []string{"sudo", "  "}}},
		{"bad-tag", streamConfig{Tag_Name: "no spaces allowed"}},
		{"bad-source-override", streamConfig{Source_Override: "not-an-ip"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.sc.validate(); err == nil {
				t.Errorf("expected an error")
			}
		})
	}
}
