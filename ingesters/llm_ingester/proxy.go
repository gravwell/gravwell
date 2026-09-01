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
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/ingesters/llm_ingester/protocol"
	"github.com/gravwell/gravwell/v4/ingesters/llm_ingester/protocol/anthropic"
)

// proxyHandler is the http.Handler for one [Listener] entry.
//
// This intentionally reimplements the request-forwarding machinery of
// net/http/httputil.ReverseProxy rather than embedding it: we need to tap the
// upstream response body as it streams (see handleStream) to feed the SSE
// reassembler while the bytes flow to the client, which ReverseProxy does not
// expose. The header handling below mirrors ReverseProxy's hop-by-hop rules.
type proxyHandler struct {
	name     string
	cfg      *listener
	proto    protocol.Protocol
	tag      entry.EntryTag
	pproc    *processors.ProcessorSet
	sessions *sessionStore
	upstream *http.Client
	pathSet  map[string]bool
	// passSet holds the provider's sibling endpoints that we do not parse but
	// always forward, even when Allow_Unknown_Paths is off (see
	// protocol.Protocol.PassthroughPaths).
	passSet map[string]bool
	lg      ingest.Logger
}

// newProxyHandler builds the handler for one listener. lg is required: every
// path through the handler reports problems by logging them, so a nil logger
// would silently discard the only signal an operator gets. Callers with no
// interest in the output (tests, mostly) pass log.NewDiscardLogger().
func newProxyHandler(name string, cfg *listener, proto protocol.Protocol, tag entry.EntryTag,
	pproc *processors.ProcessorSet, sessions *sessionStore, lg ingest.Logger) (*proxyHandler, error) {
	if lg == nil {
		return nil, errors.New("nil logger")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Insecure_Skip_TLS_Verify_Upstream {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	// The tap has to read the upstream body, so the upstream hop is
	// uncompressed: the transport asks for no encoding of its own and
	// forwardUpstream drops whatever the client negotiated (br and zstd we
	// could not decode at all, and gzip would only add latency to a stream we
	// are relaying frame by frame).
	tr.DisableCompression = true
	tr.ResponseHeaderTimeout = 0
	tr.ExpectContinueTimeout = 0
	ph := &proxyHandler{
		name:     name,
		cfg:      cfg,
		proto:    proto,
		tag:      tag,
		pproc:    pproc,
		sessions: sessions,
		upstream: &http.Client{Transport: tr},
		pathSet:  make(map[string]bool),
		passSet:  make(map[string]bool),
		lg:       lg,
	}
	for _, p := range proto.Paths() {
		ph.pathSet[p] = true
	}
	for _, p := range proto.PassthroughPaths() {
		ph.passSet[p] = true
	}
	if cfg.Allow_Unknown_Paths {
		// Loud on purpose: this turns the listener into an open relay for any
		// path, and a request the protocol module never sees still gets the
		// configured upstream credential attached to it.
		lg.Warn("listener forwards unrecognized paths, upstream credential is exposed to them",
			log.KV("listener", name), log.KV("allow_unknown_paths", true))
	}
	return ph, nil
}

func (h *proxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Gate inbound requests when a client credential is configured, before any
	// other handling and before reading the (potentially large) body. The
	// header depends on the listener's auth style (Authorization for bearer,
	// x-api-key for Anthropic).
	if want := h.cfg.ClientAuthHeader(); want != "" && r.Header.Get(h.cfg.AuthHeaderName()) != want {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !h.pathSet[r.URL.Path] {
		h.serveUnparsedPath(w, r)
		return
	}
	started := time.Now()
	ec := &emitCtx{
		tag:          h.tag,
		pproc:        h.pproc,
		listenerName: h.name,
		protocolName: h.proto.Name(),
		logMode:      h.cfg.Log_Mode,
		logToolCalls: h.cfg.Log_Tool_Calls,
		logUsage:     h.cfg.Log_Usage,
		clientIP:     getRemoteIP(r),
		lg:           h.lg,
	}

	body, ok := h.readBody(w, r)
	if !ok {
		return
	}

	parsedReq, err := h.proto.ParseRequest(body, r.Header.Get("Authorization"))
	if err != nil {
		// Forward anyway — the upstream is the source of truth for protocol
		// errors. Just log that we couldn't extract events.
		h.lg.Warn("failed to parse request body, forwarding without ingest",
			log.KV("listener", h.name), log.KVErr(err))
		h.forwardWithoutIngest(w, r, body)
		return
	}
	ec.model = parsedReq.Model
	ec.stream = parsedReq.Stream

	// Sessions are partitioned by client IP
	sessionID, newSession := h.resolveSession(r, ec.clientIP.String(), parsedReq.MessageHashes)
	ec.sessionID = sessionID
	ec.newSession = newSession

	// Emit request-side events before contacting upstream.
	emitRequestEvents(ec, parsedReq.Events)

	resp, err := h.forwardUpstream(r, body)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		h.lg.Warn("upstream request failed", log.KV("listener", h.name), log.KVErr(err))
		return
	}
	defer resp.Body.Close()
	ec.upstreamCode = resp.StatusCode

	copyResponseHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)

	if resp.StatusCode >= 400 {
		// Pipe the error body straight through; do not try to parse.
		if _, err := io.Copy(w, resp.Body); err != nil {
			h.lg.Warn("copy upstream error body", log.KVErr(err))
		}
		h.lg.Warn("upstream returned error status",
			log.KV("listener", h.name), log.KV("status", resp.StatusCode))
		return
	}

	// forwardUpstream asks for an identity encoding; an upstream that
	// compresses anyway hands us bytes the protocol parser cannot read. Relay
	// them untouched instead of feeding it garbage.
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		h.lg.Warn("upstream response is encoded, forwarding without ingest",
			log.KV("listener", h.name), log.KV("content-encoding", enc))
		if _, err := io.Copy(w, resp.Body); err != nil {
			h.lg.Warn("copy encoded upstream body", log.KVErr(err))
		}
		return
	}

	if isEventStream(resp.Header.Get("Content-Type")) {
		h.handleStream(w, resp, ec, started)
	} else {
		h.handleBuffered(w, resp, ec, started)
	}
}

