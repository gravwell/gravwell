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
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol"
	_ "github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol/anthropic"
	_ "github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol/openai"
)

func TestGetRemoteIP(t *testing.T) {
	tests := []struct {
		name       string
		xff        string
		remoteAddr string
		want       string
	}{
		{"xff-single", "203.0.113.5", "10.0.0.1:1234", "203.0.113.5"},
		{"xff-chain-takes-first", "203.0.113.5, 70.0.0.1", "10.0.0.1:1234", "203.0.113.5"},
		{"xff-padded", "  203.0.113.5  ", "10.0.0.1:1234", "203.0.113.5"},
		{"no-xff-uses-peer", "", "10.0.0.1:1234", "10.0.0.1"},
		// A present-but-invalid XFF must fall back to the peer, not 127.0.0.1.
		{"invalid-xff-falls-back-to-peer", "not-an-ip", "10.0.0.1:1234", "10.0.0.1"},
		{"empty-xff-entry-falls-back-to-peer", ", 70.0.0.1", "10.0.0.1:1234", "10.0.0.1"},
		// RemoteAddr without a port is still usable.
		{"peer-without-port", "", "10.0.0.1", "10.0.0.1"},
		// Nothing usable anywhere -> loopback default.
		{"all-invalid-defaults-loopback", "bogus", "also-bogus", "127.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := getRemoteIP(r); got.String() != tt.want {
				t.Errorf("getRemoteIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

// upstreamCapture records what the mock upstream received.
type upstreamCapture struct {
	gotContentType string
	gotAuth        string
	gotXFF         string
	gotBody        string
}

// newTestHandler builds a proxyHandler pointed at the given upstream URL,
// returning it plus the entry capture the ingest side writes to.
func newTestHandler(t *testing.T, upstreamURL string, mutate func(*listener)) (*proxyHandler, *capture) {
	t.Helper()
	proto, err := protocol.Lookup("openai-chat")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	l := &listener{
		Bind:           ":0",
		Upstream_URL:   upstreamURL,
		Protocol:       "openai-chat",
		Log_Tool_Calls: true,
		Log_Usage:      true,
	}
	if mutate != nil {
		mutate(l)
	}
	if err := l.validate(); err != nil {
		t.Fatalf("listener validate: %v", err)
	}
	sessions, err := newSessionStore(time.Hour, 0, "")
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	c := &capture{}
	ph := newProxyHandler("test", l, proto, entry.EntryTag(0),
		processors.NewProcessorSet(c), sessions, nil)
	return ph, c
}

const chatReqBody = `{"model":"gpt-4o","stream":false,"messages":[` +
	`{"role":"system","content":"be brief"},` +
	`{"role":"user","content":"hello"}]}`

const chatRespBody = `{"id":"resp-1","model":"gpt-4o",` +
	`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}],` +
	`"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`

func doChat(t *testing.T, ph *proxyHandler, body, auth string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.RemoteAddr = "203.0.113.7:5555"
	w := httptest.NewRecorder()
	ph.ServeHTTP(w, req)
	return w
}

func TestProxyNonStreaming(t *testing.T) {
	var cap upstreamCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		cap.gotBody = string(b)
		cap.gotContentType = r.Header.Get("Content-Type")
		cap.gotAuth = r.Header.Get("Authorization")
		cap.gotXFF = r.Header.Get("X-Forwarded-For")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, chatRespBody)
	}))
	defer srv.Close()

	ph, c := newTestHandler(t, srv.URL, nil)
	w := doChat(t, ph, chatReqBody, "Bearer sk-test")

	if w.Code != http.StatusOK {
		t.Fatalf("client status = %d, want 200", w.Code)
	}
	// Client gets the upstream body verbatim.
	if w.Body.String() != chatRespBody {
		t.Errorf("client body = %q, want the upstream response", w.Body.String())
	}
	// Regression guard for the header-forwarding bug: Content-Type must reach
	// the upstream, or real providers reject the request.
	if cap.gotContentType != "application/json" {
		t.Errorf("upstream Content-Type = %q, want application/json", cap.gotContentType)
	}
	if cap.gotBody != chatReqBody {
		t.Errorf("upstream body = %q, want the client body", cap.gotBody)
	}
	if cap.gotAuth != "Bearer sk-test" {
		t.Errorf("upstream Authorization = %q, want the client bearer", cap.gotAuth)
	}
	// X-Forwarded-For should carry the client IP.
	if !strings.Contains(cap.gotXFF, "203.0.113.7") {
		t.Errorf("upstream X-Forwarded-For = %q, want it to include the client IP", cap.gotXFF)
	}

	// Ingest side: system + user request events, assistant + usage response events.
	types := c.eventTypes(t)
	if countType(types, protocol.EventUserMessage) != 1 {
		t.Errorf("expected 1 user message event, got %v", types)
	}
	if countType(types, protocol.EventAssistantMessage) != 1 {
		t.Errorf("expected 1 assistant message event, got %v", types)
	}
	if countType(types, protocol.EventUsage) != 1 {
		t.Errorf("expected 1 usage event, got %v", types)
	}
}

