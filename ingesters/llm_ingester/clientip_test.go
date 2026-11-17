/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/llm_ingester/protocol"
	"github.com/gravwell/gravwell/v3/utils/trustedproxy"
)

// newTestClients returns a clientSource that discards its log output.
func newTestClients(t *testing.T, cr clientResolver) *clientSource {
	t.Helper()

	cs, err := newClientSource(cr, log.NewDiscardLogger())
	if err != nil {
		t.Fatalf("newClientSource: %v", err)
	}

	return cs
}

// newVerifyConfig returns a config that passes Verify, for setting the client
// address options on.
func newVerifyConfig() *cfgType {
	return &cfgType{
		gbl: gbl{
			IngestConfig: config.IngestConfig{
				Ingest_Secret:            "testing",
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
			},
		},
		Listener: map[string]*listener{"l1": validListener()},
	}
}

func newXFFRequest(remote string, xff ...string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.RemoteAddr = remote

	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}

	return r
}

// lockedBuffer is a log destination that is safe for concurrent writers.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (lb *lockedBuffer) Write(b []byte) (int, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	return lb.buf.Write(b)
}

func (lb *lockedBuffer) Close() error {
	return nil
}

func (lb *lockedBuffer) String() string {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	return lb.buf.String()
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name    string
		trusted []string
		ignore  bool // Ignore-Untrusted-Forwarded-For
		remote  string
		xff     []string // one element per header line
		want    string
	}{
		// legacy mode: no Trusted-Proxy, header trusted from any peer
		{
			name:   "legacy no header",
			remote: "192.0.2.10:5000",
			want:   "192.0.2.10",
		},
		{
			name:   "legacy single entry used as before",
			remote: "192.0.2.10:5000",
			xff:    []string{"203.0.113.7"},
			want:   "203.0.113.7",
		},
		{
			name:   "legacy padded entry used as before",
			remote: "192.0.2.10:5000",
			xff:    []string{"  203.0.113.7  "},
			want:   "203.0.113.7",
		},
		{
			name:   "legacy multi entry header uses leftmost",
			remote: "192.0.2.10:5000",
			xff:    []string{"203.0.113.7,198.51.100.10"},
			want:   "203.0.113.7",
		},
		{
			name:   "legacy multi entry header with spaces",
			remote: "192.0.2.10:5000",
			xff:    []string{"203.0.113.7, 198.51.100.10"},
			want:   "203.0.113.7",
		},
		{
			name:   "legacy multiple header lines use leftmost",
			remote: "192.0.2.10:5000",
			xff:    []string{"203.0.113.7", "198.51.100.10"},
			want:   "203.0.113.7",
		},
		{
			name:   "legacy entry with port",
			remote: "192.0.2.10:5000",
			xff:    []string{"203.0.113.7:4444"},
			want:   "203.0.113.7",
		},
		{
			name:   "legacy blank leading entry skipped",
			remote: "192.0.2.10:5000",
			xff:    []string{", 203.0.113.7"},
			want:   "203.0.113.7",
		},
		{
			name:   "legacy no valid entry uses peer",
			remote: "192.0.2.10:5000",
			xff:    []string{"not-an-ip"},
			want:   "192.0.2.10",
		},
		{
			name:   "legacy peer without port",
			remote: "192.0.2.10",
			want:   "192.0.2.10",
		},
		{
			name:   "legacy nothing usable falls back to loopback",
			remote: "also-bogus",
			xff:    []string{"bogus"},
			want:   "127.0.0.1",
		},

		// Ignore-Untrusted-Forwarded-For: strict with nothing trusted
		{
			name:   "ignore single entry uses peer",
			ignore: true,
			remote: "192.0.2.10:5000",
			xff:    []string{"203.0.113.7"},
			want:   "192.0.2.10",
		},
		{
			name:   "ignore multi entry uses peer",
			ignore: true,
			remote: "192.0.2.10:5000",
			xff:    []string{"203.0.113.7,198.51.100.10"},
			want:   "192.0.2.10",
		},
		{
			name:   "ignore unparseable peer falls back to loopback",
			ignore: true,
			remote: "@unix",
			xff:    []string{"203.0.113.7"},
			want:   "127.0.0.1",
		},

		// Trusted-Proxy set: strict
		{
			// a load balancer that appends the client and its own address
			name:    "trusted load balancer chain",
			trusted: []string{"10.0.0.0/8", "198.51.100.10"},
			remote:  "10.1.2.3:51234",
			xff:     []string{"203.0.113.7,198.51.100.10"},
			want:    "203.0.113.7",
		},
		{
			name:    "client supplied entry left of the client is ignored",
			trusted: []string{"10.0.0.0/8", "198.51.100.10"},
			remote:  "10.1.2.3:51234",
			xff:     []string{"192.0.2.99, 203.0.113.7, 198.51.100.10"},
			want:    "203.0.113.7",
		},
		{
			name:    "trusted proxy across header lines",
			trusted: []string{"10.0.0.1"},
			remote:  "10.0.0.1:5000",
			xff:     []string{"192.0.2.99", "203.0.113.7"},
			want:    "203.0.113.7",
		},
		{
			name:    "untrusted peer with trusted list",
			trusted: []string{"10.0.0.1"},
			remote:  "192.0.2.10:5000",
			xff:     []string{"203.0.113.7"},
			want:    "192.0.2.10",
		},
		{
			name:    "trusted list wins over ignore",
			trusted: []string{"10.0.0.1"},
			ignore:  true,
			remote:  "10.0.0.1:5000",
			xff:     []string{"203.0.113.7"},
			want:    "203.0.113.7",
		},
		{
			name:    "malformed entry stops at the trusted peer",
			trusted: []string{"10.0.0.1"},
			remote:  "10.0.0.1:5000",
			xff:     []string{"203.0.113.7, unknown"},
			want:    "10.0.0.1",
		},
		{
			name:    "ipv6 trusted peer",
			trusted: []string{"2001:db8::/64"},
			remote:  "[2001:db8::1]:443",
			xff:     []string{"2001:db8:ffff::7"},
			want:    "2001:db8:ffff::7",
		},
		{
			name:    "strict unparseable peer falls back to loopback",
			trusted: []string{"10.0.0.1"},
			remote:  "@unix",
			xff:     []string{"203.0.113.7"},
			want:    "127.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newVerifyConfig()
			cfg.Trusted_Proxy = tt.trusted
			cfg.Ignore_Untrusted_Forwarded_For = tt.ignore
			if err := cfg.Verify(); err != nil {
				t.Fatalf("Verify: %v", err)
			}

			cs := newTestClients(t, cfg.resolver)

			got := clientIP(cs.client(newXFFRequest(tt.remote, tt.xff...)))
			if !got.Equal(net.ParseIP(tt.want)) {
				t.Fatalf("got %v, want %s", got, tt.want)
			}
		})
	}
}

