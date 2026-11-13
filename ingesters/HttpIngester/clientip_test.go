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
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/utils/trustedproxy"
	"github.com/stretchr/testify/require"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name    string
		trusted []string
		ignore  bool // Ignore-Untrusted-Forwarded-For
		remote  string
		xff     []string // one element per header line
		want    string
	}{
		// legacy mode: no Trusted-Proxies, header trusted from any peer
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
			// a multi-address header used to resolve to 127.0.0.1
			name:   "legacy multi entry header uses leftmost",
			remote: "35.191.1.2:51234",
			xff:    []string{"203.0.113.7,198.51.100.10"},
			want:   "203.0.113.7",
		},
		{
			name:   "legacy multi entry header with spaces",
			remote: "35.191.1.2:51234",
			xff:    []string{"203.0.113.7, 198.51.100.10"},
			want:   "203.0.113.7",
		},
		{
			name:   "legacy entry with port",
			remote: "192.0.2.10:5000",
			xff:    []string{"203.0.113.7:4444"},
			want:   "203.0.113.7",
		},
		{
			name:   "legacy no valid entry uses peer",
			remote: "192.0.2.10:5000",
			xff:    []string{"unknown"},
			want:   "192.0.2.10",
		},
		{
			name:   "legacy unparseable peer uses header",
			remote: "@unix",
			xff:    []string{"203.0.113.7"},
			want:   "203.0.113.7",
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

		// Trusted-Proxies set: strict
		{
			// a load balancer that appends the client and its own address
			name:    "trusted load balancer chain",
			trusted: []string{"35.191.0.0/16", "198.51.100.10"},
			remote:  "35.191.1.2:51234",
			xff:     []string{"203.0.113.7,198.51.100.10"},
			want:    "203.0.113.7",
		},
		{
			name:    "client supplied entry left of the client is ignored",
			trusted: []string{"35.191.0.0/16", "198.51.100.10"},
			remote:  "35.191.1.2:51234",
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
			cfg := newTestConfig(t, "", map[string]*lst{
				"l1": {URL: "/test1", Tag_Name: "tag1"},
			})
			cfg.Trusted_Proxies = tt.trusted
			cfg.Ignore_Untrusted_Forwarded_For = tt.ignore
			require.NoError(t, cfg.Verify())

			h := &handler{lgr: log.NewDiscardLogger()}
			h.setResolver(cfg.resolver)

			got := clientIP(h.client(newXFFRequest(tt.remote, tt.xff...)))
			require.True(t, got.Equal(net.ParseIP(tt.want)), "got %v, want %s", got, tt.want)
		})
	}
}

func TestLegacyForwardedWarning(t *testing.T) {
	logBuf := &lockedBuffer{}
	h := &handler{lgr: log.New(logBuf)}
	h.setResolver(clientResolver{legacy: true})

	warnings := func() int {
		return strings.Count(logBuf.String(), "Ignore-Untrusted-Forwarded-For")
	}

	h.client(newXFFRequest("192.0.2.10:5000"))
	h.client(newXFFRequest("192.0.2.10:5000", "192.0.2.10"))
	require.Equal(t, 0, warnings(), "no warning when the header did not change the client")

	h.client(newXFFRequest("192.0.2.10:5000", "203.0.113.7"))
	require.Equal(t, 1, warnings(), "the header set the client")
	require.Contains(t, logBuf.String(), `src="203.0.113.7"`)
	require.Contains(t, logBuf.String(), `peer="192.0.2.10"`)

	for i := 0; i < 10; i++ {
		h.client(newXFFRequest("192.0.2.11:5000", "203.0.113.8"))
	}
	require.Equal(t, 1, warnings(), "warnings are rate limited across peers")

	h.legacyWarned.Store(time.Now().Add(-2 * legacyWarnInterval).UnixNano())
	h.client(newXFFRequest("192.0.2.10:5000", "203.0.113.7"))
	require.Equal(t, 2, warnings(), "the warning repeats after the interval")

	strict := &handler{lgr: log.New(logBuf)}
	strict.client(newXFFRequest("192.0.2.10:5000", "203.0.113.7"))
	require.Equal(t, 2, warnings(), "strict mode never warns")
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

			require.Equal(t, tt.want, got)
			require.Equal(t, clientIP(tt.client).String(), got["src"], "the logged address should be the entry SRC")
		})
	}
}

