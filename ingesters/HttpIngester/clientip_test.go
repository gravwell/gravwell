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
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/ingesters/utils/clientip"
	"github.com/gravwell/gravwell/v4/utils/trustedproxy"
	"github.com/stretchr/testify/require"
)

// TestVerifyTrustedProxy checks that Verify builds the resolver from the config
// and the GRAVWELL_HTTP_ environment variables. clientip.LoadResolver tests the
// options themselves.
func TestVerifyTrustedProxy(t *testing.T) {
	tests := []struct {
		name       string
		cfg        []string
		env        string
		file       string   // contents of the file named by the _FILE variable
		ignore     bool     // Ignore-Untrusted-Forwarded-For in the config
		ignoreEnv  string   // GRAVWELL_HTTP_IGNORE_UNTRUSTED_FORWARDED_FOR
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

			cfg := newTestConfig(t, "", map[string]*lst{
				"l1": {URL: "/test1", Tag_Name: "tag1"},
			})
			cfg.Trusted_Proxy = tt.cfg
			cfg.Ignore_Untrusted_Forwarded_For = tt.ignore

			err := cfg.Verify()
			if tt.wantErr != `` {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)

			require.Equal(t, clientip.NewResolver(mustList(t, tt.want...), tt.wantIgnore), cfg.resolver)
		})
	}
}

func TestGetConfigTrustedProxy(t *testing.T) {
	const conf = `
[Global]
	Ingest-Secret = "testing"
	Cleartext-Backend-Target=127.0.0.1:4023
	Bind=":8080"
	Trusted-Proxy=10.0.0.5
	Trusted-Proxy=192.168.100.0/24

[Listener "l1"]
	URL="/ingest"
	Tag-Name=tag1
`
	pth := filepath.Join(t.TempDir(), "http.conf")
	require.NoError(t, os.WriteFile(pth, []byte(conf), 0o600))

	cfg, err := GetConfig(pth, "")
	require.NoError(t, err)

	require.Equal(t, []string{"10.0.0.5", "192.168.100.0/24"}, cfg.Trusted_Proxy)
	require.Equal(t, clientip.NewResolver(mustList(t, "10.0.0.5", "192.168.100.0/24"), false), cfg.resolver)
	require.False(t, cfg.resolver.Legacy())

	bad := strings.Replace(conf, "Trusted-Proxy=10.0.0.5", "Trusted-Proxy=0.0.0.0/0", 1)
	require.NoError(t, os.WriteFile(pth, []byte(bad), 0o600))

	_, err = GetConfig(pth, "")
	require.ErrorContains(t, err, `"0.0.0.0/0"`)

	// a value of only blanks must fail rather than fall back to legacy mode
	blank := strings.NewReplacer(
		"Trusted-Proxy=10.0.0.5", `Trusted-Proxy=""`,
		"\tTrusted-Proxy=192.168.100.0/24\n", "",
	).Replace(conf)
	require.NoError(t, os.WriteFile(pth, []byte(blank), 0o600))

	_, err = GetConfig(pth, "")
	require.ErrorContains(t, err, "blank")

	// no Trusted-Proxy is legacy mode until Ignore-Untrusted-Forwarded-For is set
	noProxies := strings.NewReplacer(
		"\tTrusted-Proxy=10.0.0.5\n", "",
		"\tTrusted-Proxy=192.168.100.0/24\n", "",
	).Replace(conf)
	require.NoError(t, os.WriteFile(pth, []byte(noProxies), 0o600))

	cfg, err = GetConfig(pth, "")
	require.NoError(t, err)
	require.Empty(t, cfg.Trusted_Proxy)
	require.True(t, cfg.resolver.Legacy())

	ignore := strings.Replace(noProxies, `Bind=":8080"`, "Bind=\":8080\"\n\tIgnore-Untrusted-Forwarded-For=true", 1)
	require.NoError(t, os.WriteFile(pth, []byte(ignore), 0o600))

	cfg, err = GetConfig(pth, "")
	require.NoError(t, err)
	require.True(t, cfg.Ignore_Untrusted_Forwarded_For)
	require.False(t, cfg.resolver.Legacy())
}

