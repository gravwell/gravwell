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
		ID:               "akid",
		Secret:           "secret",
		Reader:           "line",
		TagName:          "default",
	}

	l, err := NewSQSS3Listener(cfg)
	require.NoError(t, err)
	require.NotNil(t, l)

	assert.Equal(t, cfg.Endpoint, l.sqs.Endpoint())
	assert.Equal(t, cfg.Endpoint, *l.session.Config.Endpoint)
}
