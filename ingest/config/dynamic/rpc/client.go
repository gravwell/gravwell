/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package rpc

import (
	"context"
	"crypto/hmac"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"uuid"

	"golang.org/x/net/websocket"

	"github.com/gravwell/gravwell/v4/ingest/log"
)

const (
	defaultDialTimeout = 10 * time.Second

	// defaultPingInterval keeps the session warm and is how the client notices a server
	// that has gone away without closing.  It must be comfortably inside the server's
	// idle timeout.
	defaultPingInterval = 30 * time.Second
)

// ClientConfig configures a dial.
type ClientConfig struct {
	// Webserver is the endpoint to connect to, in the normalized form Config.Verify
	// produces, e.g. http://10.0.0.1:8080 or https://gravwell.example.com.  REQUIRED.
	// The scheme is translated to ws or wss.
	Webserver string

	// Token is the shared secret.  REQUIRED.  It is never transmitted.
	Token string

	// Path is the route the server handler is mounted on.  Optional, defaults to
	// DefaultPath.
	Path string

	// ID and Class identify this ingester.  Both are bound into the handshake, so a
	// session cannot later claim to be something else.  Both optional.
	ID    uuid.UUID
	Class string

	// Handlers are the methods this client exposes to the server, which is how the
	// server pushes work down rather than the client polling for it.  Optional.
	Handlers *Mux

	// TLSConfig is used for wss connections.  Optional.
	TLSConfig *tls.Config

	// Logger is used for session level events.  Optional, defaults to discarding.
	Logger *log.Logger

	// DialTimeout, HandshakeTimeout, PingInterval, MaxPayloadBytes and MaxInflight are
	// all optional and have sane defaults.
	//
	// PingInterval follows the same rule as the server's IdleTimeout, and for the same
	// reason: zero takes the default, negative disables the keepalive.  A client that
	// silently stopped pinging because its caller left the field alone would be dropped
	// by any server with an idle timeout, which is every server by default.
	DialTimeout      time.Duration
	HandshakeTimeout time.Duration
	PingInterval     time.Duration
	MaxPayloadBytes  int
	MaxInflight      int
}

// wsURL translates the configured webserver endpoint into the websocket URL to dial.
func (c ClientConfig) wsURL() (r string, err error) {
	if c.Webserver == `` {
		err = fmt.Errorf("empty webserver endpoint")
		return
	}
	ws := c.Webserver
	if !strings.Contains(ws, `://`) {
		ws = `http://` + ws
	}
	var u *url.URL
	if u, err = url.Parse(ws); err != nil {
		err = fmt.Errorf("invalid webserver endpoint %q %w", c.Webserver, err)
		return
	} else if u.Host == `` {
		err = fmt.Errorf("invalid webserver endpoint %q missing host", c.Webserver)
		return
	}
	switch u.Scheme {
	case `http`, `ws`:
		u.Scheme = `ws`
	case `https`, `wss`:
		u.Scheme = `wss`
	default:
		err = fmt.Errorf("invalid webserver endpoint %q unsupported scheme %q", c.Webserver, u.Scheme)
		return
	}
	if c.Path != `` {
		u.Path = c.Path
	} else {
		u.Path = DefaultPath
	}
	r = u.String()
	return
}

