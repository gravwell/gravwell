package microsoft

import (
	"net/url"
	"sort"
	"strings"
	"testing"
)

// scopeOriginExceptions records the services whose Entra ID resource URI is
// deliberately not the same origin as the API host. Power BI/Fabric is the only
// one: the resource is analysis.windows.net/powerbi/api while requests go to
// api.powerbi.com. Every other service must keep audience and origin paired.
var scopeOriginExceptions = map[Service]string{
	ServicePowerBI: "https://analysis.windows.net",
}

func defaultHostConfig(t *testing.T) *Config {
	t.Helper()
	conf := &Config{
		Tenant_ID:     "11111111-1111-1111-1111-111111111111",
		Client_ID:     "22222222-2222-2222-2222-222222222222",
		Client_Secret: "synthetic-client-secret",
		Api:           []string{"entra-users"},
	}
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	return conf
}

func scopeOrigin(t *testing.T, scope string) string {
	t.Helper()
	parsed, err := url.Parse(scope)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatalf("scope %q is not an absolute URL: %v", scope, err)
	}
	return parsed.Scheme + "://" + parsed.Host
}

// TestEveryServiceKeepsOneTokenAudiencePairedWithItsHost is the assertion that
// would have caught a securitycenter-audience token being sent to
// api.security.microsoft.com. A bearer token's aud claim belongs to the
// resource it was minted for, so a service whose datasets disagree about Scope,
// or whose Scope origin is not the configured Base, cannot authenticate.
func TestEveryServiceKeepsOneTokenAudiencePairedWithItsHost(t *testing.T) {
	conf := defaultHostConfig(t)
	scopes := make(map[Service]map[string][]string)
	for _, dataset := range datasets {
		if dataset.Scope == "" {
			t.Fatalf("selector %q has no OAuth scope", dataset.Name)
		}
		if scopes[dataset.Service] == nil {
			scopes[dataset.Service] = make(map[string][]string)
		}
		scopes[dataset.Service][dataset.Scope] = append(scopes[dataset.Service][dataset.Scope], dataset.Name)
	}
	if len(scopes) == 0 {
		t.Fatal("catalog exposes no services")
	}
	for service, byScope := range scopes {
		if len(byScope) != 1 {
			distinct := make([]string, 0, len(byScope))
			for scope, selectors := range byScope {
				sort.Strings(selectors)
				distinct = append(distinct, scope+" <- "+strings.Join(selectors, ","))
			}
			sort.Strings(distinct)
			t.Fatalf("service %q mixes %d token audiences behind one host:\n%s", service, len(byScope), strings.Join(distinct, "\n"))
		}
		base := conf.Base(service)
		if base == "" {
			t.Fatalf("service %q is not wired into Config.Base", service)
		}
		var scope string
		for only := range byScope {
			scope = only
		}
		want := base
		if exception, ok := scopeOriginExceptions[service]; ok {
			want = exception
		}
		if got := scopeOrigin(t, scope); got != want {
			t.Fatalf("service %q token audience origin=%q, want %q (host=%q)", service, got, want, base)
		}
	}
}

func TestConfiguredServiceScopesOverrideCatalogAudiences(t *testing.T) {
	conf := defaultHostConfig(t)
	conf.Graph_Scope = "https://graph.microsoft.us/.default"
	conf.ARM_Scope = "https://management.usgovcloudapi.net/.default"
	conf.Defender_Scope = "https://api-gcc.securitycenter.microsoft.us/.default"
	conf.Defender_XDR_Scope = "https://api-gcc.security.microsoft.us/.default"
	conf.PowerBI_Scope = "https://analysis.usgovcloudapi.net/powerbi/api/.default"
	if err := conf.Verify(); err != nil {
		t.Fatalf("documented sovereign Power BI scope rejected: %v", err)
	}
	commercial := defaultHostConfig(t)
	commercial.PowerBI_Scope = "https://analysis.windows.net/powerbi/api/.default"
	if err := commercial.Verify(); err != nil {
		t.Fatalf("documented commercial Power BI scope rejected: %v", err)
	}
	tests := []struct {
		service Service
		want    string
	}{
		{ServiceGraph, conf.Graph_Scope},
		{ServiceARM, conf.ARM_Scope},
		{ServiceDefender, conf.Defender_Scope},
		{ServiceDefenderXDR, conf.Defender_XDR_Scope},
		{ServicePowerBI, conf.PowerBI_Scope},
	}
	for _, tc := range tests {
		if got := conf.Scope(tc.service, "https://commercial.invalid/.default"); got != tc.want {
			t.Fatalf("service %q scope=%q, want %q", tc.service, got, tc.want)
		}
	}
	if got := conf.Scope(ServiceGraph, "https://catalog.example/.default"); got != conf.Graph_Scope {
		t.Fatalf("Graph scope=%q", got)
	}
}