// serveUnparsedPath handles a request on a path the protocol module does not
// parse. A provider's API is wider than the one endpoint we understand — the
// Messages API has /v1/messages/count_tokens next to /v1/messages, and Claude
// Code calls it — so each protocol module declares those siblings as
// passthroughs and we forward them untouched, simply without ingesting.
//
// Anything else is refused by default. Forwarding an arbitrary path would
// attach the configured upstream credential to a request we know nothing
// about, so an operator who wants that has to ask for it with
// Allow_Unknown_Paths.
func (h *proxyHandler) serveUnparsedPath(w http.ResponseWriter, r *http.Request) {
	if !h.passSet[r.URL.Path] && !h.cfg.Allow_Unknown_Paths {
		h.lg.Warn("rejecting request on unrecognized path",
			log.KV("listener", h.name), log.KV("path", r.URL.Path))
		http.NotFound(w, r)
		return
	}
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	h.forwardWithoutIngest(w, r, body)
}

// readBody reads the request body subject to the configured limit, answering
// the client and returning false when it cannot be used.
func (h *proxyHandler) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(h.cfg.Max_Body)+1))
	r.Body.Close()
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		h.lg.Warn("failed to read request body", log.KV("listener", h.name), log.KVErr(err))
		return nil, false
	}
	if len(body) > h.cfg.Max_Body {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		h.lg.Warn("request body too large",
			log.KV("listener", h.name), log.KV("max_body", h.cfg.Max_Body))
		return nil, false
	}
	return body, true
}

// resolveSession decides which session a request belongs to. When the listener
// names a session header and the request carries a usable value, the client is
// telling us which conversation this is and we take its word for it; otherwise
// we derive the session by matching the message prefix.
//
// Configuring the header and then not getting one is worth saying out loud:
// either the client is not the one the operator configured for, or the header
// name is wrong. Prefix matching still covers it, but silently degrading hides
// a misconfiguration that costs session accuracy on every request.
func (h *proxyHandler) resolveSession(r *http.Request, client string, hashes []string) (string, bool) {
	if hdr := h.cfg.Session_ID_Header; hdr != "" {
		raw := r.Header.Get(hdr)
		switch id := sanitizeSessionID(raw); {
		case id != "":
			return h.sessions.ResolveExplicit(client, id)
		case raw == "":
			h.lg.Warn("session id header configured but absent, falling back to prefix matching",
				log.KV("listener", h.name), log.KV("header", hdr))
		default:
			// Deliberately does not log the value: it is client-controlled and
			// we just decided it was not fit to keep.
			h.lg.Warn("session id header value rejected, falling back to prefix matching",
				log.KV("listener", h.name), log.KV("header", hdr), log.KV("length", len(raw)))
		}
	}
	return h.sessions.Resolve(client, hashes)
}

