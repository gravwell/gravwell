package okta

import (
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/hosted/configtest"
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
				Token:  "token",
				Domain: "example.okta.com",
			},
			wantErr:  false,
			wantUUID: "55af6d4e-3d04-431b-b860-b15b921a46c5",
		},
		{
			name: "valid with UUID",
			cfg: Config{
				Token:      "token",
				Domain:     "example.okta.com",
				BaseConfig: hosted.BaseConfig{Ingester_UUID: "550e8400-e29b-41d4-a716-446655440000"},
			},
			wantErr:  false,
			wantUUID: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name: "empty token",
			cfg: Config{
				Domain: "example.okta.com",
			},
			wantErr: true,
		},
		{
			name: "missing domain",
			cfg: Config{
				Token: "token",
			},
			wantErr: true,
		},
		{
			name: "invalid domain no suffix",
			cfg: Config{
				Token:  "token",
				Domain: "example.com",
			},
			wantErr: true,
		},
		{
			name: "invalid fallback UUID",
			cfg: Config{
				Token:      "token",
				Domain:     "example.okta.com",
				BaseConfig: hosted.BaseConfig{Ingester_UUID: invalidFallbackUUID},
			},
			wantErr:  true,
			wantUUID: invalidFallbackUUID,
		},
		{
			name: "empty Ingester-UUID gets fallback",
			cfg: Config{
				Token:      "token",
				Domain:     "example.okta.com",
				BaseConfig: hosted.BaseConfig{Ingester_UUID: ""},
			},
			wantErr:  false,
			wantUUID: "55af6d4e-3d04-431b-b860-b15b921a46c5",
		},
		{
			name: "batch size too large",
			cfg: Config{
				Token:              "token",
				Domain:             "example.okta.com",
				Request_Batch_Size: 5000,
			},
			wantErr: true,
		},
		{
			name: "requests per minute too large",
			cfg: Config{
				Token:              "token",
				Domain:             "example.okta.com",
				Request_Per_Minute: 10000,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.Verify()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantUUID != "" && tt.cfg.Ingester_UUID != tt.wantUUID {
				t.Errorf("Ingester_UUID = %q, want %q", tt.cfg.Ingester_UUID, tt.wantUUID)
			}
		})
	}
}

func TestConfig_Verify_SetsDefaults(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Token:  "token",
		Domain: "example.okta.com",
	}
	if err := cfg.Verify(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Request_Batch_Size != 100 {
		t.Errorf("expected Request_Batch_Size %d, got %d", 100, cfg.Request_Batch_Size)
	}
	if cfg.Request_Per_Minute != 60 {
		t.Errorf("expected Request_Per_Minute %d, got %d", 60, cfg.Request_Per_Minute)
	}
	if cfg.Request_Burst != 10 {
		t.Errorf("expected Request_Burst %d, got %d", 10, cfg.Request_Burst)
	}
}

func TestConfig_Verify_PrefersExistingValues(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Token:              "token",
		Domain:             "example.okta.com",
		Request_Batch_Size: 200,
		Request_Per_Minute: 120,
		Request_Burst:      20,
	}
	if err := cfg.Verify(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Request_Batch_Size != 200 {
		t.Errorf("expected Request_Batch_Size 200, got %d", cfg.Request_Batch_Size)
	}
	if cfg.Request_Per_Minute != 120 {
		t.Errorf("expected Request_Per_Minute 120, got %d", cfg.Request_Per_Minute)
	}
	if cfg.Request_Burst != 20 {
		t.Errorf("expected Request_Burst 20, got %d", cfg.Request_Burst)
	}
}

func TestConfig_Verify_DomainContainsOkta(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Token:  "token",
		Domain: "sub.domain.okta.com",
	}
	err := cfg.Verify()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(cfg.Domain, "okta.com") {
		t.Error("domain should end with okta.com")
	}
}

func TestConfigEqual(t *testing.T) {
	configtest.CheckEqual(t, Config{
		BaseConfig:         hosted.BaseConfig{Ingester_UUID: defaultIngesterUUIDStr},
		Request_Batch_Size: defaultPageSize,
		Request_Per_Minute: defaultRequestPerMinute,
		Request_Burst:      defaultRequestBurst,
		Domain:             "example.okta.com",
		Token:              "token",
	})
}
