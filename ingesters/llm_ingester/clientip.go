/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/utils/trustedproxy"
	"golang.org/x/time/rate"
)

// legacyWarnInterval is the minimum time between legacy X-Forwarded-For warnings.
const legacyWarnInterval = time.Minute

// clientResolver decides how the client of a request is found.
//
// With Trusted-Proxy set, X-Forwarded-For is used only from a listed peer. With
// no Trusted-Proxy, the legacy behavior trusts the header from any peer unless
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

// clientSource resolves the client of each request for every listener. The
// legacy warning is rate limited across all listeners, so one clientSource is
// shared by all of them.
type clientSource struct {
	resolver   clientResolver // set once at startup, read without a lock
	lg         ingest.Logger
	legacyWarn rate.Sometimes // rate limits the legacy X-Forwarded-For warning
}

func newClientSource(cr clientResolver, lg ingest.Logger) (*clientSource, error) {
	if lg == nil {
		return nil, errors.New("nil logger")
	}

	return &clientSource{
		resolver:   cr,
		lg:         lg,
		legacyWarn: rate.Sometimes{Interval: legacyWarnInterval},
	}, nil
}

// client resolves the client of r with the configured resolver.
func (cs *clientSource) client(r *http.Request) trustedproxy.Client {
	c := cs.resolver.resolve(r)
	if cs.resolver.legacy && c.Addr != c.Peer {
		cs.warnLegacyForwarded(c)
	}

	return c
}

// warnLegacyForwarded logs that the client address came from an X-Forwarded-For
// header sent by an unlisted peer. It logs at most once per legacyWarnInterval.
func (cs *clientSource) warnLegacyForwarded(c trustedproxy.Client) {
	// decide under the Sometimes lock but log after it, so a slow log write
	// does not hold up other requests
	var fire bool
	cs.legacyWarn.Do(func() { fire = true })
	if !fire {
		return
	}

	cs.lg.Warn("using X-Forwarded-For from a peer that is not a trusted proxy, "+
		"this default changes in the next major release, "+
		"set Trusted-Proxy or Ignore-Untrusted-Forwarded-For",
		clientKVs("src", c)...)
}

// clientIP returns the address used as the entry SRC and the session key. It
// falls back to 127.0.0.1 only when no address could be found.
func clientIP(c trustedproxy.Client) net.IP {
	if ip := c.IP(); ip != nil {
		return ip
	}

	return net.IPv4(127, 0, 0, 1)
}

// clientKVs returns the client address under name, the same address used as the
// entry SRC. When the request carried X-Forwarded-For, or the peer did not parse,
// it also returns the TCP peer. When there is an X-Forwarded-For header, the raw
// header is logged under xff for context only, it is not the SRC or the session
// key.
func clientKVs(name string, c trustedproxy.Client) []rfc5424.SDParam {
	kvs := []rfc5424.SDParam{log.KV(name, clientIP(c).String())}
	if !c.HasForwarded() {
		if !c.Addr.IsValid() {
			kvs = append(kvs, log.KV("peer", peerString(c)))
		}
		return kvs
	}

	kvs = append(kvs, log.KV("peer", peerString(c)), log.KV("xff", c.Forwarded()))
	if c.Malformed {
		kvs = append(kvs, log.KV("xff-malformed", true))
	}

	return kvs
}

// srcKVs returns the client KVs of a request under the name src, followed by
// extra, for log lines about that request.
func srcKVs(c trustedproxy.Client, extra ...rfc5424.SDParam) []rfc5424.SDParam {
	return append(clientKVs(`src`, c), extra...)
}

// peerString returns the TCP peer for logging, or RemoteAddr verbatim when it
// did not parse.
func peerString(c trustedproxy.Client) string {
	if c.Peer.IsValid() {
		return c.Peer.String()
	}

	return c.PeerRaw
}