// forwardUpstream rewrites the request to target the configured upstream and
// executes it. The original body must be re-readable so we hand a fresh reader.
func (h *proxyHandler) forwardUpstream(r *http.Request, body []byte) (*http.Response, error) {
	upstream := h.cfg.UpstreamURL()
	outURL := upstream.JoinPath(r.URL.Path)
	outURL.RawQuery = r.URL.RawQuery
	out, err := http.NewRequestWithContext(r.Context(), r.Method, outURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	// Forward all client headers (Content-Type, provider-specific headers,
	// etc.) minus hop-by-hop headers. When an upstream credential is
	// configured it replaces the client's auth header; otherwise the client's
	// auth header passes through unchanged.
	copyRequestHeaders(out, r, h.cfg.AuthHeaderName(), h.cfg.UpstreamAuthHeader())
	// We have to read the upstream body to ingest it, so the client's encoding
	// negotiation does not carry upstream (see the transport setup in
	// newProxyHandler). The response we relay is simply uncompressed.
	out.Header.Del("Accept-Encoding")
	setForwardedFor(out, r)
	// The Anthropic Messages API requires an anthropic-version header. When the
	// proxy injects the upstream key the client never sends one, so supply a
	// configured default if the request lacks it. This path is shared with the
	// OpenAI listeners, so check the protocol explicitly rather than relying on
	// the config validation that already rejects the option elsewhere.
	if v := h.cfg.Anthropic_Version; v != "" && h.proto.Name() == anthropic.ProtocolName &&
		out.Header.Get("anthropic-version") == "" {
		out.Header.Set("anthropic-version", v)
	}
	return h.upstream.Do(out)
}

// forwardWithoutIngest is used when we can't parse the request — still proxy
// the bytes so the client gets a usable response.
func (h *proxyHandler) forwardWithoutIngest(w http.ResponseWriter, r *http.Request, body []byte) {
	resp, err := h.forwardUpstream(r, body)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		h.lg.Warn("upstream request failed", log.KV("listener", h.name), log.KVErr(err))
		return
	}
	defer resp.Body.Close()
	copyResponseHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		h.lg.Warn("copy upstream body (unparseable request)", log.KVErr(err))
	}
}

// handleBuffered reads the entire response body, parses it, then writes it to
// the client in one shot.
func (h *proxyHandler) handleBuffered(w http.ResponseWriter, resp *http.Response, ec *emitCtx, started time.Time) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.lg.Warn("read upstream body", log.KV("listener", h.name), log.KVErr(err))
		return
	}
	if _, err := w.Write(body); err != nil {
		h.lg.Warn("write client body", log.KVErr(err))
	}
	parsed, err := h.proto.ParseResponse(body)
	if err != nil {
		h.lg.Warn("parse upstream response", log.KV("listener", h.name), log.KVErr(err))
		return
	}
	ec.requestID = parsed.RequestID
	if parsed.Model != "" {
		ec.model = parsed.Model
	}
	ec.durationMs = time.Since(started).Milliseconds()
	emitResponseEvents(ec, parsed.Events)
}

// handleStream pipes the upstream SSE response to the client while feeding a
// reassembler in parallel. After the upstream stream completes, the assembled
// response is parsed and emitted.
func (h *proxyHandler) handleStream(w http.ResponseWriter, resp *http.Response, ec *emitCtx, started time.Time) {
	flusher, _ := w.(http.Flusher)
	reass := h.proto.NewStreamReassembler()
	buf := make([]byte, 32*1024)
	var streamErr error
streamLoop:
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if _, werr := w.Write(chunk); werr != nil {
				streamErr = werr
				break streamLoop
			}
			if flusher != nil {
				flusher.Flush()
			}
			if ferr := reass.Feed(bytes.Clone(chunk)); ferr != nil {
				h.lg.Warn("stream parse error",
					log.KV("listener", h.name), log.KVErr(ferr))
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				streamErr = rerr
			}
			break streamLoop
		}
	}
	if streamErr != nil {
		h.lg.Warn("stream io error", log.KV("listener", h.name), log.KVErr(streamErr))
	}
	parsed, err := reass.Finalize()
	if err != nil {
		h.lg.Warn("finalize stream", log.KV("listener", h.name), log.KVErr(err))
		return
	}
	ec.requestID = parsed.RequestID
	if parsed.Model != "" {
		ec.model = parsed.Model
	}
	ec.durationMs = time.Since(started).Milliseconds()
	emitResponseEvents(ec, parsed.Events)
}

