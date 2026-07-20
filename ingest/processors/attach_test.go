/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"os"
	"testing"

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

type attachTestConfig struct {
	Preprocessor ProcessorConfig
}

func TestAttachConfigValidation(t *testing.T) {
	// Test valid configuration with static values
	cfg := attachTestConfig{}
	if err := config.LoadConfigBytes(&cfg, []byte(validAttachConfig)); err != nil {
		t.Fatalf("Failed to load valid config: %v", err)
	}

	// Load and create the processor
	vc := cfg.Preprocessor["attach1"]
	if vc == nil {
		t.Fatal("Failed to find attach1 preprocessor")
	}

	attachCfg, err := AttachLoadConfig(vc)
	if err != nil {
		t.Fatalf("Failed to load attach config: %v", err)
	}

	processor, err := NewAttachProcessor(attachCfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}
	if processor == nil {
		t.Fatal("Processor is nil")
	}
}

func TestAttachProcessorStaticValues(t *testing.T) {
	cfg := attachTestConfig{}
	if err := config.LoadConfigBytes(&cfg, []byte(staticAttachConfig)); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	vc := cfg.Preprocessor["static"]
	attachCfg, err := AttachLoadConfig(vc)
	if err != nil {
		t.Fatalf("Failed to load attach config: %v", err)
	}

	processor, err := NewAttachProcessor(attachCfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Create test entries
	ent1 := &entry.Entry{
		Tag:  1,
		TS:   entry.Now(),
		Data: []byte("test data 1"),
	}
	ent2 := &entry.Entry{
		Tag:  1,
		TS:   entry.Now(),
		Data: []byte("test data 2"),
	}

	entries := []*entry.Entry{ent1, ent2}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(result))
	}

	// Verify static values were attached
	for i, ent := range result {
		if val, ok := ent.GetEnumeratedValue("foo"); !ok {
			t.Errorf("Entry %d: expected 'foo' enumerated value", i)
		} else if s, ok := val.(string); !ok || s != "bar" {
			t.Errorf("Entry %d: expected foo='bar', got %v", i, val)
		}

		if val, ok := ent.GetEnumeratedValue("baz"); !ok {
			t.Errorf("Entry %d: expected 'baz' enumerated value", i)
		} else if s, ok := val.(string); !ok || s != "qux" {
			t.Errorf("Entry %d: expected baz='qux', got %v", i, val)
		}

		// Verify that the "type" config key is NOT attached as an enumerated value
		if _, ok := ent.GetEnumeratedValue("type"); ok {
			t.Errorf("Entry %d: 'type' configuration key should not be attached", i)
		}
	}
}

func TestAttachProcessorHostname(t *testing.T) {
	cfg := attachTestConfig{}
	if err := config.LoadConfigBytes(&cfg, []byte(hostnameAttachConfig)); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	vc := cfg.Preprocessor["hostname"]
	attachCfg, err := AttachLoadConfig(vc)
	if err != nil {
		t.Fatalf("Failed to load attach config: %v", err)
	}

	processor, err := NewAttachProcessor(attachCfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	expectedHostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("Failed to get hostname: %v", err)
	}

	ent := &entry.Entry{
		Tag:  1,
		TS:   entry.Now(),
		Data: []byte("test data"),
	}

	result, err := processor.Process([]*entry.Entry{ent})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(result))
	}

	if val, ok := result[0].GetEnumeratedValue("host"); !ok {
		t.Error("Expected 'host' enumerated value")
	} else if s, ok := val.(string); !ok || s != expectedHostname {
		t.Errorf("Expected host='%s', got %v", expectedHostname, val)
	}
}

func TestAttachProcessorUUID(t *testing.T) {
	// $UUID is not supported in the preprocessor version of attach
	// It should return an error when attempting to use it
	cfg := attachTestConfig{}
	if err := config.LoadConfigBytes(&cfg, []byte(uuidAttachConfig)); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	vc := cfg.Preprocessor["uuid"]
	_, err := AttachLoadConfig(vc)
	if err == nil {
		t.Fatal("Expected error when using $UUID in preprocessor attach config")
	}
	if err != ErrAttachUUIDNotSupported {
		t.Fatalf("Expected ErrAttachUUIDNotSupported, got: %v", err)
	}
}

