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
	}{
		{name: "empty", input: "", expected: nil},
		{name: "single", input: "bucket1", expected: []string{"bucket1"}},
		{name: "multiple", input: "a,b,c", expected: []string{"a", "b", "c"}},
		{name: "with spaces", input: " a , b , c ", expected: []string{"a", "b", "c"}},
		{name: "trailing comma", input: "a,b,", expected: []string{"a", "b"}},
		{name: "empty entries", input: "a,,b", expected: []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, splitCSV(tt.input))
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "no resources",
			cfg:     Config{NumEvents: 10},
			wantErr: "at least one of",
		},
		{
			name:    "zero events",
			cfg:     Config{NumEvents: 0, Buckets: []string{"b"}},
			wantErr: "num-events must be positive",
		},
		{
			name:    "negative events",
			cfg:     Config{NumEvents: -1, Buckets: []string{"b"}},
			wantErr: "num-events must be positive",
		},
		{
			name: "valid s3 only",
			cfg:  Config{NumEvents: 5, Buckets: []string{"my-bucket"}},
		},
		{
			name: "valid sqs only",
			cfg:  Config{NumEvents: 5, SQSQueues: []string{"http://localhost:9324/queue/test"}},
		},
		{
			name: "valid kinesis only",
			cfg:  Config{NumEvents: 5, KinesisStreams: []string{"my-stream"}},
		},
		{
			name: "valid all",
			cfg: Config{
				NumEvents:      5,
				Buckets:        []string{"b"},
				SQSQueues:      []string{"q"},
				KinesisStreams: []string{"s"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfigEnabled(t *testing.T) {
	cfg := Config{}
	assert.False(t, cfg.S3Enabled())
	assert.False(t, cfg.SQSEnabled())
	assert.False(t, cfg.KinesisEnabled())

	cfg.Buckets = []string{"b"}
	assert.True(t, cfg.S3Enabled())

	cfg.SQSQueues = []string{"q"}
	assert.True(t, cfg.SQSEnabled())

	cfg.KinesisStreams = []string{"s"}
	assert.True(t, cfg.KinesisEnabled())
}

func TestRegisterFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg := &Config{}
	RegisterFlags(fs, cfg)

	err := fs.Parse([]string{
		"-v",
		"-s3-profile", "my-s3",
		"-s3-endpoint", "http://localhost:3900",
		"-sqs-profile", "my-sqs",
		"-sqs-endpoint", "http://localhost:9324",
		"-kinesis-profile", "my-kinesis",
		"-kinesis-endpoint", "http://localhost:4567",
		"-num-events", "25",
		"-s3-buckets", "b1,b2",
		"-sqs-queues", "q1,q2",
		"-kinesis-streams", "s1,s2",
	})
	require.NoError(t, err)

	assert.True(t, cfg.Verbose)
	assert.Equal(t, "my-s3", cfg.S3Profile)
	assert.Equal(t, "http://localhost:3900", cfg.S3Endpoint)
	assert.Equal(t, "my-sqs", cfg.SQSProfile)
	assert.Equal(t, "http://localhost:9324", cfg.SQSEndpoint)
	assert.Equal(t, "my-kinesis", cfg.KinesisProfile)
	assert.Equal(t, "http://localhost:4567", cfg.KinesisEndpoint)
	assert.Equal(t, 25, cfg.NumEvents)
	assert.Equal(t, []string{"b1", "b2"}, cfg.Buckets)
	assert.Equal(t, []string{"q1", "q2"}, cfg.SQSQueues)
	assert.Equal(t, []string{"s1", "s2"}, cfg.KinesisStreams)
}