func isEventStream(ct string) bool {
	if ct == "" {
		return false
	}
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = ct[:i]
	}
	return strings.EqualFold(strings.TrimSpace(ct), "text/event-stream")
}

// hopByHopHeaders are not forwarded between client/server hops (RFC 7230).
var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Proxy-Connection":    {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

// connectionHeaders returns the set of header names named in the source's
// Connection header. Per RFC 7230 §6.1 these are hop-by-hop for this connection
// and must not be forwarded, in addition to the well-known hopByHopHeaders.
// This mirrors net/http/httputil.ReverseProxy's handling.
func connectionHeaders(h http.Header) map[string]struct{} {
	var extra map[string]struct{}
	for _, f := range h["Connection"] {
		for sf := range strings.SplitSeq(f, ",") {
			if sf = http.CanonicalHeaderKey(strings.TrimSpace(sf)); sf != "" {
				if extra == nil {
					extra = make(map[string]struct{})
				}
				extra[sf] = struct{}{}
			}
		}
	}
	return extra
}

// credentialHeaders are every header an upstream might accept as an API
// credential. When the proxy injects its own key it must drop all of them, not
// just the one this listener's Auth-Style names: the Anthropic Messages API
// honours both "x-api-key" and "Authorization: Bearer", so a client that sets
// the one we are not replacing would have its own credential forwarded
// alongside ours and could bill the request to an account of its choosing.
var credentialHeaders = map[string]struct{}{
	http.CanonicalHeaderKey(bearerHeaderName): {},
	http.CanonicalHeaderKey(apiKeyHeaderName): {},
}

// copyRequestHeaders copies client headers to the upstream request, dropping
// hop-by-hop headers. When upstreamAuth is non-empty every client-supplied
// credential header is dropped (see credentialHeaders) and the one named by
// authHeaderName is set to upstreamAuth; when empty the client's own auth
// headers pass through untouched.
func copyRequestHeaders(dst, src *http.Request, authHeaderName, upstreamAuth string) {
	connDrop := connectionHeaders(src.Header)
	for k, vs := range src.Header {
		ck := http.CanonicalHeaderKey(k)
		if _, drop := hopByHopHeaders[ck]; drop {
			continue
		}
		if _, drop := connDrop[ck]; drop {
			continue
		}
		if upstreamAuth != "" {
			if _, drop := credentialHeaders[ck]; drop {
				continue // replaced below with the configured upstream credential
			}
		}
		for _, v := range vs {
			dst.Header.Add(k, v)
		}
	}
	if upstreamAuth != "" {
		dst.Header.Set(authHeaderName, upstreamAuth)
	}
}

func copyResponseHeaders(w http.ResponseWriter, resp *http.Response) {
	connDrop := connectionHeaders(resp.Header)
	for k, vs := range resp.Header {
		ck := http.CanonicalHeaderKey(k)
		if _, drop := hopByHopHeaders[ck]; drop {
			continue
		}
		if _, drop := connDrop[ck]; drop {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
}

// setForwardedFor appends the immediate peer's IP to the X-Forwarded-For chain,
// preserving any chain the client already sent (copyRequestHeaders carries it
// over). This keeps the real client visible to the upstream instead of
// clobbering the header with a possibly-empty inbound value.
func setForwardedFor(out, r *http.Request) {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	if peer == "" {
		return
	}
	if prior := out.Header.Get("X-Forwarded-For"); prior != "" {
		out.Header.Set("X-Forwarded-For", prior+", "+peer)
	} else {
		out.Header.Set("X-Forwarded-For", peer)
	}
}

func getRemoteIP(r *http.Request) net.IP {
	// Prefer the leading X-Forwarded-For entry (the original client), but only
	// when it actually parses. A present-but-malformed value (spoofed or
	// mangled) must not be trusted — falling back to it would collapse many
	// distinct clients onto 127.0.0.1, breaking session isolation and SRC
	// attribution. In that case, and when the header is absent, use the
	// immediate peer from RemoteAddr instead.
	host := r.Header.Get("X-Forwarded-For")
	if i := strings.IndexByte(host, ','); i >= 0 {
		host = host[:i]
	}
	if ip := net.ParseIP(strings.TrimSpace(host)); ip != nil {
		return ip
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	if ip := net.ParseIP(strings.TrimSpace(peer)); ip != nil {
		return ip
	}
	return net.ParseIP("127.0.0.1")
}