func TestHotReloadTrustedProxy(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	listeners := func() map[string]*lst {
		return map[string]*lst{"l1": {URL: "/test1", Tag_Name: "tag1"}}
	}
	newCfg := func(ignore bool, tp ...string) *cfgType {
		cfg := newTestConfig(t, "", listeners())
		cfg.Trusted_Proxy = tp
		cfg.Ignore_Untrusted_Forwarded_For = ignore
		return cfg
	}
	resolve := func(xff string) string {
		return clientip.IP(hndlr.client(newXFFRequest("10.0.0.1:5000", xff))).String()
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

// TestHotReloadTrustedProxyFile checks that each reload reads the
// Trusted-Proxy _FILE again, so an edited file takes effect on SIGHUP.
func TestHotReloadTrustedProxyFile(t *testing.T) {
	muxer := newTestMuxer(t)
	hndlr := newTestHandler(t, muxer)

	newCfg := func() *cfgType {
		return newTestConfig(t, "", map[string]*lst{"l1": {URL: "/test1", Tag_Name: "tag1"}})
	}
	resolve := func(peer string) string {
		return clientip.IP(hndlr.client(newXFFRequest(peer, "192.0.2.99, 203.0.113.7"))).String()
	}

	pth := writeTrustedProxyFile(t, "10.0.0.1\n10.0.0.2\n")
	t.Setenv(envTrustedProxy+`_FILE`, pth)

	require.NoError(t, hndlr.loadConfig(newCfg()))
	require.Equal(t, "203.0.113.7", resolve("10.0.0.1:5000"), "first line should be trusted")
	require.Equal(t, "203.0.113.7", resolve("10.0.0.2:5000"), "second line should be trusted")
	require.Equal(t, "10.0.0.3", resolve("10.0.0.3:5000"))

	require.NoError(t, os.WriteFile(pth, []byte("10.0.0.3\n"), 0o600))
	require.NoError(t, hndlr.hotReload(newCfg()))
	require.Equal(t, "10.0.0.1", resolve("10.0.0.1:5000"), "reload should drop the removed proxy")
	require.Equal(t, "203.0.113.7", resolve("10.0.0.3:5000"), "reload should trust the added proxy")

	require.NoError(t, os.WriteFile(pth, []byte(",\n"), 0o600))
	require.ErrorContains(t, hndlr.hotReload(newCfg()), "no entries")
	require.Equal(t, "203.0.113.7", resolve("10.0.0.3:5000"), "a rejected reload should keep the previous settings")
}

// TestHotReloadKeepsLegacyWarningLimit checks that a reload swaps the resolver
// but keeps the legacy warning rate limit, so a SIGHUP does not repeat the
// warning.
func TestHotReloadKeepsLegacyWarningLimit(t *testing.T) {
	logBuf := &lockedBuffer{}
	hndlr, err := newHandler(newTestMuxer(t), log.New(logBuf), nil, nil, nil, 0)
	require.NoError(t, err)

	newCfg := func() *cfgType {
		return newTestConfig(t, "", map[string]*lst{"l1": {URL: "/test1", Tag_Name: "tag1"}})
	}
	warnings := func() int {
		return strings.Count(logBuf.String(), "Ignore-Untrusted-Forwarded-For")
	}

	require.NoError(t, hndlr.loadConfig(newCfg()))
	hndlr.client(newXFFRequest("192.0.2.10:5000", "203.0.113.7"))
	require.Equal(t, 1, warnings())

	require.NoError(t, hndlr.hotReload(newCfg()))
	hndlr.client(newXFFRequest("192.0.2.10:5000", "203.0.113.7"))
	require.Equal(t, 1, warnings(), "a reload should not reset the warning rate limit")
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
			cfg.Trusted_Proxy = tt.trusted
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
			require.Equal(t, tt.xff, capture.client().Forwarded())

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
			cfg.Trusted_Proxy = tt.trusted
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
	require.True(t, cfg.resolver.Legacy())

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
	hndlr.activeRequests.Store(1)
	dl = &captureDebugLogger{}
	require.Equal(t, http.StatusTooManyRequests, serve(dl, http.MethodPost, "/test1"))
	require.Contains(t, dl.String(), " ip=192.0.2.1 ")
	require.Equal(t, 0, warnings(), "a throttled request should not resolve the client")

	hndlr.activeRequests.Store(0)
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
		cfg.Trusted_Proxy = tp
		cfg.Ignore_Untrusted_Forwarded_For = true
		return cfg
	}
	require.NoError(t, hndlr.loadConfig(newCfg("10.0.0.1")))

	done := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
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
		})
	}

	for i := range 20 {
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
			cfg.Trusted_Proxy = tt.trusted
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
			r, _ = hndlr.attachClient(trw, r)
			rh.handle(hndlr, trw, r, r.Body)
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

// TestRouteHandlersUseResolvedClient sends a request through a trusted peer to
// every route type in each mode and checks that the entry SRC is the resolved
// client and that the route's log line names the same address. In trusted mode
// the client differs from the TCP peer, so a route that read the request's peer
// instead of the attached client would fail.
//
// The test muxer has no indexer, so ServeHTTP answers with 507 before the route
// handler runs. The route handler is called directly with the request
// ServeHTTP would hand it.
func TestRouteHandlersUseResolvedClient(t *testing.T) {
	routes := []struct {
		name   string
		url    string
		body   string
		logKey string // the client key in the route's log lines
	}{
		{name: "std single", url: "/single", body: "hello", logKey: "src"},
		{name: "std multiline", url: "/multi", body: "one\ntwo\n", logKey: "src"},
		{name: "hec event", url: "/hec/event", body: `{"event":"hello"}`, logKey: "src"},
		{name: "hec raw", url: "/hec/raw", body: "one\ntwo\n", logKey: "src"},
		{
			name:   "amazon firehose",
			url:    "/afh",
			body:   `{"requestId":"r1","records":[{"data":"aGVsbG8="}]}`,
			logKey: "src",
		},
		{
			name:   "otel logs",
			url:    "/v1/logs",
			body:   `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"hello"}}]}]}]}`,
			logKey: "remoteaddress",
		},
		{
			name:   "otel metrics",
			url:    "/v1/metrics",
			body:   `{"resourceMetrics":[{"scopeMetrics":[{"metrics":[{"name":"m","gauge":{"dataPoints":[{"asInt":"1"}]}}]}]}]}`,
			logKey: "remoteaddress",
		},
	}
	modes := []struct {
		name    string
		trusted []string
		ignore  bool
		want    string
	}{
		{name: "trusted", trusted: []string{"192.0.2.1"}, want: "203.0.113.7"},
		{name: "legacy", want: "203.0.113.7"},
		{name: "ignore", ignore: true, want: "192.0.2.1"},
	}

	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			logBuf := &lockedBuffer{}
			hndlr, err := newHandler(newTestMuxer(t), log.New(logBuf), nil, nil, nil, 0)
			require.NoError(t, err)

			cfg := newTestConfig(t, "", map[string]*lst{
				"single": {URL: "/single", Tag_Name: "tag1"},
				"multi":  {URL: "/multi", Tag_Name: "tag1", Multiline: true},
			})
			cfg.HECListener = map[string]*hecCompatible{
				"hec": {URL: "/hec", Tag_Name: "tag1", TokenValue: "secret"},
			}
			cfg.AFHListener = map[string]*afh{
				"afh": {URL: "/afh", Tag_Name: "tag1", TokenValue: "secret"},
			}
			cfg.OtelLogsListener = map[string]*otelLogsListener{
				"logs": {URL: "/v1/logs", Tag_Name: "tag1"},
			}
			cfg.OtelListener = map[string]*otelMetricsListener{
				"metrics": {URL: "/v1/metrics", Tag_Name: "tag1"},
			}
			cfg.Trusted_Proxy = mode.trusted
			cfg.Ignore_Untrusted_Forwarded_For = mode.ignore
			require.NoError(t, hndlr.loadConfig(cfg))

			// main sets the body limit from the config
			prevMaxBody := maxBody
			maxBody = cfg.MaxBody()
			t.Cleanup(func() { maxBody = prevMaxBody })

			for _, rt := range routes {
				t.Run(rt.name, func(t *testing.T) {
					hndlr.RLock()
					rh, ok := hndlr.mp[route{method: http.MethodPost, uri: rt.url}]
					hndlr.RUnlock()
					require.True(t, ok, "no route for %s", rt.url)

					ew := &captureEntryWriter{}
					rh.pproc = processors.NewProcessorSet(ew)

					// serve resolves the client as ServeHTTP does and runs the route
					// handler with rdr as the body, returning the log output of the
					// route handler alone
					serve := func(body string, rdr func(*http.Request) io.Reader) (*httptest.ResponseRecorder, string) {
						// httptest.NewRequest uses 192.0.2.1:1234 as RemoteAddr
						r := httptest.NewRequest(http.MethodPost, rt.url, strings.NewReader(body))
						r.Header.Set("Content-Type", "application/json")
						r.Header.Set("X-Forwarded-For", "203.0.113.7")
						rec := httptest.NewRecorder()
						r, _ = hndlr.attachClient(rec, r)

						before := len(logBuf.String())
						rh.handle(hndlr, rec, r, rdr(r))
						return rec, logBuf.String()[before:]
					}

					rec, _ := serve(rt.body, func(r *http.Request) io.Reader { return r.Body })
					require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
					ents := ew.entries()
					require.NotEmpty(t, ents)
					for _, e := range ents {
						require.Equal(t, mode.want, e.SRC.String(), "entry SRC")
					}

					// a failed body read makes every route log a line about the request
					_, out := serve("", func(*http.Request) io.Reader {
						return iotest.ErrReader(errors.New("read failed"))
					})
					m := regexp.MustCompile(`\b` + rt.logKey + `="([^"]*)"`).FindStringSubmatch(out)
					require.NotNil(t, m, "no %s in the route log output %q", rt.logKey, out)
					require.Equal(t, mode.want, m[1], "the logged client should be the entry SRC")
				})
			}
		})
	}
}

