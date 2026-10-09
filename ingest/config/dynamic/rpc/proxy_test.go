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
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/log"
)

// req builds a request from a peer with the given headers.
func req(peer string, hdrs map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, DefaultPath, nil)
	r.RemoteAddr = peer
	for k, v := range hdrs {
		r.Header.Set(k, v)
	}
	return r
}

func trustOrDie(t *testing.T, entries ...string) *proxyTrust {
	t.Helper()
	pt, err := newProxyTrust(entries)
	if err != nil {
		t.Fatal(err)
	}
	return pt
}

// TestClientAddrUntrustedPeer is the security property: a peer we were not told to trust
// does not get to name somebody else.  Without this every header is a throttle bypass.
func TestClientAddrUntrustedPeer(t *testing.T) {
	lgr := log.NewDiscardLogger()
	pt := trustOrDie(t) // nothing trusted, the default

	for _, tc := range []struct {
		name string
		hdrs map[string]string
	}{
		{`no headers`, nil},
		{`forged X-Forwarded-For`, map[string]string{hdrXForwardedFor: `1.2.3.4`}},
		{`forged Forwarded`, map[string]string{hdrForwarded: `for=1.2.3.4`}},
		{`forged X-Real-IP`, map[string]string{hdrXRealIP: `1.2.3.4`}},
		{`forged CF-Connecting-IP`, map[string]string{hdrCFConnectingIP: `1.2.3.4`}},
		{`forged True-Client-IP`, map[string]string{hdrTrueClientIP: `1.2.3.4`}},
		{`all of them at once`, map[string]string{
			hdrXForwardedFor: `1.2.3.4`, hdrForwarded: `for=1.2.3.4`, hdrXRealIP: `1.2.3.4`,
			hdrCFConnectingIP: `1.2.3.4`, hdrTrueClientIP: `1.2.3.4`,
		}},
	} {
		if got := pt.clientAddr(req(`198.51.100.9:5555`, tc.hdrs), lgr); got != `198.51.100.9` {
			t.Errorf("%s: keyed on %q, want the real peer 198.51.100.9", tc.name, got)
		}
	}

	// and the same when some proxies are trusted, just not this peer
	pt = trustOrDie(t, `10.0.0.0/8`)
	if got := pt.clientAddr(req(`198.51.100.9:5555`, map[string]string{hdrXForwardedFor: `1.2.3.4`}), lgr); got != `198.51.100.9` {
		t.Errorf("keyed on %q, want the real peer", got)
	}
}

