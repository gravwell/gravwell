package mimecast

import (
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/configtest"
)

func TestConfig_Verify(t *testing.T) {
	t.Parallel()

	invalidFallbackUUID := "not-a-uuid"

	tests := []struct {
		name     string
		cfg      Config
		wantErr  bool
		wantUUID string
		wantHost string
	}{
		{
			name: "valid config",
			cfg: Config{
				Client_Id:     "client-id",
				Client_Secret: "client-secret",
				Api:           []Api{MtaDeliveryApi},
			},
			wantErr:  false,
			wantUUID: "e528af50-3ccf-41be-b930-78ae9e10648d",
			wantHost: defaultHost,
		},
		{
			name: "valid with UUID",
			cfg: Config{
				Client_Id:     "client-id",
				Client_Secret: "client-secret",
				Api:           []Api{MtaDeliveryApi},
				BaseConfig:    hosted.BaseConfig{Ingester_UUID: "550e8400-e29b-41d4-a716-446655440000"},
			},
			wantErr:  false,
			wantUUID: "550e8400-e29b-41d4-a716-446655440000",
			wantHost: defaultHost,
		},
		{
			name: "missing client id",
			cfg: Config{
				Client_Secret: "client-secret",
				Api:           []Api{MtaDeliveryApi},
			},
			wantErr: true,
		},
		{
			name: "missing client secret",
			cfg: Config{
				Client_Id: "client-id",
				Api:       []Api{MtaDeliveryApi},
			},
			wantErr: true,
		},
		{
			name: "unsupported api",
			cfg: Config{
				Client_Id:     "client-id",
				Client_Secret: "client-secret",
				Api:           []Api{"unsupported"},
			},
			wantErr: true,
		},
		{
			name: "tag name with multiple apis",
			cfg: Config{
				Client_Id:      "client-id",
				Client_Secret:  "client-secret",
				Api:            []Api{MtaDeliveryApi, MtaReceiptApi},
				MultiTagConfig: hosted.MultiTagConfig{Tag_Name: "tag"},
			},
			wantErr: true,
		},
		{
			name: "invalid fallback UUID",
			cfg: Config{
				Client_Id:     "client-id",
				Client_Secret: "client-secret",
				Api:           []Api{MtaDeliveryApi},
				BaseConfig:    hosted.BaseConfig{Ingester_UUID: invalidFallbackUUID},
			},
			wantErr:  true,
			wantUUID: invalidFallbackUUID,
		},
		{
			name: "empty Ingester-UUID gets fallback",
			cfg: Config{
				Client_Id:     "client-id",
				Client_Secret: "client-secret",
				Api:           []Api{MtaDeliveryApi},
				BaseConfig:    hosted.BaseConfig{Ingester_UUID: ""},
			},
			wantErr:  false,
			wantUUID: "e528af50-3ccf-41be-b930-78ae9e10648d",
			wantHost: defaultHost,
		},
		{
			name: "audit api is supported",
			cfg: Config{
				Client_Id:     "client-id",
				Client_Secret: "client-secret",
				Api:           []Api{AuditApi},
			},
			wantErr: false,
		},
		{
			name: "custom host preserved",
			cfg: Config{
				Client_Id:     "client-id",
				Client_Secret: "client-secret",
				Api:           []Api{MtaDeliveryApi},
				Host:          "https://custom.mimecast.com",
			},
			wantErr:  false,
			wantHost: "https://custom.mimecast.com",
		},
		{
			name: "both tag name and prefix",
			cfg: Config{
				Client_Id:     "client-id",
				Client_Secret: "client-secret",
				Api:           []Api{MtaDeliveryApi},
				MultiTagConfig: hosted.MultiTagConfig{
					Tag_Name:   "name",
					Tag_Prefix: "prefix",
				},
			},
			wantErr: true,
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
			if tt.wantErr {
				return
			}
			if tt.wantUUID != "" && cfg.Ingester_UUID != tt.wantUUID {
				t.Errorf("Ingester_UUID = %q, want %q", cfg.Ingester_UUID, tt.wantUUID)
			}
			if tt.wantHost != "" && cfg.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", cfg.Host, tt.wantHost)
			}
		})
	}
}

func TestConfig_Verify_SetsDefaults(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Client_Id:     "client-id",
		Client_Secret: "client-secret",
		Api:           []Api{MtaDeliveryApi},
	}
	if err := cfg.Verify(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Lookback != defaultLookback {
		t.Errorf("Lookback = %d, want %d", cfg.Lookback, defaultLookback)
	}
	if cfg.Requests_Per_Minute != defaultRequestsPerMinute {
		t.Errorf("Requests_Per_Minute = %d, want %d", cfg.Requests_Per_Minute, defaultRequestsPerMinute)
	}
	if cfg.Request_Interval != defaultInterval {
		t.Errorf("Request_Interval = %d, want %d", cfg.Request_Interval, defaultInterval)
	}
	if cfg.Host != defaultHost {
		t.Errorf("Host = %q, want %q", cfg.Host, defaultHost)
	}
}

func TestConfig_Verify_PrefersExistingValues(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Client_Id:     "client-id",
		Client_Secret: "client-secret",
		Api:           []Api{MtaDeliveryApi},
		Host:          "https://custom.com",
		PollingConfig: hosted.PollingConfig{Lookback: 48, Requests_Per_Minute: 10, Request_Interval: 600},
	}
	if err := cfg.Verify(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Lookback != 48 {
		t.Errorf("Lookback = %d, want 48", cfg.Lookback)
	}
	if cfg.Requests_Per_Minute != 10 {
		t.Errorf("Requests_Per_Minute = %d, want 10", cfg.Requests_Per_Minute)
	}
	if cfg.Request_Interval != 600 {
		t.Errorf("Request_Interval = %d, want 600", cfg.Request_Interval)
	}
	if cfg.Host != "https://custom.com" {
		t.Errorf("Host = %q, want https://custom.com", cfg.Host)
	}
}

func TestConfigEqual(t *testing.T) {
	configtest.CheckEqual(t, Config{
		BaseConfig:     hosted.BaseConfig{Ingester_UUID: defaultIngesterUUIDStr},
		MultiTagConfig: hosted.MultiTagConfig{Tag_Prefix: "mimecast"},
		PollingConfig: hosted.PollingConfig{
			Lookback:            defaultLookback,
			Requests_Per_Minute: defaultRequestsPerMinute,
			Request_Interval:    defaultInterval,
		},
		Client_Id:     "id",
		Client_Secret: "secret",
		Api:           []Api{AuditApi},
		Host:          defaultHost,
		Preprocessor:  []string{"pp"},
	})
}