func TestConfiguredServiceScopesRequireHTTPSDefaultResource(t *testing.T) {
	for name, apply := range map[string]func(*Config){
		"Graph-Scope":        func(c *Config) { c.Graph_Scope = "http://graph.microsoft.us/.default" },
		"ARM-Scope":          func(c *Config) { c.ARM_Scope = "https://management.usgovcloudapi.net/not-default" },
		"Defender-Scope":     func(c *Config) { c.Defender_Scope = "not-a-url" },
		"Defender-XDR-Scope": func(c *Config) { c.Defender_XDR_Scope = "https://api-gcc.security.microsoft.us/.default?bad=1" },
		"PowerBI-Scope":      func(c *Config) { c.PowerBI_Scope = "https://analysis.usgovcloudapi.net/.default#bad" },
	} {
		t.Run(name, func(t *testing.T) {
			conf := &Config{
				Tenant_ID: "11111111-1111-1111-1111-111111111111", Client_ID: "22222222-2222-2222-2222-222222222222",
				Client_Secret: "synthetic-client-secret", Api: []string{"entra-users"},
			}
			apply(conf)
			if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("error=%v, want %s", err, name)
			}
		})
	}
}

func TestDefenderIncidentsUsesTheDefenderXDRResourceWithoutChangingItsRoute(t *testing.T) {
	incidents, ok := datasets["defender-incidents"]
	if !ok {
		t.Fatal("missing defender-incidents selector")
	}
	if incidents.Service != ServiceDefenderXDR {
		t.Fatalf("defender-incidents service=%q, want %q", incidents.Service, ServiceDefenderXDR)
	}
	if incidents.Scope != "https://api.security.microsoft.com/.default" {
		t.Fatalf("defender-incidents scope=%q", incidents.Scope)
	}
	// The published route identity must not move with the service split.
	if incidents.Product != "Microsoft Defender for Endpoint" || incidents.TagGroup != "microsoft-defender-incidents" {
		t.Fatalf("defender-incidents product=%q tag=%q", incidents.Product, incidents.TagGroup)
	}
	if got := destinationTag(incidents, TagSchemaConsolidated); got != "ms-defender-xdr-incidents" {
		t.Fatalf("defender-incidents consolidated destination=%q", got)
	}
	if got, want := stateKey("microsoft", incidents.Name, ""), "microsoft/defender-incidents"; got != want {
		t.Fatalf("defender-incidents state key=%q, want %q", got, want)
	}

	conf := defaultHostConfig(t)
	alerts := datasets["defender-alerts"]
	if conf.Base(incidents.Service) == conf.Base(alerts.Service) {
		t.Fatalf("Defender XDR and Defender for Endpoint share origin %q", conf.Base(incidents.Service))
	}
	if got, want := conf.Base(incidents.Service), defaultDefenderXDRHost; got != want {
		t.Fatalf("Defender XDR base=%q, want %q", got, want)
	}
	if got, want := conf.Base(alerts.Service), defaultDefenderHost; got != want {
		t.Fatalf("Defender for Endpoint base=%q, want %q", got, want)
	}
}

func TestValidateURLAllowsBothDefenderOriginsAndNothingElse(t *testing.T) {
	conf := defaultHostConfig(t)
	client, err := NewClient(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{
		defaultDefenderHost + "/api/machines",
		defaultDefenderXDRHost + "/api/incidents",
	} {
		if err := client.validateURL(endpoint); err != nil {
			t.Fatalf("validateURL(%q)=%v", endpoint, err)
		}
	}
	if err := client.validateURL("https://api.security.microsoft.us/api/incidents"); err == nil {
		t.Fatal("validateURL accepted an unconfigured Defender origin")
	}
}

func TestDefenderXDRHostIsValidatedLikeEveryOtherOrigin(t *testing.T) {
	for _, raw := range []string{"http://api.security.microsoft.com", "https://api.security.microsoft.com?x=1", "not-a-url"} {
		conf := &Config{
			Tenant_ID:         "11111111-1111-1111-1111-111111111111",
			Client_ID:         "22222222-2222-2222-2222-222222222222",
			Client_Secret:     "synthetic-client-secret",
			Api:               []string{"defender-incidents"},
			Defender_XDR_Host: raw,
		}
		if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), "Defender-XDR-Host") {
			t.Fatalf("Defender-XDR-Host=%q error=%v", raw, err)
		}
	}
}