// authCaptureServer returns a mock upstream that records the Authorization
// header it received and replies with a canned chat response.
func authCaptureServer(cap *upstreamCapture) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, chatRespBody)
	}))
}

func TestProxyInjectsUpstreamAuthorization(t *testing.T) {
	var cap upstreamCapture
	srv := authCaptureServer(&cap)
	defer srv.Close()

	ph, _ := newTestHandler(t, srv.URL, func(l *listener) { l.Upstream_Authorization = "sk-real" })
	w := doChat(t, ph, chatReqBody, "Bearer sk-client")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if cap.gotAuth != "Bearer sk-real" {
		t.Errorf("upstream Authorization = %q, want the injected upstream credential", cap.gotAuth)
	}
}

func TestProxyPassesClientAuthorizationWhenNoUpstreamConfigured(t *testing.T) {
	var cap upstreamCapture
	srv := authCaptureServer(&cap)
	defer srv.Close()

	ph, _ := newTestHandler(t, srv.URL, nil)
	w := doChat(t, ph, chatReqBody, "Bearer sk-client")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if cap.gotAuth != "Bearer sk-client" {
		t.Errorf("upstream Authorization = %q, want the client's Authorization passed through", cap.gotAuth)
	}
}

func TestProxyClientAuthorizationGate(t *testing.T) {
	var cap upstreamCapture
	srv := authCaptureServer(&cap)
	defer srv.Close()

	ph, _ := newTestHandler(t, srv.URL, func(l *listener) {
		l.Client_Authorization = "gate-token"
		l.Upstream_Authorization = "sk-real"
	})

	// Correct client credential: request is gated in and the upstream sees the
	// injected credential, never the client's gate token.
	w := doChat(t, ph, chatReqBody, "Bearer gate-token")
	if w.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, want 200", w.Code)
	}
	if cap.gotAuth != "Bearer sk-real" {
		t.Errorf("upstream Authorization = %q, want the injected upstream credential", cap.gotAuth)
	}

	// Wrong credential is rejected before the upstream is contacted.
	cap.gotAuth = "sentinel"
	w = doChat(t, ph, chatReqBody, "Bearer wrong-token")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad-credential status = %d, want 401", w.Code)
	}
	// Missing credential is likewise rejected.
	w = doChat(t, ph, chatReqBody, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing-credential status = %d, want 401", w.Code)
	}
	if cap.gotAuth != "sentinel" {
		t.Errorf("upstream was contacted on rejected request (saw Authorization %q)", cap.gotAuth)
	}
}

func TestProxyDropsConnectionNamedHeaders(t *testing.T) {
	var gotUpgrade, gotXHop, gotConn, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUpgrade = r.Header.Get("Upgrade")
		gotXHop = r.Header.Get("X-Hop")
		gotConn = r.Header.Get("Connection")
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, chatRespBody)
	}))
	defer srv.Close()

	ph, _ := newTestHandler(t, srv.URL, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatReqBody))
	req.Header.Set("Content-Type", "application/json")
	// Connection names X-Hop as connection-scoped, so it must be stripped along
	// with the well-known hop-by-hop headers (Connection, Upgrade).
	req.Header.Set("Connection", "X-Hop, Upgrade")
	req.Header.Set("X-Hop", "secret")
	req.Header.Set("Upgrade", "websocket")
	req.RemoteAddr = "203.0.113.7:5555"
	w := httptest.NewRecorder()
	ph.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if gotXHop != "" {
		t.Errorf("X-Hop should be dropped (named in Connection), upstream saw %q", gotXHop)
	}
	if gotUpgrade != "" {
		t.Errorf("Upgrade is hop-by-hop and should be dropped, upstream saw %q", gotUpgrade)
	}
	if gotConn != "" {
		t.Errorf("Connection is hop-by-hop and should be dropped, upstream saw %q", gotConn)
	}
	// End-to-end headers must still make it through.
	if gotContentType != "application/json" {
		t.Errorf("Content-Type should be forwarded, upstream saw %q", gotContentType)
	}
}

