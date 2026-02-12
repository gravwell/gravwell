/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBoltHandler_OpenClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	sh, err := OpenBoltHandler(dbPath, false)
	if err != nil {
		t.Fatalf("Failed to open state handler: %v", err)
	}
	if sh == nil {
		t.Fatal("BoltHandler is nil")
	}

	if err := sh.Close(); err != nil {
		t.Fatalf("Failed to close state handler: %v", err)
	}
}

func TestBoltHandler_OpenWithSync(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_sync.db")

	sh, err := OpenBoltHandler(dbPath, true)
	if err != nil {
		t.Fatalf("Failed to open state handler with sync: %v", err)
	}
	defer sh.Close()

	if sh == nil {
		t.Fatal("BoltHandler is nil")
	}
}

func TestBoltHandler_OpenExisting(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "existing.db")
	fd, err := os.OpenFile(dbPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		t.Fatalf("Failed to create state file: %v", err)
	}
	err = fd.Close()
	if err != nil {
		t.Fatalf("Failed to close state file: %v", err)
	}
	sh, err := OpenBoltHandler(dbPath, false)
	if err != nil {
		t.Fatalf("Failed to open state handler: %v", err)
	}
	defer sh.Close()
}

func TestBoltHandler_OpenReadOnly(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "readonly.db")

	// Create the database first
	sh, err := OpenBoltHandler(dbPath, false)
	if err != nil {
		t.Fatalf("Failed to create initial db: %v", err)
	}
	if err := sh.Close(); err != nil {
		t.Fatalf("Failed to close initial db: %v", err)
	}

	// Make it readonly
	if err := os.Chmod(dbPath, 0400); err != nil {
		t.Fatalf("Failed to chmod: %v", err)
	}

	// Try to open readonly db - should fail
	sh, err = OpenBoltHandler(dbPath, false)
	if err == nil {
		if err := sh.Close(); err != nil {
			t.Fatalf("Failed to close second db: %v", err)
		}
		info, _ := os.Stat(dbPath)
		infoMode := info.Mode().Perm().String()
		t.Fatal("Expected error opening readonly database, perms:", infoMode)
	}

	// fix it
	if err := os.Chmod(dbPath, 0600); err != nil {
		t.Fatalf("Failed to chmod: %v", err)
	}

	// Try to db - should succeed
	_, err = OpenBoltHandler(dbPath, false)
	if err != nil {
		info, _ := os.Stat(dbPath)
		infoMode := info.Mode().Perm().String()
		t.Fatalf("error opening database, perms: %s, err: %v", infoMode, err)
	}
}

func TestBoltConfig_Verify(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		config  *BoltConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "empty path",
			config: &BoltConfig{
				Path: "",
			},
			wantErr: true,
		},
		{
			name: "valid config",
			config: &BoltConfig{
				Path: filepath.Join(tmpDir, "valid.db"),
				Sync: false,
			},
			wantErr: false,
		},
		{
			name: "valid config with sync",
			config: &BoltConfig{
				Path: filepath.Join(tmpDir, "valid_sync.db"),
				Sync: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Verify()
			if (err != nil) != tt.wantErr {
				t.Errorf("Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBucketWriter_ByteOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "bucket_test.db")

	sh, err := OpenBoltHandler(dbPath, false)
	if err != nil {
		t.Fatalf("Failed to open state handler: %v", err)
	}
	defer sh.Close()

	bw, err := sh.GetBucketWriter("test_bucket")
	if err != nil {
		t.Fatalf("Failed to get bucket writer: %v", err)
	}

	// Test Put and Get
	testKey := "test_key"
	testValue := []byte("test value data")

	if err := bw.Put(testKey, testValue); err != nil {
		t.Fatalf("Failed to put value: %v", err)
	}

	retrievedValue, err := bw.Get(testKey)
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}

	if string(retrievedValue) != string(testValue) {
		t.Errorf("Retrieved value mismatch: got %s, want %s", retrievedValue, testValue)
	}
}

func TestBucketWriter_StringOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "string_test.db")

	sh, err := OpenBoltHandler(dbPath, false)
	if err != nil {
		t.Fatalf("Failed to open state handler: %v", err)
	}
	defer sh.Close()

	bw, err := sh.GetBucketWriter("string_bucket")
	if err != nil {
		t.Fatalf("Failed to get bucket writer: %v", err)
	}

	tests := []struct {
		name  string
		key   string
		value string
	}{
		{
			name:  "simple string",
			key:   "simple",
			value: "hello world",
		},
		{
			name:  "empty string",
			key:   "empty",
			value: "",
		},
		{
			name:  "unicode string",
			key:   "unicode",
			value: "Hello 世界 🌍",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := bw.PutString(tt.key, tt.value); err != nil {
				t.Fatalf("PutString failed: %v", err)
			}

			retrieved, err := bw.GetString(tt.key)
			if err != nil {
				t.Fatalf("GetString failed: %v", err)
			}

			if retrieved != tt.value {
				t.Errorf("GetString mismatch: got %q, want %q", retrieved, tt.value)
			}
		})
	}
}

