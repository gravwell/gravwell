package restpoller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
)

func TestConfigRequiresSecretFileAndRejectsSecretQuery(t *testing.T) {
	directory := t.TempDir()
	credential := filepath.Join(directory, "credential")
	if err := os.WriteFile(credential, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := Config{
		BaseConfig: hosted.BaseConfig{Ingester_UUID: "a52553fc-6c15-49ff-9592-18a44aefbc73"},
		Dataset:    []string{"users"}, Base_URL: "http://127.0.0.1:5678/api/v1",
		Credential_File: credential, Allow_Plaintext: true, Query: []string{"users:api_key=bad"},
	}
	config.SetProduct("n8n")
	if err := config.Verify(); err == nil || !strings.Contains(err.Error(), "credential material") {
		t.Fatalf("Verify error=%v", err)
	}
}

func TestConfigDerivesExactRequestedN8NTags(t *testing.T) {
	directory := t.TempDir()
	credential := filepath.Join(directory, "credential")
	if err := os.WriteFile(credential, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := Config{
		BaseConfig: hosted.BaseConfig{Ingester_UUID: "8fe72cb4-a0eb-4dbb-8c1a-af42924574b7"},
		Dataset:    []string{"users", "audit", "executions", "workflows", "credentials", "tags", "variables", "projects"},
		Base_URL:   "https://customer.example/api/v1", Credential_File: credential,
	}
	config.SetProduct("n8n")
	if err := config.Verify(); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"n8n-users": true, "n8n-audit": true, "n8n-executions": true, "n8n-workflows": true,
		"n8n-creds": true, "n8n-tags": true, "n8n-variables": true, "n8n-projects": true,
	}
	for _, tag := range config.Tags() {
		delete(want, tag)
	}
	if len(want) != 0 {
		t.Fatalf("missing tags: %v; got=%v", want, config.Tags())
	}
}

func TestConfigRejectsNegativeLookback(t *testing.T) {
	directory := t.TempDir()
	credential := filepath.Join(directory, "credential")
	if err := os.WriteFile(credential, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := Config{
		BaseConfig: hosted.BaseConfig{Ingester_UUID: "8fe72cb4-a0eb-4dbb-8c1a-af42924574b7"},
		Dataset:    []string{"users"}, Base_URL: "https://customer.example/api/v1",
		Credential_File: credential,
	}
	config.Lookback = -1
	config.SetProduct("n8n")
	if err := config.Verify(); err == nil || !strings.Contains(err.Error(), "Lookback") {
		t.Fatalf("Verify error=%v", err)
	}
}

func TestSuiteQLRequiresStableReadOnlyQuery(t *testing.T) {
	directory := t.TempDir()
	credential := filepath.Join(directory, "credential")
	query := filepath.Join(directory, "query.sql")
	if err := os.WriteFile(credential, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(query, []byte("SELECT id FROM employee"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := Config{
		BaseConfig: hosted.BaseConfig{Ingester_UUID: "1dcd0e18-c3ad-464b-b753-02d9dcd40dd9"},
		Dataset:    []string{"suiteql"}, Base_URL: "https://12345.suitetalk.api.netsuite.com",
		Credential_File: credential, SuiteQL_Query_File: query,
	}
	config.SetProduct("netsuite")
	if err := config.Verify(); err == nil || !strings.Contains(err.Error(), "ORDER BY") {
		t.Fatalf("Verify error=%v", err)
	}
}
