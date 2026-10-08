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
	"testing"

	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/config"
)

func TestVerifyPreservesConfiguredIngestSecretFile(t *testing.T) {
	secret := writeTestSecret(t)
	cfg := newTestConfig(t)
	cfg.Ingest_Secret_File = secret

	if err := cfg.Verify(); err != nil {
		t.Fatalf("Verify() failed: %v", err)
	}
	base := cfg.IngestBaseConfig()
	if got := base.Secret(); got != "test-ingest-secret" {
		t.Fatalf("IngestBaseConfig().Secret() = %q, want configured file value", got)
	}
}

func TestVerifyPreservesEnvironmentIngestSecretFile(t *testing.T) {
	secret := writeTestSecret(t)
	unsetEnvForTest(t, "GRAVWELL_INGEST_SECRET")
	t.Setenv("GRAVWELL_INGEST_SECRET_FILE", secret)
	cfg := newTestConfig(t)

	if err := cfg.Verify(); err != nil {
		t.Fatalf("Verify() failed: %v", err)
	}
	base := cfg.IngestBaseConfig()
	if got := base.Secret(); got != "test-ingest-secret" {
		t.Fatalf("IngestBaseConfig().Secret() = %q, want environment file value", got)
	}
}

func newTestConfig(t *testing.T) *cfgType {
	t.Helper()
	return &cfgType{
		IngestConfig: config.IngestConfig{
			Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
		},
		State: storage.BoltConfig{Path: filepath.Join(t.TempDir(), "hosted-runner.state")},
	}
}

func writeTestSecret(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ingest.secret")
	if err := os.WriteFile(path, []byte("test-ingest-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	value, present := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
