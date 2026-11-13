/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package trustedproxy

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"runtime"
	"strings"
	"testing"
)

func mustParse(t *testing.T, entries ...string) List {
	t.Helper()

	l, err := Parse(entries)
	if err != nil {
		t.Fatalf("Parse(%q): %v", entries, err)
	}

	return l
}

func newRequest(remote string, xff ...string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote

	for _, v := range xff {
		r.Header.Add(forwardedHeader, v)
	}

	return r
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		entries []string
		want    []string // prefixes, in order
		wantErr string   // substring of the error, empty for success
	}{
		{name: "nil", entries: nil, want: nil},
		{name: "blank entries skipped", entries: []string{"", "  ", "\t"}, want: nil},
		{name: "ipv4 host", entries: []string{"10.0.0.1"}, want: []string{"10.0.0.1/32"}},
		{name: "ipv6 host", entries: []string{"2001:db8::1"}, want: []string{"2001:db8::1/128"}},
		{name: "trimmed", entries: []string{"  10.0.0.1 "}, want: []string{"10.0.0.1/32"}},
		{name: "cidr masked", entries: []string{"10.1.2.3/8"}, want: []string{"10.0.0.0/8"}},
		{name: "ipv6 cidr masked", entries: []string{"2001:db8::1/32"}, want: []string{"2001:db8::/32"}},
		{name: "mapped host unmapped", entries: []string{"::ffff:10.0.0.1"}, want: []string{"10.0.0.1/32"}},
		{name: "mapped cidr unmapped", entries: []string{"::ffff:10.1.2.3/104"}, want: []string{"10.0.0.0/8"}},
		{name: "zone dropped", entries: []string{"fe80::1%eth0"}, want: []string{"fe80::1/128"}},
		{name: "host prefix", entries: []string{"192.0.2.7/32"}, want: []string{"192.0.2.7/32"}},
		{
			name:    "multiple",
			entries: []string{"10.0.0.1", "", "172.16.0.0/12", "2001:db8::/48"},
			want:    []string{"10.0.0.1/32", "172.16.0.0/12", "2001:db8::/48"},
		},
		{name: "garbage", entries: []string{"10.0.0.1", "proxy.example.com"}, wantErr: `"proxy.example.com"`},
		{name: "bad cidr", entries: []string{"10.0.0.0/33"}, wantErr: `"10.0.0.0/33"`},
		{name: "address with port", entries: []string{"10.0.0.1:80"}, wantErr: `"10.0.0.1:80"`},
		{name: "comma list in one entry", entries: []string{"10.0.0.1,10.0.0.2"}, wantErr: `"10.0.0.1,10.0.0.2"`},
		{name: "ipv4 zero prefix", entries: []string{"0.0.0.0/0"}, wantErr: `"0.0.0.0/0"`},
		{name: "ipv4 zero prefix nonzero addr", entries: []string{"10.0.0.0/0"}, wantErr: "prefix length of 0"},
		{name: "ipv6 zero prefix", entries: []string{"::/0"}, wantErr: `"::/0"`},
		{name: "mapped zero prefix", entries: []string{"::ffff:0.0.0.0/96"}, wantErr: "prefix length of 0"},
		// peers are matched in IPv4 form, so these blocks could never match an IPv4 peer
		{name: "block spanning mapped range", entries: []string{"::ffff:0:0/95"}, wantErr: `"::ffff:0:0/95"`},
		{name: "wide block spanning mapped range", entries: []string{"::/80"}, wantErr: "::ffff:0:0/96"},
		{name: "widest block spanning mapped range", entries: []string{"::/1"}, wantErr: "never matches IPv4 peers"},
		{name: "block next to mapped range", entries: []string{"::fffe:0:0/96"}, want: []string{"::fffe:0:0/96"}},
		{name: "block below mapped range", entries: []string{"::/97"}, want: []string{"::/97"}},
		{name: "reserved block spanning mapped range", entries: []string{"::/8"}, wantErr: `"::/8"`},
		{name: "nat64 well-known prefix", entries: []string{"64:ff9b::/96"}, want: []string{"64:ff9b::/96"}},
		{name: "unique local block", entries: []string{"fc00::/7"}, want: []string{"fc00::/7"}},
		{name: "global unicast block", entries: []string{"2000::/3"}, want: []string{"2000::/3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := Parse(tt.entries)

			if tt.wantErr != `` {
				if err == nil {
					t.Fatalf("expected an error containing %q, got list %v", tt.wantErr, l.prefixes)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tt.wantErr)
				}
				if !l.Empty() {
					t.Fatalf("list not empty on error: %v", l.prefixes)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var got []string
			for _, p := range l.prefixes {
				got = append(got, p.String())
			}

			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("prefixes %v, want %v", got, tt.want)
			}
			if l.Empty() != (len(tt.want) == 0) {
				t.Fatalf("Empty() = %v with prefixes %v", l.Empty(), got)
			}
		})
	}
}

