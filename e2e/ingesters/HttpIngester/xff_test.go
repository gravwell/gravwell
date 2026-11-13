package HttpIngester

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"gravwell/e2e"
)

// TestXFF checks the SRC of entries sent with X-Forwarded-For in each client
// address mode: legacy (no Trusted-Proxies), Ignore-Untrusted-Forwarded-For,
// Trusted-Proxies listing the test client's peer, and Trusted-Proxies that does
// not list it.
func TestXFF(t *testing.T) {
	// notForwarded marks a send whose SRC must be the TCP peer. The peer address
	// depends on the Docker setup, so it is checked to be a private address that
	// is the same for every such send in a case.
	var notForwarded func(*testing.T, net.IP)
	is := func(want string) func(*testing.T, net.IP) {
		return func(t *testing.T, src net.IP) {
			if !src.Equal(net.ParseIP(want)) {
				e2e.Fatalf(t, "SRC %v, want %s", src, want)
			}
		}
	}

	// spoofed has a client-supplied entry left of the client and a trusted load
	// balancer right of it. Legacy mode takes the leftmost entry, Trusted-Proxies
	// walks from the right and stops at the first untrusted entry.
	const spoofed = "1.1.1.1, 203.0.113.7, 198.51.100.10"

	type send struct {
		xff   string
		check func(*testing.T, net.IP)
	}

	tests := []struct {
		name  string
		sends []send
	}{
		{
			name: "xff-legacy",
			sends: []send{
				{xff: "192.0.2.99", check: is("192.0.2.99")},
				{xff: "203.0.113.7,198.51.100.10", check: is("203.0.113.7")},
				{xff: spoofed, check: is("1.1.1.1")},
			},
		},
		{
			name: "xff-ignore",
			sends: []send{
				{xff: "192.0.2.99", check: notForwarded},
				{xff: "203.0.113.7,198.51.100.10", check: notForwarded},
				{xff: spoofed, check: notForwarded},
			},
		},
		{
			name: "xff",
			sends: []send{
				{xff: "192.0.2.99", check: is("192.0.2.99")},
				{xff: "203.0.113.7,198.51.100.10", check: is("203.0.113.7")},
				{xff: spoofed, check: is("203.0.113.7")},
			},
		},
		{
			// Trusted-Proxies is set but the test client's peer is not in it
			name: "xff-untrusted",
			sends: []send{
				{xff: "192.0.2.99", check: notForwarded},
				{xff: "203.0.113.7,198.51.100.10", check: notForwarded},
				{xff: spoofed, check: notForwarded},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, endpoint := setup(t, tt.name)

			for _, s := range tt.sends {
				req, err := http.NewRequest(http.MethodPost, endpoint+"/ingest", strings.NewReader(s.xff))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("X-Forwarded-For", s.xff)
				sendHttp(t, req, http.StatusOK)
			}

			c := e2e.GetClient(t)
			ents := e2e.WaitForEntries(t, c, "tag=http-"+tt.name, time.Minute, len(tt.sends), 30*time.Second)
			if len(ents) != len(tt.sends) {
				e2e.Fatalf(t, "got %d entries, want %d", len(ents), len(tt.sends))
			}

			// each entry's data is the header it was sent with
			var peer net.IP
			for _, s := range tt.sends {
				found := false
				for _, ent := range ents {
					if string(ent.Data) != s.xff {
						continue
					}
					found = true
					if s.check != nil {
						s.check(t, ent.SRC)
						continue
					}

					if ent.SRC == nil || ent.SRC.IsUnspecified() || !ent.SRC.IsPrivate() {
						e2e.Fatalf(t, "SRC %v for X-Forwarded-For %q, want the private TCP peer", ent.SRC, s.xff)
					}
					if peer == nil {
						peer = ent.SRC
					} else if !peer.Equal(ent.SRC) {
						e2e.Fatalf(t, "SRC %v for X-Forwarded-For %q, want the TCP peer %v of the other sends", ent.SRC, s.xff, peer)
					}
				}
				if !found {
					e2e.Fatalf(t, "no entry for X-Forwarded-For %q", s.xff)
				}
			}
		})
	}
}
