/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package clientip

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/utils/trustedproxy"
	"github.com/stretchr/testify/require"
)

const (
	testTrustedEnv = `GRAVWELL_TEST_TRUSTED_PROXY`
	testIgnoreEnv  = `GRAVWELL_TEST_IGNORE_UNTRUSTED_FORWARDED_FOR`
)

func TestResolve(t *testing.T) {
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
			xff:    []string{"unknown"},
			want:   "192.0.2.10",
		},
		{
			name:   "legacy peer without port",
			remote: "192.0.2.10",
			want:   "192.0.2.10",
		},
		{
			name:   "legacy unparseable peer uses header",
			remote: "@unix",
			xff:    []string{"203.0.113.7"},
			want:   "203.0.113.7",
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
			cr := NewResolver(mustList(t, tt.trusted...), tt.ignore)
			lw := NewLegacyWarner(log.NewDiscardLogger())

			got := IP(cr.Resolve(newXFFRequest(tt.remote, tt.xff...), lw))
			require.True(t, got.Equal(net.ParseIP(tt.want)), "got %v, want %s", got, tt.want)
		})
	}
}

// TestZeroResolver checks that the zero Resolver trusts no proxies.
func TestZeroResolver(t *testing.T) {
	var cr Resolver
	require.False(t, cr.Legacy())

	c := cr.Resolve(newXFFRequest("192.0.2.10:5000", "203.0.113.7"), NewLegacyWarner(log.NewDiscardLogger()))
	require.Equal(t, "192.0.2.10", c.Addr.String())
}

func TestNilLegacyWarner(t *testing.T) {
	legacy := NewResolver(trustedproxy.List{}, false)
	require.True(t, legacy.Legacy())

	c := legacy.Resolve(newXFFRequest("192.0.2.10:5000", "203.0.113.7"), nil)
	require.Equal(t, "203.0.113.7", c.Addr.String())
}

func TestLegacyWarning(t *testing.T) {
	logBuf := &lockedBuffer{}
	lw := NewLegacyWarner(log.New(logBuf))
	legacy := NewResolver(trustedproxy.List{}, false)
	require.True(t, legacy.Legacy())

	warnings := func() int {
		return strings.Count(logBuf.String(), "Ignore-Untrusted-Forwarded-For")
	}

	legacy.Resolve(newXFFRequest("192.0.2.10:5000"), lw)
	legacy.Resolve(newXFFRequest("192.0.2.10:5000", "192.0.2.10"), lw)
	require.Equal(t, 0, warnings(), "no warning when the header did not change the client")

	legacy.Resolve(newXFFRequest("192.0.2.10:5000", "203.0.113.7"), lw)
	require.Equal(t, 1, warnings(), "the header set the client")
	require.Contains(t, logBuf.String(), "trusting X-Forwarded-For from any peer because Ignore-Untrusted-Forwarded-For is false, "+
		"set Trusted-Proxy instead")
	require.Contains(t, logBuf.String(), `src="203.0.113.7" peer="192.0.2.10" xff="203.0.113.7"`)

	for i := 0; i < 10; i++ {
		legacy.Resolve(newXFFRequest("192.0.2.11:5000", "203.0.113.8"), lw)
	}
	require.Equal(t, 1, warnings(), "warnings are rate limited across peers")

	// shorten the interval and wait past it rather than wait a minute
	lw.every.Interval = 20 * time.Millisecond
	time.Sleep(2 * lw.every.Interval)
	legacy.Resolve(newXFFRequest("192.0.2.10:5000", "203.0.113.7"), lw)
	require.Equal(t, 2, warnings(), "the warning repeats after the interval")

	for _, cr := range []Resolver{
		{},
		NewResolver(trustedproxy.List{}, true),
		NewResolver(mustList(t, "192.0.2.10"), false),
	} {
		cr.Resolve(newXFFRequest("192.0.2.10:5000", "203.0.113.7"), NewLegacyWarner(log.New(logBuf)))
	}
	require.Equal(t, 2, warnings(), "strict modes never warn")
}

// TestLegacyWarningConcurrent checks that concurrent requests produce one
// warning in total, and that a slow warning write does not hold up other
// requests.
func TestLegacyWarningConcurrent(t *testing.T) {
	bw := newBlockingWriter()
	lw := NewLegacyWarner(log.New(bw))
	cr := NewResolver(trustedproxy.List{}, false)

	// the first warning blocks in the log write until released
	first := make(chan struct{})
	go func() {
		defer close(first)
		cr.Resolve(newXFFRequest("192.0.2.10:5000", "203.0.113.7"), lw)
	}()
	<-bw.entered

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cr.Resolve(newXFFRequest("192.0.2.11:5000", "203.0.113.8"), lw)
			}()
		}
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("requests blocked behind the warning write")
	}

	close(bw.release)
	<-first
	require.Equal(t, 1, strings.Count(bw.String(), "Ignore-Untrusted-Forwarded-For"))
}