func TestAttachProcessorNow(t *testing.T) {
	cfg := attachTestConfig{}
	if err := config.LoadConfigBytes(&cfg, []byte(nowAttachConfig)); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	vc := cfg.Preprocessor["now"]
	attachCfg, err := AttachLoadConfig(vc)
	if err != nil {
		t.Fatalf("Failed to load attach config: %v", err)
	}

	processor, err := NewAttachProcessor(attachCfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ent := &entry.Entry{
		Tag:  1,
		TS:   entry.Now(),
		Data: []byte("test data"),
	}

	result, err := processor.Process([]*entry.Entry{ent})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(result))
	}

	// Verify timestamp is present
	if val, ok := result[0].GetEnumeratedValue("timestamp"); !ok {
		t.Error("Expected 'timestamp' enumerated value")
	} else if _, ok := val.(entry.Timestamp); !ok {
		t.Errorf("Expected timestamp type, got %T", val)
	}
}

func TestAttachProcessorEnvVar(t *testing.T) {
	// Set up test environment variable
	os.Setenv("TEST_ATTACH_VAR", "test_value_123")
	defer os.Unsetenv("TEST_ATTACH_VAR")

	cfg := attachTestConfig{}
	if err := config.LoadConfigBytes(&cfg, []byte(envAttachConfig)); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	vc := cfg.Preprocessor["env"]
	attachCfg, err := AttachLoadConfig(vc)
	if err != nil {
		t.Fatalf("Failed to load attach config: %v", err)
	}

	processor, err := NewAttachProcessor(attachCfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ent := &entry.Entry{
		Tag:  1,
		TS:   entry.Now(),
		Data: []byte("test data"),
	}

	result, err := processor.Process([]*entry.Entry{ent})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(result))
	}

	if val, ok := result[0].GetEnumeratedValue("myenv"); !ok {
		t.Error("Expected 'myenv' enumerated value")
	} else if s, ok := val.(string); !ok || s != "test_value_123" {
		t.Errorf("Expected myenv='test_value_123', got %v", val)
	}
}

func TestAttachProcessorNilEntry(t *testing.T) {
	cfg := attachTestConfig{}
	if err := config.LoadConfigBytes(&cfg, []byte(staticAttachConfig)); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	vc := cfg.Preprocessor["static"]
	attachCfg, err := AttachLoadConfig(vc)
	if err != nil {
		t.Fatalf("Failed to load attach config: %v", err)
	}

	processor, err := NewAttachProcessor(attachCfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Test with nil entries mixed in
	ent := &entry.Entry{
		Tag:  1,
		TS:   entry.Now(),
		Data: []byte("test data"),
	}

	entries := []*entry.Entry{nil, ent, nil}
	result, err := processor.Process(entries)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should return all entries (including nils)
	if len(result) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(result))
	}

	// The valid entry should have values attached
	if val, ok := result[1].GetEnumeratedValue("foo"); !ok {
		t.Error("Expected 'foo' enumerated value on non-nil entry")
	} else if s, ok := val.(string); !ok || s != "bar" {
		t.Errorf("Expected foo='bar', got %v", val)
	}
}

