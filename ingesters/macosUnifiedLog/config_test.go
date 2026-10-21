/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/ingest/entry"
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

// TestVerifyNilStreamValue covers the nil-map-value check in Verify(). gcfg
// never actually leaves a nil *streamConfig behind a parsed section, so this
// constructs one directly rather than going through GetConfig.
func TestVerifyNilStreamValue(t *testing.T) {
	cfg, err := GetConfig(writeConfig(t, `
[Stream "system"]
`), ``)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Stream["broken"] = nil
	err = cfg.Verify()
	if err == nil || !strings.Contains(err.Error(), `stream "broken" is empty`) {
		t.Fatalf("expected the nil-stream-value error, got %v", err)
	}
}

// TestTagsNoTags covers the "no tags specified" check in Tags(). In
// production this can't happen once Verify() has run, since validate()
// always defaults an empty Tag-Name, so this calls Tags() directly on a
// streamConfig that was never validated.
func TestTagsNoTags(t *testing.T) {
	c := &cfgType{Stream: map[string]*streamConfig{"a": {}}}
	if _, err := c.Tags(); err == nil || err.Error() != "no tags specified" {
		t.Fatalf("expected \"no tags specified\", got %v", err)
	}
}

func TestDisableHostnameAttach(t *testing.T) {
	cfg, err := GetConfig(writeConfig(t, `
[Stream "system"]
`), ``)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Global.Disable_Hostname_Attach {
		t.Error("Disable-Hostname-Attach should default to false")
	}

	cfg2, err := GetConfig(writeConfig(t, `
Disable-Hostname-Attach=true

[Stream "system"]
`), ``)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg2.Global.Disable_Hostname_Attach {
		t.Error("Disable-Hostname-Attach=true did not parse")
	}
}

func TestGetConfigOverlay(t *testing.T) {
	base := writeConfig(t, `
[Stream "auth"]
	Tag-Name=macos-auth
	Level=info
	Predicate=foo
`)
	overlayDir := t.TempDir()
	overlay := `
[Stream "auth"]
	Level=debug

[Stream "extra"]
	Tag-Name=macos-extra
`
	if err := os.WriteFile(filepath.Join(overlayDir, "overlay.conf"), []byte(overlay), 0640); err != nil {
		t.Fatal(err)
	}

	cfg, err := GetConfig(base, overlayDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Stream) != 2 {
		t.Fatalf("got %d streams, want 2", len(cfg.Stream))
	}

	auth := cfg.Stream["auth"]
	if auth == nil {
		t.Fatal("overlay lost the existing \"auth\" stream")
	}
	if auth.Level != "debug" {
		t.Errorf("overlay did not override Level, got %q", auth.Level)
	}
	if auth.Predicate != "foo" {
		t.Errorf("overlay clobbered an untouched field, Predicate = %q", auth.Predicate)
	}
	if auth.Tag_Name != "macos-auth" {
		t.Errorf("overlay clobbered Tag-Name, got %q", auth.Tag_Name)
	}

	extra := cfg.Stream["extra"]
	if extra == nil || extra.Tag_Name != "macos-extra" {
		t.Fatalf("overlay did not add the new stream, got %+v", extra)
	}
}

func TestGetConfigPreprocessorInvalid(t *testing.T) {
	p := writeConfig(t, `
[Stream "auth"]
	Preprocessor=does-not-exist
`)
	_, err := GetConfig(p, ``)
	if err == nil || !strings.Contains(err.Error(), `stream "auth" preprocessor invalid`) {
		t.Fatalf("expected an error naming the offending stream, got %v", err)
	}
}

// TestGetConfigStreamValidateInvalid checks a validate() error surfaced
// through GetConfig names the offending stream.
func TestGetConfigStreamValidateInvalid(t *testing.T) {
	p := writeConfig(t, `
[Stream "auth"]
	Level=verbose
`)
	_, err := GetConfig(p, ``)
	if err == nil || !strings.Contains(err.Error(), `stream "auth"`) {
		t.Fatalf("expected an error naming the offending stream, got %v", err)
	}
}

func TestResolveSource(t *testing.T) {
	override := net.ParseIP("10.1.2.3")
	global := net.ParseIP("10.9.9.9")
	tests := []struct {
		name string
		src  net.IP // streamConfig.src, as populated by validate()
		want net.IP
	}{
		{"override wins", override, override},
		{"nil falls back to global", nil, global},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &streamConfig{src: tt.src}
			if got := resolveSource(sc, global); !got.Equal(tt.want) {
				t.Errorf("resolveSource() = %v, want %v", got, tt.want)
			}
		})
	}

	// can't fold this into the table above: net.IP(nil).Equal(nil) is true,
	// so a got.Equal(want) check wouldn't catch resolveSource returning non-nil
	t.Run("both nil", func(t *testing.T) {
		if got := resolveSource(&streamConfig{}, nil); got != nil {
			t.Errorf("resolveSource() = %v, want nil", got)
		}
	})
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

// TestShippedExampleConfig checks the config operators actually copy from
// parses, and that the inline "# ..." comment trailing Type=log is stripped
// rather than becoming part of the value.
func TestShippedExampleConfig(t *testing.T) {
	const exampleConf = "macos_unified_log.conf"
	raw, err := os.ReadFile(exampleConf)
	if err != nil {
		t.Fatalf("read %s: %v", exampleConf, err)
	}
	dir := t.TempDir()
	loadable := bytes.ReplaceAll(raw, []byte("/opt/gravwell"), []byte(dir))
	path := filepath.Join(dir, exampleConf)
	if err := os.WriteFile(path, loadable, 0600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg, err := GetConfig(path, "")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}

	sys := cfg.Stream["system"]
	if sys == nil {
		t.Fatal(`example config has no "system" stream`)
	}
	if sys.Tag_Name != "macos" {
		t.Errorf("Tag-Name = %q, want %q", sys.Tag_Name, "macos")
	}
	want := []string{"stream", "--style", "json", "--type", "log"}
	if args := sys.args(); !slices.Equal(args, want) {
		t.Errorf("args mismatch (the inline comment after Type=log may not have been stripped)\n got %q\nwant %q", args, want)
	}
	if cfg.Global.Disable_Hostname_Attach {
		t.Error("Disable-Hostname-Attach should default to false")
	}
}
