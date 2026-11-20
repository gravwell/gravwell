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
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/ingesters/llm_ingester/protocol"
	"github.com/gravwell/gravwell/v4/ingesters/utils/clientip"
	"github.com/gravwell/gravwell/v4/utils/trustedproxy"
)

// newTestClients returns a clientSource that discards its log output.
func newTestClients(t *testing.T, cr clientip.Resolver) *clientSource {
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
		Ingest_Secret:            "testing",
		Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
		Listener:                 map[string]*listener{"l1": validListener()},
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

// TestVerifyTrustedProxy checks that Verify builds the resolver from the config
// and the GRAVWELL_LLM_ environment variables. clientip.LoadResolver tests the
// options themselves.
func TestVerifyTrustedProxy(t *testing.T) {
	tests := []struct {
		name       string
		cfg        []string
		env        string
		file       string   // contents of the file named by the _FILE variable
		ignore     bool     // Ignore-Untrusted-Forwarded-For in the config
		ignoreEnv  string   // GRAVWELL_LLM_IGNORE_UNTRUSTED_FORWARDED_FOR
		want       []string // the trusted list
		wantIgnore bool
		wantErr    string
	}{
		{name: "unset is legacy mode"},
		{name: "ignore", ignore: true, wantIgnore: true},
		{name: "ignore env var", ignoreEnv: "true", wantIgnore: true},
		{name: "config list", cfg: []string{"10.0.0.1", "192.168.100.0/24"}, want: []string{"10.0.0.1", "192.168.100.0/24"}},
		{name: "env var list", env: "10.0.0.1, 192.168.100.0/24", want: []string{"10.0.0.1", "192.168.100.0/24"}},
		{name: "file list", file: "10.0.0.1\n10.0.0.2\n", want: []string{"10.0.0.1", "10.0.0.2"}},
		{name: "invalid ignore env var", ignoreEnv: "maybe", wantErr: envIgnoreUntrustedXFF},
		{name: "invalid entry", cfg: []string{"proxy.example.com"}, wantErr: `Trusted-Proxy is invalid: invalid trusted proxy "proxy.example.com"`},
		{name: "env var of only separators", env: ",", wantErr: "failed to load Trusted-Proxy: " + envTrustedProxy + ": "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != `` {
				t.Setenv(envTrustedProxy, tt.env)
			}
			if tt.file != `` {
				t.Setenv(envTrustedProxy+`_FILE`, writeTrustedProxyFile(t, tt.file))
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

			if want := clientip.NewResolver(mustList(t, tt.want...), tt.wantIgnore); !reflect.DeepEqual(cfg.resolver, want) {
				t.Fatalf("resolver %+v, want %+v", cfg.resolver, want)
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
	if want := clientip.NewResolver(mustList(t, "10.0.0.5", "192.168.100.0/24"), false); !reflect.DeepEqual(cfg.resolver, want) {
		t.Fatalf("resolver %+v, want %+v", cfg.resolver, want)
	}
	if cfg.resolver.Legacy() {
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
	if len(cfg.Trusted_Proxy) != 0 || !cfg.resolver.Legacy() {
		t.Fatalf("Trusted_Proxy %v legacy %v, want empty and legacy", cfg.Trusted_Proxy, cfg.resolver.Legacy())
	}

	write(strings.Replace(noProxies, "[Global]\n", "[Global]\n\tIgnore-Untrusted-Forwarded-For=true\n", 1))
	if cfg, err = GetConfig(pth, ""); err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if !cfg.Ignore_Untrusted_Forwarded_For || cfg.resolver.Legacy() {
		t.Fatalf("Ignore_Untrusted_Forwarded_For %v legacy %v, want true and not legacy",
			cfg.Ignore_Untrusted_Forwarded_For, cfg.resolver.Legacy())
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
	if len(cfg.Trusted_Proxy) == 0 || cfg.resolver.Legacy() {
		t.Fatalf("uncommented examples gave Trusted_Proxy %v legacy %v", cfg.Trusted_Proxy, cfg.resolver.Legacy())
	}
}

func TestNewClientSourceRequiresLogger(t *testing.T) {
	if _, err := newClientSource(clientip.Resolver{}, nil); err == nil {
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
		resolver clientip.Resolver
		sends    []send
	}{
		{
			name:     "legacy",
			resolver: clientip.NewResolver(trustedproxy.List{}, false),
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
			resolver: clientip.NewResolver(trustedproxy.List{}, true),
			sends: []send{
				{remote: "192.0.2.10:5000", wantSRC: "192.0.2.10", session: "a"},
				{remote: "192.0.2.10:5000", xff: []string{"203.0.113.7"}, wantSRC: "192.0.2.10", session: "a"},
				{remote: "192.0.2.10:5000", xff: []string{"203.0.113.7, 198.51.100.10"}, wantSRC: "192.0.2.10", session: "a"},
				{remote: "192.0.2.11:5000", xff: []string{"192.0.2.10"}, wantSRC: "192.0.2.11", session: "b"},
			},
		},
		{
			name:     "trusted proxies",
			resolver: clientip.NewResolver(mustList(t, "10.0.0.0/8", "198.51.100.10"), false),
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
			ph.clients = newTestClients(t, clientip.NewResolver(mustList(t, "10.0.0.0/8"), false))

			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Forwarded-For", "192.0.2.99, 203.0.113.7")
			req.RemoteAddr = "10.0.0.1:5000"
			ph.ServeHTTP(httptest.NewRecorder(), req)

			var line string
			for l := range strings.SplitSeq(logBuf.String(), "\n") {
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
	cs, err := newClientSource(clientip.NewResolver(trustedproxy.List{}, false), log.New(logBuf))
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

func writeTrustedProxyFile(t *testing.T, content string) string {
	t.Helper()
	pth := filepath.Join(t.TempDir(), "trusted_proxy")
	if err := os.WriteFile(pth, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return pth
}