func TestContains(t *testing.T) {
	l := mustParse(t, "10.0.0.0/8", "192.0.2.7", "2001:db8::/32", "fe80::1")

	tests := []struct {
		addr string
		want bool
	}{
		{"10.1.2.3", true},
		{"11.0.0.1", false},
		{"192.0.2.7", true},
		{"192.0.2.8", false},
		{"::ffff:10.1.2.3", true},
		{"::ffff:192.0.2.8", false},
		{"2001:db8:1::1", true},
		{"2001:db9::1", false},
		{"fe80::1%eth0", true},
		{"fe80::2%eth0", false},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			if got := l.Contains(netip.MustParseAddr(tt.addr)); got != tt.want {
				t.Fatalf("Contains(%s) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}

	if l.Contains(netip.Addr{}) {
		t.Fatal("Contains(invalid) = true")
	}

	var zero List
	if !zero.Empty() || zero.Contains(netip.MustParseAddr("10.0.0.1")) {
		t.Fatal("zero-value List must be empty and trust nothing")
	}
}

func TestIsTrustedPeer(t *testing.T) {
	l := mustParse(t, "10.0.0.1", "2001:db8::1")

	tests := []struct {
		name   string
		remote string
		xff    []string
		want   bool
	}{
		{name: "trusted ipv4", remote: "10.0.0.1:1234", want: true},
		{name: "untrusted ipv4", remote: "10.0.0.2:1234", want: false},
		{name: "trusted ipv6", remote: "[2001:db8::1]:443", want: true},
		{name: "mapped ipv4", remote: "[::ffff:10.0.0.1]:443", want: true},
		{name: "no port", remote: "10.0.0.1", want: true},
		{name: "header ignored", remote: "10.0.0.2:1234", xff: []string{"10.0.0.1"}, want: false},
		{name: "unparseable", remote: "not-an-address", want: false},
		{name: "empty", remote: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := l.IsTrustedPeer(newRequest(tt.remote, tt.xff...)); got != tt.want {
				t.Fatalf("IsTrustedPeer(%q) = %v, want %v", tt.remote, got, tt.want)
			}
		})
	}

	if l.IsTrustedPeer(nil) {
		t.Fatal("IsTrustedPeer(nil) = true")
	}
}

func TestResolve(t *testing.T) {
	trusted := []string{"10.0.0.1", "10.0.0.2", "10.0.1.0/24", "2001:db8::/64", "fe80::1"}

	tests := []struct {
		name      string
		zero      bool // resolve with the zero-value List
		remote    string
		xff       []string // one element per header line
		wantAddr  string   // empty for an invalid Addr
		wantPeer  string   // empty for an invalid Peer
		wantFwd   string
		malformed bool
	}{
		{
			name:     "no header",
			remote:   "198.51.100.1:5000",
			wantAddr: "198.51.100.1",
			wantPeer: "198.51.100.1",
		},
		{
			name:     "untrusted peer header ignored",
			remote:   "198.51.100.1:5000",
			xff:      []string{"203.0.113.5"},
			wantAddr: "198.51.100.1",
			wantPeer: "198.51.100.1",
			wantFwd:  "203.0.113.5",
		},
		{
			name:     "untrusted peer chain ignored",
			remote:   "198.51.100.1:5000",
			xff:      []string{"203.0.113.5, 10.0.0.1"},
			wantAddr: "198.51.100.1",
			wantPeer: "198.51.100.1",
			wantFwd:  "203.0.113.5, 10.0.0.1",
		},
		{
			name:     "zero value list trusts nothing",
			zero:     true,
			remote:   "10.0.0.1:5000",
			xff:      []string{"203.0.113.5"},
			wantAddr: "10.0.0.1",
			wantPeer: "10.0.0.1",
			wantFwd:  "203.0.113.5",
		},
		{
			name:     "trusted peer single entry",
			remote:   "10.0.0.1:5000",
			xff:      []string{"203.0.113.5"},
			wantAddr: "203.0.113.5",
			wantPeer: "10.0.0.1",
			wantFwd:  "203.0.113.5",
		},
		{
			name:     "trusted peer chain takes rightmost untrusted",
			remote:   "10.0.0.1:5000",
			xff:      []string{"192.0.2.66, 203.0.113.5, 10.0.0.2"},
			wantAddr: "203.0.113.5",
			wantPeer: "10.0.0.1",
			wantFwd:  "192.0.2.66, 203.0.113.5, 10.0.0.2",
		},
		{
			name:     "trusted peer chain without spaces",
			remote:   "10.0.0.1:5000",
			xff:      []string{"203.0.113.5,10.0.1.9"},
			wantAddr: "203.0.113.5",
			wantPeer: "10.0.0.1",
			wantFwd:  "203.0.113.5,10.0.1.9",
		},
		{
			name:     "all trusted returns leftmost",
			remote:   "10.0.0.1:5000",
			xff:      []string{"10.0.1.5, 10.0.0.2"},
			wantAddr: "10.0.1.5",
			wantPeer: "10.0.0.1",
			wantFwd:  "10.0.1.5, 10.0.0.2",
		},
		{
			name:     "empty header returns peer",
			remote:   "10.0.0.1:5000",
			xff:      []string{""},
			wantAddr: "10.0.0.1",
			wantPeer: "10.0.0.1",
		},
		{
			name:     "blank entries skipped",
			remote:   "10.0.0.1:5000",
			xff:      []string{" , 203.0.113.5 ,, "},
			wantAddr: "203.0.113.5",
			wantPeer: "10.0.0.1",
			wantFwd:  " , 203.0.113.5 ,, ",
		},
		{
			name:      "malformed rightmost returns peer",
			remote:    "10.0.0.1:5000",
			xff:       []string{"203.0.113.5, garbage"},
			wantAddr:  "10.0.0.1",
			wantPeer:  "10.0.0.1",
			wantFwd:   "203.0.113.5, garbage",
			malformed: true,
		},
		{
			name:      "malformed middle returns nearest trusted hop",
			remote:    "10.0.0.1:5000",
			xff:       []string{"203.0.113.5, garbage, 10.0.0.2"},
			wantAddr:  "10.0.0.2",
			wantPeer:  "10.0.0.1",
			wantFwd:   "203.0.113.5, garbage, 10.0.0.2",
			malformed: true,
		},
		{
			name:      "unknown token is malformed",
			remote:    "10.0.0.1:5000",
			xff:       []string{"unknown"},
			wantAddr:  "10.0.0.1",
			wantPeer:  "10.0.0.1",
			wantFwd:   "unknown",
			malformed: true,
		},
		{
			name:     "malformed left of the client is not reached",
			remote:   "10.0.0.1:5000",
			xff:      []string{"garbage, 203.0.113.5"},
			wantAddr: "203.0.113.5",
			wantPeer: "10.0.0.1",
			wantFwd:  "garbage, 203.0.113.5",
		},
		{
			name:     "ipv4 entries with ports",
			remote:   "10.0.0.1:5000",
			xff:      []string{"203.0.113.5:4444, 10.0.0.2:80"},
			wantAddr: "203.0.113.5",
			wantPeer: "10.0.0.1",
			wantFwd:  "203.0.113.5:4444, 10.0.0.2:80",
		},
		{
			name:     "ipv6 entry with port",
			remote:   "10.0.0.1:5000",
			xff:      []string{"[2001:db8:ffff::5]:443"},
			wantAddr: "2001:db8:ffff::5",
			wantPeer: "10.0.0.1",
			wantFwd:  "[2001:db8:ffff::5]:443",
		},
		{
			name:     "bracketed ipv6 entry without port",
			remote:   "10.0.0.1:5000",
			xff:      []string{"[2001:db8:ffff::5]"},
			wantAddr: "2001:db8:ffff::5",
			wantPeer: "10.0.0.1",
			wantFwd:  "[2001:db8:ffff::5]",
		},
		{
			name:     "ipv6 trusted peer and chain",
			remote:   "[2001:db8::10]:443",
			xff:      []string{"2001:db8:ffff::5, 2001:db8::11"},
			wantAddr: "2001:db8:ffff::5",
			wantPeer: "2001:db8::10",
			wantFwd:  "2001:db8:ffff::5, 2001:db8::11",
		},
		{
			name:     "ipv4 mapped peer and entries",
			remote:   "[::ffff:10.0.0.1]:5000",
			xff:      []string{"::ffff:203.0.113.5, ::ffff:10.0.0.2"},
			wantAddr: "203.0.113.5",
			wantPeer: "10.0.0.1",
			wantFwd:  "::ffff:203.0.113.5, ::ffff:10.0.0.2",
		},
		{
			name:     "zoned trusted peer",
			remote:   "[fe80::1%eth0]:5000",
			xff:      []string{"203.0.113.5"},
			wantAddr: "203.0.113.5",
			wantPeer: "fe80::1%eth0",
			wantFwd:  "203.0.113.5",
		},
		{
			name:     "zoned untrusted peer",
			remote:   "[fe80::2%eth0]:5000",
			xff:      []string{"203.0.113.5"},
			wantAddr: "fe80::2%eth0",
			wantPeer: "fe80::2%eth0",
			wantFwd:  "203.0.113.5",
		},
		{
			name:     "multiple header lines",
			remote:   "10.0.0.1:5000",
			xff:      []string{"192.0.2.66, 203.0.113.5", "10.0.0.2"},
			wantAddr: "203.0.113.5",
			wantPeer: "10.0.0.1",
			wantFwd:  "192.0.2.66, 203.0.113.5, 10.0.0.2",
		},
		{
			name:     "multiple header lines client on last line",
			remote:   "10.0.0.1:5000",
			xff:      []string{"192.0.2.66", "203.0.113.5"},
			wantAddr: "203.0.113.5",
			wantPeer: "10.0.0.1",
			wantFwd:  "192.0.2.66, 203.0.113.5",
		},
		{
			name:     "remote addr without port",
			remote:   "10.0.0.1",
			xff:      []string{"203.0.113.5"},
			wantAddr: "203.0.113.5",
			wantPeer: "10.0.0.1",
			wantFwd:  "203.0.113.5",
		},
		{
			name:     "ipv6 remote addr without port",
			remote:   "2001:db8::10",
			wantAddr: "2001:db8::10",
			wantPeer: "2001:db8::10",
		},
		{
			name:     "unparseable remote addr",
			remote:   "@unix",
			xff:      []string{"203.0.113.5"},
			wantAddr: "",
			wantPeer: "",
		},
		{
			name:     "empty remote addr",
			remote:   "",
			wantAddr: "",
			wantPeer: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := mustParse(t, trusted...)
			if tt.zero {
				l = List{}
			}

			c := l.Resolve(newRequest(tt.remote, tt.xff...))

			if got := addrString(c.Addr); got != tt.wantAddr {
				t.Errorf("Addr = %q, want %q", got, tt.wantAddr)
			}
			if got := addrString(c.Peer); got != tt.wantPeer {
				t.Errorf("Peer = %q, want %q", got, tt.wantPeer)
			}
			if c.PeerRaw != tt.remote {
				t.Errorf("PeerRaw = %q, want %q", c.PeerRaw, tt.remote)
			}
			if c.Forwarded != tt.wantFwd {
				t.Errorf("Forwarded = %q, want %q", c.Forwarded, tt.wantFwd)
			}
			if c.Malformed != tt.malformed {
				t.Errorf("Malformed = %v, want %v", c.Malformed, tt.malformed)
			}
		})
	}
}

func addrString(a netip.Addr) string {
	if !a.IsValid() {
		return ``
	}

	return a.String()
}

func TestResolveNilRequest(t *testing.T) {
	l := mustParse(t, "10.0.0.1")

	if c := l.Resolve(nil); c != (Client{}) {
		t.Fatalf("Resolve(nil) = %+v, want zero Client", c)
	}
}

func TestResolveHopCap(t *testing.T) {
	l := mustParse(t, "10.0.0.0/8")

	// The client sits just left of maxHops trusted entries, so the walk stops at
	// the cap before reaching it and returns the leftmost walked entry.
	entries := []string{"203.0.113.5"}
	for i := 0; i < maxHops; i++ {
		entries = append(entries, fmt.Sprintf("10.0.0.%d", i+1))
	}

	c := l.Resolve(newRequest("10.0.0.254:5000", strings.Join(entries, ", ")))
	if want := "10.0.0.1"; c.Addr.String() != want {
		t.Fatalf("Addr = %s, want %s", c.Addr, want)
	}

	// One fewer trusted entry and the client is inside the cap.
	c = l.Resolve(newRequest("10.0.0.254:5000", strings.Join(entries[:maxHops], ", ")))
	if want := "203.0.113.5"; c.Addr.String() != want {
		t.Fatalf("Addr = %s, want %s", c.Addr, want)
	}

	// The cap counts entries across header lines.
	c = l.Resolve(newRequest("10.0.0.254:5000", entries...))
	if want := "10.0.0.1"; c.Addr.String() != want {
		t.Fatalf("multi-line Addr = %s, want %s", c.Addr, want)
	}
}

func TestResolveForwardedTruncated(t *testing.T) {
	l := mustParse(t, "10.0.0.1")

	long := strings.Repeat("203.0.113.5, ", 200) + "198.51.100.7"
	c := l.Resolve(newRequest("10.0.0.1:5000", long))

	if len(c.Forwarded) != maxForwardedLen+len("...") || !strings.HasSuffix(c.Forwarded, "...") {
		t.Fatalf("Forwarded not truncated: len %d", len(c.Forwarded))
	}
	if want := "198.51.100.7"; c.Addr.String() != want {
		t.Fatalf("Addr = %s, want %s", c.Addr, want)
	}
}

func TestJoinForwarded(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{name: "nil", values: nil, want: ""},
		{name: "one value", values: []string{"203.0.113.5"}, want: "203.0.113.5"},
		{name: "blank value kept", values: []string{"203.0.113.5", ""}, want: "203.0.113.5, "},
		{name: "values joined", values: []string{"203.0.113.5", "10.0.0.1, 10.0.0.2"}, want: "203.0.113.5, 10.0.0.1, 10.0.0.2"},
		{name: "exactly at the limit", values: []string{strings.Repeat("a", maxForwardedLen)}, want: strings.Repeat("a", maxForwardedLen)},
		{name: "one byte over", values: []string{strings.Repeat("a", maxForwardedLen+1)}, want: strings.Repeat("a", maxForwardedLen) + "..."},
		{
			name:   "separator crosses the limit",
			values: []string{strings.Repeat("a", maxForwardedLen-1), "b"},
			want:   strings.Repeat("a", maxForwardedLen-1) + ",...",
		},
		{
			name:   "limit reached at a value boundary",
			values: []string{strings.Repeat("a", maxForwardedLen), ""},
			want:   strings.Repeat("a", maxForwardedLen) + "...",
		},
		{
			name:   "many lines",
			values: strings.Split(strings.Repeat("203.0.113.5|", 200), "|"),
			want:   strings.Join(strings.Split(strings.Repeat("203.0.113.5|", 200), "|"), ", ")[:maxForwardedLen] + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := joinForwarded(tt.values); got != tt.want {
				t.Fatalf("joinForwarded = %q (len %d), want %q (len %d)", got, len(got), tt.want, len(tt.want))
			}
		})
	}
}

