/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol"
	"github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol/anthropic"
)

const (
	defaultMaxBody    = 16 * 1024 * 1024 // 16 MiB
	defaultSessionTTL = 30 * time.Minute

	logModeDeltas           = "delta"
	logModeUserOnly         = "user"
	logModeFullConversation = "full"
)

type gbl struct {
	config.IngestConfig
	// State_Store_Location is the (optional) path to the persistent state file
	// for the auto-derived session tracker. It stores only message hashes
	// (never prompt content) so sessions survive a restart. Empty disables
	// persistence and keeps session state in memory only.
	State_Store_Location string
	// Session_Match_Window bounds how many of a request's most-recent messages
	// the session tracker compares when deciding whether a request continues an
	// existing conversation. Larger values are more precise but do more work per
	// request; smaller values are cheaper. Unset or non-positive uses
	// defaultMatchWindow.
	Session_Match_Window int
}

type cfgReadType struct {
	Global       gbl
	Attach       attach.AttachConfig
	Listener     map[string]*listener
	Preprocessor processors.ProcessorConfig
}

// listener configures one proxy endpoint. See the annotated example config in
// gravwell_llm_ingester.conf for the operator-facing description of each field.
type listener struct {
	// Bind is the address:port this proxy listens on (e.g. ":4180").
	Bind string
	// Upstream_URL is the base URL requests are forwarded to (e.g.
	// "https://api.openai.com"). Must be http or https and include a host.
	Upstream_URL string
	// Protocol selects the protocol module used to parse traffic (e.g.
	// "openai-chat"). Must be a name registered in the protocol package.
	Protocol string
	// Tag_Name is the Gravwell tag ingested events are written to. Defaults to
	// the default tag when unset.
	Tag_Name string
	// Log_Mode controls how much of each request is ingested: "delta" (default,
	// one entry per new logical event), "user" (user prompts only — never the
	// model's replies; tool calls and results are still captured when
	// Log_Tool_Calls is true, and the usage record when Log_Usage is true), or
	// "full" (every message in the request body, every request).
	Log_Mode string
	// Log_Tool_Calls, when true, captures function-call / MCP tool invocations
	// and their results.
	Log_Tool_Calls bool
	// Log_Usage, when true, ingests the token-accounting record returned with
	// each response. Streaming requires the client to set
	// stream_options.include_usage = true.
	Log_Usage bool
	// Auth_Style selects the header the auth credentials use: "bearer" (default,
	// "Authorization: Bearer <token>", used by OpenAI-compatible upstreams) or
	// "x-api-key" (bare "x-api-key: <token>", used by the Anthropic Messages API).
	Auth_Style string
	// Anthropic_Version, when set, is injected as the upstream "anthropic-version"
	// header if the client did not supply one. This is specific to the
	// "anthropic-messages" protocol — validate() rejects it on any other
	// listener — and matters when the proxy injects the upstream credential and
	// the client therefore never sends the version header itself.
	Anthropic_Version string
	// Client_Authorization, when set, is the bare token inbound clients must
	// present in the auth header (see Auth_Style); mismatches get a 401. Empty
	// requires no client authentication.
	Client_Authorization string
	// Upstream_Authorization, when set, is the bare token injected as the
	// upstream auth header (see Auth_Style), replacing whatever the client sent.
	// Empty passes the client's own auth header through unchanged.
	Upstream_Authorization string
	// Session_ID_Header names a request header carrying the client's own
	// conversation identifier. When set and present on a request, that value
	// identifies the session instead of the derived prefix match; unset uses
	// prefix matching alone.
	//
	// This is provider-independent: any client that stamps a stable
	// conversation identifier on its requests can be tracked this way, whatever
	// protocol the listener speaks. Claude Code's "x-claude-code-session-id" is
	// the worked example because it is the one we ship a config for, not
	// because the mechanism is Anthropic-specific.
	Session_ID_Header string
	// Allow_Unknown_Paths, when true, proxies any path to the upstream instead
	// of answering 404 for the ones neither parsed nor declared as a
	// passthrough by the protocol module.
	//
	// This is insecure and off by default. The proxy attaches the configured
	// Upstream_Authorization to whatever it forwards, so an open path list lets
	// a client point the listener at a request we never inspect and read the
	// upstream credential back out of it. The sibling endpoints a real client
	// needs are declared by the protocol module itself (see
	// protocol.Protocol.PassthroughPaths), so this should stay off unless a
	// specific client demands otherwise.
	Allow_Unknown_Paths bool
	// Session_TTL is how long idle session prefix-match state is retained,
	// expressed as a Go duration string (e.g. "30m"). Defaults to
	// defaultSessionTTL when unset.
	Session_TTL string
	// Max_Body is the maximum inbound request body size in bytes. Defaults to
	// defaultMaxBody when unset or non-positive.
	Max_Body                          int
	TLS_Certificate_File              string
	TLS_Key_File                      string
	Insecure_Skip_TLS_Verify_Upstream bool
	Preprocessor                      []string

	// derived during Verify
	sessionTTL         time.Duration
	upstreamURL        *url.URL
	authHeaderName     string
	clientAuthHeader   string
	upstreamAuthHeader string
}

