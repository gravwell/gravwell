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
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol"
)

// proxyHandler is the http.Handler for one [Listener] entry.
type proxyHandler struct {
	name     string
	cfg      *listener
	proto    protocol.Protocol
	tag      entry.EntryTag
	pproc    *processors.ProcessorSet
	sessions *sessionStore
	upstream *http.Client
	pathSet  map[string]struct{}
	lg       *log.Logger
}

func newProxyHandler(name string, cfg *listener, proto protocol.Protocol, tag entry.EntryTag,
	pproc *processors.ProcessorSet, sessions *sessionStore, lg *log.Logger) *proxyHandler {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Insecure_Skip_TLS_Verify_Upstream {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	// Streaming requires that we observe bytes as they arrive.
	tr.DisableCompression = false
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
		pathSet:  map[string]struct{}{},
		lg:       lg,
	}
	for _, p := range proto.Paths() {
		ph.pathSet[p] = struct{}{}
	}
	return ph
}

func (h *proxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.pathSet[r.URL.Path]; !ok {
		http.NotFound(w, r)
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

	body, err := io.ReadAll(io.LimitReader(r.Body, int64(h.cfg.Max_Body)+1))
	r.Body.Close()
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		emitError(ec, "read_body", err)
		return
	}
	if len(body) > h.cfg.Max_Body {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		emitError(ec, "body_too_large", fmt.Errorf("body exceeds %d bytes", h.cfg.Max_Body))
		return
	}

	parsedReq, err := h.proto.ParseRequest(body, r.Header.Get("Authorization"))
	if err != nil {
		// Forward anyway — the upstream is the source of truth for protocol
		// errors. Just log that we couldn't extract events.
		if h.lg != nil {
			h.lg.Warn("failed to parse request body, forwarding without ingest",
				log.KV("listener", h.name), log.KVErr(err))
		}
		h.forwardWithoutIngest(w, r, body, ec)
		return
	}
	ec.model = parsedReq.Model
	ec.stream = parsedReq.Stream
	ec.apiKeyHash = parsedReq.APIKeyHash

	sessionID, newSession := h.sessions.Resolve(parsedReq.APIKeyHash, parsedReq.MessageHashes)
	ec.sessionID = sessionID
	ec.newSession = newSession

	// Emit request-side events before contacting upstream.
	emitRequestEvents(ec, parsedReq.Events)

	resp, err := h.forwardUpstream(r, body)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		emitError(ec, "upstream_failed", err)
		return
	}
	defer resp.Body.Close()
	ec.upstreamCode = resp.StatusCode

	copyResponseHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)

	if resp.StatusCode >= 400 {
		// Pipe the error body straight through; do not try to parse.
		if _, err := io.Copy(w, resp.Body); err != nil && h.lg != nil {
			h.lg.Warn("copy upstream error body", log.KVErr(err))
		}
		ec.durationMs = time.Since(started).Milliseconds()
		emitError(ec, "upstream_status", fmt.Errorf("upstream returned %d", resp.StatusCode))
		return
	}

	if isEventStream(resp.Header.Get("Content-Type")) {
		h.handleStream(w, resp, ec)
	} else {
		h.handleBuffered(w, resp, ec)
	}
	ec.durationMs = time.Since(started).Milliseconds()
}

// forwardUpstream rewrites the request to target the configured upstream and
// executes it. The original body must be re-readable so we hand a fresh reader.
func (h *proxyHandler) forwardUpstream(r *http.Request, body []byte) (*http.Response, error) {
	upstream := h.cfg.UpstreamURL()
	outURL := *upstream
	outURL.Path = singleJoiningSlash(upstream.Path, r.URL.Path)
	outURL.RawQuery = r.URL.RawQuery
	out, err := http.NewRequestWithContext(r.Context(), r.Method, outURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	// Forward all client headers (Content-Type, provider-specific headers,
	// etc.) minus hop-by-hop headers, honoring the Redact-Authorization flag.
	copyRequestHeaders(out, r, h.cfg.Redact_Authorization)
	setForwardedFor(out, r)
	return h.upstream.Do(out)
}

// forwardWithoutIngest is used when we can't parse the request — still proxy
// the bytes so the client gets a usable response.
func (h *proxyHandler) forwardWithoutIngest(w http.ResponseWriter, r *http.Request, body []byte, ec *emitCtx) {
	resp, err := h.forwardUpstream(r, body)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		emitError(ec, "upstream_failed", err)
		return
	}
	defer resp.Body.Close()
	copyResponseHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil && h.lg != nil {
		h.lg.Warn("copy upstream body (unparseable request)", log.KVErr(err))
	}
}

// handleBuffered reads the entire response body, parses it, then writes it to
// the client in one shot.
func (h *proxyHandler) handleBuffered(w http.ResponseWriter, resp *http.Response, ec *emitCtx) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		emitError(ec, "read_upstream_body", err)
		return
	}
	if _, err := w.Write(body); err != nil && h.lg != nil {
		h.lg.Warn("write client body", log.KVErr(err))
	}
	parsed, err := h.proto.ParseResponse(body)
	if err != nil {
		emitError(ec, "parse_response", err)
		return
	}
	ec.requestID = parsed.RequestID
	if parsed.Model != "" {
		ec.model = parsed.Model
	}
	emitResponseEvents(ec, parsed.Events)
}

// handleStream pipes the upstream SSE response to the client while feeding a
// reassembler in parallel. After the upstream stream completes, the assembled
// response is parsed and emitted.
func (h *proxyHandler) handleStream(w http.ResponseWriter, resp *http.Response, ec *emitCtx) {
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
			if ferr := reass.Feed(append([]byte(nil), chunk...)); ferr != nil && h.lg != nil {
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
		emitError(ec, "stream_io", streamErr)
	}
	parsed, err := reass.Finalize()
	if err != nil {
		emitError(ec, "finalize_stream", err)
		return
	}
	ec.requestID = parsed.RequestID
	if parsed.Model != "" {
		ec.model = parsed.Model
	}
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
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

func copyRequestHeaders(dst, src *http.Request, redactAuth bool) {
	for k, vs := range src.Header {
		if _, drop := hopByHopHeaders[http.CanonicalHeaderKey(k)]; drop {
			continue
		}
		if redactAuth && http.CanonicalHeaderKey(k) == "Authorization" {
			continue
		}
		for _, v := range vs {
			dst.Header.Add(k, v)
		}
	}
}

func copyResponseHeaders(w http.ResponseWriter, resp *http.Response) {
	for k, vs := range resp.Header {
		if _, drop := hopByHopHeaders[http.CanonicalHeaderKey(k)]; drop {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
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
	host := r.Header.Get("X-Forwarded-For")
	if i := strings.IndexByte(host, ','); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimSpace(host)
	if host == "" {
		host, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip
	}
	return net.ParseIP("127.0.0.1")
}
