package hosted

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestParseUUID_Valid catches the bug in the current implementation where
// the err != nil condition is inverted, causing valid UUIDs to always return
// uuid.Nil. The correct condition is err == nil.
func TestParseUUID_Valid(t *testing.T) {
	s := "550e8400-e29b-41d4-a716-446655440000"
	got := ParseUUID(s)
	if got == uuid.Nil {
		t.Errorf("ParseUUID(%q) returned uuid.Nil; condition should be err == nil, not err != nil", s)
	}
	if got.String() != s {
		t.Errorf("expected %s, got %s", s, got.String())
	}
}

func TestParseUUID_Empty(t *testing.T) {
	if got := ParseUUID(""); got != uuid.Nil {
		t.Errorf("expected Nil for empty string, got %v", got)
	}
}

func TestParseUUID_Invalid(t *testing.T) {
	if got := ParseUUID("not-a-uuid"); got != uuid.Nil {
		t.Errorf("expected Nil for invalid string, got %v", got)
	}
}

func TestBaseConfig_UUID_Valid(t *testing.T) {
	b := BaseConfig{Ingester_UUID: "550e8400-e29b-41d4-a716-446655440000"}
	if b.UUID() == uuid.Nil {
		t.Error("expected non-Nil UUID")
	}
}

func TestBaseConfig_UUID_Empty(t *testing.T) {
	b := BaseConfig{}
	if b.UUID() != uuid.Nil {
		t.Error("expected Nil UUID for empty Ingester_UUID")
	}
}

func TestSingleTagConfig_ResolveTag_Set(t *testing.T) {
	c := SingleTagConfig{Tag_Name: "my-tag"}
	if got := c.ResolveTag("default"); got != "my-tag" {
		t.Errorf("expected my-tag, got %s", got)
	}
}

func TestSingleTagConfig_ResolveTag_FallsBackToDefault(t *testing.T) {
	c := SingleTagConfig{}
	if got := c.ResolveTag("default-tag"); got != "default-tag" {
		t.Errorf("expected default-tag, got %s", got)
	}
}

func TestMultiTagConfig_ValidateTags_BothSet(t *testing.T) {
	c := MultiTagConfig{Tag_Name: "x", Tag_Prefix: "y"}
	if err := c.ValidateTags(); err == nil {
		t.Error("expected error when both Tag_Name and Tag_Prefix are set")
	}
}

