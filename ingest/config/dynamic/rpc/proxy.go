/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package rpc

import (
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"

	"github.com/gravwell/gravwell/v4/ingest/log"
)

// Forwarding headers, in the order they are consulted.
//
// The two list headers come first on purpose.  A proxy appends to them, so the rightmost
// entry was written by the hop we are talking to and a client cannot forge it by sending
// a header of its own, it can only prepend entries we then walk past.  The single value
// headers carry no such structure: whatever arrives is the whole value, so a client can
// simply send one, and they are only believed when nothing better is present and only
// from a proxy we already trust to overwrite them.
const (
	hdrForwarded      = `Forwarded`        // RFC 7239, the standard one
	hdrXForwardedFor  = `X-Forwarded-For`  // the de facto standard
	hdrXRealIP        = `X-Real-IP`        // nginx
	hdrCFConnectingIP = `CF-Connecting-IP` // Cloudflare
	hdrTrueClientIP   = `True-Client-IP`   // Akamai, Cloudflare enterprise
)

// listHeaders are appended to by each hop, so the rightmost untrusted entry is the client.
var listHeaders = []string{hdrForwarded, hdrXForwardedFor}

// singleHeaders carry one address and are only consulted when no list header is present.
var singleHeaders = []string{hdrXRealIP, hdrCFConnectingIP, hdrTrueClientIP}

// proxyTrust is the set of load balancers and reverse proxies whose forwarding headers we
// are willing to believe.
//
// Nothing is trusted by default.  A forwarding header is a claim made by whoever is
// talking to us, so honoring one from an arbitrary peer would hand every caller a free
// throttle bypass: send a new X-Forwarded-For per attempt and the table never sees the
// same client twice.  Trust has to be configured, pointing at the proxies that are
// actually in front of this handler.
type proxyTrust struct {
	prefixes []netip.Prefix

	// warn fires once if forwarding headers show up from a peer we do not trust, which
	// is almost always a deployment behind a load balancer that nobody configured.  It
	// is once per server rather than per request so a flood cannot turn it into a log
	// amplifier.
	warn sync.Once
}

// newProxyTrust parses the configured entries, which may be bare addresses or CIDR blocks.
func newProxyTrust(entries []string) (pt *proxyTrust, err error) {
	pt = &proxyTrust{}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == `` {
			continue
		}
		if strings.Contains(e, `/`) {
			var p netip.Prefix
			if p, err = netip.ParsePrefix(e); err != nil {
				err = fmt.Errorf("invalid trusted proxy CIDR %q %w", e, err)
				return
			}
			pt.prefixes = append(pt.prefixes, p.Masked())
			continue
		}
		var a netip.Addr
		if a, err = netip.ParseAddr(e); err != nil {
			err = fmt.Errorf("invalid trusted proxy address %q %w", e, err)
			return
		}
		a = a.Unmap()
		pt.prefixes = append(pt.prefixes, netip.PrefixFrom(a, a.BitLen()))
	}
	return
}

// empty reports whether anything is trusted at all.
func (pt *proxyTrust) empty() bool {
	return pt == nil || len(pt.prefixes) == 0
}

