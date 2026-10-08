package microsoft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
)

func validConfig(t *testing.T) *Config {
	t.Helper()
	secret := filepath.Join(t.TempDir(), "client.secret")
	if err := os.WriteFile(secret, []byte("synthetic-test-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Config{
		BaseConfig:         hosted.BaseConfig{Ingester_UUID: "550e8400-e29b-41d4-a716-446655440000"},
		Tenant_ID:          "11111111-1111-1111-1111-111111111111",
		Client_ID:          "22222222-2222-2222-2222-222222222222",
		Client_Secret_File: secret,
		Subscription_ID:    []string{"33333333-3333-3333-3333-333333333333"},
		Api:                []string{"azure-activity"},
	}
}

func TestConfigVerifyDefaultsAndSecretFile(t *testing.T) {
	conf := validConfig(t)
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	if conf.Graph_Host != defaultGraphHost || conf.Request_Interval != defaultIntervalSeconds {
		t.Fatalf("defaults not applied: %#v", conf)
	}
}

func TestConfigEqualCoversEveryBehaviorField(t *testing.T) {
	base := validConfig(t)
	base.MultiTagConfig = hosted.MultiTagConfig{Tag_Name: "microsoft", Tag_Prefix: "prefix"}
	base.PollingConfig = hosted.PollingConfig{Lookback: 24, Requests_Per_Minute: 10, Request_Interval: 3600}
	base.Lookback = 24
	base.Graph_Host = defaultGraphHost
	base.ARM_Host = defaultARMHost
	base.Defender_Host = defaultDefenderHost
	base.PowerBI_Host = defaultPowerBIHost
	base.Auth_Host = defaultAuthHost
	base.Page_Size = 10
	base.Max_Pages = 2
	base.Overlap = 900
	base.Max_Retries = 3
	base.Ingest_Unchanged = true
	base.Normalization = "enabled"
	base.Tag_Schema = TagSchemaConsolidated

	clone := func() *Config {
		copy := *base
		copy.Subscription_ID = append([]string(nil), base.Subscription_ID...)
		copy.Api = append([]string(nil), base.Api...)
		return &copy
	}
	if !base.Equal(clone()) || !base.Equal(*clone()) {
		t.Fatal("identical pointer and value configurations must compare equal")
	}
	if base.Equal((*Config)(nil)) || base.Equal(struct{}{}) || (*Config)(nil).Equal(base) {
		t.Fatal("nil or different configuration types must not compare equal")
	}

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"Ingester_UUID", func(c *Config) { c.Ingester_UUID += "-changed" }},
		{"Tag_Name", func(c *Config) { c.Tag_Name += "-changed" }},
		{"Tag_Prefix", func(c *Config) { c.Tag_Prefix += "-changed" }},
		{"Polling_Lookback", func(c *Config) { c.PollingConfig.Lookback++ }},
		{"Requests_Per_Minute", func(c *Config) { c.Requests_Per_Minute++ }},
		{"Request_Interval", func(c *Config) { c.Request_Interval++ }},
		{"Lookback", func(c *Config) { c.Lookback++ }},
		{"Tenant_ID", func(c *Config) { c.Tenant_ID += "-changed" }},
		{"Client_ID", func(c *Config) { c.Client_ID += "-changed" }},
		{"Client_Secret_File", func(c *Config) { c.Client_Secret_File += ".changed" }},
		{"Subscription_ID", func(c *Config) { c.Subscription_ID[0] += "-changed" }},
		{"Api", func(c *Config) { c.Api[0] = "entra-users" }},
		{"Graph_Host", func(c *Config) { c.Graph_Host += "/changed" }},
		{"ARM_Host", func(c *Config) { c.ARM_Host += "/changed" }},
		{"Defender_Host", func(c *Config) { c.Defender_Host += "/changed" }},
		{"PowerBI_Host", func(c *Config) { c.PowerBI_Host += "/changed" }},
		{"Auth_Host", func(c *Config) { c.Auth_Host += "/changed" }},
		{"Page_Size", func(c *Config) { c.Page_Size++ }},
		{"Max_Pages", func(c *Config) { c.Max_Pages++ }},
		{"Overlap", func(c *Config) { c.Overlap++ }},
		{"Max_Retries", func(c *Config) { c.Max_Retries++ }},
		{"Ingest_Unchanged", func(c *Config) { c.Ingest_Unchanged = !c.Ingest_Unchanged }},
		{"Normalization", func(c *Config) { c.Normalization = "disabled" }},
		{"Tag_Schema", func(c *Config) { c.Tag_Schema = TagSchemaLegacy }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changed := clone()
			tc.mutate(changed)
			if base.Equal(changed) {
				t.Fatalf("configuration remained equal after %s changed", tc.name)
			}
		})
	}
}