const (
	authStyleBearer  = "bearer"
	authStyleAPIKey  = "x-api-key"
	bearerHeaderName = "Authorization"
	apiKeyHeaderName = "x-api-key"
)

type cfgType struct {
	gbl
	Attach       attach.AttachConfig
	Listener     map[string]*listener
	Preprocessor processors.ProcessorConfig
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var cr cfgReadType
	if err := config.LoadConfigFile(&cr, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&cr, overlayPath); err != nil {
		return nil, err
	}
	c := &cfgType{
		gbl:          cr.Global,
		Attach:       cr.Attach,
		Listener:     cr.Listener,
		Preprocessor: cr.Preprocessor,
	}
	if err := c.Verify(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *cfgType) Verify() error {
	if err := c.IngestConfig.Verify(); err != nil {
		return err
	}
	if err := c.Attach.Verify(); err != nil {
		return err
	}
	if len(c.Listener) == 0 {
		return errors.New("no listeners defined")
	}
	if err := c.Preprocessor.Validate(); err != nil {
		return err
	}
	binds := map[string]string{}
	for name, l := range c.Listener {
		if err := l.validate(); err != nil {
			return fmt.Errorf("listener %q: %w", name, err)
		}
		if other, ok := binds[l.Bind]; ok {
			return fmt.Errorf("listener %q bind %q duplicated (also used by %q)", name, l.Bind, other)
		}
		binds[l.Bind] = name
		if err := c.Preprocessor.CheckProcessors(l.Preprocessor); err != nil {
			return fmt.Errorf("listener %q preprocessor invalid: %w", name, err)
		}
	}
	return nil
}

func (l *listener) validate() error {
	if l.Bind == "" {
		return errors.New("missing Bind")
	}
	if l.Upstream_URL == "" {
		return errors.New("missing Upstream-URL")
	}
	u, err := url.Parse(l.Upstream_URL)
	if err != nil {
		return fmt.Errorf("invalid Upstream-URL: %w", err)
	} else if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("Upstream-URL scheme must be http or https, got %q", u.Scheme)
	} else if u.Host == "" {
		return errors.New("Upstream-URL must include a host")
	}
	l.upstreamURL = u
	if l.Protocol == "" {
		return errors.New("missing Protocol")
	}
	if _, err := protocol.Lookup(l.Protocol); err != nil {
		return err
	}
	if l.Tag_Name == "" {
		l.Tag_Name = entry.DefaultTagName
	}
	if err := ingest.CheckTag(l.Tag_Name); err != nil {
		return fmt.Errorf("invalid Tag-Name %q: %w", l.Tag_Name, err)
	}
	switch l.Log_Mode {
	case "":
		l.Log_Mode = logModeDeltas
	case logModeDeltas, logModeUserOnly, logModeFullConversation:
	default:
		return fmt.Errorf("invalid Log-Mode %q (want %q, %q, or %q)",
			l.Log_Mode, logModeDeltas, logModeUserOnly, logModeFullConversation)
	}
	if l.Max_Body <= 0 {
		l.Max_Body = defaultMaxBody
	}
	// Options that only mean something to one provider are rejected elsewhere
	// rather than silently ignored: a config that sets them on the wrong
	// listener is a mistake the operator wants to hear about at startup.
	if l.Anthropic_Version != "" && l.Protocol != anthropic.ProtocolName {
		return fmt.Errorf("Anthropic-Version is only valid with Protocol %q, got %q",
			anthropic.ProtocolName, l.Protocol)
	}
	if l.Session_ID_Header != "" {
		if !validHeaderName(l.Session_ID_Header) {
			return fmt.Errorf("invalid Session-ID-Header %q", l.Session_ID_Header)
		}
		l.Session_ID_Header = http.CanonicalHeaderKey(l.Session_ID_Header)
	}
	if l.Session_TTL == "" {
		l.sessionTTL = defaultSessionTTL
	} else {
		d, err := time.ParseDuration(l.Session_TTL)
		if err != nil {
			return fmt.Errorf("invalid Session-TTL: %w", err)
		} else if d <= 0 {
			return errors.New("Session-TTL must be positive")
		}
		l.sessionTTL = d
	}
	if l.TLS_Certificate_File != "" || l.TLS_Key_File != "" {
		if l.TLS_Certificate_File == "" || l.TLS_Key_File == "" {
			return errors.New("both TLS-Certificate-File and TLS-Key-File must be set")
		}
		if _, err := tls.LoadX509KeyPair(l.TLS_Certificate_File, l.TLS_Key_File); err != nil {
			return fmt.Errorf("TLS keypair: %w", err)
		}
	}
	// Auth tokens are configured bare. Auth_Style selects the header and scheme:
	// "bearer" (default) speaks "Authorization: Bearer <token>"; "x-api-key"
	// speaks a bare "x-api-key: <token>" for the Anthropic Messages API. Empty
	// tokens leave the corresponding header handling off.
	switch l.Auth_Style {
	case "", authStyleBearer:
		l.Auth_Style = authStyleBearer
		l.authHeaderName = bearerHeaderName
		if l.Client_Authorization != "" {
			l.clientAuthHeader = "Bearer " + l.Client_Authorization
		}
		if l.Upstream_Authorization != "" {
			l.upstreamAuthHeader = "Bearer " + l.Upstream_Authorization
		}
	case authStyleAPIKey:
		l.authHeaderName = apiKeyHeaderName
		l.clientAuthHeader = l.Client_Authorization
		l.upstreamAuthHeader = l.Upstream_Authorization
	default:
		return fmt.Errorf("invalid Auth-Style %q (want %q or %q)",
			l.Auth_Style, authStyleBearer, authStyleAPIKey)
	}
	return nil
}

