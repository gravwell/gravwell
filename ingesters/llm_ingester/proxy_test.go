/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol"
	_ "github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol/openai"
)

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
	sessions, err := newSessionStore(time.Hour, "")
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

func TestProxyUnknownPath404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ph, _ := newTestHandler(t, srv.URL, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader("{}"))
	req.RemoteAddr = "203.0.113.7:5555"
	w := httptest.NewRecorder()
	ph.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown path status = %d, want 404", w.Code)
	}
}