func TestLegacyForwardedWarning(t *testing.T) {
	logBuf := &lockedBuffer{}
	cs, err := newClientSource(clientResolver{legacy: true}, log.New(logBuf))
	if err != nil {
		t.Fatalf("newClientSource: %v", err)
	}

	warnings := func() int {
		return strings.Count(logBuf.String(), "Ignore-Untrusted-Forwarded-For")
	}

	cs.client(newXFFRequest("192.0.2.10:5000"))
	cs.client(newXFFRequest("192.0.2.10:5000", "192.0.2.10"))
	if n := warnings(); n != 0 {
		t.Fatalf("%d warnings when the header did not change the client, want 0", n)
	}

	cs.client(newXFFRequest("192.0.2.10:5000", "203.0.113.7"))
	if n := warnings(); n != 1 {
		t.Fatalf("%d warnings after the header set the client, want 1", n)
	}
	for _, want := range []string{`src="203.0.113.7"`, `peer="192.0.2.10"`, `xff="203.0.113.7"`} {
		if !strings.Contains(logBuf.String(), want) {
			t.Fatalf("warning %q does not contain %s", logBuf.String(), want)
		}
	}

	for i := 0; i < 10; i++ {
		cs.client(newXFFRequest("192.0.2.11:5000", "203.0.113.8"))
	}
	if n := warnings(); n != 1 {
		t.Fatalf("%d warnings, want 1: warnings are rate limited across peers", n)
	}

	cs.legacyWarned.Store(int64(monoNow() - 2*legacyWarnInterval))
	cs.client(newXFFRequest("192.0.2.10:5000", "203.0.113.7"))
	if n := warnings(); n != 2 {
		t.Fatalf("%d warnings, want 2: the warning repeats after the interval", n)
	}

	for _, cr := range []clientResolver{{}, {trusted: mustList(t, "192.0.2.10")}} {
		strict, err := newClientSource(cr, log.New(logBuf))
		if err != nil {
			t.Fatalf("newClientSource: %v", err)
		}

		strict.client(newXFFRequest("192.0.2.10:5000", "203.0.113.7"))
	}
	if n := warnings(); n != 2 {
		t.Fatalf("%d warnings, want 2: strict modes never warn", n)
	}
}

