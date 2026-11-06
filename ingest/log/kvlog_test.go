package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewjam/rfc5424"
)

func TestKVLogger_Base(t *testing.T) {
	filename := filepath.Join(t.ArtifactDir(), "base.log")
	logger, err := NewFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	kvl := NewLoggerWithKV(logger, KV("all", "value"))

	tests := []struct {
		name string
		kv   []rfc5424.SDParam
		// method should be ref to func on kvl. ex: kvl.Debug
		// this is a bit wacky
		method func(string, ...rfc5424.SDParam) error
		match  string
	}{
		{
			name:   "plain info",
			kv:     []rfc5424.SDParam{},
			method: kvl.Info,
			match:  "",
		},
		{
			name:   "info with extra kv",
			kv:     []rfc5424.SDParam{KV("test", "exists")},
			method: kvl.Info,
			match:  `test="exists"`,
		},
		{
			name:   "critical with extra kv",
			kv:     []rfc5424.SDParam{KV("extra", "exists")},
			method: kvl.Critical,
			match:  `test="exists"`,
		},
		{
			name:   "plain critical",
			kv:     []rfc5424.SDParam{},
			method: kvl.Critical,
			match:  ``,
		},
	}

	for _, tt := range tests {
		if err = tt.method("test", tt.kv...); err != nil {
			t.Fatal(err)
		}
	}

	if err = kvl.Close(); err != nil {
		t.Fatal(err)
	}
	bts, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	s := string(bts)

	// every line should have the kv from the logger
	for num, line := range strings.Split(s, "\n") {
		if line == "" {
			continue
		}
		if !strings.Contains(line, `all="value"`) {
			t.Errorf("KV %q not found in %s on line %d", `all="value"`, filename, num+1)
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			if tt.match != "" && !strings.Contains(s, tt.match) {
				t.Errorf("KV %q not found in %s", tt.match, filename)
			}
		})
	}
}

func TestKVLogger_WithDepth(t *testing.T) {
	filename := filepath.Join(t.ArtifactDir(), "depth.log")
	logger, err := NewFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	kvl := NewLoggerWithKV(logger, KV("all", "value"))

	tests := []struct {
		name string
		kv   []rfc5424.SDParam
		// method should be ref to func on kvl. ex: kvl.DebugWithDepth
		// this is a bit wacky
		method func(int, string, ...rfc5424.SDParam) error
		match  string
	}{
		{
			name:   "plain info",
			kv:     []rfc5424.SDParam{},
			method: kvl.InfoWithDepth,
			match:  "",
		},
		{
			name:   "info with extra kv",
			kv:     []rfc5424.SDParam{KV("test", "exists")},
			method: kvl.InfoWithDepth,
			match:  `test="exists"`,
		},
		{
			name:   "critical with extra kv",
			kv:     []rfc5424.SDParam{KV("extra", "exists")},
			method: kvl.CriticalWithDepth,
			match:  `test="exists"`,
		},
		{
			name:   "plain critical",
			kv:     []rfc5424.SDParam{},
			method: kvl.CriticalWithDepth,
			match:  ``,
		},
	}

	for _, tt := range tests {
		if err = tt.method(DEFAULT_DEPTH+1, "test", tt.kv...); err != nil {
			t.Fatal(err)
		}
	}

	if err = kvl.Close(); err != nil {
		t.Fatal(err)
	}
	bts, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	s := string(bts)

	// every line should have the kv from the logger
	for num, line := range strings.Split(s, "\n") {
		if line == "" {
			continue
		}
		if !strings.Contains(line, `all="value"`) {
			t.Errorf("KV %q not found in %s on line %d", `all="value"`, filename, num+1)
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.match != "" && !strings.Contains(s, tt.match) {
				t.Errorf("KV %q not found in %s", tt.match, filename)
			}
		})
	}
}
