package main

import (
	"testing"

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/stretchr/testify/assert"
)

func TestS3Config_Verify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		conf    *cfgType
		wantErr bool
	}{
		{
			name: "valid minimal",
			conf: &cfgType{
				IngestConfig: config.IngestConfig{
					Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
					Ingest_Secret:            "secret",
				},
				State_Store_Location: "/tmp/s3_state",
				Bucket: map[string]*bucket{
					"mybucket": {
						AuthConfig: AuthConfig{
							Region:      "us-east-1",
							Bucket_Name: "mybucket",
							Bucket_ARN:  "arn:aws:s3:::mybucket",
						},
						Credentials_Type: "static",
						ID:               "id",
						Secret:           "secret",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid sqs/s3 w/ endpoint",
			conf: &cfgType{
				IngestConfig: config.IngestConfig{
					Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
					Ingest_Secret:            "secret",
				},
				State_Store_Location: "/tmp/s3_state",
				SQS_S3_Listener: map[string]*sqsS3{
					"sqs1": {
						Region:           "us-east-1",
						Queue_URL:        "http://localhost:9324/q",
						Endpoint:         "http://localhost:9324",
						Credentials_Type: "static",
						ID:               "id",
						Secret:           "secret",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing state store",
			conf: &cfgType{
				IngestConfig: config.IngestConfig{
					Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
					Ingest_Secret:            "secret",
				},
				Bucket: map[string]*bucket{
					"mybucket": {AuthConfig: AuthConfig{Region: "us-east-1", Bucket_Name: "mybucket"}},
				},
			},
			wantErr: true,
		},
		{
			name: "mutual exclusion w/ endpoint and arn",
			conf: &cfgType{
				IngestConfig: config.IngestConfig{
					Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
					Ingest_Secret:            "secret",
				},
				State_Store_Location: "/tmp/s3_state",
				Bucket: map[string]*bucket{
					"mybucket": {
						AuthConfig: AuthConfig{
							Region:      "us-east-1",
							Endpoint:    "http://localhost:9324",
							Bucket_Name: "mybucket",
							Bucket_ARN:  "arn:aws:s3:::mybucket",
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.conf.Verify()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