// Dial connects to a route handler, authenticates against the shared token, and returns
// the authenticated session.  The returned session is live, its read loop is running, and
// the caller owns closing it.
//
// The context bounds the dial and the handshake.  The underlying websocket library does
// not take a context, so cancellation is enforced with deadlines rather than by
// interrupting a call already in flight.
func Dial(ctx context.Context, cfg ClientConfig) (s *Session, err error) {
	if cfg.Token == `` {
		err = ErrEmptyToken
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err = ctx.Err(); err != nil {
		return
	}
	lgr := cfg.Logger
	if lgr == nil {
		lgr = log.NewDiscardLogger()
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = defaultDialTimeout
	}
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = defaultHandshakeTimeout
	}
	if cfg.MaxPayloadBytes <= 0 {
		cfg.MaxPayloadBytes = defaultMaxPayloadBytes
	}

	var uri string
	if uri, err = cfg.wsURL(); err != nil {
		return
	}
	var wcfg *websocket.Config
	if wcfg, err = websocket.NewConfig(uri, uri); err != nil {
		err = fmt.Errorf("failed to build websocket config %w", err)
		return
	}
	wcfg.TlsConfig = cfg.TLSConfig
	wcfg.Header = make(http.Header)

	dialTimeout := cfg.DialTimeout
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d < dialTimeout {
			dialTimeout = d
		}
	}
	if dialTimeout <= 0 {
		err = context.DeadlineExceeded
		return
	}
	wcfg.Dialer = &net.Dialer{Timeout: dialTimeout}

	var conn *websocket.Conn
	if conn, err = websocket.DialConfig(wcfg); err != nil {
		if conn != nil {
			conn.Close()
		}
		err = fmt.Errorf("failed to dial %s %w", uri, err)
		return
	}
	conn.MaxPayloadBytes = cfg.MaxPayloadBytes

	if err = authenticateClient(ctx, conn, cfg); err != nil {
		conn.Close()
		return
	}

	s = newSession(conn, cfg.Handlers, lgr, cfg.ID, cfg.Class, cfg.MaxInflight, 0)
	go s.serve()

	if interval := resolvePingInterval(cfg.PingInterval); interval > 0 {
		go s.keepalive(interval)
	}
	return
}

// resolvePingInterval applies the documented rule: zero takes the default, negative
// disables the keepalive, anything else is used as given.  A zero return means no
// keepalive goroutine at all.
func resolvePingInterval(d time.Duration) time.Duration {
	switch {
	case d == 0:
		return defaultPingInterval
	case d < 0:
		return 0
	}
	return d
}

// authenticateClient runs the client half of the challenge/response.
//
// The server proof is checked before the client sends anything derived from the token, so
// an endpoint that cannot prove it holds the shared secret learns nothing from us.
func authenticateClient(ctx context.Context, conn *websocket.Conn, cfg ClientConfig) (err error) {
	deadline := time.Now().Add(cfg.HandshakeTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return
	}
	defer conn.SetDeadline(time.Time{})

	var cnonce []byte
	if cnonce, err = newNonce(); err != nil {
		return
	}
	hello := clientHello{
		Version: ProtocolVersion,
		Nonce:   cnonce,
		ID:      cfg.ID,
		Class:   cfg.Class,
	}
	if err = websocket.JSON.Send(conn, hello); err != nil {
		return fmt.Errorf("failed to send hello %w", err)
	}

	var sc serverChallenge
	if err = websocket.JSON.Receive(conn, &sc); err != nil {
		return fmt.Errorf("failed to read challenge %w", err)
	}
	if sc.Version != ProtocolVersion {
		return ErrBadVersion
	} else if !validNonce(sc.Nonce) {
		return ErrBadNonce
	}

	// verify the server before proving anything about ourselves
	want := proof(cfg.Token, serverProofLabel, cnonce, sc.Nonce, cfg.ID, cfg.Class)
	if !hmac.Equal(want, sc.Proof) {
		return fmt.Errorf("%w, the server does not hold the shared token", ErrAuthFailed)
	}

	cr := clientResponse{
		Proof: proof(cfg.Token, clientProofLabel, cnonce, sc.Nonce, cfg.ID, cfg.Class),
	}
	if err = websocket.JSON.Send(conn, cr); err != nil {
		return fmt.Errorf("failed to send response %w", err)
	}

	var res authResult
	if err = websocket.JSON.Receive(conn, &res); err != nil {
		return fmt.Errorf("failed to read auth result %w", err)
	}
	if !res.OK {
		if res.Error != `` {
			return fmt.Errorf("%w: %s", ErrAuthFailed, res.Error)
		}
		return ErrAuthFailed
	}
	return
}

// keepalive pings the server until the session ends.  This is what notices a server that
// went away without closing cleanly, and it keeps the server's idle timeout from firing.
func (s *Session) keepalive(interval time.Duration) {
	tckr := time.NewTicker(interval)
	defer tckr.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-tckr.C:
			ctx, cf := context.WithTimeout(s.ctx, interval)
			err := s.Ping(ctx)
			cf()
			if err != nil {
				s.lgr.Error("dynamic rpc keepalive failed", log.KVErr(err))
				s.closeWith(fmt.Errorf("keepalive failed %w", err))
				return
			}
		}
	}
}
