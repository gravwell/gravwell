/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package thinkst

import (
	"slices"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/configtest"
)

func validConfig() *Config {
	c := &Config{
		Domain: "example.canary.tools",
		Token:  "token",
		Api:    []Api{IncidentApi},
	}
	c.Tag_Name = "thinkst"
	return c
}

func TestConfigVerify(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{name: "valid incident", mutate: func(c *Config) {}, wantErr: false},
		{name: "valid audit", mutate: func(c *Config) { c.Api = []Api{AuditApi} }, wantErr: false},
		{name: "valid without tag name, falls back to api name", mutate: func(c *Config) { c.Tag_Name = "" }, wantErr: false},
		{name: "valid tag prefix without tag name", mutate: func(c *Config) { c.Tag_Name = ""; c.Tag_Prefix = "canary" }, wantErr: false},
		{name: "missing domain", mutate: func(c *Config) { c.Domain = "" }, wantErr: true},
		{name: "missing token", mutate: func(c *Config) { c.Token = "" }, wantErr: true},
		{name: "missing api", mutate: func(c *Config) { c.Api = nil }, wantErr: true},
		{name: "bad api", mutate: func(c *Config) { c.Api = []Api{"bogus"} }, wantErr: true},
		{name: "tag name with multiple apis", mutate: func(c *Config) { c.Api = []Api{IncidentApi, AuditApi} }, wantErr: true},
		{name: "tag name and tag prefix together", mutate: func(c *Config) { c.Tag_Prefix = "canary" }, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := validConfig()
			test.mutate(c)
			err := c.Verify()
			if test.wantErr && err == nil {
				t.Errorf("got nil error, want error")
			} else if !test.wantErr && err != nil {
				t.Errorf("got error %v, want nil", err)
			}
		})
	}
}

func TestConfigVerifyDefaults(t *testing.T) {
	c := validConfig()
	if err := c.Verify(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Lookback != defaultLookback {
		t.Errorf("got lookback %d, want %d", c.Lookback, defaultLookback)
	}
	if c.Requests_Per_Minute != defaultRequestsPerMinute {
		t.Errorf("got requests-per-minute %d, want %d", c.Requests_Per_Minute, defaultRequestsPerMinute)
	}
	if c.Request_Interval != defaultInterval {
		t.Errorf("got request-interval %d, want %d", c.Request_Interval, defaultInterval)
	}
	if c.Ingester_UUID != defaultIngesterUUIDStr {
		t.Errorf("got Ingester-UUID %q, want default %q", c.Ingester_UUID, defaultIngesterUUIDStr)
	}
}

func TestConfigVerifyRejectsBadIngesterUUID(t *testing.T) {
	c := validConfig()
	c.Ingester_UUID = "not-a-uuid"
	if err := c.Verify(); err == nil {
		t.Fatal("expected error for invalid Ingester-UUID, got nil")
	}
}

func TestApiTag(t *testing.T) {
	tests := []struct {
		name   string
		tag    string
		prefix string
		want   string
	}{
		{name: "explicit tag wins", tag: "override", prefix: "canary", want: "override"},
		{name: "prefix without tag", tag: "", prefix: "canary", want: "canary-incident"},
		{name: "neither set falls back to api name", tag: "", prefix: "", want: "incident"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IncidentApi.Tag(test.tag, test.prefix); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestConfigTags(t *testing.T) {
	c := validConfig()
	c.Api = []Api{IncidentApi, AuditApi}
	c.Tag_Name = ""
	c.Tag_Prefix = "canary"

	got := c.Tags()
	want := []string{"canary-incident", "canary-audit"}
	if !slices.Equal(got, want) {
		t.Errorf("got tags %v, want %v", got, want)
	}
}

func TestConfigEqual(t *testing.T) {
	configtest.CheckEqual(t, Config{
		BaseConfig:     hosted.BaseConfig{Ingester_UUID: defaultIngesterUUIDStr},
		MultiTagConfig: hosted.MultiTagConfig{Tag_Name: "thinkst"},
		PollingConfig: hosted.PollingConfig{
			Lookback:            defaultLookback,
			Requests_Per_Minute: defaultRequestsPerMinute,
			Request_Interval:    defaultInterval,
		},
		Domain: "example.canary.tools",
		Token:  "token",
		Api:    []Api{IncidentApi},
	})
}
