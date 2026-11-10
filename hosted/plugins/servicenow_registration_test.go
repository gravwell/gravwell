package plugins

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/plugins/servicenow"
	"github.com/gravwell/gravwell/v3/hosted/plugins/tester"
)

func serviceNowRegistrationConfig(t *testing.T) *servicenow.Config {
	t.Helper()
	secret := filepath.Join(t.TempDir(), "servicenow.json")
	if err := os.WriteFile(secret, []byte(`{"mode":"bearer","bearer_token":"synthetic-test-token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return &servicenow.Config{
		BaseConfig:    hosted.BaseConfig{Ingester_UUID: "42000000-0000-4000-8000-000000000001"},
		Instance:      "https://example.service-now.com",
		Secret_File:   secret,
		Product:       []string{"itsm"},
		Normalization: "enabled",
	}
}

func TestServiceNowRegistrationPreservesExistingPlugin(t *testing.T) {
	existing := &tester.Config{}
	existing.Tag_Name = "existing-customer-tag"
	c := Configs{Tester: map[string]*tester.Config{"existing": existing}}
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	}
	before, err := c.Tags()
	if err != nil {
		t.Fatal(err)
	}
	if c.IngesterCount() != 1 {
		t.Fatal("existing count changed")
	}
	cfg := serviceNowRegistrationConfig(t)
	c.ServiceNow = map[string]*servicenow.Config{"servicenow": cfg}
	if err = c.Verify(); err != nil {
		t.Fatal(err)
	}
	after, err := c.Tags()
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range before {
		if !slices.Contains(after, tag) {
			t.Fatalf("lost existing tag %q", tag)
		}
	}
	for _, tag := range cfg.Tags() {
		if !slices.Contains(after, tag) {
			t.Fatalf("missing ServiceNow tag %q", tag)
		}
	}
	if c.IngesterCount() != 2 {
		t.Fatal("new plugin was not counted")
	}
	found := map[string]bool{}
	for name, b := range c.Builders() {
		found[name] = true
		if name == "servicenow" && b.Config() != cfg {
			t.Fatal("builder config changed")
		}
	}
	if !found["existing"] || !found["servicenow"] {
		t.Fatalf("builders incomplete: %v", found)
	}
}

func TestServiceNowRegistrationRejectsNil(t *testing.T) {
	c := Configs{ServiceNow: map[string]*servicenow.Config{"nil": nil}}
	if c.Verify() == nil {
		t.Fatal("nil config accepted")
	}
}
