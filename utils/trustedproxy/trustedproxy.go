/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package trustedproxy resolves the client address of an HTTP request through a
// configured list of trusted reverse proxies and load balancers.
//
// The X-Forwarded-For header is used only when the TCP peer is a trusted proxy.
// The header is then walked from the right, skipping trusted proxies, and the
// first address that is not a trusted proxy is the client.
package trustedproxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

const (
	// maxHops caps how many X-Forwarded-For entries are walked, counted from the right.
	maxHops = 32

	// maxForwardedLen caps the length of Client.Forwarded.
	maxForwardedLen = 1024

	forwardedHeader = `X-Forwarded-For`
)

var (
	errTrustAll     = errors.New("a prefix length of 0 would trust every address")
	errSpansMapped  = errors.New("an IPv6 block that spans ::ffff:0:0/96 never matches IPv4 peers, list IPv4 ranges in IPv4 form")
	ipv4MappedRange = netip.MustParsePrefix(`::ffff:0:0/96`)
)

// List is a parsed list of trusted proxy addresses and CIDR blocks.
// The zero value is an empty list that trusts nothing.
type List struct {
	prefixes []netip.Prefix
}

// Parse parses a list of IPv4 or IPv6 addresses and CIDR blocks.
//
// Entries are trimmed and blank entries are skipped. A bare address becomes a
// single host prefix, a CIDR block is masked to its network address, and
// IPv4-mapped IPv6 entries are stored in their IPv4 form. An entry with a prefix
// length of 0 is rejected. Any other IPv6 block that overlaps the IPv4-mapped
// range ::ffff:0:0/96 is rejected too, since peers are matched in IPv4 form and
// could never match it. Errors name the offending entry.
func Parse(entries []string) (List, error) {
	var l List

	for _, e := range entries {
		if e = strings.TrimSpace(e); e == `` {
			continue
		}

		pfx, err := parseEntry(e)
		if err != nil {
			return List{}, fmt.Errorf("invalid trusted proxy %q: %w", e, err)
		}

		l.prefixes = append(l.prefixes, pfx)
	}

	return l, nil
}

func parseEntry(e string) (netip.Prefix, error) {
	if !strings.Contains(e, `/`) {
		addr, err := netip.ParseAddr(e)
		if err != nil {
			return netip.Prefix{}, err
		}

		addr = addr.Unmap().WithZone(``)

		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}

	pfx, err := netip.ParsePrefix(e)
	if err != nil {
		return netip.Prefix{}, err
	}

	// ::ffff:a.b.c.d/n covers IPv4 space, store it as a.b.c.d/(n-96) so that
	// unmapped IPv4 peers match it.
	if addr := pfx.Addr(); addr.Is4In6() && pfx.Bits() >= 96 {
		pfx = netip.PrefixFrom(addr.Unmap(), pfx.Bits()-96)
	}

	if pfx.Bits() == 0 {
		return netip.Prefix{}, errTrustAll
	}
	if pfx.Addr().Is6() && pfx.Overlaps(ipv4MappedRange) {
		return netip.Prefix{}, errSpansMapped
	}

	return pfx.Masked(), nil
}

// Empty reports whether the list has no entries.
func (l List) Empty() bool {
	return len(l.prefixes) == 0
}

// Contains reports whether a falls inside any entry. An IPv4-mapped IPv6 address
// is compared in its IPv4 form, and an IPv6 zone is ignored.
func (l List) Contains(a netip.Addr) bool {
	a = a.Unmap().WithZone(``)

	for _, pfx := range l.prefixes {
		if pfx.Contains(a) {
			return true
		}
	}

	return false
}

// IsTrustedPeer reports whether the TCP peer of r is in the list. It reads only
// r.RemoteAddr and never consults a forwarding header.
func (l List) IsTrustedPeer(r *http.Request) bool {
	if r == nil {
		return false
	}

	peer, ok := parseHost(r.RemoteAddr)
	if !ok {
		return false
	}

	return l.Contains(peer)
}

// Client is the resolved client of a request.
type Client struct {
	// Addr is the resolved client address. It is invalid only when the peer
	// address could not be parsed.
	Addr netip.Addr

	// Peer is the TCP peer address taken from RemoteAddr.
	Peer netip.Addr

	// PeerRaw is RemoteAddr verbatim.
	PeerRaw string

	// Forwarded is every X-Forwarded-For value joined with ", ", truncated for
	// logging. It is recorded whether or not the peer is trusted.
	Forwarded string

	// Malformed reports that the walk stopped at an X-Forwarded-For entry that is
	// not an address. Addr is then the nearest trusted hop to its right.
	Malformed bool
}

