//go:build upstream_registration

package microsoft_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/plugins"
	"github.com/gravwell/gravwell/v3/hosted/plugins/microsoft"
	"github.com/gravwell/gravwell/v3/hosted/plugins/tester"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
)

type publishedRunnerConfig struct {
	Global config.IngestConfig
	Attach attach.AttachConfig
	State  storage.BoltConfig
	plugins.Configs
}

func withRegistrationSecret(t *testing.T, text string) string {
	t.Helper()
	path := "synthetic-client-secret"
	text = strings.ReplaceAll(text, "/opt/gravwell/secrets/microsoft-client.secret", path)
	return strings.ReplaceAll(text, "SYNTHETIC_CLIENT_SECRET_FILE", path)
}

func TestPublishedExamplesParseThroughRegisteredRunnerConfig(t *testing.T) {
	for _, name := range []string{"example.conf", "microsoft365.conf.example", "microsoft_every_api.conf.example"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			text := string(body)
			text = withRegistrationSecret(t, text)
			var cfg publishedRunnerConfig
			if err := config.LoadConfigBytes(&cfg, []byte(text)); err != nil {
				t.Fatal(err)
			}
			if len(cfg.Microsoft) != 1 {
				t.Fatalf("Microsoft stanzas=%d, want 1", len(cfg.Microsoft))
			}
			for _, stanza := range cfg.Microsoft {
				if err := stanza.Verify(); err != nil {
					t.Fatal(err)
				}
				if stanza.Ingester_UUID != "2b823d47-3626-4451-ba18-92916bb401fc" {
					t.Fatalf("default UUID=%q", stanza.Ingester_UUID)
				}
			}
		})
	}
}

func TestRegisteredMicrosoftConfigAndBuilderPreserveExisting(t *testing.T) {
	old := &tester.Config{}
	old.Tag_Name = "existing-customer-tag"
	cfg := plugins.Configs{Tester: map[string]*tester.Config{"existing": old}}
	text := `[Microsoft "cloud"]
Ingester-UUID="550e8400-e29b-41d4-a716-446655440000"
Tenant-ID="11111111-1111-1111-1111-111111111111"
Client-ID="22222222-2222-2222-2222-222222222222"
Client-Secret="SYNTHETIC_CLIENT_SECRET_FILE"
Api="entra-signins"
Lookback=36
`
	text = withRegistrationSecret(t, text)
	if err := config.LoadConfigBytes(&cfg, []byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Verify(); err != nil {
		t.Fatal(err)
	}
	if cfg.IngesterCount() != 2 || cfg.Microsoft["cloud"].Lookback != 36 {
		t.Fatal("registered config lost a stanza or duration")
	}
	tags, err := cfg.Tags()
	if err != nil || !slices.Contains(tags, "existing-customer-tag") || !slices.Contains(tags, "ms-entra-signin") {
		t.Fatalf("tags=%v error=%v", tags, err)
	}
	seen := make(map[string]bool)
	for name, builder := range cfg.Builders() {
		seen[name] = true
		if name == "cloud" {
			if builder.UUID().String() != cfg.Microsoft[name].Ingester_UUID {
				t.Fatal("builder UUID changed")
			}
			// Construction validates the production muxer's standard Runtime
			// implementation without contacting a backend.
			if _, err := builder.Build(&ingest.IngestMuxer{}, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !seen["cloud"] || !seen["existing"] {
		t.Fatal("registered builder missing")
	}
	cfg.Microsoft = map[string]*microsoft.Config{"invalid": nil}
	if cfg.Verify() == nil {
		t.Fatal("nil Microsoft stanza accepted")
	}
}

func TestRegisteredMicrosoftConfigRejectsSecondDefaultUUID(t *testing.T) {
	text := `[Microsoft "first"]
Tenant-ID="11111111-1111-1111-1111-111111111111"
Client-ID="22222222-2222-2222-2222-222222222222"
Client-Secret="SYNTHETIC_CLIENT_SECRET_FILE"
Api="entra-users"

[Microsoft "second"]
Tenant-ID="11111111-1111-1111-1111-111111111111"
Client-ID="22222222-2222-2222-2222-222222222222"
Client-Secret="SYNTHETIC_CLIENT_SECRET_FILE"
Api="entra-groups"
`
	text = withRegistrationSecret(t, text)
	var cfg plugins.Configs
	if err := config.LoadConfigBytes(&cfg, []byte(text)); err != nil {
		t.Fatal(err)
	}
	err := cfg.Verify()
	if err == nil || !strings.Contains(err.Error(), "share Ingester-UUID") {
		t.Fatalf("Verify() error=%v, want duplicate default UUID rejection", err)
	}
}

func TestRegisteredMicrosoftConfigAcceptsExplicitUniqueAdditionalUUID(t *testing.T) {
	text := `[Microsoft "first"]
Tenant-ID="11111111-1111-1111-1111-111111111111"
Client-ID="22222222-2222-2222-2222-222222222222"
Client-Secret="SYNTHETIC_CLIENT_SECRET_FILE"
Api="entra-users"

[Microsoft "second"]
Ingester-UUID="660e8400-e29b-41d4-a716-446655440000"
Tenant-ID="11111111-1111-1111-1111-111111111111"
Client-ID="22222222-2222-2222-2222-222222222222"
Client-Secret="SYNTHETIC_CLIENT_SECRET_FILE"
Api="entra-groups"
`
	text = withRegistrationSecret(t, text)
	var cfg plugins.Configs
	if err := config.LoadConfigBytes(&cfg, []byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Verify(); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, builder := range cfg.Builders() {
		seen[builder.UUID().String()] = true
	}
	if len(seen) != 2 {
		t.Fatalf("effective UUIDs=%v, want two unique identities", seen)
	}
}

func TestMicrosoftUUIDValidationDoesNotChangeOtherPluginPolicy(t *testing.T) {
	const shared = "550e8400-e29b-41d4-a716-446655440000"
	microsoftConfig := &microsoft.Config{
		BaseConfig: hosted.BaseConfig{Ingester_UUID: shared},
		Tenant_ID:  "11111111-1111-1111-1111-111111111111", Client_ID: "22222222-2222-2222-2222-222222222222",
		Client_Secret: "synthetic-client-secret", Api: []string{"entra-users"},
	}
	existing := &tester.Config{BaseConfig: hosted.BaseConfig{Ingester_UUID: shared}}
	configs := plugins.Configs{
		Microsoft: map[string]*microsoft.Config{"cloud": microsoftConfig},
		Tester:    map[string]*tester.Config{"existing": existing},
	}
	if err := configs.Verify(); err != nil {
		t.Fatalf("Microsoft-local UUID validation changed another plugin: %v", err)
	}
}

func TestRegisteredMicrosoftConfigAcceptsStandardClientSecret(t *testing.T) {
	text := `[Microsoft "standard-secret"]
Tenant-ID="11111111-1111-1111-1111-111111111111"
Client-ID="22222222-2222-2222-2222-222222222222"
Client-Secret="synthetic-client-secret"
Api="entra-users"
`
	var cfg plugins.Configs
	if err := config.LoadConfigBytes(&cfg, []byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Verify(); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Microsoft["standard-secret"].Client_Secret; got != "synthetic-client-secret" {
		t.Fatalf("Client-Secret=%q", got)
	}
}