func TestBucketWriter_TimeOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "time_test.db")

	sh, err := OpenBoltHandler(dbPath, false)
	if err != nil {
		t.Fatalf("Failed to open state handler: %v", err)
	}
	defer sh.Close()

	bw, err := sh.GetBucketWriter("time_bucket")
	if err != nil {
		t.Fatalf("Failed to get bucket writer: %v", err)
	}

	tests := []struct {
		name string
		time time.Time
	}{
		{
			name: "current time",
			time: time.Now(),
		},
		{
			name: "zero time",
			time: time.Time{},
		},
		{
			name: "specific time",
			time: time.Date(2025, 12, 12, 15, 30, 45, 123456789, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := tt.name

			if err := bw.PutTime(key, tt.time); err != nil {
				t.Fatalf("PutTime failed: %v", err)
			}

			retrieved, err := bw.GetTime(key)
			if err != nil {
				t.Fatalf("GetTime failed: %v", err)
			}

			// Compare with RFC3339Nano precision
			expected := tt.time.Format(time.RFC3339Nano)
			got := retrieved.Format(time.RFC3339Nano)
			if got != expected {
				t.Errorf("GetTime mismatch: got %s, want %s", got, expected)
			}
		})
	}
}

func TestBucketWriter_Int64Operations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "int64_test.db")

	sh, err := OpenBoltHandler(dbPath, false)
	if err != nil {
		t.Fatalf("Failed to open state handler: %v", err)
	}
	defer sh.Close()

	bw, err := sh.GetBucketWriter("int64_bucket")
	if err != nil {
		t.Fatalf("Failed to get bucket writer: %v", err)
	}

	tests := []struct {
		name  string
		key   string
		value int64
	}{
		{
			name:  "zero",
			key:   "zero",
			value: 0,
		},
		{
			name:  "positive",
			key:   "positive",
			value: 12345678901234,
		},
		{
			name:  "negative",
			key:   "negative",
			value: -9876543210987,
		},
		{
			name:  "max int64",
			key:   "max",
			value: 9223372036854775807,
		},
		{
			name:  "min int64",
			key:   "min",
			value: -9223372036854775808,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := bw.PutInt64(tt.key, tt.value); err != nil {
				t.Fatalf("PutInt64 failed: %v", err)
			}

			retrieved, err := bw.GetInt64(tt.key)
			if err != nil {
				t.Fatalf("GetInt64 failed: %v", err)
			}

			if retrieved != tt.value {
				t.Errorf("GetInt64 mismatch: got %d, want %d", retrieved, tt.value)
			}
		})
	}
}

func TestBucketWriter_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "notfound_test.db")

	sh, err := OpenBoltHandler(dbPath, false)
	if err != nil {
		t.Fatalf("Failed to open state handler: %v", err)
	}
	defer sh.Close()

	bw, err := sh.GetBucketWriter("notfound_bucket")
	if err != nil {
		t.Fatalf("Failed to get bucket writer: %v", err)
	}

	// Test getting non-existent keys
	_, err = bw.Get("nonexistent")
	if err != ErrStorageNotFound {
		t.Errorf("Expected ErrStorageNotFound, got: %v", err)
	}

	_, err = bw.GetString("nonexistent")
	if err != ErrStorageNotFound {
		t.Errorf("Expected ErrStorageNotFound for GetString, got: %v", err)
	}

	_, err = bw.GetTime("nonexistent")
	if err != ErrStorageNotFound {
		t.Errorf("Expected ErrStorageNotFound for GetTime, got: %v", err)
	}

	_, err = bw.GetInt64("nonexistent")
	if err != ErrStorageNotFound {
		t.Errorf("Expected ErrStorageNotFound for GetInt64, got: %v", err)
	}
}

func TestBucketWriter_MultipleBuckets(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "multi_bucket_test.db")

	sh, err := OpenBoltHandler(dbPath, false)
	if err != nil {
		t.Fatalf("Failed to open state handler: %v", err)
	}
	defer sh.Close()

	// Create multiple bucket writers
	bw1, err := sh.GetBucketWriter("bucket1")
	if err != nil {
		t.Fatalf("Failed to get bucket1 writer: %v", err)
	}

	bw2, err := sh.GetBucketWriter("bucket2")
	if err != nil {
		t.Fatalf("Failed to get bucket2 writer: %v", err)
	}

	// Put same key in both buckets with different values
	key := "shared_key"
	value1 := "value from bucket 1"
	value2 := "value from bucket 2"

	if err := bw1.PutString(key, value1); err != nil {
		t.Fatalf("Failed to put in bucket1: %v", err)
	}

	if err := bw2.PutString(key, value2); err != nil {
		t.Fatalf("Failed to put in bucket2: %v", err)
	}

	// Verify isolation
	retrieved1, err := bw1.GetString(key)
	if err != nil {
		t.Fatalf("Failed to get from bucket1: %v", err)
	}
	if retrieved1 != value1 {
		t.Errorf("Bucket1 value mismatch: got %q, want %q", retrieved1, value1)
	}

	retrieved2, err := bw2.GetString(key)
	if err != nil {
		t.Fatalf("Failed to get from bucket2: %v", err)
	}
	if retrieved2 != value2 {
		t.Errorf("Bucket2 value mismatch: got %q, want %q", retrieved2, value2)
	}
}

