/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package thinkst

import "testing"

func validConfig() *Config {
	c := &Config{
		Domain: "example.canary.tools",
		Token:  "token",
		Api:    IncidentApi,
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
		{name: "valid audit", mutate: func(c *Config) { c.Api = AuditApi }, wantErr: false},
		{name: "missing tag", mutate: func(c *Config) { c.Tag_Name = "" }, wantErr: true},
		{name: "missing domain", mutate: func(c *Config) { c.Domain = "" }, wantErr: true},
		{name: "missing token", mutate: func(c *Config) { c.Token = "" }, wantErr: true},
		{name: "bad api", mutate: func(c *Config) { c.Api = "bogus" }, wantErr: true},
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
}
