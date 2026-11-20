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

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/ingesters/utils/clientip"
	"github.com/gravwell/gravwell/v3/utils/trustedproxy"
)

type clientCtxKey struct{}

// setResolver swaps in a new resolver. Requests read it without taking the
// handler lock.
func (h *handler) setResolver(cr clientip.Resolver) {
	h.resolver.Store(&cr)
}

// client resolves the client of r with the configured resolver. A handler with
// no resolver set trusts no proxies.
func (h *handler) client(r *http.Request) trustedproxy.Client {
	var cr clientip.Resolver
	if p := h.resolver.Load(); p != nil {
		cr = *p
	}

	return cr.Resolve(r, h.legacyWarn)
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
// to its TCP peer. That fallback exists only for the health check and throttled
// 429 paths, which answer before the client is attached. Any other use of it is
// a bug.
func requestClient(r *http.Request) trustedproxy.Client {
	if c, ok := clientFromContext(r); ok {
		return c
	}

	return trustedproxy.List{}.Resolve(r)
}

// requestIP returns the entry SRC for r, the same address srcKVs logs.
func requestIP(r *http.Request) net.IP {
	return clientip.IP(requestClient(r))
}

// srcKVs returns the client KVs of r under the name src, followed by extra.
func srcKVs(r *http.Request, extra ...rfc5424.SDParam) []rfc5424.SDParam {
	return append(clientip.KVs(`src`, requestClient(r)), extra...)
}
