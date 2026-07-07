package hosted

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestBaseConfig_VerifyIngesterUUID_Valid catches that valid UUIDs pass validation.
func TestBaseConfig_VerifyIngesterUUID_Valid(t *testing.T) {
	t.Parallel()
	b := BaseConfig{Ingester_UUID: "550e8400-e29b-41d4-a716-446655440000"}
	if err := b.VerifyIngesterUUID(); err != nil {
		t.Errorf("VerifyIngesterUUID() = %v, want nil", err)
	}
}

// TestBaseConfig_VerifyIngesterUUID_Invalid catches that invalid UUIDs return ErrInvalidIngesterUUID.
func TestBaseConfig_VerifyIngesterUUID_Invalid(t *testing.T) {
	t.Parallel()
	b := BaseConfig{Ingester_UUID: "not-a-uuid"}
	err := b.VerifyIngesterUUID()
	if err == nil {
		t.Fatal("VerifyIngesterUUID() = nil, want error")
	}
	if err.Error() == "" {
		t.Error("VerifyIngesterUUID() error message is empty")
	}
}

// TestBaseConfig_VerifyIngesterUUID_Empty catches that empty UUID is treated as invalid by uuid.Parse.
func TestBaseConfig_VerifyIngesterUUID_Empty(t *testing.T) {
	t.Parallel()
	b := BaseConfig{Ingester_UUID: ""}
	if err := b.VerifyIngesterUUID(); err == nil {
		t.Errorf("VerifyIngesterUUID() = nil, want error for empty string (uuid.Parse rejects it)")
	}
}

// TestBaseConfig_VerifyIngesterUUID_Malformed catches that shorter UUID strings are rejected.
func TestBaseConfig_VerifyIngesterUUID_Malformed(t *testing.T) {
	t.Parallel()
	b := BaseConfig{Ingester_UUID: "550e8400-e29b-41d4"}
	if err := b.VerifyIngesterUUID(); err == nil {
		t.Fatal("VerifyIngesterUUID() = nil, want error for malformed UUID")
	}
}

// TestBaseConfig_ApplyDefaultIngesterUUID_Empty catches that empty UUID gets defaulted.
func TestBaseConfig_ApplyDefaultIngesterUUID_Empty(t *testing.T) {
	t.Parallel()
	want := "d3667414-e373-4692-a1e2-3a18147e5aa6"
	b := BaseConfig{}
	b.ApplyDefaultIngesterUUID(want)
	if b.Ingester_UUID != want {
		t.Errorf("Ingester_UUID = %q, want %q", b.Ingester_UUID, want)
	}
}

// TestBaseConfig_ApplyDefaultIngesterUUID_Filled catches that non-empty UUID is preserved.
func TestBaseConfig_ApplyDefaultIngesterUUID_Filled(t *testing.T) {
	t.Parallel()
	existing := "550e8400-e29b-41d4-a716-446655440000"
	defaultUUID := "d3667414-e373-4692-a1e2-3a18147e5aa6"
	b := BaseConfig{Ingester_UUID: existing}
	b.ApplyDefaultIngesterUUID(defaultUUID)
	if b.Ingester_UUID != existing {
		t.Errorf("Ingester_UUID = %q, want %q (should not change)", b.Ingester_UUID, existing)
	}
}

// TestBaseConfig_ApplyDefaultIngesterUUID_UUIDNil catches that uuid.Nil formatted string is preserved (it's non-empty).
func TestBaseConfig_ApplyDefaultIngesterUUID_UUIDNil(t *testing.T) {
	t.Parallel()
	existing := uuid.Nil.String() // "00000000-0000-0000-0000-000000000000"
	b := BaseConfig{Ingester_UUID: existing}
	b.ApplyDefaultIngesterUUID("d3667414-e373-4692-a1e2-3a18147e5aa6")
	// uuid.Nil.String() is the string "00000000-0000-0000-0000-000000000000" which is non-empty,
	// so cmp.Or treats it as set and preserves it.
	if b.Ingester_UUID != existing {
		t.Errorf("Ingester_UUID = %q, want %q (uuid.Nil string is non-empty so preserved)", b.Ingester_UUID, existing)
	}
}

// TestBaseConfig_ApplyDefaultIngesterUUID_Whitespace catches that whitespace-only UUID is preserved.
func TestBaseConfig_ApplyDefaultIngesterUUID_Whitespace(t *testing.T) {
	t.Parallel()
	whitespace := "   "
	b := BaseConfig{Ingester_UUID: whitespace}
	b.ApplyDefaultIngesterUUID("d3667414-e373-4692-a1e2-3a18147e5aa6")
	if b.Ingester_UUID != whitespace {
		t.Errorf("Ingester_UUID = %q, want %q (whitespace is non-empty)", b.Ingester_UUID, whitespace)
	}
}

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