func TestVerifyTrustedProxies(t *testing.T) {
	tests := []struct {
		name       string
		cfg        []string
		env        string
		ignore     bool     // Ignore-Untrusted-Forwarded-For in the config
		ignoreEnv  string   // GRAVWELL_HTTP_IGNORE_UNTRUSTED_FORWARDED_FOR
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
			name:    "ipv6 trust all rejected",
			cfg:     []string{"::/0"},
			wantErr: `"::/0"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != `` {
				t.Setenv(envTrustedProxies, tt.env)
			}
			if tt.ignoreEnv != `` {
				t.Setenv(envIgnoreUntrustedXFF, tt.ignoreEnv)
			}

			cfg := newTestConfig(t, "", map[string]*lst{
				"l1": {URL: "/test1", Tag_Name: "tag1"},
			})
			cfg.Trusted_Proxies = tt.cfg
			cfg.Ignore_Untrusted_Forwarded_For = tt.ignore

			err := cfg.Verify()
			if tt.wantErr != `` {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)

			require.Equal(t, tt.wantLegacy, cfg.resolver.legacy, "legacy mode")
			for _, a := range tt.trusted {
				require.True(t, cfg.resolver.trusted.Contains(netipAddr(t, a)), "%s should be trusted", a)
			}
			for _, a := range tt.untrusted {
				require.False(t, cfg.resolver.trusted.Contains(netipAddr(t, a)), "%s should not be trusted", a)
			}
		})
	}
}

func TestGetConfigTrustedProxies(t *testing.T) {
	const conf = `
[Global]
	Ingest-Secret = "testing"
	Cleartext-Backend-Target=127.0.0.1:4023
	Bind=":8080"
	Trusted-Proxies=10.0.0.5
	Trusted-Proxies=192.168.100.0/24

[Listener "l1"]
	URL="/ingest"
	Tag-Name=tag1
`
	pth := filepath.Join(t.TempDir(), "http.conf")
	require.NoError(t, os.WriteFile(pth, []byte(conf), 0o600))

	cfg, err := GetConfig(pth, "")
	require.NoError(t, err)

	require.Equal(t, []string{"10.0.0.5", "192.168.100.0/24"}, cfg.Trusted_Proxies)
	require.True(t, cfg.resolver.trusted.Contains(netipAddr(t, "10.0.0.5")))
	require.True(t, cfg.resolver.trusted.Contains(netipAddr(t, "192.168.100.200")))
	require.False(t, cfg.resolver.trusted.Contains(netipAddr(t, "10.0.0.6")))
	require.False(t, cfg.resolver.legacy)

	bad := strings.Replace(conf, "Trusted-Proxies=10.0.0.5", "Trusted-Proxies=0.0.0.0/0", 1)
	require.NoError(t, os.WriteFile(pth, []byte(bad), 0o600))

	_, err = GetConfig(pth, "")
	require.ErrorContains(t, err, `"0.0.0.0/0"`)

	// no Trusted-Proxies is legacy mode until Ignore-Untrusted-Forwarded-For is set
	noProxies := strings.NewReplacer(
		"\tTrusted-Proxies=10.0.0.5\n", "",
		"\tTrusted-Proxies=192.168.100.0/24\n", "",
	).Replace(conf)
	require.NoError(t, os.WriteFile(pth, []byte(noProxies), 0o600))

	cfg, err = GetConfig(pth, "")
	require.NoError(t, err)
	require.Empty(t, cfg.Trusted_Proxies)
	require.True(t, cfg.resolver.legacy)

	ignore := strings.Replace(noProxies, `Bind=":8080"`, "Bind=\":8080\"\n\tIgnore-Untrusted-Forwarded-For=true", 1)
	require.NoError(t, os.WriteFile(pth, []byte(ignore), 0o600))

	cfg, err = GetConfig(pth, "")
	require.NoError(t, err)
	require.True(t, cfg.Ignore_Untrusted_Forwarded_For)
	require.False(t, cfg.resolver.legacy)
}

func TestHotReloadTrustedProxies(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	listeners := func() map[string]*lst {
		return map[string]*lst{"l1": {URL: "/test1", Tag_Name: "tag1"}}
	}
	newCfg := func(ignore bool, tp ...string) *cfgType {
		cfg := newTestConfig(t, "", listeners())
		cfg.Trusted_Proxies = tp
		cfg.Ignore_Untrusted_Forwarded_For = ignore
		return cfg
	}
	resolve := func(xff string) string {
		return clientIP(hndlr.client(newXFFRequest("10.0.0.1:5000", xff))).String()
	}

	require.NoError(t, hndlr.loadConfig(newCfg(false, "10.0.0.0/8")))
	require.Equal(t, "203.0.113.7", resolve("192.0.2.99, 203.0.113.7"), "loadConfig should set the list")

	require.NoError(t, hndlr.hotReload(newCfg(true)))
	require.Equal(t, "10.0.0.1", resolve("192.0.2.99, 203.0.113.7"), "reload should stop trusting the header")

	require.NoError(t, hndlr.hotReload(newCfg(false)))
	require.Equal(t, "192.0.2.99", resolve("192.0.2.99, 203.0.113.7"), "reload should switch to legacy mode")

	require.NoError(t, hndlr.hotReload(newCfg(false, "10.0.0.1")))
	require.Equal(t, "203.0.113.7", resolve("192.0.2.99, 203.0.113.7"), "reload should switch to the trusted list")

	require.NoError(t, hndlr.hotReload(newCfg(true)))
	require.Equal(t, "10.0.0.1", resolve("192.0.2.99, 203.0.113.7"))

	require.Error(t, hndlr.hotReload(newCfg(false, "0.0.0.0/0")))
	require.Equal(t, "10.0.0.1", resolve("192.0.2.99, 203.0.113.7"), "a rejected reload should keep the previous settings")
}

// TestServeHTTPClient drives real requests over TCP through handler.ServeHTTP
// into a login handler and checks the resolved client and the login log line.
func TestServeHTTPClient(t *testing.T) {
	tests := []struct {
		name     string
		trusted  []string
		ignore   bool
		xff      string
		wantAddr string
		wantLog  []string
	}{
		{
			name:     "no header",
			wantAddr: "127.0.0.1",
			wantLog:  []string{`src="127.0.0.1"`},
		},
		{
			name:     "legacy single entry",
			xff:      "203.0.113.7",
			wantAddr: "203.0.113.7",
			wantLog:  []string{`src="203.0.113.7"`, `peer="127.0.0.1"`, `xff="203.0.113.7"`, "Ignore-Untrusted-Forwarded-For"},
		},
		{
			name:     "legacy multi entry uses leftmost",
			xff:      "203.0.113.7, 198.51.100.10",
			wantAddr: "203.0.113.7",
			wantLog:  []string{`src="203.0.113.7"`, `peer="127.0.0.1"`, `xff="203.0.113.7, 198.51.100.10"`},
		},
		{
			name:     "ignore untrusted header",
			ignore:   true,
			xff:      "203.0.113.7, 198.51.100.10",
			wantAddr: "127.0.0.1",
			wantLog:  []string{`src="127.0.0.1"`, `peer="127.0.0.1"`, `xff="203.0.113.7, 198.51.100.10"`},
		},
		{
			name:     "trusted peer",
			trusted:  []string{"127.0.0.1", "198.51.100.10"},
			xff:      "203.0.113.7, 198.51.100.10",
			wantAddr: "203.0.113.7",
			wantLog:  []string{`src="203.0.113.7"`, `peer="127.0.0.1"`, `xff="203.0.113.7, 198.51.100.10"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logBuf := &lockedBuffer{}
			muxer := newTestMuxer(t)
			lg = log.New(logBuf)
			hndlr := newTestHandler(t, muxer)

			cfg := newTestConfig(t, "", map[string]*lst{
				"jwt": {
					URL:      "/jwt",
					Tag_Name: "tag1",
					auth: auth{
						AuthType: jwtT,
						LoginURL: "/jwt/login",
						Username: "user",
						Password: "pass",
					},
				},
			})
			cfg.Trusted_Proxies = tt.trusted
			cfg.Ignore_Untrusted_Forwarded_For = tt.ignore
			require.NoError(t, hndlr.loadConfig(cfg))

			capture := &captureAuth{}
			require.NoError(t, hndlr.addAuthHandler(http.MethodPost, "/capture", capture))

			srv := httptest.NewServer(hndlr)

			post := func(pth string, form url.Values) {
				req, err := http.NewRequest(http.MethodPost, srv.URL+pth, strings.NewReader(form.Encode()))
				require.NoError(t, err)
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if tt.xff != `` {
					req.Header.Set("X-Forwarded-For", tt.xff)
				}

				resp, err := http.DefaultClient.Do(req)
				require.NoError(t, err)
				resp.Body.Close()
			}

			post("/capture", nil)
			post("/jwt/login", url.Values{"username": {"user"}, "password": {"wrong"}})

			// Close waits for the handlers, so the captures and log lines are complete.
			srv.Close()

			require.Equal(t, tt.wantAddr, capture.client().String())
			require.Equal(t, tt.xff, capture.client().Forwarded)

			out := logBuf.String()
			require.Contains(t, out, "failed login")
			for _, want := range tt.wantLog {
				require.Contains(t, out, want)
			}
			if tt.xff == `` {
				require.NotContains(t, out, "xff=")
			}
		})
	}
}

