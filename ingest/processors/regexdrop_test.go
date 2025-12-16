/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestRegexDropConfigValidation(t *testing.T) {
	// Test valid configuration
	validConfig := RegexDropConfig{
		Regex:  `test`,
		Invert: false,
	}

	_, err := NewRegexDropper(validConfig)
	if err != nil {
		t.Errorf("Valid config should not return error: %v", err)
	}

	// Test invalid regex
	invalidConfig := RegexDropConfig{
		Regex:  `[`,
		Invert: false,
	}

	_, err = NewRegexDropper(invalidConfig)
	if err == nil {
		t.Error("Invalid regex should return error")
	}

	// Test empty regex
	emptyConfig := RegexDropConfig{
		Regex:  ``,
		Invert: false,
	}

	_, err = NewRegexDropper(emptyConfig)
	if err == nil {
		t.Error("Empty regex should return error")
	}
}

func TestRegexDropProcessor(t *testing.T) {
	// Create a regex drop processor that drops entries matching "drop"
	cfg := RegexDropConfig{
		Regex:  `drop`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Test with mixed entries - some match, some don't
	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("This should be kept"),
	}

	entry2 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("This should drop this entry"),
	}

	entry3 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("Also kept"),
	}

	entries := []*entry.Entry{entry1, entry2, entry3}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 results, got %d", len(result))
	}

	// Verify the correct entries were kept
	if string(result[0].Data) != "This should be kept" {
		t.Errorf("Expected first entry to be 'This should be kept', got '%s'", string(result[0].Data))
	}

	if string(result[1].Data) != "Also kept" {
		t.Errorf("Expected second entry to be 'Also kept', got '%s'", string(result[1].Data))
	}
}

func TestRegexDropProcessorInvert(t *testing.T) {
	// Create a regex drop processor with Invert=true (keep only matches)
	cfg := RegexDropConfig{
		Regex:  `keep`,
		Invert: true,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Test with mixed entries
	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("This should be dropped"),
	}

	entry2 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("keep this entry"),
	}

	entry3 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("Also dropped"),
	}

	entry4 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("keep this one too"),
	}

	entries := []*entry.Entry{entry1, entry2, entry3, entry4}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 results, got %d", len(result))
	}

	// Verify the correct entries were kept (only those with "keep")
	if string(result[0].Data) != "keep this entry" {
		t.Errorf("Expected first entry to be 'keep this entry', got '%s'", string(result[0].Data))
	}

	if string(result[1].Data) != "keep this one too" {
		t.Errorf("Expected second entry to be 'keep this one too', got '%s'", string(result[1].Data))
	}
}

func TestRegexDropProcessorNoMatches(t *testing.T) {
	// Test when no entries match
	cfg := RegexDropConfig{
		Regex:  `xyz`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("This is a test string"),
	}

	entry2 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("Another test string"),
	}

	entries := []*entry.Entry{entry1, entry2}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// All entries should be kept since none match
	if len(result) != 2 {
		t.Errorf("Expected 2 results, got %d", len(result))
	}
}

func TestRegexDropProcessorAllMatch(t *testing.T) {
	// Test when all entries match
	cfg := RegexDropConfig{
		Regex:  `test`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("This is a test string"),
	}

	entry2 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("Another test string"),
	}

	entries := []*entry.Entry{entry1, entry2}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// All entries should be dropped since all match
	if len(result) != 0 {
		t.Errorf("Expected 0 results, got %d", len(result))
	}
}

func TestRegexDropProcessorNilEntry(t *testing.T) {
	// Test with nil entries
	cfg := RegexDropConfig{
		Regex:  `test`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	entries := []*entry.Entry{nil, nil}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected 0 results for nil entries, got %d", len(result))
	}
}

func TestRegexDropProcessorEmptyEntry(t *testing.T) {
	// Test with empty entry data
	cfg := RegexDropConfig{
		Regex:  `test`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte(""),
	}

	entries := []*entry.Entry{entry1}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Empty entry should be kept since it doesn't match "test"
	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}
}

func TestRegexDropProcessorComplexRegex(t *testing.T) {
	// Test with complex regex pattern (email addresses)
	cfg := RegexDropConfig{
		Regex:  `\w+@\w+\.\w+`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("Normal log entry"),
	}

	entry2 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("Contact user@example.com for info"),
	}

	entry3 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("Another normal entry"),
	}

	entries := []*entry.Entry{entry1, entry2, entry3}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Only entries without email addresses should be kept
	if len(result) != 2 {
		t.Errorf("Expected 2 results, got %d", len(result))
	}

	if string(result[0].Data) != "Normal log entry" {
		t.Errorf("Expected 'Normal log entry', got '%s'", string(result[0].Data))
	}

	if string(result[1].Data) != "Another normal entry" {
		t.Errorf("Expected 'Another normal entry', got '%s'", string(result[1].Data))
	}
}