func TestAttachProcessorEmptyEntries(t *testing.T) {
	cfg := attachTestConfig{}
	if err := config.LoadConfigBytes(&cfg, []byte(staticAttachConfig)); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	vc := cfg.Preprocessor["static"]
	attachCfg, err := AttachLoadConfig(vc)
	if err != nil {
		t.Fatalf("Failed to load attach config: %v", err)
	}

	processor, err := NewAttachProcessor(attachCfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	result, err := processor.Process([]*entry.Entry{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result != nil {
		t.Errorf("Expected nil result for empty input, got %v", result)
	}
}

func TestAttachProcessorConfigNil(t *testing.T) {
	cfg := attachTestConfig{}
	if err := config.LoadConfigBytes(&cfg, []byte(staticAttachConfig)); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	vc := cfg.Preprocessor["static"]
	attachCfg, err := AttachLoadConfig(vc)
	if err != nil {
		t.Fatalf("Failed to load attach config: %v", err)
	}

	processor, err := NewAttachProcessor(attachCfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	err = processor.Config(nil)
	if err == nil {
		t.Error("Config with nil should return error")
	}
}

func TestAttachProcessorConfigInvalidType(t *testing.T) {
	cfg := attachTestConfig{}
	if err := config.LoadConfigBytes(&cfg, []byte(staticAttachConfig)); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	vc := cfg.Preprocessor["static"]
	attachCfg, err := AttachLoadConfig(vc)
	if err != nil {
		t.Fatalf("Failed to load attach config: %v", err)
	}

	processor, err := NewAttachProcessor(attachCfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	err = processor.Config("invalid type")
	if err == nil {
		t.Error("Config with invalid type should return error")
	}
}

func TestAttachProcessorManualUUID(t *testing.T) {
	// To avoid manually constructing gcfg.Idx (which is complex),
	// we load a valid config and then inject $UUID.
	cfg := attachTestConfig{}
	if err := config.LoadConfigBytes(&cfg, []byte(validAttachConfig)); err != nil {
		t.Fatalf("Failed to load valid config: %v", err)
	}

	vc := cfg.Preprocessor["attach1"]
	attachCfg, err := AttachLoadConfig(vc)
	if err != nil {
		t.Fatalf("Failed to load attach config: %v", err)
	}

	// Inject $UUID into the valid config
	// We use "foo" which we know exists in validAttachConfig
	val := "$UUID"
	attachCfg["foo"] = val

	_, err = NewAttachProcessor(attachCfg)
	if err == nil {
		t.Fatal("Expected error when using $UUID in manual attach config")
	}
	if err != ErrAttachUUIDNotSupported {
		t.Fatalf("Expected ErrAttachUUIDNotSupported, got: %v", err)
	}
}

func TestAttachProcessorConfigUUID(t *testing.T) {
	// Create a valid processor first
	cfg := attachTestConfig{}
	if err := config.LoadConfigBytes(&cfg, []byte(validAttachConfig)); err != nil {
		t.Fatalf("Failed to load valid config: %v", err)
	}

	vc := cfg.Preprocessor["attach1"]
	attachCfg, err := AttachLoadConfig(vc)
	if err != nil {
		t.Fatalf("Failed to load attach config: %v", err)
	}

	p, err := NewAttachProcessor(attachCfg)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Now try to reconfigure with a UUID config
	// We reuse the valid config but inject $UUID
	val := "$UUID"
	// We need a copy of the config map to avoid modifying the original if it was shared (it's not deeper than this test)
	// But to be safe and clean:
	uuidCfg := attachCfg
	uuidCfg["foo"] = val

	if err := p.Config(uuidCfg); err == nil {
		t.Fatal("Expected error when reconfiguring with $UUID")
	} else if err != ErrAttachUUIDNotSupported {
		t.Fatalf("Expected ErrAttachUUIDNotSupported, got: %v", err)
	}
}

/* Test configurations */

const validAttachConfig = `
[Preprocessor "attach1"]
	Type=attach
	foo="bar"
	baz="qux"
`

const staticAttachConfig = `
[Preprocessor "static"]
	Type=attach
	foo="bar"
	baz="qux"
`

const hostnameAttachConfig = `
[Preprocessor "hostname"]
	Type=attach
	host=$HOSTNAME
`

const uuidAttachConfig = `
[Preprocessor "uuid"]
	Type=attach
	id=$UUID
`

const nowAttachConfig = `
[Preprocessor "now"]
	Type=attach
	timestamp=$NOW
`

const envAttachConfig = `
[Preprocessor "env"]
	Type=attach
	myenv=$TEST_ATTACH_VAR
`