type captureAuth struct {
	mtx sync.Mutex
	c   trustedproxy.Client
}

func (ca *captureAuth) Login(w http.ResponseWriter, r *http.Request) {
	ca.mtx.Lock()
	ca.c = requestClient(r)
	ca.mtx.Unlock()

	w.WriteHeader(http.StatusOK)
}

func (ca *captureAuth) AuthRequest(*http.Request) error {
	return nil
}

func (ca *captureAuth) client() trustedproxy.Client {
	ca.mtx.Lock()
	defer ca.mtx.Unlock()

	return ca.c
}

type lockedBuffer struct {
	mtx sync.Mutex
	buf bytes.Buffer
}

func (lb *lockedBuffer) Write(b []byte) (int, error) {
	lb.mtx.Lock()
	defer lb.mtx.Unlock()

	return lb.buf.Write(b)
}

func (lb *lockedBuffer) Close() error {
	return nil
}

func (lb *lockedBuffer) String() string {
	lb.mtx.Lock()
	defer lb.mtx.Unlock()

	return lb.buf.String()
}

func newXFFRequest(remote string, xff ...string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/ingest", nil)
	r.RemoteAddr = remote

	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}

	return r
}

func mustList(t *testing.T, entries ...string) trustedproxy.List {
	t.Helper()

	l, err := trustedproxy.Parse(entries)
	require.NoError(t, err)

	return l
}

