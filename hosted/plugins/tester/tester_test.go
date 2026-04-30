package tester

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestConfig_Verify(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "empty config",
			config:  Config{},
			wantErr: false,
		},
		{
			name: "valid interval",
			config: Config{
				Interval: "5s",
			},
			wantErr: false,
		},
		{
			name: "valid UUID and interval",
			config: Config{
				Ingester_UUID: "550e8400-e29b-41d4-a716-446655440000",
				Interval:      "100ms",
			},
			wantErr: false,
		},
		{
			name: "invalid interval",
			config: Config{
				Interval: "not-a-duration",
			},
			wantErr: true,
		},
		{
			name: "negative interval",
			config: Config{
				Interval: "-5s",
			},
			wantErr: false, // ParseDuration accepts negative, Verify doesn't check
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Verify()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_Verify_GeneratesUUID(t *testing.T) {
	cfg := Config{}
	if err := cfg.Verify(); err != nil {
		t.Fatalf("Config.Verify() unexpected error: %v", err)
	}
	if cfg.Ingester_UUID == "" {
		t.Error("Config.Verify() did not generate UUID when empty")
	}
	if _, err := uuid.Parse(cfg.Ingester_UUID); err != nil {
		t.Errorf("Config.Verify() generated invalid UUID: %v", err)
	}
}

func TestConfig_Verify_PreservesExistingUUID(t *testing.T) {
	originalUUID := "550e8400-e29b-41d4-a716-446655440000"
	cfg := Config{
		Ingester_UUID: originalUUID,
	}
	if err := cfg.Verify(); err != nil {
		t.Fatalf("Config.Verify() unexpected error: %v", err)
	}
	if cfg.Ingester_UUID != originalUUID {
		t.Errorf("Config.Verify() changed UUID, got %v want %v", cfg.Ingester_UUID, originalUUID)
	}
}

func TestConfig_interval(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		want     time.Duration
	}{
		{
			name:     "empty interval",
			interval: "",
			want:     defaultInterval,
		},
		{
			name:     "valid interval",
			interval: "5s",
			want:     5 * time.Second,
		},
		{
			name:     "milliseconds",
			interval: "500ms",
			want:     500 * time.Millisecond,
		},
		{
			name:     "minutes",
			interval: "2m",
			want:     2 * time.Minute,
		},
		{
			name:     "invalid interval",
			interval: "invalid",
			want:     defaultInterval,
		},
		{
			name:     "zero interval",
			interval: "0s",
			want:     defaultInterval,
		},
		{
			name:     "negative interval",
			interval: "-5s",
			want:     defaultInterval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Interval: tt.interval,
			}
			got := cfg.interval()
			if got != tt.want {
				t.Errorf("Config.interval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_UUID(t *testing.T) {
	tests := []struct {
		name          string
		ingesterUUID  string
		want          uuid.UUID
	}{
		{
			name:         "empty UUID",
			ingesterUUID: "",
			want:         uuid.Nil,
		},
		{
			name:         "valid UUID",
			ingesterUUID: "550e8400-e29b-41d4-a716-446655440000",
			want:         uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		},
		{
			name:         "invalid UUID",
			ingesterUUID: "not-a-uuid",
			want:         uuid.Nil,
		},
		{
			name:         "malformed UUID",
			ingesterUUID: "550e8400-e29b-41d4",
			want:         uuid.Nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Ingester_UUID: tt.ingesterUUID,
			}
			got := cfg.UUID()
			if got != tt.want {
				t.Errorf("Config.UUID() = %v, want %v", got, tt.want)
			}
		})
	}
}
