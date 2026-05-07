package main

import (
	"testing"

	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBucketReader_Endpoint(t *testing.T) {
	t.Parallel()

	cfg := BucketConfig{
		AuthConfig: AuthConfig{
			Region:      "us-east-1",
			Bucket_Name: "test-bucket",
			Endpoint:    "http://localhost:9300",
		},
		Credentials_Type: "static",
		ID:               "akid",
		Secret:           "secret",
		Reader:           "line",
		TagName:          "default",
		Name:             "garage-test",
		Proc:             &processors.ProcessorSet{},
		Logger:           log.NewDiscardLogger(),
	}

	br, err := NewBucketReader(cfg)
	require.NoError(t, err)
	require.NotNil(t, br)

	require.NotNil(t, br.session.Config.Endpoint)
	assert.Equal(t, cfg.Endpoint, *br.session.Config.Endpoint)
}

func TestAuthConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		auth    AuthConfig
		wantErr bool
	}{
		{
			name: "standard aws with arn",
			auth: AuthConfig{
				Region:     "us-east-1",
				Bucket_ARN: "arn:aws:s3:::my-bucket",
			},
			wantErr: false,
		},
		{
			name: "custom endpoint with name",
			auth: AuthConfig{
				Region:      "us-east-1",
				Endpoint:    "http://localhost:9000",
				Bucket_Name: "my-bucket",
			},
			wantErr: false,
		},
		{
			name: "custom endpoint missing name",
			auth: AuthConfig{
				Region:   "us-east-1",
				Endpoint: "http://localhost:9000",
			},
			wantErr: true,
		},
		{
			name: "missing region",
			auth: AuthConfig{
				Bucket_ARN: "arn:aws:s3:::my-bucket",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.auth.validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
