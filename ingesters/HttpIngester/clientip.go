/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/utils/trustedproxy"
)

// legacyWarnInterval is the minimum time between legacy X-Forwarded-For warnings.
const legacyWarnInterval = time.Minute

// clientResolver decides how the client of a request is found.
//
// With Trusted-Proxies set, X-Forwarded-For is used only from a listed peer. With
// no Trusted-Proxies, the legacy behavior trusts the header from any peer unless
// Ignore-Untrusted-Forwarded-For is set, in which case the client is the peer.
type clientResolver struct {
	trusted trustedproxy.List
	legacy  bool
}

func (cr clientResolver) resolve(r *http.Request) trustedproxy.Client {
	if cr.legacy {
		return trustedproxy.ResolveLegacy(r)
	}

	return cr.trusted.Resolve(r)
}

type clientCtxKey struct{}

// setResolver swaps in a new resolver. Requests read it without taking the
// handler lock.
func (h *handler) setResolver(cr clientResolver) {
	h.resolver.Store(&cr)
}

// client resolves the client of r with the configured resolver. A handler with
// no resolver set trusts no proxies.
func (h *handler) client(r *http.Request) trustedproxy.Client {
	var cr clientResolver
	if p := h.resolver.Load(); p != nil {
		cr = *p
	}

	c := cr.resolve(r)
	if cr.legacy && c.Addr != c.Peer {
		h.warnLegacyForwarded(c)
	}

	return c
}

// warnLegacyForwarded logs that the client address came from an X-Forwarded-For
// header sent by an unlisted peer. It logs at most once per legacyWarnInterval.
func (h *handler) warnLegacyForwarded(c trustedproxy.Client) {
	now := time.Now().UnixNano()

	last := h.legacyWarned.Load()
	if last != 0 && now-last < int64(legacyWarnInterval) {
		return
	}
	if !h.legacyWarned.CompareAndSwap(last, now) {
		return
	}

	h.lgr.Warn("using X-Forwarded-For from a peer that is not a trusted proxy, "+
		"this default changes in the next major release, "+
		"set Trusted-Proxies or Ignore-Untrusted-Forwarded-For",
		clientKVs("src", c)...)
}

// attachClient resolves the client of r and attaches it to the returned request
// for the auth handlers and request logging. Middleware wrapping the handler only
// holds the original request, so the client is also recorded on a trackingRW.
func (h *handler) attachClient(w http.ResponseWriter, r *http.Request) (*http.Request, trustedproxy.Client) {
	c := h.client(r)
	if trw, ok := w.(*trackingRW); ok {
		trw.client = &c
	}

	return withClient(r, c), c
}

// withClient attaches a resolved client to r so that auth handlers and request
// logging use the same address as the entry SRC.
func withClient(r *http.Request, c trustedproxy.Client) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), clientCtxKey{}, c))
}

func clientFromContext(r *http.Request) (trustedproxy.Client, bool) {
	c, ok := r.Context().Value(clientCtxKey{}).(trustedproxy.Client)
	return c, ok
}

// requestClient returns the client attached to r. A request without one resolves
// to its TCP peer.
func requestClient(r *http.Request) trustedproxy.Client {
	if c, ok := clientFromContext(r); ok {
		return c
	}

	return trustedproxy.List{}.Resolve(r)
}

// clientIP returns the address used as the entry SRC. It falls back to 127.0.0.1
// only when no address could be found.
func clientIP(c trustedproxy.Client) net.IP {
	if ip := c.IP(); ip != nil {
		return ip
	}

	return net.IPv4(127, 0, 0, 1)
}

// clientKVs returns the client address under name, the same address used as the
// entry SRC. When the request carried X-Forwarded-For, or the peer did not parse,
// it also returns the TCP peer, and the forwarded chain when there is one.
func clientKVs(name string, c trustedproxy.Client) []rfc5424.SDParam {
	kvs := []rfc5424.SDParam{log.KV(name, clientIP(c).String())}
	if c.Forwarded == `` {
		if !c.Addr.IsValid() {
			kvs = append(kvs, log.KV("peer", peerString(c)))
		}
		return kvs
	}

	kvs = append(kvs, log.KV("peer", peerString(c)), log.KV("xff", c.Forwarded))
	if c.Malformed {
		kvs = append(kvs, log.KV("xff-malformed", true))
	}

	return kvs
}

// peerString returns the TCP peer for logging, or RemoteAddr verbatim when it
// did not parse.
func peerString(c trustedproxy.Client) string {
	if c.Peer.IsValid() {
		return c.Peer.String()
	}

	return c.PeerRaw
}

// srcKVs returns the client KVs of r under the name src, followed by extra.
func srcKVs(r *http.Request, extra ...rfc5424.SDParam) []rfc5424.SDParam {
	return append(clientKVs(`src`, requestClient(r)), extra...)
}
