package sqs

import (
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
)

func TestConfig_Verify(t *testing.T) {
	t.Parallel()

	invalidFallbackUUID := "not-a-uuid"

	tests := []struct {
		name     string
		cfg      Config
		wantErr  bool
		wantUUID string
	}{
		{
			name: "valid config",
			cfg: Config{
				Queue_URL:        "https://sqs.us-east-1.amazonaws.com/123456789012/queue",
				Region:           "us-east-1",
				Credentials_Type: "static",
				AKID:             "akid",
				Secret:           "secret",
			},
			wantErr:  false,
			wantUUID: defaultIngesterUUIDStr,
		},
		{
			name: "valid with explicit UUID",
			cfg: Config{
				Queue_URL:        "https://sqs.us-east-1.amazonaws.com/123456789012/queue",
				Region:           "us-east-1",
				Credentials_Type: "static",
				AKID:             "akid",
				Secret:           "secret",
				BaseConfig:       hosted.BaseConfig{Ingester_UUID: "550e8400-e29b-41d4-a716-446655440000"},
			},
			wantErr:  false,
			wantUUID: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name: "valid environment credentials",
			cfg: Config{
				Queue_URL:        "https://sqs.us-east-1.amazonaws.com/123456789012/queue",
				Region:           "us-east-1",
				Credentials_Type: "environment",
			},
			wantErr:  false,
			wantUUID: defaultIngesterUUIDStr,
		},
		{
			name: "valid ec2role credentials",
			cfg: Config{
				Queue_URL:        "https://sqs.us-east-1.amazonaws.com/123456789012/queue",
				Region:           "us-east-1",
				Credentials_Type: "ec2role",
			},
			wantErr:  false,
			wantUUID: defaultIngesterUUIDStr,
		},
		{
			name: "missing queue url",
			cfg: Config{
				Region:           "us-east-1",
				Credentials_Type: "static",
				AKID:             "akid",
				Secret:           "secret",
			},
			wantErr: true,
		},
		{
			name: "missing region",
			cfg: Config{
				Queue_URL:        "https://sqs.us-east-1.amazonaws.com/123456789012/queue",
				Credentials_Type: "static",
				AKID:             "akid",
				Secret:           "secret",
			},
			wantErr: true,
		},
		{
			name: "invalid credentials type",
			cfg: Config{
				Queue_URL:        "https://sqs.us-east-1.amazonaws.com/123456789012/queue",
				Region:           "us-east-1",
				Credentials_Type: "bogus",
			},
			wantErr: true,
		},
		{
			name: "static credentials missing AKID",
			cfg: Config{
				Queue_URL:        "https://sqs.us-east-1.amazonaws.com/123456789012/queue",
				Region:           "us-east-1",
				Credentials_Type: "static",
				Secret:           "secret",
			},
			wantErr: true,
		},
		{
			name: "static credentials missing secret",
			cfg: Config{
				Queue_URL:        "https://sqs.us-east-1.amazonaws.com/123456789012/queue",
				Region:           "us-east-1",
				Credentials_Type: "static",
				AKID:             "akid",
			},
			wantErr: true,
		},
		{
			name: "invalid fallback UUID",
			cfg: Config{
				Queue_URL:        "https://sqs.us-east-1.amazonaws.com/123456789012/queue",
				Region:           "us-east-1",
				Credentials_Type: "static",
				AKID:             "akid",
				Secret:           "secret",
				BaseConfig:       hosted.BaseConfig{Ingester_UUID: invalidFallbackUUID},
			},
			wantErr:  true,
			wantUUID: invalidFallbackUUID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := tt.cfg
			err := cfg.Verify()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantUUID != "" && cfg.Ingester_UUID != tt.wantUUID {
				t.Errorf("Ingester_UUID = %q, want %q", cfg.Ingester_UUID, tt.wantUUID)
			}
		})
	}
}

func TestConfig_Tags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
		want []string
	}{
		{
			name: "default tag",
			cfg:  Config{},
			want: []string{"default"},
		},
		{
			name: "custom tag",
			cfg:  Config{SingleTagConfig: hosted.SingleTagConfig{Tag_Name: "sqs-orders"}},
			want: []string{"sqs-orders"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.cfg.Tags()
			if len(got) != len(tt.want) {
				t.Fatalf("Tags() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Tags()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
