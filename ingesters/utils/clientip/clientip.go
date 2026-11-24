/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package clientip finds the client address of a request for HTTP based
// ingesters. It builds on utils/trustedproxy with the ingester config options
// Trusted-Proxy and Ignore-Untrusted-Forwarded-For, the legacy X-Forwarded-For
// warning, and the log KVs that name the client.
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/utils/trustedproxy"
	"golang.org/x/time/rate"
)

// legacyWarnInterval is the minimum time between legacy X-Forwarded-For warnings.
const legacyWarnInterval = time.Minute

// Resolver decides how the client of a request is found.
//
// With Trusted-Proxy set, X-Forwarded-For is used only from a listed peer. With
// no Trusted-Proxy the client is the peer, unless Ignore-Untrusted-Forwarded-For
// is false, in which case the legacy behavior trusts the header from any peer.
//
// A Resolver holds no state beyond its settings, so it can be swapped on a
// config reload. The zero value trusts no proxies.
type Resolver struct {
	trusted trustedproxy.List
	legacy  bool
}

// NewResolver returns the Resolver for a trusted proxy list and the
// Ignore-Untrusted-Forwarded-For setting.
func NewResolver(trusted trustedproxy.List, ignoreUntrusted bool) Resolver {
	return Resolver{
		trusted: trusted,
		legacy:  trusted.Empty() && !ignoreUntrusted,
	}
}

// LoadIgnoreUntrusted returns the Ignore-Untrusted-Forwarded-For value set by the
// environment variable envName, or the file named by envName_FILE, and true when
// neither is set. Ingesters seed their config with it before reading the config
// file, so a value in the file or an overlay takes precedence.
func LoadIgnoreUntrusted(envName string) (bool, error) {
	// start from false: LoadEnvVar leaves a bool that is already true alone,
	// and fills in the default of true only when the variable is unset
	var v bool
	if err := config.LoadEnvVar(&v, envName, true); err != nil {
		return false, fmt.Errorf("failed to load %s %w", envName, err)
	}

	return v, nil
}

// LoadResolver fills the Trusted-Proxy config value from the environment
// variable trustedEnv where the config leaves it unset, then parses it with the
// Ignore-Untrusted-Forwarded-For setting into a Resolver. Errors name the option
// or variable at fault.
func LoadResolver(trusted *[]string, ignoreUntrusted bool, trustedEnv string) (Resolver, error) {
	if err := config.LoadEnvVar(trusted, trustedEnv, nil); err != nil {
		return Resolver{}, fmt.Errorf("failed to load Trusted-Proxy: %w", err) // err names the variable
	}
	tp, err := trustedproxy.Parse(*trusted)
	if err != nil {
		return Resolver{}, fmt.Errorf("Trusted-Proxy is invalid: %w", err)
	}

	return NewResolver(tp, ignoreUntrusted), nil
}

// Legacy reports whether X-Forwarded-For is trusted from any peer.
func (cr Resolver) Legacy() bool {
	return cr.legacy
}

// Resolve returns the client of r. When the client came from an X-Forwarded-For
// header that only the legacy behavior trusts, lw logs a warning. A nil lw logs
// nothing.
func (cr Resolver) Resolve(r *http.Request, lw *LegacyWarner) trustedproxy.Client {
	if !cr.legacy {
		return cr.trusted.Resolve(r)
	}

	c := trustedproxy.ResolveLegacy(r)
	if c.Addr != c.Peer {
		lw.warn(c)
	}

	return c
}

// Logger is the logger the legacy warning is written to.
type Logger interface {
	Warn(string, ...rfc5424.SDParam) error
}

// LegacyWarner rate limits the legacy X-Forwarded-For warning. Share one across
// every listener, and keep it across config reloads, so the limit holds for the
// whole ingester.
type LegacyWarner struct {
	lg    Logger
	every rate.Sometimes
}

// NewLegacyWarner returns a LegacyWarner that writes to lg.
func NewLegacyWarner(lg Logger) *LegacyWarner {
	return &LegacyWarner{
		lg:    lg,
		every: rate.Sometimes{Interval: legacyWarnInterval},
	}
}

// warn logs that the client address came from an X-Forwarded-For header sent by
// an unlisted peer. It logs at most once per legacyWarnInterval.
func (lw *LegacyWarner) warn(c trustedproxy.Client) {
	if lw == nil {
		return
	}

	// decide under the Sometimes lock but log after it, so a slow log write
	// does not hold up other requests
	var fire bool
	lw.every.Do(func() { fire = true })
	if !fire {
		return
	}

	lw.lg.Warn("trusting X-Forwarded-For from any peer because Ignore-Untrusted-Forwarded-For is false, "+
		"set Trusted-Proxy instead",
		KVs("src", c)...)
}

// IP returns the address used as the entry SRC. It falls back to 127.0.0.1 only
// when no address could be found.
func IP(c trustedproxy.Client) net.IP {
	if ip := c.IP(); ip != nil {
		return ip
	}

	return net.IPv4(127, 0, 0, 1)
}

// KVs returns the client address under name, the same address IP returns for
// the entry SRC. When the request carried X-Forwarded-For, or the peer did not
// parse, it also returns the TCP peer. When there is an X-Forwarded-For header,
// the raw header is logged under xff for context only, it is not the SRC.
func KVs(name string, c trustedproxy.Client) []rfc5424.SDParam {
	kvs := []rfc5424.SDParam{log.KV(name, IP(c).String())}
	if !c.HasForwarded() {
		if !c.Addr.IsValid() {
			kvs = append(kvs, log.KV("peer", peerString(c)))
		}
		return kvs
	}

	kvs = append(kvs, log.KV("peer", peerString(c)), log.KV("xff", c.Forwarded()))
	if c.Malformed {
		kvs = append(kvs, log.KV("xff-malformed", true))
	}

	return kvs
}

// peerString returns the TCP peer for logging, or RemoteAddr verbatim when it
// did not parse.
func peerString(c trustedproxy.Client) string {
	if c.Peer.IsValid() {
		return c.Peer.String()
	}

	return c.PeerRaw
}