func netipAddr(t *testing.T, s string) netip.Addr {
	t.Helper()

	a, err := netip.ParseAddr(s)
	require.NoError(t, err)

	return a
}

// TestDebugMiddlewareLogsResolvedClient checks that the -v debug middleware logs
// the client the handler resolved, not the TCP peer.
func TestDebugMiddlewareLogsResolvedClient(t *testing.T) {
	tests := []struct {
		name    string
		trusted []string
		ignore  bool
		wantIP  string
	}{
		{name: "legacy", wantIP: "203.0.113.7"},
		{name: "ignore", ignore: true, wantIP: "192.0.2.1"},
		{name: "trusted", trusted: []string{"192.0.2.1"}, wantIP: "203.0.113.7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			muxer := newTestMuxer(t)
			hndlr := newTestHandler(t, muxer)

			cfg := newTestConfig(t, "", map[string]*lst{
				"l1": {URL: "/test1", Tag_Name: "tag1"},
			})
			cfg.Trusted_Proxies = tt.trusted
			cfg.Ignore_Untrusted_Forwarded_For = tt.ignore
			require.NoError(t, hndlr.loadConfig(cfg))

			dl := &captureDebugLogger{}
			// debug logging turns on tracking without being asked
			chain := newChain(hndlr, true, false, dl)

			// httptest.NewRequest uses 192.0.2.1:1234 as RemoteAddr
			r := httptest.NewRequest(http.MethodPost, "/unknown", strings.NewReader("x"))
			r.Header.Set("X-Forwarded-For", "203.0.113.7")
			chain.ServeHTTP(httptest.NewRecorder(), r)

			require.Contains(t, dl.String(), "http debug code=404")
			require.Contains(t, dl.String(), " ip="+tt.wantIP+" ")
			require.Contains(t, dl.String(), " peer=192.0.2.1 ")
		})
	}
}