// TestLegacyForwardedWarningConcurrent checks that concurrent requests on every
// listener produce one warning in total.
func TestLegacyForwardedWarningConcurrent(t *testing.T) {
	logBuf := &lockedBuffer{}
	cs, err := newClientSource(clientResolver{legacy: true}, log.New(logBuf))
	if err != nil {
		t.Fatalf("newClientSource: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cs.client(newXFFRequest("192.0.2.10:5000", "203.0.113.7"))
		}()
	}
	wg.Wait()

	if n := strings.Count(logBuf.String(), "Ignore-Untrusted-Forwarded-For"); n != 1 {
		t.Fatalf("%d warnings from concurrent requests, want 1", n)
	}
}

func TestClientKVs(t *testing.T) {
	tests := []struct {
		name   string
		client trustedproxy.Client
		want   map[string]string
	}{
		{
			name:   "no header",
			client: trustedproxy.List{}.Resolve(newXFFRequest("192.0.2.10:5000")),
			want:   map[string]string{"src": "192.0.2.10"},
		},
		{
			name:   "header from untrusted peer",
			client: trustedproxy.List{}.Resolve(newXFFRequest("192.0.2.10:5000", "203.0.113.7")),
			want:   map[string]string{"src": "192.0.2.10", "peer": "192.0.2.10", "xff": "203.0.113.7"},
		},
		{
			name: "malformed header from trusted peer",
			client: mustList(t, "10.0.0.1").Resolve(
				newXFFRequest("10.0.0.1:5000", "203.0.113.7, unknown"),
			),
			want: map[string]string{
				"src":           "10.0.0.1",
				"peer":          "10.0.0.1",
				"xff":           "203.0.113.7, unknown",
				"xff-malformed": "true",
			},
		},
		{
			name:   "legacy header with unparseable peer",
			client: trustedproxy.ResolveLegacy(newXFFRequest("@unix", "203.0.113.7")),
			want:   map[string]string{"src": "203.0.113.7", "peer": "@unix", "xff": "203.0.113.7"},
		},
		{
			// the logged address is the SRC fallback, with the raw peer beside it
			name:   "unparseable peer",
			client: trustedproxy.List{}.Resolve(newXFFRequest("@unix")),
			want:   map[string]string{"src": "127.0.0.1", "peer": "@unix"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string]string{}
			for _, kv := range clientKVs("src", tt.client) {
				got[kv.Name] = kv.Value
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			if got["src"] != clientIP(tt.client).String() {
				t.Fatalf("logged src %q is not the entry SRC %v", got["src"], clientIP(tt.client))
			}
		})
	}
}