func TestRegexDropProcessorCaseInsensitive(t *testing.T) {
	// Test case-insensitive matching using (?i) flag
	cfg := RegexDropConfig{
		Regex:  `(?i)error`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("This is an ERROR"),
	}

	entry2 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("This is an Error"),
	}

	entry3 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("This is fine"),
	}

	entry4 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("This has error in it"),
	}

	entries := []*entry.Entry{entry1, entry2, entry3, entry4}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Only the entry without "error" (case-insensitive) should be kept
	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}

	if string(result[0].Data) != "This is fine" {
		t.Errorf("Expected 'This is fine', got '%s'", string(result[0].Data))
	}
}

func TestRegexDropProcessorJSON(t *testing.T) {
	// Test dropping JSON entries with specific fields
	cfg := RegexDropConfig{
		Regex:  `"level":"error"`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte(`{"level":"info","message":"All good"}`),
	}

	entry2 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte(`{"level":"error","message":"Something bad"}`),
	}

	entry3 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte(`{"level":"warning","message":"Be careful"}`),
	}

	entries := []*entry.Entry{entry1, entry2, entry3}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Only non-error entries should be kept
	if len(result) != 2 {
		t.Errorf("Expected 2 results, got %d", len(result))
	}
}

func TestRegexDropProcessorMixedNilAndValid(t *testing.T) {
	// Test with mix of nil and valid entries
	cfg := RegexDropConfig{
		Regex:  `drop`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("Keep this"),
	}

	entry2 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("drop this"),
	}

	entries := []*entry.Entry{nil, entry1, nil, entry2, nil}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Only entry1 should be kept (nil entries are skipped, entry2 matches and is dropped)
	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}

	if string(result[0].Data) != "Keep this" {
		t.Errorf("Expected 'Keep this', got '%s'", string(result[0].Data))
	}
}

func TestRegexDropProcessorConfigUpdate(t *testing.T) {
	// Test updating configuration
	cfg := RegexDropConfig{
		Regex:  `drop`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Update configuration to use Invert=true
	newCfg := RegexDropConfig{
		Regex:  `keep`,
		Invert: true,
	}

	err = processor.Config(newCfg)
	if err != nil {
		t.Fatalf("Failed to update config: %v", err)
	}

	// Test that the new config is applied
	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("keep this entry"),
	}

	entry2 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("drop this entry"),
	}

	entries := []*entry.Entry{entry1, entry2}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// With Invert=true and Regex="keep", only entries with "keep" should be kept
	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}

	if string(result[0].Data) != "keep this entry" {
		t.Errorf("Expected 'keep this entry', got '%s'", string(result[0].Data))
	}
}

func TestRegexDropProcessorConfigNil(t *testing.T) {
	// Test Config with nil value
	cfg := RegexDropConfig{
		Regex:  `test`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	err = processor.Config(nil)
	if err == nil {
		t.Error("Config with nil should return error")
	}
}

func TestRegexDropProcessorConfigInvalidType(t *testing.T) {
	// Test Config with invalid type
	cfg := RegexDropConfig{
		Regex:  `test`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	err = processor.Config("invalid type")
	if err == nil {
		t.Error("Config with invalid type should return error")
	}
}

func TestRegexDropProcessorConfigEmptyRegex(t *testing.T) {
	// Test Config with empty regex
	cfg := RegexDropConfig{
		Regex:  `test`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	newCfg := RegexDropConfig{
		Regex:  ``,
		Invert: false,
	}

	err = processor.Config(newCfg)
	if err == nil {
		t.Error("Config with empty regex should return error")
	}
}

func TestRegexDropProcessorConfigInvalidRegex(t *testing.T) {
	// Test Config with invalid regex
	cfg := RegexDropConfig{
		Regex:  `test`,
		Invert: false,
	}

	processor, err := NewRegexDropper(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	newCfg := RegexDropConfig{
		Regex:  `[`,
		Invert: false,
	}

	err = processor.Config(newCfg)
	if err == nil {
		t.Error("Config with invalid regex should return error")
	}
}