// Resolve returns the client of r.
//
// When the TCP peer is not in the list, the peer is the client. Otherwise the
// X-Forwarded-For entries are walked from the right, at most 32 of them, and the
// first entry that is not a trusted proxy is the client. If every walked entry is
// trusted the leftmost one is the client, and if there are no entries the peer is
// the client. X-Real-IP and Forwarded are not read.
func (l List) Resolve(r *http.Request) Client {
	if r == nil {
		return Client{}
	}

	peer, ok := parseHost(r.RemoteAddr)
	if !ok {
		return Client{PeerRaw: r.RemoteAddr}
	}

	values := r.Header.Values(forwardedHeader)

	c := Client{
		Addr:      peer,
		Peer:      peer,
		PeerRaw:   r.RemoteAddr,
		Forwarded: joinForwarded(values),
	}

	if !l.Contains(peer) {
		return c
	}

	var hops [maxHops]string
	n := lastEntries(values, hops[:])

	for _, e := range hops[:n] {
		a, ok := parseHost(e)
		if !ok {
			c.Malformed = true
			return c
		}

		if !l.Contains(a) {
			c.Addr = a
			return c
		}

		c.Addr = a
	}

	return c
}

// ResolveLegacy returns the client of r by trusting X-Forwarded-For from any peer.
// The client is the leftmost entry that is an address, at most 32 entries in. When
// no entry is an address the client is the peer, and Malformed reports that an
// entry was skipped.
//
// The header is client-controlled, so the result can be any address the sender
// chooses. ResolveLegacy exists only to keep the behavior of ingesters that
// trusted the header before trusted proxies were configurable. Never use it for
// a security decision. It will be removed in the next major release.
func ResolveLegacy(r *http.Request) Client {
	if r == nil {
		return Client{}
	}

	values := r.Header.Values(forwardedHeader)

	c := Client{
		PeerRaw:   r.RemoteAddr,
		Forwarded: joinForwarded(values),
	}
	if peer, ok := parseHost(r.RemoteAddr); ok {
		c.Addr = peer
		c.Peer = peer
	}

	var hops [maxHops]string
	n := firstEntries(values, hops[:])

	for _, e := range hops[:n] {
		if a, ok := parseHost(e); ok {
			c.Addr = a
			return c
		}

		c.Malformed = true
	}

	return c
}

// String returns the client address, falling back to the host part of PeerRaw
// and then to PeerRaw itself when the peer could not be parsed.
func (c Client) String() string {
	if c.Addr.IsValid() {
		return c.Addr.String()
	}

	if host, _, err := net.SplitHostPort(c.PeerRaw); err == nil {
		return host
	}

	return c.PeerRaw
}

// IP returns the client address as a net.IP, or nil when Addr is invalid.
func (c Client) IP() net.IP {
	if !c.Addr.IsValid() {
		return nil
	}

	return net.IP(c.Addr.AsSlice())
}

// parseHost parses an address with an optional port: 1.2.3.4, 1.2.3.4:5, ::1,
// [::1] or [::1]:443. IPv4-mapped IPv6 addresses are returned in their IPv4 form.
func parseHost(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)

	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap(), true
	}

	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap(), true
	}

	if len(s) > 2 && s[0] == '[' && s[len(s)-1] == ']' {
		if a, err := netip.ParseAddr(s[1 : len(s)-1]); err == nil {
			return a.Unmap(), true
		}
	}

	return netip.Addr{}, false
}

// lastEntries fills dst with the non-blank comma-separated entries of values,
// starting from the rightmost, and returns how many it stored. It stops when dst
// is full, so an oversized header is never split in full.
func lastEntries(values []string, dst []string) (n int) {
	for _, v := range slices.Backward(values) {
		for n < len(dst) {
			var e string
			if idx := strings.LastIndexByte(v, ','); idx >= 0 {
				e, v = v[idx+1:], v[:idx]
			} else {
				e, v = v, ``
			}

			if e = strings.TrimSpace(e); e != `` {
				dst[n] = e
				n++
			}

			if v == `` {
				break
			}
		}

		if n == len(dst) {
			break
		}
	}

	return n
}

// firstEntries is lastEntries from the left: it fills dst with the non-blank
// comma-separated entries of values, starting from the leftmost.
func firstEntries(values []string, dst []string) (n int) {
	for _, v := range values {
		for n < len(dst) {
			var e string
			if idx := strings.IndexByte(v, ','); idx >= 0 {
				e, v = v[:idx], v[idx+1:]
			} else {
				e, v = v, ``
			}

			if e = strings.TrimSpace(e); e != `` {
				dst[n] = e
				n++
			}

			if v == `` {
				break
			}
		}

		if n == len(dst) {
			break
		}
	}

	return n
}

// joinForwarded joins the X-Forwarded-For values for logging, truncated to
// maxForwardedLen bytes.
func joinForwarded(values []string) string {
	// build the string up to the limit rather than joining the whole header,
	// which can be as large as the server's header limit
	var sb strings.Builder
	write := func(v string) bool {
		if rem := maxForwardedLen - sb.Len(); len(v) > rem {
			sb.WriteString(v[:rem])
			sb.WriteString(`...`)
			return false
		}
		sb.WriteString(v)
		return true
	}

	for i, v := range values {
		if i > 0 && !write(`, `) {
			break
		}
		if !write(v) {
			break
		}
	}

	return sb.String()
}
