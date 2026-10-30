/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/plugins"
	"github.com/gravwell/gravwell/v3/hosted/plugins/tester"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/config"
)

func TestCfgTypeVerifyPersistsFileBackedIngestSecret(t *testing.T) {
	const secret = "file-backed-ingest-secret"
	directory := t.TempDir()
	secretPath := filepath.Join(directory, "ingest.secret")
	if err := os.WriteFile(secretPath, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatalf("write ingest secret: %v", err)
	}

	cfg := &cfgType{
		IngestConfig: config.IngestConfig{
			Ingest_Secret_File:       secretPath,
			Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
		},
		State: storage.BoltConfig{
			Path: filepath.Join(directory, "hosted-runner.state"),
			Sync: true,
		},
	}

	if err := cfg.Verify(); err != nil {
		t.Fatalf("verify config: %v", err)
	}
	if got := cfg.IngestConfig.Secret(); got != secret {
		t.Fatalf("file-backed ingest secret was not retained: got %q, want %q", got, secret)
	}
}

func TestCfgTypeVerifyRejectsEmptyFileBackedIngestSecret(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content []byte
	}{
		{name: "empty", content: nil},
		{name: "newline", content: []byte("\n")},
		{name: "crlf", content: []byte("\r\n")},
		{name: "nul", content: []byte{0}},
		{name: "spaces", content: []byte("   ")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			secretPath := filepath.Join(directory, "ingest.secret")
			if err := os.WriteFile(secretPath, tc.content, 0o600); err != nil {
				t.Fatalf("write ingest secret: %v", err)
			}

			cfg := validCfgType(directory)
			cfg.Ingest_Secret = ""
			cfg.Ingest_Secret_File = secretPath
			if err := cfg.Verify(); err == nil || !strings.Contains(err.Error(), "Ingest-Secret-File") {
				t.Fatalf("Verify() error = %v, want Ingest-Secret-File error", err)
			}
			if err := cfg.Verify(); err == nil || !strings.Contains(err.Error(), "Ingest-Secret-File") {
				t.Fatalf("second Verify() error = %v, want persistent Ingest-Secret-File error", err)
			}
		})
	}
}

func TestCfgTypeVerifyPreservesDirectIngestSecretPrecedence(t *testing.T) {
	directory := t.TempDir()
	secretPath := filepath.Join(directory, "ingest.secret")
	if err := os.WriteFile(secretPath, nil, 0o600); err != nil {
		t.Fatalf("write ingest secret: %v", err)
	}

	cfg := validCfgType(directory)
	cfg.Ingest_Secret = "direct-ingest-secret"
	cfg.Ingest_Secret_File = secretPath
	if err := cfg.Verify(); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if got := cfg.Secret(); got != "direct-ingest-secret" {
		t.Fatalf("Secret() = %q, want direct secret", got)
	}
}

func TestCfgTypeVerifyRejectsInvalidResolvedTag(t *testing.T) {
	directory := t.TempDir()
	cfg := validCfgType(directory)
	cfg.Configs = plugins.Configs{
		Tester: map[string]*tester.Config{
			"invalid-tag": {
				SingleTagConfig: hosted.SingleTagConfig{Tag_Name: "bad tag!"},
			},
		},
	}
	if err := cfg.Verify(); err == nil || !strings.Contains(err.Error(), "invalid tag") {
		t.Fatalf("Verify() error = %v, want invalid tag error", err)
	}
}

func validCfgType(directory string) *cfgType {
	return &cfgType{
		IngestConfig: config.IngestConfig{
			Ingest_Secret:            "test-ingest-secret",
			Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
		},
		State: storage.BoltConfig{
			Path: filepath.Join(directory, "hosted-runner.state"),
			Sync: true,
		},
	}
}
