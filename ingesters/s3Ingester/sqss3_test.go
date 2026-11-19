package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSQSS3Listener_Endpoint(t *testing.T) {
	t.Parallel()

	cfg := SQSS3Config{
		Region:           "us-east-1",
		Queue:            "http://localhost:9324/q",
		Endpoint:         "http://localhost:9324",
		Credentials_Type: "static",
		ID:               "test-access-key-id",
		Secret:           "test-secret-key",
		Reader:           "line",
		TagName:          "default",
	}

	l, err := NewSQSS3Listener(t.Context(), cfg)
	require.NoError(t, err)
	require.NotNil(t, l)

	assert.Equal(t, cfg.Endpoint, l.sqs.Endpoint())
}

func TestNewSQSS3Listener_AssumeRole(t *testing.T) {
	t.Parallel()

	cfg := SQSS3Config{
		Region:                  "us-east-1",
		Queue:                   "https://sqs.us-east-1.amazonaws.com/123456789012/q",
		Credentials_Type:        "assumerole",
		Role_ARN:                "arn:aws:iam::123456789012:role/gravwell-ingest",
		Source_Credentials_Type: "static",
		ID:                      "test-access-key-id",
		Secret:                  "test-secret-key",
		Reader:                  "line",
		TagName:                 "default",
	}

	l, err := NewSQSS3Listener(t.Context(), cfg)
	require.NoError(t, err)
	require.NotNil(t, l)
}
