package main

import (
	"encoding/json"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			name: "valid bucket w/ assumerole",
			conf: &cfgType{
				IngestConfig: config.IngestConfig{
					Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
					Ingest_Secret:            "secret",
				},
				State_Store_Location: "/tmp/s3_state",
				Bucket: map[string]*bucket{
					"mybucket": {
						AuthConfig: AuthConfig{
							Region:     "us-east-1",
							Bucket_ARN: "arn:aws:s3:::mybucket",
						},
						Credentials_Type: "assumerole",
						Role_ARN:         "arn:aws:iam::123456789012:role/gravwell-ingest",
						External_ID:      "some-id",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid sqs/s3 w/ assumerole and static source",
			conf: &cfgType{
				IngestConfig: config.IngestConfig{
					Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
					Ingest_Secret:            "secret",
				},
				State_Store_Location: "/tmp/s3_state",
				SQS_S3_Listener: map[string]*sqsS3{
					"sqs1": {
						Region:                  "us-east-1",
						Queue_URL:               "https://sqs.us-east-1.amazonaws.com/123456789012/q",
						Credentials_Type:        "assumerole",
						Role_ARN:                "arn:aws:iam::123456789012:role/gravwell-ingest",
						Source_Credentials_Type: "static",
						ID:                      "id",
						Secret:                  "secret",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "bucket Role-ARN without assumerole",
			conf: &cfgType{
				IngestConfig: config.IngestConfig{
					Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
					Ingest_Secret:            "secret",
				},
				State_Store_Location: "/tmp/s3_state",
				Bucket: map[string]*bucket{
					"mybucket": {
						AuthConfig: AuthConfig{
							Region:     "us-east-1",
							Bucket_ARN: "arn:aws:s3:::mybucket",
						},
						Credentials_Type: "static",
						ID:               "id",
						Secret:           "secret",
						Role_ARN:         "arn:aws:iam::123456789012:role/gravwell-ingest",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "sqs/s3 External-ID without assumerole",
			conf: &cfgType{
				IngestConfig: config.IngestConfig{
					Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
					Ingest_Secret:            "secret",
				},
				State_Store_Location: "/tmp/s3_state",
				SQS_S3_Listener: map[string]*sqsS3{
					"sqs1": {
						Region:           "us-east-1",
						Queue_URL:        "https://sqs.us-east-1.amazonaws.com/123456789012/q",
						Credentials_Type: "environment",
						External_ID:      "some-id",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "assumerole missing Role-ARN",
			conf: &cfgType{
				IngestConfig: config.IngestConfig{
					Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
					Ingest_Secret:            "secret",
				},
				State_Store_Location: "/tmp/s3_state",
				Bucket: map[string]*bucket{
					"mybucket": {
						AuthConfig: AuthConfig{
							Region:     "us-east-1",
							Bucket_ARN: "arn:aws:s3:::mybucket",
						},
						Credentials_Type: "assumerole",
					},
				},
			},
			wantErr: true,
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

func TestS3Config_ParseAssumeRole(t *testing.T) {
	t.Parallel()

	const conf = `
[Global]
Ingest-Secret=secret
Cleartext-Backend-Target=127.0.0.1:4023
State-Store-Location=/tmp/s3_state

[Bucket "b"]
	Region=us-east-1
	Bucket-ARN="arn:aws:s3:::mybucket"
	Credentials-Type=assumerole
	Role-ARN="arn:aws:iam::123456789012:role/gravwell-ingest"
	External-ID="some-id"
	Source-Credentials-Type=environment

[SQS-S3-Listener "s"]
	Region=us-east-1
	Queue-URL="https://sqs.us-east-1.amazonaws.com/123456789012/q"
	Credentials-Type=assumerole
	Role-ARN="arn:aws:iam::123456789012:role/gravwell-ingest"
	External-ID="other-id"
	Source-Credentials-Type=ec2role
`
	var cr cfgReadType
	require.NoError(t, config.LoadConfigBytes(&cr, []byte(conf)))

	b := cr.Bucket["b"]
	require.NotNil(t, b)
	assert.Equal(t, "arn:aws:iam::123456789012:role/gravwell-ingest", b.Role_ARN)
	assert.Equal(t, "some-id", b.External_ID)
	assert.Equal(t, "environment", b.Source_Credentials_Type)
	assert.NoError(t, b.credentialsConfig().Validate())

	s := cr.SQS_S3_Listener["s"]
	require.NotNil(t, s)
	assert.Equal(t, "arn:aws:iam::123456789012:role/gravwell-ingest", s.Role_ARN)
	assert.Equal(t, "other-id", s.External_ID)
	assert.Equal(t, "ec2role", s.Source_Credentials_Type)
	assert.NoError(t, s.credentialsConfig().Validate())
}

// The config is reported to the indexer as JSON.
func TestS3Config_MarshalCredentials(t *testing.T) {
	t.Parallel()

	unused, err := json.Marshal(bucket{Credentials_Type: "static", ID: "id", Secret: "secret"})
	require.NoError(t, err)
	assert.NotContains(t, string(unused), "Role_ARN")
	assert.NotContains(t, string(unused), "External_ID")
	assert.NotContains(t, string(unused), "Source_Credentials_Type")

	used, err := json.Marshal(sqsS3{
		Credentials_Type:        "assumerole",
		Role_ARN:                "arn:aws:iam::123456789012:role/gravwell-ingest",
		External_ID:             "external-id-value",
		Source_Credentials_Type: "environment",
	})
	require.NoError(t, err)
	assert.Contains(t, string(used), `"Role_ARN":"arn:aws:iam::123456789012:role/gravwell-ingest"`)
	assert.Contains(t, string(used), `"Source_Credentials_Type":"environment"`)
	assert.NotContains(t, string(used), "external-id-value")
}
