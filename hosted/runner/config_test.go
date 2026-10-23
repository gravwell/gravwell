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

func TestVerifyPersistsFileBackedIngestSecret(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "ingest.secret")
	const want = "test-ingest-secret"
	if err := os.WriteFile(secretPath, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := cfgType{
		IngestConfig: config.IngestConfig{
			Ingest_Secret_File:       secretPath,
			Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
			Log_Level:                "INFO",
		},
		State: storage.BoltConfig{Path: filepath.Join(dir, "state.db")},
	}
	if err := cfg.Verify(); err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if got := cfg.Ingest_Secret; got != want {
		t.Fatalf("file-backed ingest secret was not retained: got %q, want %q", got, want)
	}
}