// TestClientAddrTrustedProxy covers the load balancer cases we actually expect.
func TestClientAddrTrustedProxy(t *testing.T) {
	lgr := log.NewDiscardLogger()
	pt := trustOrDie(t, `10.0.0.0/8`, `192.168.1.1`, `2001:db8:beef::/48`)

	for _, tc := range []struct {
		name string
		peer string
		hdrs map[string]string
		want string
	}{
		{`X-Forwarded-For single`, `10.1.2.3:443`,
			map[string]string{hdrXForwardedFor: `203.0.113.7`}, `203.0.113.7`},
		{`X-Forwarded-For chain, client first`, `10.1.2.3:443`,
			map[string]string{hdrXForwardedFor: `203.0.113.7, 10.9.9.9`}, `203.0.113.7`},
		{`X-Forwarded-For with a port`, `10.1.2.3:443`,
			map[string]string{hdrXForwardedFor: `203.0.113.7:51234`}, `203.0.113.7`},
		{`X-Forwarded-For IPv6`, `10.1.2.3:443`,
			map[string]string{hdrXForwardedFor: `2001:db8::42`}, `2001:db8::42`},
		{`X-Forwarded-For bracketed IPv6 with port`, `10.1.2.3:443`,
			map[string]string{hdrXForwardedFor: `[2001:db8::42]:4711`}, `2001:db8::42`},
		{`X-Forwarded-For messy spacing`, `10.1.2.3:443`,
			map[string]string{hdrXForwardedFor: `  203.0.113.7 ,  10.9.9.9  `}, `203.0.113.7`},
		{`Forwarded RFC 7239`, `10.1.2.3:443`,
			map[string]string{hdrForwarded: `for=203.0.113.7;proto=https;by=10.1.2.3`}, `203.0.113.7`},
		{`Forwarded quoted IPv6`, `10.1.2.3:443`,
			map[string]string{hdrForwarded: `for="[2001:db8::42]:4711";proto=https`}, `2001:db8::42`},
		{`Forwarded chain`, `10.1.2.3:443`,
			map[string]string{hdrForwarded: `for=203.0.113.7, for=10.9.9.9`}, `203.0.113.7`},
		{`Forwarded wins over X-Forwarded-For`, `10.1.2.3:443`,
			map[string]string{hdrForwarded: `for=203.0.113.7`, hdrXForwardedFor: `198.51.100.1`}, `203.0.113.7`},
		{`X-Real-IP when no list header`, `10.1.2.3:443`,
			map[string]string{hdrXRealIP: `203.0.113.7`}, `203.0.113.7`},
		{`CF-Connecting-IP`, `10.1.2.3:443`,
			map[string]string{hdrCFConnectingIP: `203.0.113.7`}, `203.0.113.7`},
		{`True-Client-IP`, `10.1.2.3:443`,
			map[string]string{hdrTrueClientIP: `203.0.113.7`}, `203.0.113.7`},
		{`a list header beats a forgeable single one`, `10.1.2.3:443`,
			map[string]string{hdrXForwardedFor: `203.0.113.7`, hdrXRealIP: `1.2.3.4`}, `203.0.113.7`},
		{`bare trusted address`, `192.168.1.1:443`,
			map[string]string{hdrXForwardedFor: `203.0.113.7`}, `203.0.113.7`},
		{`trusted IPv6 proxy`, `[2001:db8:beef::1]:443`,
			map[string]string{hdrXForwardedFor: `203.0.113.7`}, `203.0.113.7`},
		{`trusted proxy sending nothing`, `10.1.2.3:443`, nil, `10.1.2.3`},
		{`every hop is ours`, `10.1.2.3:443`,
			map[string]string{hdrXForwardedFor: `10.4.4.4, 10.9.9.9`}, `10.4.4.4`},
		{`v4 mapped v6 normalizes`, `10.1.2.3:443`,
			map[string]string{hdrXForwardedFor: `::ffff:203.0.113.7`}, `203.0.113.7`},
		{`garbage entries are skipped`, `10.1.2.3:443`,
			map[string]string{hdrXForwardedFor: `not-an-ip, 203.0.113.7`}, `203.0.113.7`},
		{`RFC 7239 obfuscated identifiers are not addresses`, `10.1.2.3:443`,
			map[string]string{hdrForwarded: `for=_hidden, for=203.0.113.7`}, `203.0.113.7`},
		{`unknown is not an address`, `10.1.2.3:443`,
			map[string]string{hdrForwarded: `for=unknown`}, `10.1.2.3`},
	} {
		if got := pt.clientAddr(req(tc.peer, tc.hdrs), lgr); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestClientAddrSpoofThroughTrustedProxy is the attack that the rightmost walk exists to
// stop.  A client behind a real load balancer can put anything it likes at the front of
// X-Forwarded-For, and the proxy appends the client's real address after it.
func TestClientAddrSpoofThroughTrustedProxy(t *testing.T) {
	lgr := log.NewDiscardLogger()
	pt := trustOrDie(t, `10.0.0.0/8`)

	// the attacker is 203.0.113.9 and claims to be someone else
	for _, forged := range []string{
		`1.2.3.4`,
		`1.2.3.4, 5.6.7.8`,
		`10.0.0.1`, // claiming to be the proxy itself
		`10.0.0.1, 10.0.0.2, 10.0.0.3`,
	} {
		// the proxy appends what it actually saw
		xff := forged + `, 203.0.113.9`
		got := pt.clientAddr(req(`10.1.2.3:443`, map[string]string{hdrXForwardedFor: xff}), lgr)
		if got != `203.0.113.9` {
			t.Errorf("X-Forwarded-For %q keyed on %q, want the address the proxy observed", xff, got)
		}
	}

	// a flood of forged entries must not let each attempt key a different entry, which is
	// what would defeat the throttle
	seen := map[string]bool{}
	for i := range 50 {
		xff := fmt.Sprintf("10.9.%d.%d, 203.0.113.9", i/256, i%256)
		seen[pt.clientAddr(req(`10.1.2.3:443`, map[string]string{hdrXForwardedFor: xff}), lgr)] = true
	}
	if len(seen) != 1 {
		t.Errorf("a client rolling forged entries produced %d throttle keys, want 1", len(seen))
	}
}

// TestProxyTrustParsing covers the configuration guard.
func TestProxyTrustParsing(t *testing.T) {
	if _, err := newProxyTrust([]string{`10.0.0.0/8`, `192.168.1.1`, `2001:db8::/32`, `::1`, ` `}); err != nil {
		t.Errorf("a valid set was refused: %v", err)
	}
	for _, bad := range []string{`not-an-ip`, `10.0.0.0/99`, `10.0.0.0/`, `300.1.1.1`} {
		if _, err := newProxyTrust([]string{bad}); err == nil {
			t.Errorf("%q should not parse as a trusted proxy", bad)
		}
	}
	// a bad entry has to fail the whole server rather than quietly trust less than asked
	if _, err := NewServer(ServerConfig{Token: testToken, TrustedProxies: []string{`nonsense`}}); err == nil {
		t.Error(`a server with an unparseable trusted proxy should not start`)
	}
	pt := trustOrDie(t)
	if !pt.empty() {
		t.Error(`an empty configuration should trust nothing`)
	}
}

// TestServerThrottlesPerForwardedClient is the end to end load balancer case: two
// different clients arriving through one proxy must not share a throttle entry.
func TestServerThrottlesPerForwardedClient(t *testing.T) {
	srv, err := NewServer(ServerConfig{
		Token:          testToken,
		AuthRateWindow: time.Hour,
		// httptest connects over loopback, so that is our "load balancer"
		TrustedProxies: []string{`127.0.0.0/8`, `::1`},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	get := func(client string) int {
		r, err := http.NewRequest(http.MethodGet, ts.URL+DefaultPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set(hdrXForwardedFor, client)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// each distinct client gets its own allowance even though they share a peer address
	for _, c := range []string{`203.0.113.1`, `203.0.113.2`, `203.0.113.3`} {
		if code := get(c); code == http.StatusTooManyRequests {
			t.Errorf("client %s was throttled on its first attempt", c)
		}
	}
	// and a repeat from one of them is throttled without affecting the others
	if code := get(`203.0.113.1`); code != http.StatusTooManyRequests {
		t.Errorf("a repeat attempt got %d, want %d", code, http.StatusTooManyRequests)
	}
	if code := get(`203.0.113.4`); code == http.StatusTooManyRequests {
		t.Error(`a fresh client was throttled by another client's attempt`)
	}
}

// TestServerIgnoresHeadersWithoutTrust is the same setup with the trust left unconfigured,
// which is the default.  Everything collapses onto the proxy's address, which is exactly
// why the configuration matters.
func TestServerIgnoresHeadersWithoutTrust(t *testing.T) {
	srv, err := NewServer(ServerConfig{Token: testToken, AuthRateWindow: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	get := func(client string) int {
		r, _ := http.NewRequest(http.MethodGet, ts.URL+DefaultPath, nil)
		r.Header.Set(hdrXForwardedFor, client)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if code := get(`203.0.113.1`); code == http.StatusTooManyRequests {
		t.Error(`the first attempt was throttled`)
	}
	// a different claimed client, same real peer, still throttled.  No bypass.
	if code := get(`203.0.113.2`); code != http.StatusTooManyRequests {
		t.Errorf("got %d, want %d, an untrusted header bypassed the throttle",
			code, http.StatusTooManyRequests)
	}
}

// TestThrottleKey covers the reduction from client address to throttle key.
func TestThrottleKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{`IPv4 is keyed whole`, `203.0.113.7`, `203.0.113.7`},
		{`IPv4 loopback`, `127.0.0.1`, `127.0.0.1`},
		{`IPv6 is keyed on the /64`, `2001:db8::42`, `2001:db8::/64`},
		{`IPv6 with a populated prefix`, `2001:db8:1:2:3:4:5:6`, `2001:db8:1:2::/64`},
		{`IPv6 loopback`, `::1`, `::/64`},
		{`v4 mapped v6 is keyed as v4`, `::ffff:203.0.113.7`, `203.0.113.7`},
		{`a zone does not create a second key`, `fe80::1%eth0`, `fe80::/64`},
		{`garbage is keyed verbatim`, `not-an-address`, `not-an-address`},
		{`empty stays empty`, ``, ``},
	} {
		if got := throttleKey(tc.in); got != tc.want {
			t.Errorf("%s: throttleKey(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}

	// the whole point: everything inside one allocation is one key, and neighbouring
	// allocations are still told apart
	base := throttleKey(`2001:db8:aaaa:bbbb::1`)
	for _, same := range []string{
		`2001:db8:aaaa:bbbb::2`,
		`2001:db8:aaaa:bbbb::dead:beef`,
		`2001:db8:aaaa:bbbb:ffff:ffff:ffff:ffff`,
	} {
		if got := throttleKey(same); got != base {
			t.Errorf("%s keyed as %q, want the same /64 as the base %q", same, got, base)
		}
	}
	for _, other := range []string{
		`2001:db8:aaaa:bbbc::1`, // the adjacent /64
		`2001:db8:aaaa::1`,
		`2001:db8:cccc:bbbb::1`,
	} {
		if got := throttleKey(other); got == base {
			t.Errorf("%s keyed as %q, a different allocation should not share a key", other, got)
		}
	}
	// and a v6 key can never collide with a v4 one, they are different shapes
	if throttleKey(`2001:db8::/64`) == throttleKey(`203.0.113.7`) {
		t.Error(`a v6 prefix key collided with a v4 key`)
	}
}

// TestServerThrottlesIPv6ByPrefix is the address rolling attack end to end.  A client that
// owns a /64 must not get a fresh allowance for every address in it.
func TestServerThrottlesIPv6ByPrefix(t *testing.T) {
	srv, err := NewServer(ServerConfig{
		Token:          testToken,
		AuthRateWindow: time.Hour,
		TrustedProxies: []string{`127.0.0.0/8`, `::1`},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	get := func(client string) int {
		r, err := http.NewRequest(http.MethodGet, ts.URL+DefaultPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set(hdrXForwardedFor, client)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// the first address in the prefix gets its attempt
	if code := get(`2001:db8:aaaa:bbbb::1`); code == http.StatusTooManyRequests {
		t.Fatal(`the first attempt from a prefix was throttled`)
	}
	// rolling through the rest of the /64 buys nothing
	for i := 2; i < 200; i++ {
		addr := fmt.Sprintf("2001:db8:aaaa:bbbb::%x", i)
		if code := get(addr); code != http.StatusTooManyRequests {
			t.Fatalf("%s got %d, an address roll inside one /64 bypassed the throttle", addr, code)
		}
	}
	// the table saw one entry for all of it, so the flood never even approached the
	// ceiling that would degrade everyone else
	if n := srv.thr.tracked(); n != 1 {
		t.Errorf("the throttle tracked %d entries for one /64, want 1", n)
	}
	if n := srv.thr.degradedCount(); n != 0 {
		t.Errorf("the flood pushed %d attempts onto the global limiter, it should not have come close", n)
	}

	// a genuinely different allocation is still its own client
	if code := get(`2001:db8:aaaa:bbbc::1`); code == http.StatusTooManyRequests {
		t.Error(`a neighbouring /64 was throttled by another allocation's attempt`)
	}
	// and IPv4 clients are unaffected by any of it
	if code := get(`203.0.113.7`); code == http.StatusTooManyRequests {
		t.Error(`an IPv4 client was throttled by the IPv6 flood`)
	}
	if code := get(`203.0.113.8`); code == http.StatusTooManyRequests {
		t.Error(`a second IPv4 client shared the first one's entry`)
	}
	if code := get(`203.0.113.7`); code != http.StatusTooManyRequests {
		t.Error(`an IPv4 client got a second attempt inside the window`)
	}
}