func TestKVs(t *testing.T) {
	tests := []struct {
		name   string
		client trustedproxy.Client
		want   []string // name=value, in order
	}{
		{
			name:   "no header",
			client: trustedproxy.List{}.Resolve(newXFFRequest("192.0.2.10:5000")),
			want:   []string{"src=192.0.2.10"},
		},
		{
			// an empty header line logs like no header
			name:   "empty header",
			client: trustedproxy.ResolveLegacy(newXFFRequest("192.0.2.10:5000", "")),
			want:   []string{"src=192.0.2.10"},
		},
		{
			name:   "header from untrusted peer",
			client: trustedproxy.List{}.Resolve(newXFFRequest("192.0.2.10:5000", "203.0.113.7")),
			want:   []string{"src=192.0.2.10", "peer=192.0.2.10", "xff=203.0.113.7"},
		},
		{
			name: "malformed header from trusted peer",
			client: mustList(t, "10.0.0.1").Resolve(
				newXFFRequest("10.0.0.1:5000", "203.0.113.7, unknown"),
			),
			want: []string{"src=10.0.0.1", "peer=10.0.0.1", "xff=203.0.113.7, unknown", "xff-malformed=true"},
		},
		{
			name:   "legacy header with unparseable peer",
			client: trustedproxy.ResolveLegacy(newXFFRequest("@unix", "203.0.113.7")),
			want:   []string{"src=203.0.113.7", "peer=@unix", "xff=203.0.113.7"},
		},
		{
			// the logged address is the SRC fallback, with the raw peer beside it
			name:   "unparseable peer",
			client: trustedproxy.List{}.Resolve(newXFFRequest("@unix")),
			want:   []string{"src=127.0.0.1", "peer=@unix"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, kv := range KVs("src", tt.client) {
				got = append(got, kv.Name+"="+kv.Value)
			}

			require.Equal(t, tt.want, got)
			require.Equal(t, "src="+IP(tt.client).String(), got[0], "the logged address should be the entry SRC")
		})
	}
}

