package main

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
		wantErr  bool
	}{
		{name: "empty", input: "", expected: nil},
		{name: "single", input: "hub1", expected: []string{"hub1"}},
		{name: "multiple", input: "a,b,c", expected: []string{"a", "b", "c"}},
		{name: "with spaces", input: " a , b , c ", expected: []string{"a", "b", "c"}},
		{name: "trailing comma", input: "a,b,", expected: []string{"a", "b"}},
		{name: "empty entries", input: "a,,b", expected: []string{"a", "b"}},
		{name: "quoted field with comma", input: `"a,b",c`, expected: []string{"a,b", "c"}},
		{name: "invalid quoting", input: `a,"b`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := splitCSV(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	valid := func() Config {
		return Config{
			Endpoint:  "localhost",
			TokenName: "RootManageSharedAccessKey",
			TokenKey:  "key",
			EventHubs: []string{"test"},
			NumEvents: 10,
		}
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "valid", mutate: func(c *Config) {}},
		{name: "zero events", mutate: func(c *Config) { c.NumEvents = 0 }, wantErr: "num-events must be positive"},
		{name: "negative events", mutate: func(c *Config) { c.NumEvents = -1 }, wantErr: "num-events must be positive"},
		{name: "no event hubs", mutate: func(c *Config) { c.EventHubs = nil }, wantErr: "at least one event hub"},
		{name: "no endpoint", mutate: func(c *Config) { c.Endpoint = "" }, wantErr: "endpoint must not be empty"},
		{name: "no token name", mutate: func(c *Config) { c.TokenName = "" }, wantErr: "token-name must not be empty"},
		{name: "no token key", mutate: func(c *Config) { c.TokenKey = "" }, wantErr: "token-key must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRegisterFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg := &Config{}
	RegisterFlags(fs, cfg)

	err := fs.Parse([]string{
		"-v",
		"-endpoint", "myemulatorhost",
		"-token-name", "myPolicy",
		"-token-key", "mykey",
		"-num-events", "25",
		"-event-hubs", "hub1,hub2",
	})
	require.NoError(t, err)

	assert.True(t, cfg.Verbose)
	assert.Equal(t, "myemulatorhost", cfg.Endpoint)
	assert.Equal(t, "myPolicy", cfg.TokenName)
	assert.Equal(t, "mykey", cfg.TokenKey)
	assert.Equal(t, 25, cfg.NumEvents)
	assert.Equal(t, []string{"hub1", "hub2"}, cfg.EventHubs)
}

func TestRegisterFlagsDefaults(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg := &Config{}
	RegisterFlags(fs, cfg)

	require.NoError(t, fs.Parse(nil))

	assert.Equal(t, "localhost", cfg.Endpoint)
	assert.Equal(t, "RootManageSharedAccessKey", cfg.TokenName)
	assert.Equal(t, 10, cfg.NumEvents)
	assert.Equal(t, []string{"test"}, cfg.EventHubs)
}