// TestJoinForwardedBounded checks that an oversized header, which is joined for
// every peer, costs an allocation near the log limit and not the header size.
func TestJoinForwardedBounded(t *testing.T) {
	values := []string{strings.Repeat("203.0.113.5, ", 40000), strings.Repeat("198.51.100.7, ", 40000)}

	const runs = 100
	var got string
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < runs; i++ {
		got = joinForwarded(values)
	}
	runtime.ReadMemStats(&after)

	if len(got) != maxForwardedLen+len("...") {
		t.Fatalf("len %d, want %d", len(got), maxForwardedLen+len("..."))
	}
	if perRun := (after.TotalAlloc - before.TotalAlloc) / runs; perRun > 4*maxForwardedLen {
		t.Fatalf("allocated %d bytes per call for a %d byte header", perRun, len(values[0])+len(values[1]))
	}
}

func TestClientStringAndIP(t *testing.T) {
	tests := []struct {
		name    string
		client  Client
		wantStr string
		wantIP  net.IP
	}{
		{
			name:    "ipv4",
			client:  Client{Addr: netip.MustParseAddr("203.0.113.5")},
			wantStr: "203.0.113.5",
			wantIP:  net.ParseIP("203.0.113.5"),
		},
		{
			name:    "ipv6",
			client:  Client{Addr: netip.MustParseAddr("2001:db8::5")},
			wantStr: "2001:db8::5",
			wantIP:  net.ParseIP("2001:db8::5"),
		},
		{
			name:    "unparseable peer with port",
			client:  Client{PeerRaw: "bogus:1234"},
			wantStr: "bogus",
		},
		{
			name:    "unparseable peer without port",
			client:  Client{PeerRaw: "@unix"},
			wantStr: "@unix",
		},
		{
			name:    "zero",
			client:  Client{},
			wantStr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.client.String(); got != tt.wantStr {
				t.Errorf("String() = %q, want %q", got, tt.wantStr)
			}

			got := tt.client.IP()
			if tt.wantIP == nil {
				if got != nil {
					t.Errorf("IP() = %v, want nil", got)
				}
				return
			}
			if !got.Equal(tt.wantIP) {
				t.Errorf("IP() = %v, want %v", got, tt.wantIP)
			}
		})
	}

	// IPv4 clients come back as 4-byte addresses.
	if ip := (Client{Addr: netip.MustParseAddr("::ffff:203.0.113.5").Unmap()}).IP(); len(ip) != net.IPv4len {
		t.Fatalf("IPv4 IP() length %d, want %d", len(ip), net.IPv4len)
	}
}

