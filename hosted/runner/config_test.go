package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetConfigRetainsFileBackedIngestSecret(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "ingest.secret")
	const secret = "fixture-only-ingest-secret"
	if err := os.WriteFile(secretPath, []byte(secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := GetConfig(writeRunnerConfig(t, dir,
		`Ingest-Secret-File="`+filepath.ToSlash(secretPath)+`"`), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Secret(); got != secret {
		t.Fatalf("file-backed ingest secret was not retained: got length %d", len(got))
	}
}

func TestGetConfigRejectsInvalidIngestSecretFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		path func(string) string
	}{
		{name: "missing", path: func(dir string) string { return filepath.Join(dir, "missing.secret") }},
		{name: "not-regular", path: func(dir string) string { return dir }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.ToSlash(tc.path(dir))
			_, err := GetConfig(writeRunnerConfig(t, dir, `Ingest-Secret-File="`+path+`"`), "")
			if err == nil || !strings.Contains(err.Error(), "Failed to load Ingest-Secret") {
				t.Fatalf("invalid ingest-secret file was accepted: %v", err)
			}
		})
	}
}

func TestGetConfigPreservesDirectIngestSecretPrecedence(t *testing.T) {
	dir := t.TempDir()
	cfg, err := GetConfig(writeRunnerConfig(t, dir, strings.Join([]string{
		`Ingest-Secret="direct-fixture-secret"`,
		`Ingest-Secret-File="` + filepath.ToSlash(filepath.Join(dir, "missing.secret")) + `"`,
	}, "\n")), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Secret(); got != "direct-fixture-secret" {
		t.Fatal("direct ingest secret did not retain precedence")
	}
}

func writeRunnerConfig(t *testing.T, dir, secretSetting string) string {
	t.Helper()
	path := filepath.Join(dir, "hosted_runner.conf")
	body := `[Global]
Label="Hosted Runner Config Test"
Ingester-UUID="00000000-0000-0000-0000-000000000001"
` + secretSetting + `
Cleartext-Backend-Target="127.0.0.1:4023"
Ingest-Cache-Path="` + filepath.ToSlash(filepath.Join(dir, "ingest.cache")) + `"
Max-Ingest-Cache=1
Cache-Mode="always"
Log-Level="INFO"

[State]
Path="` + filepath.ToSlash(filepath.Join(dir, "runner.state")) + `"
Sync=true
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
