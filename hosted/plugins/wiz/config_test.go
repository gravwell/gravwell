/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package wiz

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/hosted/configtest"
)

func baseConfig() *Config {
	return &Config{
		Client_Id:     "id",
		Client_Secret: "secret",
		Endpoint:      "https://api.us1.app.wiz.io/graphql",
		Tag_Name:      "wiz",
	}
}

func TestConfigVerify(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{name: "valid", mutate: func(*Config) {}},
		{name: "missing client id", mutate: func(c *Config) { c.Client_Id = "" }, wantErr: true},
		{name: "missing client secret", mutate: func(c *Config) { c.Client_Secret = "" }, wantErr: true},
		{name: "missing endpoint", mutate: func(c *Config) { c.Endpoint = "" }, wantErr: true},
		{name: "endpoint not https", mutate: func(c *Config) { c.Endpoint = "http://api.us1.app.wiz.io/graphql" }, wantErr: true},
		{name: "endpoint wrong domain", mutate: func(c *Config) { c.Endpoint = "https://api.evil.com/graphql" }, wantErr: true},
		{name: "endpoint lookalike", mutate: func(c *Config) { c.Endpoint = "https://app.wiz.io.evil.com/graphql" }, wantErr: true},
		{name: "bad auth url", mutate: func(c *Config) { c.Auth_URL = "https://auth.evil.com/oauth/token" }, wantErr: true},
		{name: "missing tag name", mutate: func(c *Config) { c.Tag_Name = "" }, wantErr: true},
		{name: "valid tag override", mutate: func(c *Config) { c.Tag_Override = []string{"Audit:wiz-audit"} }},
		{name: "malformed tag override", mutate: func(c *Config) { c.Tag_Override = []string{"Audit"} }, wantErr: true},
		{name: "unknown source tag override", mutate: func(c *Config) { c.Tag_Override = []string{"Nope:wiz-nope"} }, wantErr: true},
		{name: "invalid override tag", mutate: func(c *Config) { c.Tag_Override = []string{"Audit:bad tag"} }, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := baseConfig()
			test.mutate(c)
			err := c.Verify()
			if test.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	c := baseConfig()
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	}
	if c.Auth_URL != defaultAuthURL {
		t.Errorf("Auth_URL = %q, want %q", c.Auth_URL, defaultAuthURL)
	}
	if c.Audience != defaultAudience {
		t.Errorf("Audience = %q, want %q", c.Audience, defaultAudience)
	}
	if c.Page_Size != defaultPageSize {
		t.Errorf("Page_Size = %d, want %d", c.Page_Size, defaultPageSize)
	}
	if c.Max_Pages_Per_Type != defaultMaxPagesPerType {
		t.Errorf("Max_Pages_Per_Type = %d, want %d", c.Max_Pages_Per_Type, defaultMaxPagesPerType)
	}
	if c.Request_Interval != defaultInterval {
		t.Errorf("Request_Interval = %d, want %d", c.Request_Interval, defaultInterval)
	}
}

func TestConfigTagRouting(t *testing.T) {
	c := baseConfig()
	c.Tag_Override = []string{"Audit:wiz-audit", "VulnerabilityFinding:wiz-vulns"}
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	}

	if got := c.tagFor(sourceAudit); got != "wiz-audit" {
		t.Errorf("tagFor(Audit) = %q, want wiz-audit", got)
	}
	if got := c.tagFor(sourceVulnerability); got != "wiz-vulns" {
		t.Errorf("tagFor(VulnerabilityFinding) = %q, want wiz-vulns", got)
	}
	// no override falls back to the default tag
	if got := c.tagFor(sourceIssue); got != "wiz" {
		t.Errorf("tagFor(Issues) = %q, want wiz", got)
	}

	tags := c.Tags()
	want := map[string]bool{"wiz": true, "wiz-audit": true, "wiz-vulns": true}
	if len(tags) != len(want) {
		t.Fatalf("Tags() = %v, want %d unique tags", tags, len(want))
	}
	for _, tag := range tags {
		if !want[tag] {
			t.Errorf("unexpected tag %q in %v", tag, tags)
		}
	}
}

func TestConfigQueryOverride(t *testing.T) {
	dir := t.TempDir()
	qpath := filepath.Join(dir, "audit.graphql")
	doc := "query($first: Int){ auditLogEntries(first:$first){ nodes{id} pageInfo{hasNextPage endCursor} } }"
	if err := os.WriteFile(qpath, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("valid", func(t *testing.T) {
		c := baseConfig()
		c.Query_Override = []string{"Audit:" + qpath}
		if err := c.Verify(); err != nil {
			t.Fatal(err)
		}
		if c.queries[sourceAudit] != doc {
			t.Errorf("query override not loaded: %q", c.queries[sourceAudit])
		}
	})

	t.Run("unknown source", func(t *testing.T) {
		c := baseConfig()
		c.Query_Override = []string{"Nope:" + qpath}
		if err := c.Verify(); err == nil {
			t.Fatal("expected error for unknown source")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		c := baseConfig()
		c.Query_Override = []string{"Audit:" + filepath.Join(dir, "nope.graphql")}
		if err := c.Verify(); err == nil {
			t.Fatal("expected error for missing file")
		}
	})
}

func equalConfig() Config {
	return Config{
		BaseConfig: hosted.BaseConfig{Ingester_UUID: defaultIngesterUUIDStr},
		PollingConfig: hosted.PollingConfig{
			Lookback:            defaultLookback,
			Requests_Per_Minute: defaultRequestsPerMinute,
			Request_Interval:    defaultInterval,
		},
		Client_Id:          "id",
		Client_Secret:      "secret",
		Endpoint:           "https://api.us1.app.wiz.io/graphql",
		Auth_URL:           defaultAuthURL,
		Audience:           defaultAudience,
		Page_Size:          defaultPageSize,
		Max_Pages_Per_Type: defaultMaxPagesPerType,
		Tag_Name:           "wiz",
		Tag_Override:       []string{sourceAudit + ":wiz-audit"},
		tags:               map[string]string{sourceAudit: "wiz-audit"},
		queries:            map[string]string{sourceAudit: "query{}"},
	}
}

func TestConfigEqual(t *testing.T) {
	configtest.CheckEqual(t, equalConfig())
}

// the parsed override maps are unexported so configtest can't reach them, cover them by hand.
// The query documents matter because an edited override file changes the query without
// changing a single byte of the config file.
func TestConfigEqualParsedOverrides(t *testing.T) {
	base := equalConfig()
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{`tags added`, func(c *Config) { c.tags[sourceIssue] = "wiz-issue" }},
		{`tags changed`, func(c *Config) { c.tags[sourceAudit] = "wiz-other" }},
		{`tags dropped`, func(c *Config) { c.tags = nil }},
		{`queries changed`, func(c *Config) { c.queries[sourceAudit] = "query{different}" }},
		{`queries dropped`, func(c *Config) { c.queries = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nc := equalConfig()
			tt.mutate(&nc)
			if base.Equal(&nc) {
				t.Errorf("Equal ignored %s", tt.name)
			}
		})
	}
}