func TestResolveLegacy(t *testing.T) {
	tests := []struct {
		name      string
		remote    string
		xff       []string // one element per header line
		wantAddr  string   // empty for an invalid Addr
		wantPeer  string   // empty for an invalid Peer
		wantFwd   string
		malformed bool
	}{
		{
			name:     "no header",
			remote:   "198.51.100.1:5000",
			wantAddr: "198.51.100.1",
			wantPeer: "198.51.100.1",
		},
		{
			name:     "single entry from any peer",
			remote:   "198.51.100.1:5000",
			xff:      []string{"203.0.113.5"},
			wantAddr: "203.0.113.5",
			wantPeer: "198.51.100.1",
			wantFwd:  "203.0.113.5",
		},
		{
			name:     "chain takes leftmost",
			remote:   "198.51.100.1:5000",
			xff:      []string{"203.0.113.5,198.51.100.10"},
			wantAddr: "203.0.113.5",
			wantPeer: "198.51.100.1",
			wantFwd:  "203.0.113.5,198.51.100.10",
		},
		{
			name:     "chain with spaces",
			remote:   "198.51.100.1:5000",
			xff:      []string{" 203.0.113.5 , 198.51.100.10"},
			wantAddr: "203.0.113.5",
			wantPeer: "198.51.100.1",
			wantFwd:  " 203.0.113.5 , 198.51.100.10",
		},
		{
			name:     "multiple header lines",
			remote:   "198.51.100.1:5000",
			xff:      []string{"203.0.113.5", "198.51.100.10"},
			wantAddr: "203.0.113.5",
			wantPeer: "198.51.100.1",
			wantFwd:  "203.0.113.5, 198.51.100.10",
		},
		{
			name:     "blank first line skipped",
			remote:   "198.51.100.1:5000",
			xff:      []string{" , ", "203.0.113.5"},
			wantAddr: "203.0.113.5",
			wantPeer: "198.51.100.1",
			wantFwd:  " , , 203.0.113.5",
		},
		{
			name:      "unparseable leftmost skipped",
			remote:    "198.51.100.1:5000",
			xff:       []string{"unknown, 203.0.113.5"},
			wantAddr:  "203.0.113.5",
			wantPeer:  "198.51.100.1",
			wantFwd:   "unknown, 203.0.113.5",
			malformed: true,
		},
		{
			name:      "no valid entry uses peer",
			remote:    "198.51.100.1:5000",
			xff:       []string{"unknown, garbage"},
			wantAddr:  "198.51.100.1",
			wantPeer:  "198.51.100.1",
			wantFwd:   "unknown, garbage",
			malformed: true,
		},
		{
			name:     "empty header uses peer",
			remote:   "198.51.100.1:5000",
			xff:      []string{""},
			wantAddr: "198.51.100.1",
			wantPeer: "198.51.100.1",
		},
		{
			name:     "ports stripped",
			remote:   "198.51.100.1:5000",
			xff:      []string{"[2001:db8::5]:443, 203.0.113.5:80"},
			wantAddr: "2001:db8::5",
			wantPeer: "198.51.100.1",
			wantFwd:  "[2001:db8::5]:443, 203.0.113.5:80",
		},
		{
			name:     "ipv4 mapped entry",
			remote:   "198.51.100.1:5000",
			xff:      []string{"::ffff:203.0.113.5"},
			wantAddr: "203.0.113.5",
			wantPeer: "198.51.100.1",
			wantFwd:  "::ffff:203.0.113.5",
		},
		{
			name:     "unparseable remote addr with header",
			remote:   "@unix",
			xff:      []string{"203.0.113.5"},
			wantAddr: "203.0.113.5",
			wantFwd:  "203.0.113.5",
		},
		{
			name:   "unparseable remote addr without header",
			remote: "@unix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ResolveLegacy(newRequest(tt.remote, tt.xff...))

			if got := addrString(c.Addr); got != tt.wantAddr {
				t.Errorf("Addr = %q, want %q", got, tt.wantAddr)
			}
			if got := addrString(c.Peer); got != tt.wantPeer {
				t.Errorf("Peer = %q, want %q", got, tt.wantPeer)
			}
			if c.PeerRaw != tt.remote {
				t.Errorf("PeerRaw = %q, want %q", c.PeerRaw, tt.remote)
			}
			if c.Forwarded != tt.wantFwd {
				t.Errorf("Forwarded = %q, want %q", c.Forwarded, tt.wantFwd)
			}
			if c.Malformed != tt.malformed {
				t.Errorf("Malformed = %v, want %v", c.Malformed, tt.malformed)
			}
		})
	}

	if c := ResolveLegacy(nil); c != (Client{}) {
		t.Fatalf("ResolveLegacy(nil) = %+v, want zero Client", c)
	}
}

func TestResolveLegacyHopCap(t *testing.T) {
	// maxHops unparseable entries before the address: the walk stops at the cap.
	entries := make([]string, 0, maxHops+1)
	for i := 0; i < maxHops; i++ {
		entries = append(entries, "unknown")
	}
	entries = append(entries, "203.0.113.5")

	c := ResolveLegacy(newRequest("198.51.100.1:5000", strings.Join(entries, ", ")))
	if want := "198.51.100.1"; c.Addr.String() != want || !c.Malformed {
		t.Fatalf("Addr = %s Malformed = %v, want %s and true", c.Addr, c.Malformed, want)
	}

	// One fewer and the address is inside the cap, across header lines too.
	c = ResolveLegacy(newRequest("198.51.100.1:5000", entries[1:]...))
	if want := "203.0.113.5"; c.Addr.String() != want {
		t.Fatalf("Addr = %s, want %s", c.Addr, want)
	}
}