func TestProxyStreaming(t *testing.T) {
	sse := "data: {\"id\":\"s1\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hel\"}}]}\n\n" +
		"data: {\"id\":\"s1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"}}]}\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, sse)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	ph, c := newTestHandler(t, srv.URL, nil)
	w := doChat(t, ph, chatReqBody, "Bearer sk-test")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	// Client receives the raw SSE bytes.
	if w.Body.String() != sse {
		t.Errorf("client stream body = %q, want raw SSE", w.Body.String())
	}
	// Reassembled assistant message is ingested.
	types := c.eventTypes(t)
	if countType(types, protocol.EventAssistantMessage) != 1 {
		t.Fatalf("expected reassembled assistant message, got %v", types)
	}
	for _, e := range c.ents {
		if v, _ := e.GetEnumeratedValue("event_type"); v == protocol.EventAssistantMessage {
			if string(e.Data) != "Hello" {
				t.Errorf("reassembled content = %q, want Hello", e.Data)
			}
		}
	}
}

// gravwell/issues#2679: with Log-Mode=user the reporter still saw
// response.assistant_message entries under tag=llm. Exercise the full proxy
// path — buffered and streaming — the way the repro does.
func TestProxyUserModeSuppressesAssistant(t *testing.T) {
	userMode := func(l *listener) { l.Log_Mode = logModeUserOnly }

	t.Run("buffered", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, chatRespBody)
		}))
		defer srv.Close()

		ph, c := newTestHandler(t, srv.URL, userMode)
		if w := doChat(t, ph, chatReqBody, "Bearer sk-test"); w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		types := c.eventTypes(t)
		if countType(types, protocol.EventAssistantMessage) != 0 {
			t.Errorf("Log-Mode=user ingested an assistant message, got %v", types)
		}
		if countType(types, protocol.EventUserMessage) != 1 {
			t.Errorf("expected the user prompt to be ingested, got %v", types)
		}
		if countType(types, protocol.EventUsage) != 1 {
			t.Errorf("expected the usage record (Log-Usage=true), got %v", types)
		}
	})

	t.Run("streaming", func(t *testing.T) {
		sse := "data: {\"id\":\"s1\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
			"data: [DONE]\n\n"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, sse)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}))
		defer srv.Close()

		ph, c := newTestHandler(t, srv.URL, userMode)
		w := doChat(t, ph, chatReqBody, "Bearer sk-test")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		// The client still gets the full stream; only ingest is filtered.
		if w.Body.String() != sse {
			t.Errorf("client stream body = %q, want raw SSE", w.Body.String())
		}
		if types := c.eventTypes(t); countType(types, protocol.EventAssistantMessage) != 0 {
			t.Errorf("Log-Mode=user ingested a reassembled assistant message, got %v", types)
		}
	})
}

func TestProxyUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":"boom"}`)
	}))
	defer srv.Close()

	ph, _ := newTestHandler(t, srv.URL, nil)
	w := doChat(t, ph, chatReqBody, "Bearer sk-test")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	// An upstream 5xx is piped straight through to the client verbatim; the
	// proxy logs it rather than ingesting an event.
	if w.Body.String() != `{"error":"boom"}` {
		t.Errorf("error body not piped through: %q", w.Body.String())
	}
}

