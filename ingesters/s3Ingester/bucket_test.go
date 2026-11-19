package main

import (
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
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
		ID:               "test-access-key-id",
		Secret:           "test-secret-key",
		Reader:           "line",
		TagName:          "default",
		Name:             "garage-test",
		Proc:             &processors.ProcessorSet{},
		Logger:           log.NewDiscardLogger(),
	}

	br, err := NewBucketReader(t.Context(), cfg)
	require.NoError(t, err)
	require.NotNil(t, br)

	assert.Equal(t, cfg.Endpoint, br.AuthConfig.Endpoint)
}

func TestNewBucketReader_AssumeRole(t *testing.T) {
	t.Parallel()

	newCfg := func() BucketConfig {
		return BucketConfig{
			AuthConfig: AuthConfig{
				Region:     "us-east-1",
				Bucket_ARN: "arn:aws:s3:::test-bucket",
			},
			Credentials_Type:        "assumerole",
			ID:                      "test-access-key-id",
			Secret:                  "test-secret-key",
			Role_ARN:                "arn:aws:iam::123456789012:role/gravwell-ingest",
			Source_Credentials_Type: "static",
			External_ID:             "some-id",
			Reader:                  "line",
			TagName:                 "default",
			Name:                    "assumerole-test",
			Proc:                    &processors.ProcessorSet{},
			Logger:                  log.NewDiscardLogger(),
		}
	}

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		br, err := NewBucketReader(t.Context(), newCfg())
		require.NoError(t, err)
		require.NotNil(t, br)
	})

	t.Run("Role-ARN without assumerole", func(t *testing.T) {
		t.Parallel()
		cfg := newCfg()
		cfg.Credentials_Type = "static"
		_, err := NewBucketReader(t.Context(), cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Role-ARN requires Credentials-Type=assumerole")
	})
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
