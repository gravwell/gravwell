package sqs_common

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQSListener(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		conf             *Config
		expectedEndpoint string
	}{
		{
			name: "standard aws",
			conf: &Config{
				Queue:       "https://sqs.us-east-1.amazonaws.com/12345/my-queue",
				Region:      "us-east-1",
				Credentials: credentials.NewStaticCredentials("akid", "secret", ""),
			},
			expectedEndpoint: "",
		},
		{
			name: "custom endpoint",
			conf: &Config{
				Queue:       "http://localhost:9324/000000000000/test-queue",
				Region:      "elasticmq",
				Endpoint:    "http://localhost:9324",
				Credentials: credentials.NewStaticCredentials("akid", "secret", ""),
			},
			expectedEndpoint: "http://localhost:9324",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, err := SQSListener(tt.conf)
			require.NoError(t, err)
			require.NotNil(t, s)
			require.NotNil(t, s.sess)

			assert.Equal(t, tt.conf.Region, *s.sess.Config.Region)

			if tt.expectedEndpoint == "" {
				assert.Nil(t, s.sess.Config.Endpoint)
			} else {
				require.NotNil(t, s.sess.Config.Endpoint)
				assert.Equal(t, tt.expectedEndpoint, *s.sess.Config.Endpoint)
			}
		})
	}
}

func TestGetCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ctype   string
		akid    string
		secret  string
		wantErr bool
	}{
		{"static valid", "static", "akid", "secret", false},
		{"static, missing id", "static", "", "secret", true},
		{"static, missing secret", "static", "akid", "", true},
		{"environment", "environment", "", "", false},
		{"ec2 role", "ec2role", "", "", false},
		{"invalid type", "foobar", "", "", true},
		{"default (static)", "", "akid", "secret", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			creds, err := GetCredentials(tt.ctype, tt.akid, tt.secret)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, creds)
			}
		})
	}
}
