package slack

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
)

func TestConfigDefaultsAndTags(t *testing.T) {
	config := &Config{API_Token: "synthetic-token"}
	if err := config.Verify(); err != nil {
		t.Fatal(err)
	}
	if config.Host != defaultHost || config.Lookback != defaultLookback ||
		config.Requests_Per_Minute != defaultRequestsPerMinute || config.Request_Interval != defaultRequestInterval {
		t.Fatalf("defaults not applied: %+v", config)
	}
	if config.Ingester_UUID != defaultIngesterUUIDStr {
		t.Fatalf("UUID=%q", config.Ingester_UUID)
	}
	if got := config.Tags(); len(got) != 1 || got[0] != "slack-audit" {
		t.Fatalf("tags=%v", got)
	}

	config.Tag_Name = "custom"
	if got := config.Tags(); len(got) != 1 || got[0] != "custom" {
		t.Fatalf("custom tags=%v", got)
	}

	explicit := validConfig("https://api.slack.com")
	wantUUID := explicit.Ingester_UUID
	if err := explicit.Verify(); err != nil {
		t.Fatal(err)
	}
	if explicit.Ingester_UUID != wantUUID {
		t.Fatalf("explicit UUID changed from %q to %q", wantUUID, explicit.Ingester_UUID)
	}
}

func TestConfigRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"missing token", func(c *Config) { c.API_Token = " " }, "API-Token"},
		{"negative lookback", func(c *Config) { c.Lookback = -1 }, "Lookback"},
		{"zero rpm after explicit negative", func(c *Config) { c.Requests_Per_Minute = -1 }, "Requests-Per-Minute"},
		{"high rpm", func(c *Config) { c.Requests_Per_Minute = maximumRequestsPerMinute + 1 }, "Requests-Per-Minute"},
		{"short interval", func(c *Config) { c.Request_Interval = minimumRequestInterval - 1 }, "Request-Interval"},
		{"tag conflict", func(c *Config) { c.Tag_Name, c.Tag_Prefix = "one", "two" }, "cannot"},
		{"relative host", func(c *Config) { c.Host = "/api" }, "absolute"},
		{"host path", func(c *Config) { c.Host = "https://api.slack.com/path" }, "path"},
		{"host userinfo", func(c *Config) { c.Host = "https://user@example.com" }, "userinfo"},
		{"remote plaintext", func(c *Config) { c.Host = "http://example.com" }, "HTTPS"},
		{"invalid uuid", func(c *Config) { c.Ingester_UUID = "invalid" }, "Ingester-UUID"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := validConfig("https://api.slack.com")
			tc.mutate(config)
			if err := config.Verify(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Verify error=%v want substring %q", err, tc.want)
			}
		})
	}
}

func TestConfigDurationOverflowBounds(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("overflow case requires a 64-bit int")
	}
	config := validConfig("https://api.slack.com")
	config.Lookback = int(math.MaxInt64/int64(time.Hour)) + 1
	if err := config.Verify(); err == nil || !strings.Contains(err.Error(), "Lookback") {
		t.Fatalf("Lookback error=%v", err)
	}
	config = validConfig("https://api.slack.com")
	config.Request_Interval = math.MaxInt
	if err := config.Verify(); err == nil || !strings.Contains(err.Error(), "Request-Interval") {
		t.Fatalf("Request-Interval error=%v", err)
	}
}

func TestConfigEqualityAndSanitization(t *testing.T) {
	config := validConfig("https://api.slack.com")
	config.MultiTagConfig = hosted.MultiTagConfig{Tag_Name: "slack-audit"}
	if err := config.Verify(); err != nil {
		t.Fatal(err)
	}
	copy := *config
	if !config.Equal(copy) || !config.Equal(&copy) {
		t.Fatal("equivalent configs compare unequal")
	}
	copy.API_Token = "other"
	if config.Equal(copy) {
		t.Fatal("changed secret compares equal")
	}
	if config.Equal(nil) || config.Equal(struct{}{}) {
		t.Fatal("invalid equality target compares equal")
	}

	body, err := json.Marshal(config.SanitizedConfig())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), config.API_Token) || !strings.Contains(string(body), "api.slack.com") {
		t.Fatalf("bad sanitized config: %s", body)
	}
}

func TestConfigAllowsOnlyLoopbackPlaintext(t *testing.T) {
	for _, host := range []string{"http://127.0.0.1:8080", "http://[::1]:8080", "http://localhost:8080"} {
		config := validConfig(host)
		if err := config.Verify(); err != nil {
			t.Fatalf("%s: %v", host, err)
		}
	}
}

func TestTokenFileIsBoundedProtectedAndWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slack.token")
	if err := os.WriteFile(path, []byte("file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := validConfig("https://api.slack.com")
	config.API_Token = "direct-token"
	config.API_Token_File = path
	if err := config.Verify(); err != nil {
		t.Fatal(err)
	}
	if got, err := config.credential(); err != nil || got != "file-token" {
		t.Fatalf("credential=%q error=%v", got, err)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.credential(); err == nil || !strings.Contains(err.Error(), "group or other") {
		t.Fatalf("permissive token error=%v", err)
	}

	if err := os.WriteFile(path, []byte(strings.Repeat("x", maximumTokenFileSize+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.credential(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized token error=%v", err)
	}

	if err := os.WriteFile(path, []byte(strings.Repeat("x", maximumTokenFileSize)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := config.credential(); err != nil || len(got) != maximumTokenFileSize {
		t.Fatalf("boundary credential length=%d error=%v", len(got), err)
	}
}

func TestTokenRejectsStructuredOrNonRegularInput(t *testing.T) {
	for _, token := range []string{"", " \t", "one\ntwo", "\none", "one\n\n", "one\x00two"} {
		if _, err := validateToken(token, "API-Token"); err == nil {
			t.Fatalf("token %q accepted", token)
		}
	}
	if _, err := readTokenFile(t.TempDir()); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory token error=%v", err)
	}
	target := filepath.Join(t.TempDir(), "target.token")
	if err := os.WriteFile(target, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(t.TempDir(), "symlink.token")
	if err := os.Symlink(target, symlink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := readTokenFile(symlink); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("symlink token error=%v", err)
	}
}