// TestDebugChainResolvesAfterThrottle checks that with debug logging on, health
// checks and throttled requests never resolve the client, so they log the TCP
// peer and never trigger the legacy X-Forwarded-For warning.
func TestDebugChainResolvesAfterThrottle(t *testing.T) {
	logBuf := &lockedBuffer{}
	muxer := newTestMuxer(t)
	lg = log.New(logBuf)
	hndlr := newTestHandler(t, muxer)

	cfg := newTestConfig(t, "/health", map[string]*lst{
		"l1": {URL: "/test1", Tag_Name: "tag1"},
	})
	require.NoError(t, hndlr.loadConfig(cfg))
	require.True(t, cfg.resolver.legacy)

	warnings := func() int {
		return strings.Count(logBuf.String(), "Ignore-Untrusted-Forwarded-For")
	}
	serve := func(dl debugLogger, method, pth string) int {
		// httptest.NewRequest uses 192.0.2.1:1234 as RemoteAddr
		r := httptest.NewRequest(method, pth, strings.NewReader("x"))
		r.Header.Set("X-Forwarded-For", "203.0.113.7")
		rec := httptest.NewRecorder()
		newChain(hndlr, true, true, dl).ServeHTTP(rec, r)
		return rec.Code
	}

	dl := &captureDebugLogger{}
	// the test muxer has no indexer, so the health check reports it would block
	require.Equal(t, http.StatusInsufficientStorage, serve(dl, http.MethodGet, "/health"))
	require.Contains(t, dl.String(), " ip=192.0.2.1 ")
	require.Equal(t, 0, warnings(), "a health check should not resolve the client")

	hndlr.maxConcurrentRequests = 1
	atomic.StoreInt64(&hndlr.activeRequests, 1)
	dl = &captureDebugLogger{}
	require.Equal(t, http.StatusTooManyRequests, serve(dl, http.MethodPost, "/test1"))
	require.Contains(t, dl.String(), " ip=192.0.2.1 ")
	require.Equal(t, 0, warnings(), "a throttled request should not resolve the client")

	atomic.StoreInt64(&hndlr.activeRequests, 0)
	dl = &captureDebugLogger{}
	require.Equal(t, http.StatusNotFound, serve(dl, http.MethodPost, "/unknown"))
	require.Contains(t, dl.String(), " ip=203.0.113.7 ")
	require.Equal(t, 1, warnings(), "a served request resolves the client")
}

// TestResolverSwapConcurrent resolves clients while hot reloads swap the
// resolver, for the race detector.
func TestResolverSwapConcurrent(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	newCfg := func(tp ...string) *cfgType {
		cfg := newTestConfig(t, "", map[string]*lst{"l1": {URL: "/test1", Tag_Name: "tag1"}})
		cfg.Trusted_Proxies = tp
		cfg.Ignore_Untrusted_Forwarded_For = true
		return cfg
	}
	require.NoError(t, hndlr.loadConfig(newCfg("10.0.0.1")))

	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				got := hndlr.client(newXFFRequest("10.0.0.1:5000", "203.0.113.7")).Addr.String()
				if got != "203.0.113.7" && got != "10.0.0.1" {
					t.Errorf("resolved %s, want the forwarded client or the peer", got)
					return
				}
			}
		}()
	}

	for i := 0; i < 20; i++ {
		if i%2 == 0 {
			require.NoError(t, hndlr.hotReload(newCfg()))
		} else {
			require.NoError(t, hndlr.hotReload(newCfg("10.0.0.1")))
		}
	}
	close(done)
	wg.Wait()

	require.Equal(t, "203.0.113.7", hndlr.client(newXFFRequest("10.0.0.1:5000", "203.0.113.7")).Addr.String(), "the last reload trusts the peer")
}