func TestVerifyTrustedProxy(t *testing.T) {
	tests := []struct {
		name       string
		cfg        []string
		env        string
		ignore     bool     // Ignore-Untrusted-Forwarded-For in the config
		ignoreEnv  string   // GRAVWELL_LLM_IGNORE_UNTRUSTED_FORWARDED_FOR
		trusted    []string // addresses expected to be trusted
		untrusted  []string // addresses expected not to be trusted
		wantLegacy bool
		wantErr    string
	}{
		{
			name:       "unset is legacy mode",
			untrusted:  []string{"127.0.0.1", "10.0.0.1"},
			wantLegacy: true,
		},
		{
			name:      "ignore turns off legacy mode",
			ignore:    true,
			untrusted: []string{"127.0.0.1", "10.0.0.1"},
		},
		{
			name:      "ignore env var turns off legacy mode",
			ignoreEnv: "true",
			untrusted: []string{"127.0.0.1"},
		},
		{
			name:       "ignore env var false keeps legacy mode",
			ignoreEnv:  "false",
			wantLegacy: true,
		},
		{
			name:      "trusted list is never legacy",
			cfg:       []string{"10.0.0.1"},
			trusted:   []string{"10.0.0.1"},
			untrusted: []string{"10.0.0.2"},
		},
		{
			name:      "invalid ignore env var",
			ignoreEnv: "maybe",
			wantErr:   envIgnoreUntrustedXFF,
		},
		{
			name:      "addresses and blocks",
			cfg:       []string{"10.0.0.1", "192.168.100.0/24", "2001:db8::/64"},
			trusted:   []string{"10.0.0.1", "192.168.100.7", "2001:db8::5"},
			untrusted: []string{"10.0.0.2", "192.168.101.7"},
		},
		{
			name:      "env var comma list",
			env:       "10.0.0.1, 192.168.100.0/24",
			trusted:   []string{"10.0.0.1", "192.168.100.7"},
			untrusted: []string{"10.0.0.2"},
		},
		{
			name:      "config wins over env var",
			cfg:       []string{"10.0.0.1"},
			env:       "10.0.0.2",
			trusted:   []string{"10.0.0.1"},
			untrusted: []string{"10.0.0.2"},
		},
		{
			name:    "invalid entry",
			cfg:     []string{"10.0.0.1", "proxy.example.com"},
			wantErr: `"proxy.example.com"`,
		},
		{
			name:    "invalid env entry",
			env:     "10.0.0.1,nope",
			wantErr: `"nope"`,
		},
		{
			name:    "ipv4 trust all rejected",
			cfg:     []string{"0.0.0.0/0"},
			wantErr: `"0.0.0.0/0"`,
		},
		{
			name:    "ipv4 split trust all rejected",
			cfg:     []string{"0.0.0.0/1", "128.0.0.0/1"},
			wantErr: "cover every IPv4 address",
		},
		{
			name:    "blank entries rejected",
			cfg:     []string{"", " "},
			wantErr: "blank",
		},
		{
			name:    "env var of only separators rejected",
			env:     ",",
			wantErr: "blank",
		},
		{
			name:    "env var of only whitespace rejected",
			env:     " ",
			wantErr: "blank",
		},
		{
			// the config value wins over the env var, so it must not be dropped
			name:    "blank config value with env var rejected",
			cfg:     []string{""},
			env:     "10.0.0.1",
			wantErr: "blank",
		},
		{
			name:    "ipv6 trust all rejected",
			cfg:     []string{"::/0"},
			wantErr: `"::/0"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != `` {
				t.Setenv(envTrustedProxy, tt.env)
			}
			if tt.ignoreEnv != `` {
				t.Setenv(envIgnoreUntrustedXFF, tt.ignoreEnv)
			}

			cfg := newVerifyConfig()
			cfg.Trusted_Proxy = tt.cfg
			cfg.Ignore_Untrusted_Forwarded_For = tt.ignore

			err := cfg.Verify()
			if tt.wantErr != `` {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Verify error %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}

			if cfg.resolver.legacy != tt.wantLegacy {
				t.Fatalf("legacy mode %v, want %v", cfg.resolver.legacy, tt.wantLegacy)
			}
			for _, a := range tt.trusted {
				if !cfg.resolver.trusted.Contains(netip.MustParseAddr(a)) {
					t.Errorf("%s should be trusted", a)
				}
			}
			for _, a := range tt.untrusted {
				if cfg.resolver.trusted.Contains(netip.MustParseAddr(a)) {
					t.Errorf("%s should not be trusted", a)
				}
			}
		})
	}
}

func TestGetConfigTrustedProxy(t *testing.T) {
	const conf = `
[Global]
	Ingest-Secret = "testing"
	Cleartext-Backend-Target=127.0.0.1:4023
	Trusted-Proxy=10.0.0.5
	Trusted-Proxy=192.168.100.0/24

[Listener "l1"]
	Bind = ":4180"
	Upstream-URL = "http://example.com"
	Protocol = "openai-chat"
`
	pth := filepath.Join(t.TempDir(), "llm.conf")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(pth, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write(conf)
	cfg, err := GetConfig(pth, "")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}

	if !reflect.DeepEqual(cfg.Trusted_Proxy, []string{"10.0.0.5", "192.168.100.0/24"}) {
		t.Fatalf("Trusted_Proxy %v", cfg.Trusted_Proxy)
	}
	for a, want := range map[string]bool{"10.0.0.5": true, "192.168.100.200": true, "10.0.0.6": false} {
		if got := cfg.resolver.trusted.Contains(netip.MustParseAddr(a)); got != want {
			t.Errorf("Contains(%s) = %v, want %v", a, got, want)
		}
	}
	if cfg.resolver.legacy {
		t.Fatal("a trusted list must not be legacy mode")
	}

	write(strings.Replace(conf, "Trusted-Proxy=10.0.0.5", "Trusted-Proxy=0.0.0.0/0", 1))
	if _, err = GetConfig(pth, ""); err == nil || !strings.Contains(err.Error(), `"0.0.0.0/0"`) {
		t.Fatalf("GetConfig error %v, want one naming 0.0.0.0/0", err)
	}

	// a value of only blanks must fail rather than fall back to legacy mode
	write(strings.NewReplacer(
		"Trusted-Proxy=10.0.0.5", `Trusted-Proxy=""`,
		"\tTrusted-Proxy=192.168.100.0/24\n", "",
	).Replace(conf))
	if _, err = GetConfig(pth, ""); err == nil || !strings.Contains(err.Error(), "blank") {
		t.Fatalf("GetConfig error %v, want one for blank Trusted-Proxy", err)
	}

	// no Trusted-Proxy is legacy mode until Ignore-Untrusted-Forwarded-For is set
	noProxies := strings.NewReplacer(
		"\tTrusted-Proxy=10.0.0.5\n", "",
		"\tTrusted-Proxy=192.168.100.0/24\n", "",
	).Replace(conf)

	write(noProxies)
	if cfg, err = GetConfig(pth, ""); err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if len(cfg.Trusted_Proxy) != 0 || !cfg.resolver.legacy {
		t.Fatalf("Trusted_Proxy %v legacy %v, want empty and legacy", cfg.Trusted_Proxy, cfg.resolver.legacy)
	}

	write(strings.Replace(noProxies, "[Global]\n", "[Global]\n\tIgnore-Untrusted-Forwarded-For=true\n", 1))
	if cfg, err = GetConfig(pth, ""); err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if !cfg.Ignore_Untrusted_Forwarded_For || cfg.resolver.legacy {
		t.Fatalf("Ignore_Untrusted_Forwarded_For %v legacy %v, want true and not legacy",
			cfg.Ignore_Untrusted_Forwarded_For, cfg.resolver.legacy)
	}
}

// TestShippedExampleConfigTrustedProxy checks that the commented
// Trusted-Proxy examples in the shipped config parse once uncommented, and
// that the shipped config keeps the legacy default.
func TestShippedExampleConfigTrustedProxy(t *testing.T) {
	raw, err := os.ReadFile("gravwell_llm_ingester.conf")
	if err != nil {
		t.Fatal(err)
	}

	enabled := strings.NewReplacer(
		"#Cleartext-Backend-Target", "Cleartext-Backend-Target",
		"#Trusted-Proxy", "Trusted-Proxy",
	).Replace(string(raw))
	if enabled == strings.Replace(string(raw), "#Cleartext-Backend-Target", "Cleartext-Backend-Target", 1) {
		t.Fatal("the example config no longer carries a commented Trusted-Proxy example")
	}

	pth := filepath.Join(t.TempDir(), "llm.conf")
	if err := os.WriteFile(pth, []byte(enabled), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := GetConfig(pth, "")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if len(cfg.Trusted_Proxy) == 0 || cfg.resolver.legacy {
		t.Fatalf("uncommented examples gave Trusted_Proxy %v legacy %v", cfg.Trusted_Proxy, cfg.resolver.legacy)
	}
}

func TestNewClientSourceRequiresLogger(t *testing.T) {
	if _, err := newClientSource(clientResolver{}, nil); err == nil {
		t.Fatal("newClientSource accepted a nil logger")
	}
}

func TestNewProxyHandlerRequiresClientSource(t *testing.T) {
	l := validListener()
	if err := l.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	proto, err := protocol.Lookup(l.Protocol)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	sessions, err := newSessionStore(time.Hour, 0, "")
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}

	_, err = newProxyHandler("test", l, proto, entry.EntryTag(0),
		processors.NewProcessorSet(&capture{}), sessions, nil, log.NewDiscardLogger())
	if err == nil {
		t.Fatal("newProxyHandler accepted a nil client source")
	}
}

// TestProxyClientAddress sends real requests through ServeHTTP and checks the
// entry SRC and which requests share a derived session. Every request carries
// the same messages, so two requests share a session exactly when they resolve
// to the same client.
func TestProxyClientAddress(t *testing.T) {
	type send struct {
		remote  string
		xff     []string
		wantSRC string
		session string // requests with the same label must share a session
	}

	tests := []struct {
		name     string
		resolver clientResolver
		sends    []send
	}{
		{
			name:     "legacy",
			resolver: clientResolver{legacy: true},
			sends: []send{
				{remote: "192.0.2.10:5000", xff: []string{"203.0.113.7"}, wantSRC: "203.0.113.7", session: "a"},
				// the key is the leftmost address, as for a single-address header
				{remote: "192.0.2.10:5000", xff: []string{"203.0.113.7, 198.51.100.10"}, wantSRC: "203.0.113.7", session: "a"},
				{remote: "192.0.2.11:5000", xff: []string{"203.0.113.7", "198.51.100.10"}, wantSRC: "203.0.113.7", session: "a"},
				{remote: "192.0.2.10:5000", xff: []string{"198.51.100.20, 203.0.113.7"}, wantSRC: "198.51.100.20", session: "b"},
				{remote: "192.0.2.10:5000", wantSRC: "192.0.2.10", session: "c"},
				{remote: "192.0.2.10:5000", xff: []string{"not-an-ip"}, wantSRC: "192.0.2.10", session: "c"},
			},
		},
		{
			name:     "ignore untrusted forwarded for",
			resolver: clientResolver{},
			sends: []send{
				{remote: "192.0.2.10:5000", wantSRC: "192.0.2.10", session: "a"},
				{remote: "192.0.2.10:5000", xff: []string{"203.0.113.7"}, wantSRC: "192.0.2.10", session: "a"},
				{remote: "192.0.2.10:5000", xff: []string{"203.0.113.7, 198.51.100.10"}, wantSRC: "192.0.2.10", session: "a"},
				{remote: "192.0.2.11:5000", xff: []string{"192.0.2.10"}, wantSRC: "192.0.2.11", session: "b"},
			},
		},
		{
			name:     "trusted proxies",
			resolver: clientResolver{trusted: mustList(t, "10.0.0.0/8", "198.51.100.10")},
			sends: []send{
				{remote: "10.0.0.1:5000", xff: []string{"203.0.113.7"}, wantSRC: "203.0.113.7", session: "a"},
				// entries left of the client do not change the key
				{remote: "10.0.0.2:5000", xff: []string{"192.0.2.99, 203.0.113.7, 198.51.100.10"}, wantSRC: "203.0.113.7", session: "a"},
				{remote: "10.0.0.1:5000", xff: []string{"192.0.2.98", "203.0.113.7"}, wantSRC: "203.0.113.7", session: "a"},
				// an untrusted peer is its own client whatever the header says
				{remote: "192.0.2.10:5000", xff: []string{"203.0.113.7"}, wantSRC: "192.0.2.10", session: "b"},
				{remote: "10.0.0.1:5000", wantSRC: "10.0.0.1", session: "c"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, chatRespBody)
			}))
			defer srv.Close()

			ph, c := newTestHandler(t, srv.URL, nil)
			ph.clients = newTestClients(t, tt.resolver)

			labels := map[string]string{} // session label to session ID
			ids := map[string]string{}    // session ID to session label

			for i, s := range tt.sends {
				c.mu.Lock()
				first := len(c.ents)
				c.mu.Unlock()

				req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatReqBody))
				req.Header.Set("Content-Type", "application/json")
				req.RemoteAddr = s.remote
				for _, v := range s.xff {
					req.Header.Add("X-Forwarded-For", v)
				}

				w := httptest.NewRecorder()
				ph.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					t.Fatalf("send %d: status %d", i, w.Code)
				}

				c.mu.Lock()
				ents := c.ents[first:]
				c.mu.Unlock()
				if len(ents) == 0 {
					t.Fatalf("send %d: nothing ingested", i)
				}

				for _, e := range ents {
					if !e.SRC.Equal(net.ParseIP(s.wantSRC)) {
						t.Fatalf("send %d: SRC %v, want %s", i, e.SRC, s.wantSRC)
					}
				}

				v, ok := ents[0].GetEnumeratedValue("session_id")
				if !ok {
					t.Fatalf("send %d: entry missing session_id", i)
				}
				id, _ := v.(string)

				if want, ok := labels[s.session]; ok && want != id {
					t.Fatalf("send %d: session %q, want %q shared with label %s", i, id, want, s.session)
				}
				if other, ok := ids[id]; ok && other != s.session {
					t.Fatalf("send %d: session %q is shared with label %s, want a session of its own", i, id, other)
				}
				labels[s.session] = id
				ids[id] = s.session
			}
		})
	}
}

// TestProxyLogsClient checks that warnings about a request carry the resolved
// client under src, the TCP peer, and the raw X-Forwarded-For header under xff.
func TestProxyLogsClient(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*listener)
		body   string
		want   string // the warning
	}{
		{name: "unparseable body", body: "{not json", want: "failed to parse request body"},
		{
			name:   "body too large",
			mutate: func(l *listener) { l.Max_Body = 4 },
			body:   chatReqBody,
			want:   "request body too large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, chatRespBody)
			}))
			defer srv.Close()

			logBuf := &lockedBuffer{}
			ph, _ := newTestHandler(t, srv.URL, tt.mutate)
			ph.lg = log.New(logBuf)
			ph.clients = newTestClients(t, clientResolver{trusted: mustList(t, "10.0.0.0/8")})

			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Forwarded-For", "192.0.2.99, 203.0.113.7")
			req.RemoteAddr = "10.0.0.1:5000"
			ph.ServeHTTP(httptest.NewRecorder(), req)

			var line string
			for _, l := range strings.Split(logBuf.String(), "\n") {
				if strings.Contains(l, tt.want) {
					line = l
				}
			}
			if line == "" {
				t.Fatalf("no %q warning in %q", tt.want, logBuf.String())
			}
			for _, want := range []string{`src="203.0.113.7"`, `peer="10.0.0.1"`, `xff="192.0.2.99, 203.0.113.7"`} {
				if !strings.Contains(line, want) {
					t.Fatalf("warning %q does not contain %s", line, want)
				}
			}
		})
	}
}

// TestProxyClientSourceShared checks that listeners built with the same
// clientSource share the legacy warning rate limit.
func TestProxyClientSourceShared(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, chatRespBody)
	}))
	defer srv.Close()

	logBuf := &lockedBuffer{}
	cs, err := newClientSource(clientResolver{legacy: true}, log.New(logBuf))
	if err != nil {
		t.Fatalf("newClientSource: %v", err)
	}

	for _, name := range []string{"one", "two"} {
		ph, _ := newTestHandler(t, srv.URL, nil)
		ph.name = name
		ph.clients = cs

		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatReqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "203.0.113.7")
		req.RemoteAddr = "192.0.2.10:5000"
		ph.ServeHTTP(httptest.NewRecorder(), req)
	}

	if n := strings.Count(logBuf.String(), "Ignore-Untrusted-Forwarded-For"); n != 1 {
		t.Fatalf("%d warnings across two listeners, want 1", n)
	}
}

func TestSetForwardedFor(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		prior  []string // X-Forwarded-For lines already on the upstream request
		want   []string
	}{
		{name: "no prior chain", remote: "192.0.2.10:5000", want: []string{"192.0.2.10"}},
		{name: "prior chain", remote: "192.0.2.10:5000", prior: []string{"203.0.113.7"}, want: []string{"203.0.113.7, 192.0.2.10"}},
		{
			name:   "every prior line kept",
			remote: "192.0.2.10:5000",
			prior:  []string{"203.0.113.7", "198.51.100.10, 198.51.100.11"},
			want:   []string{"203.0.113.7, 198.51.100.10, 198.51.100.11, 192.0.2.10"},
		},
		{name: "peer without port", remote: "192.0.2.10", want: []string{"192.0.2.10"}},
		{
			name:   "ipv6 peer",
			remote: "[2001:db8::1]:443",
			prior:  []string{"203.0.113.7"},
			want:   []string{"203.0.113.7, 2001:db8::1"},
		},
		{name: "empty prior line", remote: "192.0.2.10:5000", prior: []string{""}, want: []string{"192.0.2.10"}},
		{
			name:   "blank prior lines dropped",
			remote: "192.0.2.10:5000",
			prior:  []string{"", "203.0.113.7", "  ", "198.51.100.10"},
			want:   []string{"203.0.113.7, 198.51.100.10, 192.0.2.10"},
		},
		{name: "no peer", remote: "", prior: []string{"203.0.113.7"}, want: []string{"203.0.113.7"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newXFFRequest(tt.remote)
			out := httptest.NewRequest(http.MethodPost, "/", nil)
			for _, v := range tt.prior {
				out.Header.Add("X-Forwarded-For", v)
			}

			setForwardedFor(out, r)

			if got := out.Header.Values("X-Forwarded-For"); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("X-Forwarded-For %q, want %q", got, tt.want)
			}
		})
	}
}

func mustList(t *testing.T, entries ...string) trustedproxy.List {
	t.Helper()

	l, err := trustedproxy.Parse(entries)
	if err != nil {
		t.Fatalf("Parse(%q): %v", entries, err)
	}

	return l
}