// captureEntryWriter records the entries a route handler writes.
type captureEntryWriter struct {
	mtx  sync.Mutex
	ents []*entry.Entry
}

func (cw *captureEntryWriter) WriteEntry(e *entry.Entry) error {
	cw.mtx.Lock()
	defer cw.mtx.Unlock()

	cw.ents = append(cw.ents, e)
	return nil
}

func (cw *captureEntryWriter) WriteEntryContext(_ context.Context, e *entry.Entry) error {
	return cw.WriteEntry(e)
}

func (cw *captureEntryWriter) WriteBatch(ents []*entry.Entry) error {
	cw.mtx.Lock()
	defer cw.mtx.Unlock()

	cw.ents = append(cw.ents, ents...)
	return nil
}

func (cw *captureEntryWriter) WriteBatchContext(_ context.Context, ents []*entry.Entry) error {
	return cw.WriteBatch(ents)
}

func (cw *captureEntryWriter) entries() []*entry.Entry {
	cw.mtx.Lock()
	defer cw.mtx.Unlock()

	return slices.Clone(cw.ents)
}

func writeTrustedProxyFile(t *testing.T, content string) string {
	t.Helper()
	pth := filepath.Join(t.TempDir(), "trusted_proxy")
	if err := os.WriteFile(pth, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return pth
}