// TestListenerDebugLogsResolvedClient checks the per-listener debug wrappers
// (-debug <listener>) log the resolved client. The failed AuthRequest reads it
// from the request context, the failed login and the failed post read it from
// the trackingRW.
//
// The test muxer has no indexer, so ServeHTTP answers posts that pass auth with
// 507 before the route handler runs. The route handler is called directly with
// the request and writer ServeHTTP would hand it.
func TestListenerDebugLogsResolvedClient(t *testing.T) {
	tests := []struct {
		name    string
		trusted []string
	}{
		{name: "legacy"},
		{name: "trusted", trusted: []string{"192.0.2.1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dl := &captureDebugLogger{}
			prevListener, prevLogger := *debugListener, DefaultDebugLogger
			*debugListener, DefaultDebugLogger = "l1", dl
			t.Cleanup(func() { *debugListener, DefaultDebugLogger = prevListener, prevLogger })

			hndlr, err := newHandler(newTestMuxer(t), log.NewDiscardLogger(), nil, nil, nil, 0)
			require.NoError(t, err)
			cfg := newTestConfig(t, "", map[string]*lst{
				"l1": {
					URL:      "/l1",
					Tag_Name: "tag1",
					auth: auth{
						AuthType: jwtT,
						LoginURL: "/l1/login",
						Username: "user",
						Password: "pass",
					},
				},
			})
			cfg.Trusted_Proxies = tt.trusted
			require.NoError(t, hndlr.loadConfig(cfg))

			// per-listener debugging turns on tracking but not the debug middleware
			chain := newChain(hndlr, false, true, nil)
			serve := func(pth, body, token string) *httptest.ResponseRecorder {
				// httptest.NewRequest uses 192.0.2.1:1234 as RemoteAddr
				r := httptest.NewRequest(http.MethodPost, pth, strings.NewReader(body))
				r.Header.Set("X-Forwarded-For", "203.0.113.7")
				if pth == "/l1/login" {
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				if token != `` {
					r.Header.Set("Authorization", "Bearer "+token)
				}
				rec := httptest.NewRecorder()
				chain.ServeHTTP(rec, r)
				return rec
			}

			require.Equal(t, http.StatusUnauthorized, serve("/l1", "x", "not-a-token").Code)
			require.Equal(t, http.StatusForbidden, serve("/l1/login", "username=user&password=wrong", "").Code)

			hndlr.RLock()
			rh, ok := hndlr.mp[route{method: http.MethodPost, uri: "/l1"}]
			hndlr.RUnlock()
			require.True(t, ok)

			r := httptest.NewRequest(http.MethodPost, "/l1", strings.NewReader(""))
			r.Header.Set("X-Forwarded-For", "203.0.113.7")
			rec := httptest.NewRecorder()
			trw := &trackingRW{ResponseWriter: rec}
			r, c := hndlr.attachClient(trw, r)
			rh.handle(hndlr, trw, r, r.Body, clientIP(c))
			require.Equal(t, http.StatusBadRequest, rec.Code, "an empty post fails in the route handler")

			var lines []string
			for _, l := range dl.lines {
				if strings.HasPrefix(l, "http debug ") {
					lines = append(lines, l)
				}
			}
			require.Len(t, lines, 3, "AuthRequest, Login and the route handler should each log")
			for i, code := range []string{"code=401", "code=403", "code=400"} {
				require.Contains(t, lines[i], code)
				require.Contains(t, lines[i], " ip=203.0.113.7 ")
				require.Contains(t, lines[i], " peer=192.0.2.1 ")
			}
		})
	}
}
