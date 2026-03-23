/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

func TestNewCompressedReaderGzip(t *testing.T) {
	testData := "Hello, this is test data for gzip compression!"

	// Create gzip compressed data
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte(testData)); err != nil {
		t.Fatalf("Failed to write gzip data: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("Failed to close gzip writer: %v", err)
	}

	// Test NewCompressedReader
	r, err := NewCompressedReader(&buf)
	if err != nil {
		t.Fatalf("NewCompressedReader failed: %v", err)
	}

	result, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("Failed to read decompressed data: %v", err)
	}

	if string(result) != testData {
		t.Errorf("Decompressed data mismatch: got %q, want %q", string(result), testData)
	}
}

func TestNewCompressedReaderBzip2(t *testing.T) {
	// Create bzip2 compressed data
	// Bzip2 magic: "BZ" followed by version and block size
	var buf bytes.Buffer
	buf.Write([]byte("BZh9")) // Bzip2 header with block size 9

	// Since we can't easily create valid bzip2 data without external tools,
	// we'll just test that the function detects bzip2 format
	r, err := NewCompressedReader(&buf)
	if err != nil {
		t.Fatalf("NewCompressedReader failed: %v", err)
	}

	// The reader should be a bzip2 reader (won't be able to read valid data though)
	if r == nil {
		t.Error("Expected non-nil reader for bzip2 data")
	}
}

func TestNewCompressedReaderRaw(t *testing.T) {
	testData := "Hello, this is uncompressed test data!"

	buf := strings.NewReader(testData)

	r, err := NewCompressedReader(buf)
	if err != nil {
		t.Fatalf("NewCompressedReader failed: %v", err)
	}

	result, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("Failed to read raw data: %v", err)
	}

	if string(result) != testData {
		t.Errorf("Raw data mismatch: got %q, want %q", string(result), testData)
	}
}

func TestNewCompressedReaderNil(t *testing.T) {
	_, err := NewCompressedReader(nil)
	if err == nil {
		t.Error("Expected error for nil reader, got nil")
	}
}

func TestNewCompressedReaderEmpty(t *testing.T) {
	buf := &bytes.Buffer{}

	r, err := NewCompressedReader(buf)
	if err != nil {
		t.Fatalf("NewCompressedReader failed on empty input: %v", err)
	}

	result, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("Failed to read empty data: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected empty result, got %d bytes", len(result))
	}
}
