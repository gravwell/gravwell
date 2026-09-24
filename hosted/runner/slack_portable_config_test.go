package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the production entry point, not just the duration helper.
func TestPortableSlackDefaultConfig(t *testing.T) {
	b, err := os.ReadFile("testdata/slack-laptop.conf")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		raw   string
		hours int
	}{{"7d", 168}, {"24h", 24}, {"1d12h", 36}} {
		t.Run(tc.raw, func(t *testing.T) {
			dir := t.TempDir()
			const fixtureSecret = "fixture-only-not-a-real-credential"
			for _, name := range []string{"slack-token", "ingest.secret"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(fixtureSecret), 0600); err != nil {
					t.Fatal(err)
				}
			}
			body := strings.ReplaceAll(string(b), "/opt/gravwell/secrets/", filepath.ToSlash(dir)+"/")
			body = strings.ReplaceAll(body, "/opt/gravwell/state/slack.state", filepath.ToSlash(filepath.Join(dir, "slack.state")))
			body = strings.ReplaceAll(body, "/opt/gravwell/cache/slack.cache", filepath.ToSlash(filepath.Join(dir, "slack.cache")))
			body = strings.ReplaceAll(body, "Lookback=7d", "Lookback="+tc.raw)
			path := filepath.Join(dir, "hosted_runner.conf")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := GetConfig(path, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Slack) != 1 || cfg.Slack["primary"].Lookback != tc.hours {
				t.Fatal("packaged Slack lookback or stanza mismatch")
			}
			if cfg.Slack["primary"].Tag_Name != "slack-audit" {
				t.Fatal("unexpected tag")
			}
			if cfg.Secret() != fixtureSecret {
				t.Fatal("Ingest-Secret-File value was discarded after validation")
			}
		})
	}
}
