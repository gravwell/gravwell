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
	errAllBlank     = errors.New("every trusted proxy entry is blank")
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
// Entries are trimmed and blank entries are skipped, but a non-empty list in
// which every entry is blank is rejected rather than read as no list. A bare
// address becomes a single host prefix, a CIDR block is masked to its network
// address, and IPv4-mapped IPv6 entries are stored in their IPv4 form. An entry
// with a prefix length of 0 is rejected, as is a list whose entries together
// cover every IPv4 or every IPv6 address. Any other IPv6 block that overlaps the
// IPv4-mapped range ::ffff:0:0/96 is rejected too, since peers are matched in
// IPv4 form and could never match it. An error about a single entry names
// that entry.
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

	if len(entries) > 0 && len(l.prefixes) == 0 {
		return List{}, errAllBlank
	}
	if err := checkCoverage(l.prefixes); err != nil {
		return List{}, err
	}

	return l, nil
}

// checkCoverage rejects prefixes that together cover a whole address family.
// IPv4-mapped peers are matched in IPv4 form, so the IPv6 check counts
// ::ffff:0:0/96 as covered.
func checkCoverage(prefixes []netip.Prefix) error {
	var v4, v6 []netip.Prefix
	for _, pfx := range prefixes {
		if pfx.Addr().Is4() {
			v4 = append(v4, pfx)
		} else {
			v6 = append(v6, pfx)
		}
	}

	if coversAll(v4, netip.IPv4Unspecified()) {
		return errors.New("the trusted proxies together cover every IPv4 address, list only the addresses of your proxies")
	}
	if len(v6) > 0 && coversAll(append(v6, ipv4MappedRange), netip.IPv6Unspecified()) {
		return errors.New("the trusted proxies together cover every IPv6 address, list only the addresses of your proxies")
	}

	return nil
}

// coversAll reports whether the masked prefixes, all of first's family, cover
// every address of that family. first is the lowest address of the family. It
// sorts prefixes in place.
func coversAll(prefixes []netip.Prefix, first netip.Addr) bool {
	slices.SortFunc(prefixes, func(a, b netip.Prefix) int {
		return a.Addr().Compare(b.Addr())
	})

	// next is the lowest address not yet covered
	next := first
	for _, pfx := range prefixes {
		if pfx.Addr().Compare(next) > 0 {
			return false
		}

		end := lastAddr(pfx)
		if !end.Next().IsValid() {
			// end is the highest address of the family
			return true
		}
		if end.Compare(next) >= 0 {
			next = end.Next()
		}
	}

	return false
}

// lastAddr returns the highest address in pfx.
func lastAddr(pfx netip.Prefix) netip.Addr {
	b := pfx.Addr().AsSlice()
	for i := pfx.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 0x80 >> (i % 8)
	}

	a, _ := netip.AddrFromSlice(b)
	return a
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

	// forwarded holds the X-Forwarded-For header lines, recorded whether or not
	// the peer is trusted. It aliases the request header slice, so it must not
	// be mutated.
	forwarded []string

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
		forwarded: values,
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
// chooses. ResolveLegacy backs the ingester option
// Ignore-Untrusted-Forwarded-For=false, which keeps the behavior of ingesters
// that trusted the header before trusted proxies were configurable. Never use it
// for a security decision.
func ResolveLegacy(r *http.Request) Client {
	if r == nil {
		return Client{}
	}

	values := r.Header.Values(forwardedHeader)

	c := Client{
		PeerRaw:   r.RemoteAddr,
		forwarded: values,
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

// Forwarded returns the raw X-Forwarded-For header for logging only, with
// multiple header lines joined with ", " and truncated. It is never the client
// address, use Addr for that.
func (c Client) Forwarded() string {
	return joinForwarded(c.forwarded)
}

// HasForwarded reports whether Forwarded is non-empty, without building it. A
// single empty header line does not count.
func (c Client) HasForwarded() bool {
	return len(c.forwarded) > 1 || len(c.forwarded) == 1 && c.forwarded[0] != ``
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

// joinForwarded joins the X-Forwarded-For values for Client.Forwarded,
// truncated to maxForwardedLen bytes. It does not pick the client.
func joinForwarded(values []string) string {
	// the common single short header line is returned without allocating
	if len(values) == 1 && len(values[0]) <= maxForwardedLen {
		return values[0]
	}

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
