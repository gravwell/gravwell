package main

import (
	"flag"
	"fmt"
	"strings"
)

// Config holds all parsed configuration for the data generator.
type Config struct {
	Verbose bool

	// S3
	S3Profile  string
	S3Endpoint string
	Buckets    []string

	// SQS
	SQSProfile  string
	SQSEndpoint string
	SQSQueues   []string

	// Kinesis
	KinesisProfile  string
	KinesisEndpoint string
	KinesisStreams  []string

	NumEvents int
}

// RegisterFlags registers all CLI flags on the given FlagSet.
func RegisterFlags(fs *flag.FlagSet, cfg *Config) {
	fs.BoolVar(&cfg.Verbose, "v", false, "Enable verbose output")

	fs.StringVar(&cfg.S3Profile, "s3-profile", "", "AWS profile for S3")
	fs.StringVar(&cfg.S3Endpoint, "s3-endpoint", "", "Custom endpoint for S3")

	fs.StringVar(&cfg.SQSProfile, "sqs-profile", "", "AWS profile for SQS")
	fs.StringVar(&cfg.SQSEndpoint, "sqs-endpoint", "", "Custom endpoint for SQS")

	fs.StringVar(&cfg.KinesisProfile, "kinesis-profile", "", "AWS profile for Kinesis")
	fs.StringVar(&cfg.KinesisEndpoint, "kinesis-endpoint", "", "Custom endpoint for Kinesis")

	fs.IntVar(&cfg.NumEvents, "num-events", 10, "Number of events to generate per resource")

	// These are parsed as comma-separated strings via custom handling.
	fs.Func("s3-buckets", "Comma-separated list of S3 buckets", func(s string) error {
		cfg.Buckets = splitCSV(s)
		return nil
	})
	fs.Func("sqs-queues", "Comma-separated list of SQS queue URLs", func(s string) error {
		cfg.SQSQueues = splitCSV(s)
		return nil
	})
	fs.Func("kinesis-streams", "Comma-separated list of Kinesis stream names", func(s string) error {
		cfg.KinesisStreams = splitCSV(s)
		return nil
	})
}

// Validate checks the config for logical errors.
func (c *Config) Validate() error {
	if c.NumEvents <= 0 {
		return fmt.Errorf("num-events must be positive, got %d", c.NumEvents)
	}
	if len(c.Buckets) == 0 && len(c.SQSQueues) == 0 && len(c.KinesisStreams) == 0 {
		return fmt.Errorf("at least one of -s3-buckets, -sqs-queues, or -kinesis-streams must be specified")
	}
	return nil
}

// S3Enabled returns true if S3 resources are configured.
func (c *Config) S3Enabled() bool {
	return len(c.Buckets) > 0
}

// SQSEnabled returns true if SQS resources are configured.
func (c *Config) SQSEnabled() bool {
	return len(c.SQSQueues) > 0
}

// KinesisEnabled returns true if Kinesis resources are configured.
func (c *Config) KinesisEnabled() bool {
	return len(c.KinesisStreams) > 0
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