func TestProxyUnparseableRequestForwarded(t *testing.T) {
	var reached bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	ph, c := newTestHandler(t, srv.URL, nil)
	w := doChat(t, ph, `{not valid json`, "Bearer sk-test")
	if !reached {
		t.Error("unparseable request should still be forwarded to upstream")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (forwarded)", w.Code)
	}
	// No request/response events for a body we couldn't parse.
	if len(c.ents) != 0 {
		t.Errorf("expected no ingested events for unparseable request, got %v", c.eventTypes(t))
	}
}

// --- Anthropic Messages API (x-api-key auth) ---

// newAnthropicTestHandler builds a proxyHandler for the anthropic-messages
// protocol pointed at the given upstream URL.
func newAnthropicTestHandler(t *testing.T, upstreamURL string, mutate func(*listener)) (*proxyHandler, *capture) {
	t.Helper()
	proto, err := protocol.Lookup("anthropic-messages")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	l := &listener{
		Bind:           ":0",
		Upstream_URL:   upstreamURL,
		Protocol:       "anthropic-messages",
		Auth_Style:     "x-api-key",
		Log_Tool_Calls: true,
		Log_Usage:      true,
	}
	if mutate != nil {
		mutate(l)
	}
	if err := l.validate(); err != nil {
		t.Fatalf("listener validate: %v", err)
	}
	sessions, err := newSessionStore(time.Hour, 0, "")
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	c := &capture{}
	ph := newProxyHandler("test", l, proto, entry.EntryTag(0),
		processors.NewProcessorSet(c), sessions, nil)
	return ph, c
}

const msgReqBody = `{"model":"claude-opus-4-8","max_tokens":64,"stream":false,` +
	`"system":"be brief","messages":[{"role":"user","content":"hello"}]}`

const msgRespBody = `{"id":"msg_1","model":"claude-opus-4-8","role":"assistant",` +
	`"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn",` +
	`"usage":{"input_tokens":5,"output_tokens":1}}`

// doMessages issues a /v1/messages request with an optional x-api-key header.
func doMessages(t *testing.T, ph *proxyHandler, body, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}
	req.RemoteAddr = "203.0.113.7:5555"
	w := httptest.NewRecorder()
	ph.ServeHTTP(w, req)
	return w
}

func TestProxyAnthropicInjectsAPIKeyAndVersion(t *testing.T) {
	var gotAPIKey, gotVersion, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, msgRespBody)
	}))
	defer srv.Close()

	// Gating mode: the client sends no key; the proxy injects the real key as
	// x-api-key and supplies the required anthropic-version header.
	ph, c := newAnthropicTestHandler(t, srv.URL, func(l *listener) {
		l.Upstream_Authorization = "sk-ant-real"
		l.Anthropic_Version = "2023-06-01"
	})
	w := doMessages(t, ph, msgReqBody, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if gotAPIKey != "sk-ant-real" {
		t.Errorf("upstream x-api-key = %q, want the injected key", gotAPIKey)
	}
	if gotVersion != "2023-06-01" {
		t.Errorf("upstream anthropic-version = %q, want the injected default", gotVersion)
	}
	if gotAuth != "" {
		t.Errorf("upstream should not receive an Authorization header, saw %q", gotAuth)
	}
	// Ingest side: system + user request events, assistant + usage response events.
	types := c.eventTypes(t)
	if countType(types, protocol.EventUserMessage) != 1 {
		t.Errorf("expected 1 user message event, got %v", types)
	}
	if countType(types, protocol.EventAssistantMessage) != 1 {
		t.Errorf("expected 1 assistant message event, got %v", types)
	}
	if countType(types, protocol.EventUsage) != 1 {
		t.Errorf("expected 1 usage event, got %v", types)
	}
}

func TestProxyAnthropicPassesClientKeyThrough(t *testing.T) {
	var gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, msgRespBody)
	}))
	defer srv.Close()

	// Pass-through mode: no upstream credential configured, so the client's own
	// x-api-key reaches the upstream unchanged.
	ph, _ := newAnthropicTestHandler(t, srv.URL, nil)
	w := doMessages(t, ph, msgReqBody, "sk-ant-client")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if gotAPIKey != "sk-ant-client" {
		t.Errorf("upstream x-api-key = %q, want the client's key passed through", gotAPIKey)
	}
}

