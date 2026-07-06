package msgraph

import (
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
)

func TestConfig_Verify_IngesterUUID(t *testing.T) {
	validUUID := "550e8400-e29b-41d4-a716-446655440000"
	defaultFallback := "d3667414-e373-4692-a1e2-3a18147e5aa6"

	tests := []struct {
		name         string
		ingesterUUID string
		wantErr      bool
		wantUUID     string
	}{
		{
			name:         "empty Ingester-UUID gets fallback",
			ingesterUUID: "",
			wantErr:      false,
			wantUUID:     defaultFallback,
		},
		{
			name:         "invalid UUID returns error",
			ingesterUUID: "not-a-uuid",
			wantErr:      true,
			wantUUID:     "not-a-uuid",
		},
		{
			name:         "valid UUID is preserved",
			ingesterUUID: validUUID,
			wantErr:      false,
			wantUUID:     validUUID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := Config{
				Tenant_ID:     "t",
				Client_ID:     "c",
				Client_Secret: "s",
				Content_Type:  []ContentType{ContentAlerts},
				BaseConfig:    hosted.BaseConfig{Ingester_UUID: tt.ingesterUUID},
			}
			err := cfg.Verify()
			if (err != nil) != tt.wantErr {
				t.Errorf("Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
			if cfg.Ingester_UUID != tt.wantUUID {
				t.Errorf("Ingester_UUID = %q, want %q", cfg.Ingester_UUID, tt.wantUUID)
			}
		})
	}
}

func TestConfig_Verify_AggregatesErrors(t *testing.T) {
	cfg := Config{
		Tenant_ID:     "",
		Client_ID:     "c",
		Client_Secret: "s",
		Content_Type:  []ContentType{ContentAlerts},
		BaseConfig:    hosted.BaseConfig{Ingester_UUID: "not-a-uuid"},
	}
	err := cfg.Verify()
	if err == nil {
		t.Fatal("expected multiple errors, got nil")
	}
	if cfg.Ingester_UUID != "not-a-uuid" {
		t.Error("Ingester-UUID should not change on error")
	}
}
