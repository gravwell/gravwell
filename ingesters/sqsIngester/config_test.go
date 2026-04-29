package main

import (
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/stretchr/testify/assert"
)

func TestConfig_Verify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		conf    *cfgType
		wantErr bool
	}{
		{
			name: "valid std config",
			conf: &cfgType{
				IngestConfig: config.IngestConfig{
					Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
					Ingest_Secret:            "secret",
				},
				Queue: map[string]*queue{
					"default": {
						Queue_URL:        "https://sqs.us-east-1.amazonaws.com/123/q",
						Region:           "us-east-1",
						Credentials_Type: "static",
						AKID:             "akid",
						Secret:           "secret",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid local endpoint config",
			conf: &cfgType{
				IngestConfig: config.IngestConfig{
					Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
					Ingest_Secret:            "secret",
				},
				Queue: map[string]*queue{
					"local": {
						Queue_URL:        "http://localhost:9324/q",
						Region:           "us-east-1",
						Endpoint:         "http://localhost:9324",
						Credentials_Type: "static",
						AKID:             "akid",
						Secret:           "secret",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing region",
			conf: &cfgType{
				IngestConfig: config.IngestConfig{
					Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
					Ingest_Secret:            "secret",
				},
				Queue: map[string]*queue{
					"bad": {
						Queue_URL: "https://sqs.us-east-1.amazonaws.com/123/q",
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
