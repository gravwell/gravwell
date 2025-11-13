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

func TestRegexReplaceConfigValidation(t *testing.T) {
	// Test valid configuration
	validConfig := RegexReplaceConfig{
		Regex:       `test`,
		Replacement: `replacement`,
	}

	if err := validConfig.validate(); err != nil {
		t.Errorf("Valid config should not return error: %v", err)
	}

	// Test invalid regex
	invalidConfig := RegexReplaceConfig{
		Regex:       `[`,
		Replacement: `replacement`,
	}

	if err := invalidConfig.validate(); err == nil {
		t.Error("Invalid regex should return error")
	}

	// Test empty regex
	emptyConfig := RegexReplaceConfig{
		Regex:       ``,
		Replacement: `replacement`,
	}

	if err := emptyConfig.validate(); err == nil {
		t.Error("Empty regex should return error")
	}
}

func TestRegexReplaceProcessor(t *testing.T) {
	// Create a regex replace processor
	cfg := RegexReplaceConfig{
		Regex:         `test`,
		Replacement:   `replacement`,
		CaseSensitive: true,
	}

	processor, err := NewRegexReplacer(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Test with simple text
	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("This is a test string with test words"),
	}

	entries := []*entry.Entry{entry1}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}

	expected := "This is a replacement string with replacement words"
	actual := string(result[0].Data)
	if actual != expected {
		t.Errorf("Expected '%s', got '%s'", expected, actual)
	}
}

func TestRegexReplaceProcessorCaseInsensitive(t *testing.T) {
	// Create a case-insensitive regex replace processor
	cfg := RegexReplaceConfig{
		Regex:         `test`,
		Replacement:   `replacement`,
		CaseSensitive: false,
	}

	processor, err := NewRegexReplacer(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Test with mixed case text
	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("This is a TEST string with Test words"),
	}

	entries := []*entry.Entry{entry1}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}

	expected := "This is a replacement string with replacement words"
	actual := string(result[0].Data)
	if actual != expected {
		t.Errorf("Expected '%s', got '%s'", expected, actual)
	}
}

func TestRegexReplaceProcessorWithCaptureGroups(t *testing.T) {
	// Test with capture groups
	cfg := RegexReplaceConfig{
		Regex:         `(\w+)@(\w+\.\w+)`,
		Replacement:   `$1 at $2`,
		CaseSensitive: true,
	}

	processor, err := NewRegexReplacer(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Test with email-like text
	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("Contact us at user@example.com or admin@test.org"),
	}

	entries := []*entry.Entry{entry1}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}

	expected := "Contact us at user at example.com or admin at test.org"
	actual := string(result[0].Data)
	if actual != expected {
		t.Errorf("Expected '%s', got '%s'", expected, actual)
	}
}

func TestRegexReplaceProcessorEmptyEntry(t *testing.T) {
	// Test with empty entry
	cfg := RegexReplaceConfig{
		Regex:         `test`,
		Replacement:   `replacement`,
		CaseSensitive: true,
	}

	processor, err := NewRegexReplacer(cfg)
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

	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}

	if string(result[0].Data) != "" {
		t.Errorf("Expected empty data, got '%s'", string(result[0].Data))
	}
}

func TestRegexReplaceProcessorNoMatch(t *testing.T) {
	// Test with no matches
	cfg := RegexReplaceConfig{
		Regex:         `xyz`,
		Replacement:   `replacement`,
		CaseSensitive: true,
	}

	processor, err := NewRegexReplacer(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("This is a test string"),
	}

	entries := []*entry.Entry{entry1}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}

	expected := "This is a test string"
	actual := string(result[0].Data)
	if actual != expected {
		t.Errorf("Expected '%s', got '%s'", expected, actual)
	}
}

func TestRegexReplaceProcessorMultipleMatches(t *testing.T) {
	// Test with multiple matches
	cfg := RegexReplaceConfig{
		Regex:         `a`,
		Replacement:   `X`,
		CaseSensitive: true,
	}

	processor, err := NewRegexReplacer(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("banana"),
	}

	entries := []*entry.Entry{entry1}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}

	expected := "bXnXnX"
	actual := string(result[0].Data)
	if actual != expected {
		t.Errorf("Expected '%s', got '%s'", expected, actual)
	}
}

func TestRegexReplaceProcessorJSON(t *testing.T) {
	// Test with JSON data
	cfg := RegexReplaceConfig{
		Regex:         `"name":"(?P<m>[^"]*)"`,
		Replacement:   `"name":"${m}_modified"`,
		CaseSensitive: true,
	}

	processor, err := NewRegexReplacer(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte(`{"name":"john","age":30,"city":"new york"}`),
	}

	entries := []*entry.Entry{entry1}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}

	expected := `{"name":"john_modified","age":30,"city":"new york"}`
	actual := string(result[0].Data)
	if actual != expected {
		t.Errorf("Expected '%s', got '%s'", expected, actual)
	}
}

func TestRegexReplaceProcessorSpecialChars(t *testing.T) {
	// Test with special regex characters
	cfg := RegexReplaceConfig{
		Regex:         `\d+`,
		Replacement:   `NUMBER`,
		CaseSensitive: true,
	}

	processor, err := NewRegexReplacer(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	entry1 := &entry.Entry{
		Tag:  1,
		SRC:  nil,
		TS:   entry.Now(),
		Data: []byte("Phone: 123-456-7890, Age: 25, ID: 987654321"),
	}

	entries := []*entry.Entry{entry1}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}

	expected := "Phone: NUMBER-NUMBER-NUMBER, Age: NUMBER, ID: NUMBER"
	actual := string(result[0].Data)
	if actual != expected {
		t.Errorf("Expected '%s', got '%s'", expected, actual)
	}
}

func TestRegexReplaceProcessorNilEntry(t *testing.T) {
	// Test with nil entry
	cfg := RegexReplaceConfig{
		Regex:         `test`,
		Replacement:   `replacement`,
		CaseSensitive: true,
	}

	processor, err := NewRegexReplacer(cfg)
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

func TestRegexReplaceProcessorConfigUpdate(t *testing.T) {
	// Test updating configuration
	cfg := RegexReplaceConfig{
		Regex:         `test`,
		Replacement:   `replacement`,
		CaseSensitive: true,
	}

	processor, err := NewRegexReplacer(cfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Update configuration
	newCfg := RegexReplaceConfig{
		Regex:         `old`,
		Replacement:   `new`,
		CaseSensitive: false,
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
		Data: []byte("This is an OLD string with OLD words"),
	}

	entries := []*entry.Entry{entry1}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}

	expected := "This is an new string with new words"
	actual := string(result[0].Data)
	if actual != expected {
		t.Errorf("Expected '%s', got '%s'", expected, actual)
	}
}
