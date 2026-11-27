package llm_ingester

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"gravwell/e2e"
)

// TestXFF checks the SRC of entries sent with X-Forwarded-For in each client
// address mode: legacy (no Trusted-Proxy), Ignore-Untrusted-Forwarded-For,
// Trusted-Proxy listing the test client's peer, and Trusted-Proxy that does
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
	// balancer right of it. Legacy mode takes the leftmost entry, Trusted-Proxy
	// walks from the right and stops at the first untrusted entry.
	const spoofed = "1.1.1.1, 203.0.113.7, 198.51.100.10"

	type send struct {
		xff   []string // one element per header line
		check func(*testing.T, net.IP)
	}

	tests := []struct {
		name  string
		sends []send
	}{
		{
			name: "xff-legacy",
			sends: []send{
				{xff: []string{"192.0.2.99"}, check: is("192.0.2.99")},
				{xff: []string{"203.0.113.7,198.51.100.10"}, check: is("203.0.113.7")},
				{xff: []string{"203.0.113.8", "198.51.100.10"}, check: is("203.0.113.8")},
				{xff: []string{spoofed}, check: is("1.1.1.1")},
			},
		},
		{
			name: "xff-ignore",
			sends: []send{
				{xff: []string{"192.0.2.99"}, check: notForwarded},
				{xff: []string{"203.0.113.7,198.51.100.10"}, check: notForwarded},
				{xff: []string{"203.0.113.8", "198.51.100.10"}, check: notForwarded},
				{xff: []string{spoofed}, check: notForwarded},
			},
		},
		{
			name: "xff",
			sends: []send{
				{xff: []string{"192.0.2.99"}, check: is("192.0.2.99")},
				{xff: []string{"203.0.113.7,198.51.100.10"}, check: is("203.0.113.7")},
				{xff: []string{"203.0.113.8", "198.51.100.10"}, check: is("203.0.113.8")},
				{xff: []string{spoofed}, check: is("203.0.113.7")},
			},
		},
		{
			// Trusted-Proxy is set but the test client's peer is not in it
			name: "xff-untrusted",
			sends: []send{
				{xff: []string{"192.0.2.99"}, check: notForwarded},
				{xff: []string{"203.0.113.7,198.51.100.10"}, check: notForwarded},
				{xff: []string{"203.0.113.8", "198.51.100.10"}, check: notForwarded},
				{xff: []string{spoofed}, check: notForwarded},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, endpoint := setup(t, tt.name)

			// each send's prompt is a unique marker that ends up in its entry
			for i, s := range tt.sends {
				sendChat(t, endpoint, marker(tt.name, i), s.xff)
			}

			c := e2e.GetClient(t)
			ents := e2e.WaitForEntries(t, c, "tag=llm-"+tt.name, time.Minute, len(tt.sends), 30*time.Second)
			if len(ents) != len(tt.sends) {
				e2e.Fatalf(t, "got %d entries, want %d", len(ents), len(tt.sends))
			}

			var peer net.IP
			for i, s := range tt.sends {
				found := false
				for _, ent := range ents {
					if !strings.Contains(string(ent.Data), marker(tt.name, i)) {
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

func marker(name string, i int) string {
	return fmt.Sprintf("%s-send-%d", name, i)
}

// sendChat posts a chat completion request with one user message. The
// listener's upstream is unreachable, so the proxy answers 502 after ingesting
// the request.
func sendChat(t *testing.T, endpoint, prompt string, xff []string) {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"model":    "e2e",
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint+"/v1/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for _, v := range xff {
		req.Header.Add("X-Forwarded-For", v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e2e.Fatal(t, err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		e2e.Fatalf(t, "status %d, want %d from the unreachable upstream", resp.StatusCode, http.StatusBadGateway)
	}
}