func TestProxyAnthropicClientGate(t *testing.T) {
	var gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, msgRespBody)
	}))
	defer srv.Close()

	ph, _ := newAnthropicTestHandler(t, srv.URL, func(l *listener) {
		l.Client_Authorization = "gate-key"
		l.Upstream_Authorization = "sk-ant-real"
	})

	// Correct client key: gated in; upstream sees the injected key, not the gate.
	w := doMessages(t, ph, msgReqBody, "gate-key")
	if w.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, want 200", w.Code)
	}
	if gotAPIKey != "sk-ant-real" {
		t.Errorf("upstream x-api-key = %q, want the injected key", gotAPIKey)
	}

	// Wrong / missing key is rejected before the upstream is contacted.
	gotAPIKey = "sentinel"
	if w = doMessages(t, ph, msgReqBody, "wrong-key"); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad-key status = %d, want 401", w.Code)
	}
	if w = doMessages(t, ph, msgReqBody, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing-key status = %d, want 401", w.Code)
	}
	if gotAPIKey != "sentinel" {
		t.Errorf("upstream was contacted on a rejected request (saw x-api-key %q)", gotAPIKey)
	}
}

// A path the protocol module does not parse is proxied by default (the client
// may need a sibling endpoint we have nothing to say about) and 404s only when
// the listener is explicitly narrowed to the parsed paths.
func TestProxyUnknownPath(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"input_tokens":7}`)
	}))
	defer srv.Close()

	t.Run("passthrough", func(t *testing.T) {
		ph, cap := newTestHandler(t, srv.URL, nil)
		req := httptest.NewRequest(http.MethodPost, "/v1/embeddings",
			strings.NewReader(`{"input":"hi"}`))
		req.RemoteAddr = "203.0.113.7:5555"
		w := httptest.NewRecorder()
		ph.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("unknown path status = %d, want 200", w.Code)
		}
		if w.Body.String() != `{"input_tokens":7}` {
			t.Errorf("client body = %q", w.Body.String())
		}
		if gotPath != "/v1/embeddings" || gotBody != `{"input":"hi"}` {
			t.Errorf("upstream saw path %q body %q", gotPath, gotBody)
		}
		if got := cap.eventTypes(t); len(got) != 0 {
			t.Errorf("ingested %v from an unparsed path, want nothing", got)
		}
	})

	t.Run("rejected", func(t *testing.T) {
		ph, _ := newTestHandler(t, srv.URL, func(l *listener) {
			l.Reject_Unknown_Paths = true
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader("{}"))
		req.RemoteAddr = "203.0.113.7:5555"
		w := httptest.NewRecorder()
		ph.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("unknown path status = %d, want 404", w.Code)
		}
	})

	t.Run("client-auth-still-enforced", func(t *testing.T) {
		ph, _ := newTestHandler(t, srv.URL, func(l *listener) {
			l.Client_Authorization = "gate"
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader("{}"))
		req.RemoteAddr = "203.0.113.7:5555"
		w := httptest.NewRecorder()
		ph.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated unknown path status = %d, want 401", w.Code)
		}
	})
}

// The upstream hop must be uncompressed: we can only ingest bytes we can read,
// and the client's own encoding negotiation (br, zstd) is not something the
// reassembler can decode.
func TestProxyDropsClientAcceptEncoding(t *testing.T) {
	var gotEncoding string
	var sawHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding, sawHeader = r.Header.Get("Accept-Encoding"), len(r.Header.Values("Accept-Encoding")) > 0
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, msgRespBody)
	}))
	defer srv.Close()

	ph, c := newAnthropicTestHandler(t, srv.URL, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(msgReqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.RemoteAddr = "203.0.113.7:5555"
	w := httptest.NewRecorder()
	ph.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if sawHeader {
		t.Errorf("upstream Accept-Encoding = %q, want the header dropped", gotEncoding)
	}
	if countType(c.eventTypes(t), protocol.EventAssistantMessage) != 1 {
		t.Errorf("expected the response to be parsed, got %v", c.eventTypes(t))
	}
}

// An upstream that compresses anyway still gets relayed byte-for-byte; we skip
// ingest rather than hand the parser bytes it cannot read.
func TestProxyEncodedResponseRelayedWithoutIngest(t *testing.T) {
	var payload bytes.Buffer
	zw := gzip.NewWriter(&payload)
	io.WriteString(zw, msgRespBody)
	zw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(payload.Bytes())
	}))
	defer srv.Close()

	ph, c := newAnthropicTestHandler(t, srv.URL, nil)
	w := doMessages(t, ph, msgReqBody, "sk-ant-client")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("client Content-Encoding = %q, want it preserved", w.Header().Get("Content-Encoding"))
	}
	if !bytes.Equal(w.Body.Bytes(), payload.Bytes()) {
		t.Error("client body was not the upstream bytes")
	}
	// The request side is still logged; only the unreadable response is skipped.
	types := c.eventTypes(t)
	if countType(types, protocol.EventAssistantMessage) != 0 || countType(types, protocol.EventUsage) != 0 {
		t.Errorf("response events logged from an encoded body: %v", types)
	}
	if countType(types, protocol.EventUserMessage) != 1 {
		t.Errorf("expected the request to still be logged, got %v", types)
	}
}

// Claude Code stamps every request with its own conversation ID; when the
// listener names that header we adopt it as the session identity instead of
// deriving one from the message prefix.
func TestProxySessionIDHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, msgRespBody)
	}))
	defer srv.Close()

	ph, c := newAnthropicTestHandler(t, srv.URL, func(l *listener) {
		l.Session_ID_Header = "x-claude-code-session-id"
	})
	do := func(sid, body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if sid != "" {
			req.Header.Set("x-claude-code-session-id", sid)
		}
		req.RemoteAddr = "203.0.113.7:5555"
		w := httptest.NewRecorder()
		ph.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	}
	// Two requests on the same client session ID, with histories that share no
	// prefix at all: prefix matching would call the second one a new session.
	do("11111111-2222-3333-4444-555555555555", msgReqBody)
	do("11111111-2222-3333-4444-555555555555",
		`{"model":"claude-opus-4-8","max_tokens":64,"messages":[{"role":"user","content":"totally unrelated"}]}`)
	// A different session ID from the same client is a different conversation.
	do("99999999-2222-3333-4444-555555555555", msgReqBody)
	// A missing header falls back to prefix matching, which mints its own ID.
	do("", msgReqBody)

	c.mu.Lock()
	defer c.mu.Unlock()
	sessions := map[string]bool{}
	newFlags := map[string]int{}
	for _, e := range c.ents {
		v, ok := e.GetEnumeratedValue("session_id")
		if !ok {
			t.Fatal("entry missing session_id EV")
		}
		id, _ := v.(string)
		sessions[id] = true
		if _, isNew := e.GetEnumeratedValue("new_session"); isNew {
			newFlags[id]++
		}
	}
	if !sessions["11111111-2222-3333-4444-555555555555"] {
		t.Errorf("client session ID was not adopted, saw %v", sessions)
	}
	if !sessions["99999999-2222-3333-4444-555555555555"] {
		t.Errorf("second client session ID was not adopted, saw %v", sessions)
	}
	if len(sessions) != 3 {
		t.Errorf("session count = %d, want 3 (two named, one derived): %v", len(sessions), sessions)
	}
	// The named session is new exactly once, on its first request.
	if n := newFlags["11111111-2222-3333-4444-555555555555"]; n == 0 {
		t.Error("first request on a named session was not flagged new")
	}
}

// An unusable value in the session header (oversized, empty, non-printable) is
// ignored in favor of prefix matching rather than trusted onto every entry.
func TestProxySessionIDHeaderRejectsJunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, msgRespBody)
	}))
	defer srv.Close()

	ph, c := newAnthropicTestHandler(t, srv.URL, func(l *listener) {
		l.Session_ID_Header = "x-claude-code-session-id"
	})
	junk := strings.Repeat("a", maxSessionIDLen+1)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(msgReqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-claude-code-session-id", junk)
	req.RemoteAddr = "203.0.113.7:5555"
	w := httptest.NewRecorder()
	ph.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.ents) == 0 {
		t.Fatal("nothing ingested")
	}
	for _, e := range c.ents {
		v, _ := e.GetEnumeratedValue("session_id")
		if id, _ := v.(string); id == junk {
			t.Fatal("oversized session header value was used as the session ID")
		}
	}
}