func TestMultiTagConfig_ValidateTags_OnlyName(t *testing.T) {
	c := MultiTagConfig{Tag_Name: "x"}
	if err := c.ValidateTags(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMultiTagConfig_ValidateTags_OnlyPrefix(t *testing.T) {
	c := MultiTagConfig{Tag_Prefix: "y"}
	if err := c.ValidateTags(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMultiTagConfig_ValidateTags_NeitherSet(t *testing.T) {
	c := MultiTagConfig{}
	if err := c.ValidateTags(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMultiTagConfig_ResolveTag(t *testing.T) {
	tests := []struct {
		name          string
		cfg           MultiTagConfig
		kind          string
		defaultPrefix string
		want          string
	}{
		{
			name:          "Tag_Name overrides everything",
			cfg:           MultiTagConfig{Tag_Name: "explicit"},
			kind:          "alerts",
			defaultPrefix: "msgraph",
			want:          "explicit",
		},
		{
			name:          "Tag_Prefix prepended to kind",
			cfg:           MultiTagConfig{Tag_Prefix: "azure"},
			kind:          "alerts",
			defaultPrefix: "msgraph",
			want:          "azure-alerts",
		},
		{
			name:          "default prefix used when neither set",
			cfg:           MultiTagConfig{},
			kind:          "alerts",
			defaultPrefix: "msgraph",
			want:          "msgraph-alerts",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ResolveTag(tt.kind, tt.defaultPrefix); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPollingConfig_ApplyDefaults_SetsZeroValues(t *testing.T) {
	p := PollingConfig{}
	p.ApplyDefaults(168, 10, 30)
	if p.Lookback != 168 {
		t.Errorf("Lookback: got %d, want 168", p.Lookback)
	}
	if p.Requests_Per_Minute != 10 {
		t.Errorf("RPM: got %d, want 10", p.Requests_Per_Minute)
	}
	if p.Request_Interval != 30 {
		t.Errorf("Interval: got %d, want 30", p.Request_Interval)
	}
}

func TestPollingConfig_ApplyDefaults_DoesNotOverrideSetValues(t *testing.T) {
	p := PollingConfig{Lookback: 48, Requests_Per_Minute: 5, Request_Interval: 60}
	p.ApplyDefaults(168, 10, 30)
	if p.Lookback != 48 {
		t.Errorf("Lookback should not change: got %d", p.Lookback)
	}
	if p.Requests_Per_Minute != 5 {
		t.Errorf("RPM should not change: got %d", p.Requests_Per_Minute)
	}
	if p.Request_Interval != 60 {
		t.Errorf("Interval should not change: got %d", p.Request_Interval)
	}
}

func TestPollingConfig_LookbackDuration(t *testing.T) {
	p := PollingConfig{Lookback: 24}
	if got := p.LookbackDuration(); got != 24*time.Hour {
		t.Errorf("expected 24h, got %v", got)
	}
}

func TestPollingConfig_ContinueAfterInterval(t *testing.T) {
	p := PollingConfig{Request_Interval: 30}
	c := p.ContinueAfterInterval()
	if c == nil {
		t.Fatal("expected non-nil Continuation")
	}
	if c.Delay != 30*time.Second {
		t.Errorf("expected 30s delay, got %v", c.Delay)
	}
}

func TestPollingConfig_PendingOrInterval(t *testing.T) {
	p := PollingConfig{Request_Interval: 30}

	t.Run("pending returns immediate", func(t *testing.T) {
		c := p.PendingOrInterval(true)
		if c == nil {
			t.Fatal("expected non-nil")
		}
		if c.Delay != 0 {
			t.Errorf("expected zero delay, got %v", c.Delay)
		}
	})

	t.Run("not pending returns interval", func(t *testing.T) {
		c := p.PendingOrInterval(false)
		if c == nil {
			t.Fatal("expected non-nil")
		}
		if c.Delay != 30*time.Second {
			t.Errorf("expected 30s delay, got %v", c.Delay)
		}
	})
}

func TestVerifyIngesterUUIDWithFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		fallback   string
		wantErr    bool
		wantUUID   string
		errWrapped bool
	}{
		{
			name:       "empty sets fallback",
			input:      "",
			fallback:   "fallback-uuid",
			wantErr:    false,
			wantUUID:   "fallback-uuid",
			errWrapped: false,
		},
		{
			name:       "valid UUID passes through",
			input:      "550e8400-e29b-41d4-a716-446655440000",
			fallback:   "fallback",
			wantErr:    false,
			wantUUID:   "550e8400-e29b-41d4-a716-446655440000",
			errWrapped: false,
		},
		{
			name:       "invalid UUID returns error",
			input:      "not-a-uuid",
			fallback:   "fallback",
			wantErr:    true,
			wantUUID:   "not-a-uuid",
			errWrapped: true,
		},
		{
			name:       "invalid UUID does not override",
			input:      "bogus-uuid-string",
			fallback:   "d3667414-e373-4692-a1e2-3a18147e5aa6",
			wantErr:    true,
			wantUUID:   "bogus-uuid-string",
			errWrapped: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := BaseConfig{Ingester_UUID: tt.input}
			err := b.VerifyIngesterUUIDWithFallback(tt.fallback)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyIngesterUUIDWithFallback(%q, %q) error = %v, wantErr %v", tt.input, tt.fallback, err, tt.wantErr)
			}
			if tt.errWrapped {
				if !errors.Is(err, ErrInvalidIngesterUUID) {
					t.Errorf("expected error to wrap ErrInvalidIngesterUUID, got %v", err)
				}
			}
			if b.Ingester_UUID != tt.wantUUID {
				t.Errorf("Ingester_UUID = %q, want %q", b.Ingester_UUID, tt.wantUUID)
			}
		})
	}
}