func TestLoadIgnoreUntrusted(t *testing.T) {
	tests := []struct {
		name    string
		env     *string // nil leaves the variable unset
		file    string  // contents of the file named by the _FILE variable
		want    bool
		wantErr string
	}{
		{name: "unset defaults to true", want: true},
		{name: "true", env: new("true"), want: true},
		{name: "false", env: new("false"), want: false},
		{name: "zero", env: new("0"), want: false},
		{name: "file false", file: "false\n", want: false},
		{name: "env wins over file", env: new("true"), file: "false\n", want: true},
		{name: "invalid", env: new("maybe"), wantErr: "failed to load " + testIgnoreEnv + " "},
		{name: "empty", env: new(""), wantErr: testIgnoreEnv},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != nil {
				t.Setenv(testIgnoreEnv, *tt.env)
			}
			if tt.file != `` {
				t.Setenv(testIgnoreEnv+`_FILE`, writeTrustedProxyFile(t, tt.file))
			}

			got, err := LoadIgnoreUntrusted(testIgnoreEnv)
			if tt.wantErr != `` {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLoadResolver(t *testing.T) {
	tests := []struct {
		name       string
		cfg        []string
		env        string
		file       string   // contents of the file named by the _FILE variable
		ignore     bool     // Ignore-Untrusted-Forwarded-For
		wantCfg    []string // Trusted-Proxy after the environment is loaded
		trusted    []string // addresses expected to be trusted
		untrusted  []string // addresses expected not to be trusted
		wantLegacy bool
		wantErr    string
	}{
		{
			name:       "ignore false is legacy mode",
			untrusted:  []string{"127.0.0.1", "10.0.0.1"},
			wantLegacy: true,
		},
		{
			name:      "ignore true is not legacy mode",
			ignore:    true,
			untrusted: []string{"127.0.0.1", "10.0.0.1"},
		},
		{
			name:      "trusted list is never legacy",
			cfg:       []string{"10.0.0.1"},
			wantCfg:   []string{"10.0.0.1"},
			trusted:   []string{"10.0.0.1"},
			untrusted: []string{"10.0.0.2"},
		},
		{
			name:      "addresses and blocks",
			cfg:       []string{"10.0.0.1", "192.168.100.0/24", "2001:db8::/64"},
			wantCfg:   []string{"10.0.0.1", "192.168.100.0/24", "2001:db8::/64"},
			trusted:   []string{"10.0.0.1", "192.168.100.7", "2001:db8::5"},
			untrusted: []string{"10.0.0.2", "192.168.101.7"},
		},
		{
			name:      "env var comma list",
			env:       "10.0.0.1, 192.168.100.0/24",
			wantCfg:   []string{"10.0.0.1", "192.168.100.0/24"},
			trusted:   []string{"10.0.0.1", "192.168.100.7"},
			untrusted: []string{"10.0.0.2"},
		},
		{
			name:      "config wins over env var",
			cfg:       []string{"10.0.0.1"},
			env:       "10.0.0.2",
			wantCfg:   []string{"10.0.0.1"},
			trusted:   []string{"10.0.0.1"},
			untrusted: []string{"10.0.0.2"},
		},
		{
			name:    "invalid entry",
			cfg:     []string{"10.0.0.1", "proxy.example.com"},
			wantErr: `Trusted-Proxy is invalid: invalid trusted proxy "proxy.example.com"`,
		},
		{
			name:    "invalid env entry",
			env:     "10.0.0.1,nope",
			wantErr: `Trusted-Proxy is invalid: invalid trusted proxy "nope"`,
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
			wantErr: "failed to load Trusted-Proxy: " + testTrustedEnv + ": ",
		},
		{
			name:    "env var of only whitespace rejected",
			env:     " ",
			wantErr: "no entries",
		},
		{
			// the config value wins over the env var, so it must not be dropped
			name:    "blank config value with env var rejected",
			cfg:     []string{""},
			env:     "10.0.0.1",
			wantErr: "blank",
		},
		{
			name:      "file one entry per line",
			file:      "10.0.0.1\n10.0.0.2\r\n192.168.100.0/24\n",
			wantCfg:   []string{"10.0.0.1", "10.0.0.2", "192.168.100.0/24"},
			trusted:   []string{"10.0.0.1", "10.0.0.2", "192.168.100.7"},
			untrusted: []string{"10.0.0.3"},
		},
		{
			name:      "file commas and lines",
			file:      "10.0.0.1, 10.0.0.2\n10.0.0.3\n",
			wantCfg:   []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
			trusted:   []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
			untrusted: []string{"10.0.0.4"},
		},
		{
			name:      "env var wins over file",
			env:       "10.0.0.1",
			file:      "10.0.0.2\n",
			wantCfg:   []string{"10.0.0.1"},
			trusted:   []string{"10.0.0.1"},
			untrusted: []string{"10.0.0.2"},
		},
		{
			name:    "file of only separators rejected",
			file:    ",,\n,\n",
			wantErr: "no entries",
		},
		{
			name:    "empty file rejected",
			file:    "\n",
			wantErr: "failed to load Trusted-Proxy: " + testTrustedEnv + "_FILE ",
		},
		{
			name:    "invalid file entry",
			file:    "10.0.0.1\nnope\n",
			wantErr: `"nope"`,
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
				t.Setenv(testTrustedEnv, tt.env)
			}
			if tt.file != `` {
				t.Setenv(testTrustedEnv+`_FILE`, writeTrustedProxyFile(t, tt.file))
			}
			trusted := tt.cfg
			cr, err := LoadResolver(&trusted, tt.ignore, testTrustedEnv)
			if tt.wantErr != `` {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)

			require.Equal(t, tt.wantCfg, trusted, "Trusted-Proxy")
			require.Equal(t, tt.wantLegacy, cr.Legacy(), "legacy mode")
			for _, a := range tt.trusted {
				require.True(t, cr.trusted.Contains(netip.MustParseAddr(a)), "%s should be trusted", a)
			}
			for _, a := range tt.untrusted {
				require.False(t, cr.trusted.Contains(netip.MustParseAddr(a)), "%s should not be trusted", a)
			}
		})
	}
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

func writeTrustedProxyFile(t *testing.T, content string) string {
	t.Helper()
	pth := filepath.Join(t.TempDir(), "trusted_proxy")
	require.NoError(t, os.WriteFile(pth, []byte(content), 0o600))
	return pth
}

// lockedBuffer is a log destination that is safe for concurrent writers.
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

// blockingWriter is a log destination whose first write blocks until release is
// closed. entered is closed when that write starts.
type blockingWriter struct {
	lockedBuffer
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func newBlockingWriter() *blockingWriter {
	return &blockingWriter{entered: make(chan struct{}), release: make(chan struct{})}
}

func (bw *blockingWriter) Write(b []byte) (int, error) {
	bw.once.Do(func() {
		close(bw.entered)
		<-bw.release
	})

	return bw.lockedBuffer.Write(b)
}
