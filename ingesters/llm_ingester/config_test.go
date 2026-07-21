/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"testing"
	"time"
)

// validListener returns a minimally-valid listener for mutation in table tests.
func validListener() *listener {
	return &listener{
		Bind:         ":4180",
		Protocol:     "openai-chat",
		Upstream_URL: "http://example.com",
	}
}

func TestListenerValidateDefaults(t *testing.T) {
	l := validListener()
	if err := l.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if l.Log_Mode != logModeDeltas {
		t.Errorf("Log_Mode default = %q, want %q", l.Log_Mode, logModeDeltas)
	}
	if l.Max_Body != defaultMaxBody {
		t.Errorf("Max_Body default = %d, want %d", l.Max_Body, defaultMaxBody)
	}
	if l.sessionTTL != defaultSessionTTL {
		t.Errorf("sessionTTL default = %v, want %v", l.sessionTTL, defaultSessionTTL)
	}
	if l.upstreamURL == nil {
		t.Error("upstreamURL not populated")
	}
}

func TestListenerValidateErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*listener)
	}{
		{"missing-bind", func(l *listener) { l.Bind = "" }},
		{"bad-upstream-scheme", func(l *listener) { l.Upstream_URL = "ftp://example.com" }},
		{"upstream-no-host", func(l *listener) { l.Upstream_URL = "http://" }},
		{"unparseable-upstream", func(l *listener) { l.Upstream_URL = "http://[::1" }},
		{"missing-protocol", func(l *listener) { l.Protocol = "" }},
		{"unknown-protocol", func(l *listener) { l.Protocol = "does-not-exist" }},
		{"invalid-log-mode", func(l *listener) { l.Log_Mode = "bogus" }},
		{"bad-session-ttl", func(l *listener) { l.Session_TTL = "not-a-duration" }},
		{"negative-session-ttl", func(l *listener) { l.Session_TTL = "-5m" }},
		{"tls-only-cert", func(l *listener) { l.TLS_Certificate_File = "cert.pem" }},
		{"tls-only-key", func(l *listener) { l.TLS_Key_File = "key.pem" }},
		{"invalid-auth-style", func(l *listener) { l.Auth_Style = "digest" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := validListener()
			tt.mutate(l)
			if err := l.validate(); err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestListenerValidateSessionTTLParsed(t *testing.T) {
	l := validListener()
	l.Session_TTL = "45m"
	if err := l.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if l.sessionTTL != 45*time.Minute {
		t.Errorf("sessionTTL = %v, want 45m", l.sessionTTL)
	}
}

func TestListenerAuthStyle(t *testing.T) {
	// Default (bearer): Authorization header, "Bearer <token>" values.
	l := validListener()
	l.Client_Authorization = "ctok"
	l.Upstream_Authorization = "utok"
	if err := l.validate(); err != nil {
		t.Fatalf("validate bearer: %v", err)
	}
	if l.Auth_Style != authStyleBearer {
		t.Errorf("Auth_Style default = %q, want %q", l.Auth_Style, authStyleBearer)
	}
	if l.AuthHeaderName() != bearerHeaderName {
		t.Errorf("AuthHeaderName = %q, want %q", l.AuthHeaderName(), bearerHeaderName)
	}
	if l.ClientAuthHeader() != "Bearer ctok" || l.UpstreamAuthHeader() != "Bearer utok" {
		t.Errorf("bearer headers = %q / %q", l.ClientAuthHeader(), l.UpstreamAuthHeader())
	}

	// x-api-key: x-api-key header, bare token values.
	l = validListener()
	l.Protocol = "anthropic-messages"
	l.Auth_Style = "x-api-key"
	l.Client_Authorization = "ctok"
	l.Upstream_Authorization = "utok"
	if err := l.validate(); err != nil {
		t.Fatalf("validate x-api-key: %v", err)
	}
	if l.AuthHeaderName() != apiKeyHeaderName {
		t.Errorf("AuthHeaderName = %q, want %q", l.AuthHeaderName(), apiKeyHeaderName)
	}
	if l.ClientAuthHeader() != "ctok" || l.UpstreamAuthHeader() != "utok" {
		t.Errorf("x-api-key headers = %q / %q (want bare tokens)", l.ClientAuthHeader(), l.UpstreamAuthHeader())
	}
}

func TestVerifyNoListeners(t *testing.T) {
	c := &cfgType{Listener: map[string]*listener{}}
	if err := c.Verify(); err == nil {
		t.Error("expected error when no listeners are defined")
	}
}

func TestListenerAccessors(t *testing.T) {
	l := validListener()
	if err := l.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if l.TLSEnabled() {
		t.Error("TLSEnabled should be false with no cert/key")
	}
	if l.UpstreamURL() == nil {
		t.Error("UpstreamURL nil after validate")

	}
	if l.SessionTTL() != defaultSessionTTL {
		t.Errorf("SessionTTL = %v", l.SessionTTL())
	}
}

func TestTags(t *testing.T) {
	c := &cfgType{
		Listener: map[string]*listener{
			"a": {Tag_Name: "llm"},
			"b": {Tag_Name: "llm"},   // duplicate collapses
			"c": {Tag_Name: "audit"}, //
		},
	}
	tags, err := c.Tags()
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 unique tags, got %v", tags)
	}
	// sorted
	if tags[0] != "audit" || tags[1] != "llm" {
		t.Errorf("tags not sorted/deduped: %v", tags)
	}
}