func TestBucketWriter_UpdateValue(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "update_test.db")

	sh, err := OpenBoltHandler(dbPath, false)
	if err != nil {
		t.Fatalf("Failed to open state handler: %v", err)
	}
	defer sh.Close()

	bw, err := sh.GetBucketWriter("update_bucket")
	if err != nil {
		t.Fatalf("Failed to get bucket writer: %v", err)
	}

	key := "update_key"
	value1 := "initial value"
	value2 := "updated value"

	// Put initial value
	if err := bw.PutString(key, value1); err != nil {
		t.Fatalf("Failed to put initial value: %v", err)
	}

	// Verify initial value
	retrieved, err := bw.GetString(key)
	if err != nil {
		t.Fatalf("Failed to get initial value: %v", err)
	}
	if retrieved != value1 {
		t.Errorf("Initial value mismatch: got %q, want %q", retrieved, value1)
	}

	// Update value
	if err := bw.PutString(key, value2); err != nil {
		t.Fatalf("Failed to update value: %v", err)
	}

	// Verify updated value
	retrieved, err = bw.GetString(key)
	if err != nil {
		t.Fatalf("Failed to get updated value: %v", err)
	}
	if retrieved != value2 {
		t.Errorf("Updated value mismatch: got %q, want %q", retrieved, value2)
	}
}

func TestBucketWriter_NilChecks(t *testing.T) {
	var bw *BucketWriter

	// Test all methods with nil receiver
	_, err := bw.Get("key")
	if err == nil {
		t.Error("Expected error for Get on nil BucketWriter")
	}

	err = bw.Put("key", []byte("value"))
	if err == nil {
		t.Error("Expected error for Put on nil BucketWriter")
	}

	_, err = bw.GetString("key")
	if err == nil {
		t.Error("Expected error for GetString on nil BucketWriter")
	}

	err = bw.PutString("key", "value")
	if err == nil {
		t.Error("Expected error for PutString on nil BucketWriter")
	}

	_, err = bw.GetTime("key")
	if err == nil {
		t.Error("Expected error for GetTime on nil BucketWriter")
	}

	err = bw.PutTime("key", time.Now())
	if err == nil {
		t.Error("Expected error for PutTime on nil BucketWriter")
	}

	_, err = bw.GetInt64("key")
	if err == nil {
		t.Error("Expected error for GetInt64 on nil BucketWriter")
	}

	err = bw.PutInt64("key", 123)
	if err == nil {
		t.Error("Expected error for PutInt64 on nil BucketWriter")
	}
}

func TestBoltHandler_NilChecks(t *testing.T) {
	var sh *BoltHandler

	err := sh.Close()
	if err == nil {
		t.Error("Expected error for Close on nil BoltHandler")
	}

	_, err = sh.GetBucketWriter("test")
	if err == nil {
		t.Error("Expected error for GetBucketWriter on nil BoltHandler")
	}
}

func TestBucketWriter_EmptyValues(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty_test.db")

	sh, err := OpenBoltHandler(dbPath, false)
	if err != nil {
		t.Fatalf("Failed to open state handler: %v", err)
	}
	defer sh.Close()

	bw, err := sh.GetBucketWriter("empty_bucket")
	if err != nil {
		t.Fatalf("Failed to get bucket writer: %v", err)
	}

	// Test empty byte slice
	key := "empty_bytes"
	if err := bw.Put(key, []byte{}); err != nil {
		t.Fatalf("Failed to put empty bytes: %v", err)
	}

	retrieved, err := bw.Get(key)
	if err != nil {
		t.Fatalf("Failed to get empty bytes: %v", err)
	}
	if len(retrieved) != 0 {
		t.Errorf("Expected empty byte slice, got length %d", len(retrieved))
	}

	// Test nil byte slice
	key2 := "nil_bytes"
	if err := bw.Put(key2, nil); err != nil {
		t.Fatalf("Failed to put nil bytes: %v", err)
	}

	retrieved2, err := bw.Get(key2)
	if err != nil {
		t.Fatalf("Failed to get nil bytes: %v", err)
	}
	if len(retrieved2) != 0 {
		t.Errorf("Expected empty/nil byte slice, got length %d", len(retrieved2))
	}
}
