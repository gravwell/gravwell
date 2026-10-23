/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
		// Anthropic-Version means nothing to an OpenAI listener; a config that
		// sets it there is a mistake, not something to quietly ignore.
		{"anthropic-version-on-openai", func(l *listener) {
			l.Protocol = "openai-chat"
			l.Anthropic_Version = "2023-06-01"
		}},
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

func TestListenerSessionIDHeader(t *testing.T) {
	// Header lookups are canonicalized, so the configured name is too.
	l := validListener()
	l.Session_ID_Header = "x-claude-code-session-id"
	if err := l.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if l.Session_ID_Header != "X-Claude-Code-Session-Id" {
		t.Errorf("Session_ID_Header = %q, want it canonicalized", l.Session_ID_Header)
	}
	// A name that could never match anything is a config error, not a silently
	// dead setting.
	for _, bad := range []string{"has space", "trailing:", "new\nline"} {
		l = validListener()
		l.Session_ID_Header = bad
		if err := l.validate(); err == nil {
			t.Errorf("expected an error for Session-ID-Header %q", bad)
		}
	}
}

// The shipped example config is documentation operators copy from, so it has
// to parse. It defines no backend target (so a fresh install does not point
// anywhere real), which the ingest config rejects; uncommenting the example
// target it already carries is enough to make it loadable.
func TestShippedExampleConfig(t *testing.T) {
	const exampleConf = "gravwell_llm_ingester.conf"
	raw, err := os.ReadFile(exampleConf)
	if err != nil {
		t.Fatalf("read %s: %v", exampleConf, err)
	}
	const commented = "#Cleartext-Backend-Target=127.0.0.1:4023"
	if !bytes.Contains(raw, []byte(commented)) {
		t.Fatalf("%s no longer carries the example backend target", exampleConf)
	}
	loadable := bytes.Replace(raw, []byte(commented),
		[]byte(strings.TrimPrefix(commented, "#")), 1)
	path := filepath.Join(t.TempDir(), exampleConf)
	if err := os.WriteFile(path, loadable, 0600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	c, err := GetConfig(path, "")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	// Both built-in protocols are exercised by the example, so a rename or a
	// bad field name in either listener shows up here.
	protos := map[string]bool{}
	for name, l := range c.Listener {
		protos[l.Protocol] = true
		if l.Protocol == "anthropic-messages" {
			// The example enables the Claude Code session header; confirm the
			// setting actually reached the listener rather than being parsed
			// and dropped.
			if l.Session_ID_Header != "X-Claude-Code-Session-Id" {
				t.Errorf("listener %q Session_ID_Header = %q", name, l.Session_ID_Header)
			}
			if l.AuthHeaderName() != apiKeyHeaderName {
				t.Errorf("listener %q auth header = %q, want %q", name, l.AuthHeaderName(), apiKeyHeaderName)
			}
		}
	}
	for _, want := range []string{"openai-chat", "anthropic-messages"} {
		if !protos[want] {
			t.Errorf("example config has no %q listener (found %v)", want, protos)
		}
	}
}

// Anthropic-Version is accepted on the listener it belongs to, and only there.
func TestListenerAnthropicVersionProtocolGated(t *testing.T) {
	l := validListener()
	l.Protocol = "anthropic-messages"
	l.Auth_Style = "x-api-key"
	l.Anthropic_Version = "2023-06-01"
	if err := l.validate(); err != nil {
		t.Fatalf("Anthropic-Version on an anthropic listener: %v", err)
	}

	// Session-ID-Header, by contrast, is provider-independent and is valid on
	// any listener.
	l = validListener()
	l.Protocol = "openai-chat"
	l.Session_ID_Header = "x-my-app-conversation"
	if err := l.validate(); err != nil {
		t.Fatalf("Session-ID-Header on an openai listener: %v", err)
	}
}
