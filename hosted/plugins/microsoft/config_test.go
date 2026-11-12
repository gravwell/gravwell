package microsoft

import (
	"slices"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
)

func TestConfigUsesStandardPollingDefaults(t *testing.T) {
	c := &Config{Tenant_ID: "11111111-1111-1111-1111-111111111111", Client_ID: "22222222-2222-2222-2222-222222222222", Client_Secret: "synthetic-client-secret", Api: []string{"entra-users"}}
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	}
	if c.Lookback != defaultLookback || c.Requests_Per_Minute != defaultRequestsPerMinute || c.Request_Interval != defaultIntervalSeconds {
		t.Fatalf("polling=%+v", c.PollingConfig)
	}
	if c.Ingester_UUID != defaultIngesterUUIDStr {
		t.Fatalf("uuid=%q", c.Ingester_UUID)
	}
}

func TestConfigConsolidatedTagNameRequiresOneSemanticFamily(t *testing.T) {
	conf := &Config{
		Tenant_ID: "11111111-1111-1111-1111-111111111111", Client_ID: "22222222-2222-2222-2222-222222222222",
		Client_Secret: "synthetic-client-secret", Api: []string{"entra-users", "entra-groups"},
		Tag_Schema: TagSchemaConsolidated, MultiTagConfig: hosted.MultiTagConfig{Tag_Name: "entra-directory"},
	}
	if err := conf.Verify(); err != nil {
		t.Fatalf("same-family Tag-Name rejected: %v", err)
	}
	conf.Api = append(conf.Api, "entra-signins")
	if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), "one consolidated tag") {
		t.Fatalf("cross-family Tag-Name error=%v", err)
	}
}

func TestClientSecretValidationAndManagementLookback(t *testing.T) {
	base := func() *Config {
		return &Config{
			Tenant_ID: "11111111-1111-1111-1111-111111111111", Client_ID: "22222222-2222-2222-2222-222222222222",
			Client_Secret: "synthetic-client-secret", Publisher_Identifier: managementTestPublisher, Api: []string{"microsoft365-audit-exchange"},
		}
	}
	if err := base().Verify(); err != nil {
		t.Fatal(err)
	}
	empty := base()
	empty.Client_Secret = "  "
	if err := empty.Verify(); err == nil || !strings.Contains(err.Error(), "Client-Secret") {
		t.Fatalf("empty secret error=%v", err)
	}
	tooOld := base()
	tooOld.Lookback = 169
	if err := tooOld.Verify(); err == nil || !strings.Contains(err.Error(), "168") {
		t.Fatalf("lookback error=%v", err)
	}
	badHost := base()
	badHost.Management_Host = "https://manage.office.com/proxy"
	if err := badHost.Verify(); err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("Management-Host error=%v", err)
	}
	missingPublisher := base()
	missingPublisher.Publisher_Identifier = ""
	if err := missingPublisher.Verify(); err == nil || !strings.Contains(err.Error(), "Publisher-Identifier") {
		t.Fatalf("missing publisher error=%v", err)
	}
	invalidPublisher := base()
	invalidPublisher.Publisher_Identifier = "not-a-uuid"
	if err := invalidPublisher.Verify(); err == nil || !strings.Contains(err.Error(), "Publisher-Identifier") {
		t.Fatalf("invalid publisher error=%v", err)
	}
}

func TestManagementScopeFollowsCloudAndSupportsExplicitProxyResource(t *testing.T) {
	base := func() *Config {
		return &Config{
			Tenant_ID: "11111111-1111-1111-1111-111111111111", Client_ID: "22222222-2222-2222-2222-222222222222",
			Client_Secret: "synthetic-client-secret", Publisher_Identifier: managementTestPublisher,
			Api: []string{"microsoft365-audit-exchange"}, Management_Host: "https://manage-gcc.office.com",
		}
	}
	cloud := base()
	if err := cloud.Verify(); err != nil {
		t.Fatal(err)
	}
	if cloud.Management_Scope != "https://manage-gcc.office.com/.default" {
		t.Fatalf("derived scope=%q", cloud.Management_Scope)
	}
	proxy := base()
	proxy.Management_Host = "https://proxy.example.test"
	proxy.Management_Scope = "https://manage.office.com/.default"
	if err := proxy.Verify(); err != nil {
		t.Fatal(err)
	}
	invalid := base()
	invalid.Management_Scope = "https://manage.office.com/not-default"
	if err := invalid.Verify(); err == nil || !strings.Contains(err.Error(), "Management-Scope") {
		t.Fatalf("invalid scope error=%v", err)
	}
}