func TestConfigConsolidatedTagNameRequiresOneSemanticFamily(t *testing.T) {
	conf := validConfig(t)
	conf.Api = []string{"entra-users", "entra-groups"}
	conf.Tag_Schema = TagSchemaConsolidated
	conf.Tag_Name = "entra-directory"
	if err := conf.Verify(); err != nil {
		t.Fatalf("same-family Tag-Name rejected: %v", err)
	}
	conf.Api = append(conf.Api, "entra-signins")
	if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), "one consolidated tag") {
		t.Fatalf("cross-family Tag-Name error=%v", err)
	}
}

func TestConfigRejectsZeroUUID(t *testing.T) {
	conf := validConfig(t)
	conf.Ingester_UUID = "00000000-0000-0000-0000-000000000000"
	if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), "non-zero") {
		t.Fatalf("error=%v", err)
	}
}

func TestConfigRejectsAllZeroVendorIdentifiers(t *testing.T) {
	for _, field := range []string{"Tenant-ID", "Client-ID", "Subscription-ID"} {
		conf := validConfig(t)
		switch field {
		case "Tenant-ID":
			conf.Tenant_ID = "00000000-0000-0000-0000-000000000000"
		case "Client-ID":
			conf.Client_ID = "00000000-0000-0000-0000-000000000000"
		case "Subscription-ID":
			conf.Subscription_ID = []string{"00000000-0000-0000-0000-000000000000"}
		}
		if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), field) {
			t.Fatalf("%s error=%v", field, err)
		}
	}
}

func TestConfigRequiresSubscriptionOnlyForAzureScope(t *testing.T) {
	conf := validConfig(t)
	conf.Subscription_ID = nil
	if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), "Subscription-ID") {
		t.Fatalf("error=%v", err)
	}
	conf.Api = []string{"entra-directory-audits"}
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigRejectsNonHTTPSBase(t *testing.T) {
	conf := validConfig(t)
	conf.Graph_Host = "http://graph.example.invalid"
	if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), "Graph-Host") {
		t.Fatalf("error=%v", err)
	}
}

func TestConfigNormalizationAliasesTagsAndState(t *testing.T) {
	for _, value := range []string{"enabled", "true", "ENABLED", "TRUE"} {
		conf := validConfig(t)
		conf.Normalization = value
		if err := conf.Verify(); err != nil {
			t.Fatalf("Normalization=%q: %v", value, err)
		}
		if got := conf.Tag("azure-activity"); got != "azure-activity-normalized" {
			t.Fatalf("Normalization=%q tag=%q", value, got)
		}
		if conf.StateNamespace() != "microsoft-normalized-v1" {
			t.Fatalf("Normalization=%q state=%q", value, conf.StateNamespace())
		}
	}
	for _, value := range []string{"", "disabled", "false", "DISABLED", "FALSE"} {
		conf := validConfig(t)
		conf.Normalization = value
		if err := conf.Verify(); err != nil {
			t.Fatalf("Normalization=%q: %v", value, err)
		}
		if got := conf.Tag("azure-activity"); got != "azure-activity" {
			t.Fatalf("Normalization=%q tag=%q", value, got)
		}
		if conf.StateNamespace() != "microsoft" {
			t.Fatalf("Normalization=%q state=%q", value, conf.StateNamespace())
		}
	}
	conf := validConfig(t)
	conf.Normalization = "sometimes"
	if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), "Normalization") {
		t.Fatalf("invalid normalization error=%v", err)
	}
}

func TestConfigRejectsUserInfoAndOversizedSecretFile(t *testing.T) {
	conf := validConfig(t)
	conf.Auth_Host = "https://synthetic:synthetic@login.microsoftonline.com"
	if err := conf.Verify(); err == nil {
		t.Error("credential-bearing URL accepted")
	}
	conf = validConfig(t)
	if err := os.WriteFile(conf.Client_Secret_File, []byte(strings.Repeat("x", (1<<20)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := conf.Verify(); err == nil {
		t.Fatal("oversized secret file accepted")
	}
}
