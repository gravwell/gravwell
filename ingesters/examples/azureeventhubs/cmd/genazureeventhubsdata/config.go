package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"strings"
)

// Config holds all parsed configuration for the data generator.
type Config struct {
	Verbose bool

	// Endpoint is the Event Hubs emulator host (no scheme), e.g. "localhost".
	Endpoint  string
	TokenName string
	TokenKey  string

	EventHubs []string

	NumEvents int
}

// RegisterFlags registers all CLI flags on the given FlagSet.
func RegisterFlags(fs *flag.FlagSet, cfg *Config) {
	fs.BoolVar(&cfg.Verbose, "v", false, "Enable verbose output")

	fs.StringVar(&cfg.Endpoint, "endpoint", "localhost", "Event Hubs emulator host (no scheme)")
	fs.StringVar(&cfg.TokenName, "token-name", "RootManageSharedAccessKey", "Shared access policy name")
	fs.StringVar(&cfg.TokenKey, "token-key", "emulatorSasKeyNotForProduction==", "Shared access policy key")

	fs.IntVar(&cfg.NumEvents, "num-events", 10, "Number of events to generate per event hub")

	cfg.EventHubs = []string{"test"}
	fs.Func("event-hubs", "Comma-separated list of event hub names (default \"test\")", func(s string) error {
		var err error
		cfg.EventHubs, err = splitCSV(s)
		return err
	})
}

// Validate checks the config for logical errors.
func (c *Config) Validate() error {
	if c.NumEvents <= 0 {
		return fmt.Errorf("num-events must be positive, got %d", c.NumEvents)
	}
	if len(c.EventHubs) == 0 {
		return fmt.Errorf("at least one event hub must be specified via -event-hubs")
	}
	if c.Endpoint == "" {
		return fmt.Errorf("endpoint must not be empty")
	}
	if c.TokenName == "" {
		return fmt.Errorf("token-name must not be empty")
	}
	if c.TokenKey == "" {
		return fmt.Errorf("token-key must not be empty")
	}
	return nil
}

func splitCSV(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	reader := csv.NewReader(strings.NewReader(s))
	reader.TrimLeadingSpace = true
	record, err := reader.Read()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(record))
	for _, f := range record {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out, nil
}