func TestEveryMicrosoftSelectorUsesStandardPollingBounds(t *testing.T) {
	for name := range datasets {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				lookback      int
				interval, rpm int
				valid         bool
			}{
				{1, 60, 1, true}, {24, 3600, 10, true}, {168, 3600, 10, true},
				{672, 9223372036, 10, true}, {673, 60, 1, false}, {0, 60, 1, true},
				{24, 59, 10, false}, {24, 9223372037, 10, false}, {24, 3600, 6001, false},
			} {
				cfg := &Config{
					Tenant_ID: "11111111-1111-1111-1111-111111111111", Client_ID: "22222222-2222-2222-2222-222222222222",
					Client_Secret: "synthetic-client-secret", Subscription_ID: []string{"33333333-3333-4333-8333-333333333333"}, Api: []string{name},
					Publisher_Identifier: managementTestPublisher,
					PollingConfig:        hosted.PollingConfig{Lookback: tc.lookback, Request_Interval: tc.interval, Requests_Per_Minute: tc.rpm},
				}
				err := cfg.Verify()
				valid := tc.valid
				if datasets[name].Kind == KindManagementActivity && tc.lookback > 168 {
					valid = false
				}
				if (err == nil) != valid {
					t.Errorf("Lookback=%d interval=%d rpm=%d error=%v", tc.lookback, tc.interval, tc.rpm, err)
				}
			}
		})
	}
	if got := len(datasets); got != 105 {
		t.Fatalf("selectors=%d, want 105", got)
	}
}

func TestConfigEqualCoversPublicFields(t *testing.T) {
	base := &Config{BaseConfig: hosted.BaseConfig{Ingester_UUID: defaultIngesterUUIDStr}, PollingConfig: hosted.PollingConfig{Lookback: 24, Requests_Per_Minute: 10, Request_Interval: 3600}, Tenant_ID: "tenant", Client_ID: "client", Client_Secret: "synthetic-client-secret", Subscription_ID: []string{"sub"}, Api: []string{"entra-users"}, Graph_Host: defaultGraphHost, ARM_Host: defaultARMHost, Defender_Host: defaultDefenderHost, PowerBI_Host: defaultPowerBIHost, Management_Host: defaultManagementHost, Auth_Host: defaultAuthHost}
	clone := *base
	clone.Subscription_ID, clone.Api = slices.Clone(base.Subscription_ID), slices.Clone(base.Api)
	if !base.Equal(&clone) || !base.Equal(clone) {
		t.Fatal("equal config rejected")
	}
	clone = *base
	clone.Publisher_Identifier = managementTestPublisher
	if base.Equal(&clone) {
		t.Fatal("Publisher-Identifier was omitted from Config.Equal")
	}
	clone = *base
	clone.Management_Scope = "https://manage-gcc.office.com/.default"
	if base.Equal(&clone) {
		t.Fatal("Management-Scope was omitted from Config.Equal")
	}
	clone = *base
	clone.Defender_XDR_Host = "https://api.security.microsoft.us"
	if base.Equal(&clone) {
		t.Fatal("Defender-XDR-Host was omitted from Config.Equal")
	}
	clone.Lookback++
	if base.Equal(&clone) {
		t.Fatal("polling change ignored")
	}
	clone = *base
	clone.Api = []string{"entra-groups"}
	if base.Equal(&clone) {
		t.Fatal("API change ignored")
	}
}

func TestConfigValidationAndTags(t *testing.T) {
	c := validTestConfig(t, "entra-users", "entra-groups")
	if got := c.Tags(); !slices.Equal(got, []string{"ms-entra-directory"}) {
		t.Fatalf("tags=%v", got)
	}
	c.Tag_Prefix = "customer"
	if got := c.Tags(); !slices.Equal(got, []string{"customer-ms-entra-directory"}) {
		t.Fatalf("prefixed tags=%v", got)
	}
	c.Tag_Name = "one"
	if err := c.Verify(); err == nil || !strings.Contains(err.Error(), "Tag-Name") {
		t.Fatalf("error=%v", err)
	}
}
