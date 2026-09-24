package servicenow

import (
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
)

func serviceNowEqualConfig() Config {
	return Config{
		BaseConfig:          hosted.BaseConfig{Ingester_UUID: "42000000-0000-4000-8000-000000000001"},
		MultiTagConfig:      hosted.MultiTagConfig{Tag_Prefix: "servicenow"},
		PollingConfig:       hosted.PollingConfig{Lookback: 24, Requests_Per_Minute: 60, Request_Interval: 300},
		Instance:            "https://example.service-now.com",
		Secret_File:         "/run/secrets/servicenow.json",
		Product:             []string{"itsm"},
		API:                 []string{"audit"},
		API_Endpoint:        []string{"attachment-metadata"},
		Table:               []string{"incident"},
		Table_Override:      []string{`audit={"Table":"sys_audit"}`},
		Selector:            []string{"audit"},
		Selector_Override:   []string{`audit={"Table":"sys_audit"}`},
		Page_Size:           100,
		Max_Pages:           10,
		Timeout:             30,
		Overlap:             intPointer(300),
		Max_Retries:         intPointer(4),
		Skip_Unavailable:    true,
		Normalization:       "enabled",
		Normalization_Field: []string{"users:userName=user_name"},
	}
}

func appendConfigValue(values []string, value string) []string {
	result := append([]string(nil), values...)
	return append(result, value)
}

func TestConfigEqual(t *testing.T) {
	base := serviceNowEqualConfig()
	identical := serviceNowEqualConfig()
	if !base.Equal(&identical) || !base.Equal(identical) {
		t.Fatal("identical ServiceNow configs are not equal")
	}
	if base.Equal(nil) || base.Equal((*Config)(nil)) || base.Equal(struct{}{}) {
		t.Fatal("nil or foreign ServiceNow config compared equal")
	}

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"Ingester_UUID", func(c *Config) { c.Ingester_UUID = "42000000-0000-4000-8000-000000000002" }},
		{"Tag_Name", func(c *Config) { c.Tag_Name = "servicenow-one" }},
		{"Tag_Prefix", func(c *Config) { c.Tag_Prefix = "servicenow-other" }},
		{"Lookback", func(c *Config) { c.Lookback++ }},
		{"Requests_Per_Minute", func(c *Config) { c.Requests_Per_Minute++ }},
		{"Request_Interval", func(c *Config) { c.Request_Interval++ }},
		{"Instance", func(c *Config) { c.Instance = "https://other.service-now.com" }},
		{"Secret_File", func(c *Config) { c.Secret_File = "/run/secrets/other.json" }},
		{"Product", func(c *Config) { c.Product = appendConfigValue(c.Product, "itom") }},
		{"API", func(c *Config) { c.API = appendConfigValue(c.API, "incidents") }},
		{"API_Endpoint", func(c *Config) { c.API_Endpoint = appendConfigValue(c.API_Endpoint, "change-models") }},
		{"Table", func(c *Config) { c.Table = appendConfigValue(c.Table, "sys_user") }},
		{"Table_Override", func(c *Config) {
			c.Table_Override = appendConfigValue(c.Table_Override, `incidents={"Table":"incident"}`)
		}},
		{"Selector", func(c *Config) { c.Selector = appendConfigValue(c.Selector, "incidents") }},
		{"Selector_Override", func(c *Config) {
			c.Selector_Override = appendConfigValue(c.Selector_Override, `incidents={"Table":"incident"}`)
		}},
		{"Page_Size", func(c *Config) { c.Page_Size++ }},
		{"Max_Pages", func(c *Config) { c.Max_Pages++ }},
		{"Timeout", func(c *Config) { c.Timeout++ }},
		{"Overlap", func(c *Config) { c.Overlap = intPointer(301) }},
		{"Max_Retries", func(c *Config) { c.Max_Retries = intPointer(5) }},
		{"Skip_Unavailable", func(c *Config) { c.Skip_Unavailable = false }},
		{"Normalization", func(c *Config) { c.Normalization = "disabled" }},
		{"Normalization_Field", func(c *Config) { c.Normalization_Field = appendConfigValue(c.Normalization_Field, "all:id=sys_id") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := serviceNowEqualConfig()
			test.mutate(&changed)
			if base.Equal(&changed) {
				t.Fatalf("Equal ignored %s", test.name)
			}
		})
	}
}

func TestConfigEqualUsesEffectivePointerDefaults(t *testing.T) {
	base := serviceNowEqualConfig()
	effectiveDefaults := serviceNowEqualConfig()
	effectiveDefaults.Overlap = nil
	effectiveDefaults.Max_Retries = nil
	if !base.Equal(&effectiveDefaults) {
		t.Fatal("nil and explicit default pointer values are not equal")
	}
}