// AuthHeaderName returns the header the auth credentials use ("Authorization"
// for the bearer style, "x-api-key" for the Anthropic style).
func (l *listener) AuthHeaderName() string {
	return l.authHeaderName
}

// ClientAuthHeader returns the full auth header value an inbound client must
// present (see AuthHeaderName), or "" when no client authentication is required.
func (l *listener) ClientAuthHeader() string {
	return l.clientAuthHeader
}

// UpstreamAuthHeader returns the auth header value to inject on the upstream
// request (see AuthHeaderName), or "" to pass the client's own value through
// unchanged.
func (l *listener) UpstreamAuthHeader() string {
	return l.upstreamAuthHeader
}

func (l *listener) TLSEnabled() bool {
	return l.TLS_Certificate_File != "" && l.TLS_Key_File != ""
}

func (l *listener) UpstreamURL() *url.URL {
	return l.upstreamURL
}

func (l *listener) SessionTTL() time.Duration {
	return l.sessionTTL
}

// validHeaderName reports whether s is a legal HTTP field name (RFC 7230
// token). Header lookups are canonicalized, so case does not matter, but a
// value with illegal characters would silently never match.
func validHeaderName(s string) bool {
	const tokenExtra = `!#$%&'*+-.^_` + "`" + `|~`
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune(tokenExtra, r):
		default:
			return false
		}
	}
	return s != ""
}

// Tags returns the list of tags used across all listeners.
func (c *cfgType) Tags() ([]string, error) {
	seen := map[string]bool{}
	var tags []string
	for _, l := range c.Listener {
		if l.Tag_Name == "" || seen[l.Tag_Name] {
			continue
		}
		seen[l.Tag_Name] = true
		tags = append(tags, l.Tag_Name)
	}
	if len(tags) == 0 {
		return nil, errors.New("no tags configured")
	}
	sort.Strings(tags)
	return tags, nil
}

func (c *cfgType) IngestBaseConfig() config.IngestConfig {
	return c.IngestConfig
}

func (c *cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
}