// trusted reports whether an address is one of our proxies.
func (pt *proxyTrust) trusted(a netip.Addr) bool {
	if pt == nil {
		return false
	}
	a = a.Unmap()
	for _, p := range pt.prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// clientAddr resolves the address the throttle should key on.
//
// The peer address is the only thing we know to be real, so it is used unless the peer is
// a proxy we were told to trust.  From a trusted proxy the forwarding headers are walked
// from the right, skipping hops that are themselves trusted, and the first address that
// is not one of ours is the client.  That is what makes prepended entries harmless: a
// client can put anything at the front of the list, we never get that far.
//
// The returned string is a normalized address, so a v4 mapped v6 form and its plain v4
// form key the same entry.
func (pt *proxyTrust) clientAddr(r *http.Request, lgr *log.Logger) string {
	if r == nil {
		return ``
	}
	peer, ok := parseAddrLoose(r.RemoteAddr)
	if !ok {
		// unparseable, fall back to the raw value so it still keys something stable
		return r.RemoteAddr
	}
	if !pt.trusted(peer) {
		if pt.empty() && hasForwardingHeader(r) {
			pt.warn.Do(func() {
				lgr.Warn("dynamic rpc saw forwarding headers but no trusted proxies are configured, throttling by the peer address",
					log.KV("remote", peer.String()))
			})
		}
		return peer.String()
	}

	for _, h := range listHeaders {
		values := r.Header.Values(h)
		if len(values) == 0 {
			continue
		}
		var chain []netip.Addr
		for _, v := range values {
			if h == hdrForwarded {
				chain = append(chain, parseForwarded(v)...)
			} else {
				chain = append(chain, parseAddrList(v)...)
			}
		}
		// rightmost entry that is not one of our own proxies
		for _, c := range slices.Backward(chain) {
			if !pt.trusted(c) {
				return c.String()
			}
		}
		// every hop in the chain is ours, the leftmost is as far back as we can see
		if len(chain) > 0 {
			return chain[0].String()
		}
	}

	for _, h := range singleHeaders {
		if a, ok := parseAddrLoose(r.Header.Get(h)); ok {
			return a.String()
		}
	}

	return peer.String()
}

// hasForwardingHeader reports whether the request carries any header we would have used
// had the peer been trusted.
func hasForwardingHeader(r *http.Request) bool {
	for _, h := range append(append([]string{}, listHeaders...), singleHeaders...) {
		if r.Header.Get(h) != `` {
			return true
		}
	}
	return false
}

// parseAddrList splits a comma separated address list, as X-Forwarded-For carries.
func parseAddrList(v string) (r []netip.Addr) {
	for part := range strings.SplitSeq(v, `,`) {
		if a, ok := parseAddrLoose(part); ok {
			r = append(r, a)
		}
	}
	return
}

// parseForwarded pulls the for= parameters out of an RFC 7239 Forwarded header, in order.
// Elements are comma separated and each carries semicolon separated parameters, so
// "for=192.0.2.60;proto=http, for=\"[2001:db8::1]:4711\"" yields two addresses.
func parseForwarded(v string) (r []netip.Addr) {
	for element := range strings.SplitSeq(v, `,`) {
		for param := range strings.SplitSeq(element, `;`) {
			k, val, found := strings.Cut(param, `=`)
			if !found || !strings.EqualFold(strings.TrimSpace(k), `for`) {
				continue
			}
			if a, ok := parseAddrLoose(val); ok {
				r = append(r, a)
			}
		}
	}
	return
}

// parseAddrLoose accepts the shapes an address turns up in across these headers: bare,
// with a port, bracketed, bracketed with a port, and quoted as RFC 7239 allows.
func parseAddrLoose(v string) (a netip.Addr, ok bool) {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, `"`)
	v = strings.TrimSpace(v)
	if v == `` {
		return
	}
	// RFC 7239 obfuscated and unknown identifiers are not addresses
	if strings.HasPrefix(v, `_`) || strings.EqualFold(v, `unknown`) {
		return
	}
	if p, err := netip.ParseAddr(v); err == nil {
		return p.Unmap(), true
	}
	if ap, err := netip.ParseAddrPort(v); err == nil {
		return ap.Addr().Unmap(), true
	}
	if strings.HasPrefix(v, `[`) && strings.HasSuffix(v, `]`) {
		if p, err := netip.ParseAddr(v[1 : len(v)-1]); err == nil {
			return p.Unmap(), true
		}
	}
	return
}

// ipv6ThrottlePrefix is how much of an IPv6 address the throttle keys on.
//
// A single customer is normally handed a /64 or larger, so the low 64 bits are theirs to
// roll through at no cost.  Keying on the prefix means an address rolling flood has to
// move between allocations rather than between addresses, which is a far harder thing to
// come by, and it costs a legitimate client nothing: everything behind one allocation is
// one client's worth of traffic anyway.
//
// IPv4 is keyed on the whole address, there is no equivalent slack to take away.
const ipv6ThrottlePrefix = 64

// throttleKey reduces a client address to the key the throttle counts against.
//
// This is deliberately separate from working out who the client is.  clientAddr answers
// "which address is this", which is what belongs in a log line, and this answers "what do
// we hold responsible", which is coarser on purpose.
func throttleKey(addr string) string {
	a, err := netip.ParseAddr(addr)
	if err != nil {
		return addr // not something we can reason about, key it verbatim
	}
	// a zone is a property of the local interface rather than of the peer, and carrying
	// it would let the same peer key two entries
	a = a.WithZone(``).Unmap()
	if a.Is4() {
		return a.String()
	}
	p, err := a.Prefix(ipv6ThrottlePrefix)
	if err != nil {
		return a.String() // cannot mask it, fall back to the whole address
	}
	return p.String()
}
