/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package rpc

import (
	"crypto/hmac"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/log"
	"golang.org/x/net/websocket"
)

const (
	// defaultHandshakeTimeout bounds how long an unauthenticated connection may sit on
	// the server.  An attacker should not be able to hold sockets open by connecting and
	// then saying nothing.
	defaultHandshakeTimeout = 3 * time.Second

	// defaultIdleTimeout closes a session that has gone quiet.  Clients are expected to
	// ping well inside this.
	defaultIdleTimeout = 2 * time.Minute
)

// ServerConfig configures the route handler.
type ServerConfig struct {
	// Token is the shared secret.  REQUIRED.  It is never transmitted, see the package
	// documentation for what that does and does not buy.
	Token string

	// Handlers are the methods this server exposes to clients.  Optional, a server that
	// only makes calls out to its clients can leave it nil.
	Handlers *Mux

	// OnSession is called with each authenticated session, in its own goroutine, so the
	// application can hold the handle and make calls back to that ingester.  Optional.
	// The session is already closed by the time it returns if the connection dropped.
	OnSession func(*Session)

	// Logger is used for connection level events.  Optional, defaults to discarding.
	Logger *log.Logger

	// HandshakeTimeout bounds the authentication exchange.  Optional.
	HandshakeTimeout time.Duration

	// IdleTimeout closes a session that has not received anything in this long.
	// Optional: zero takes the default, negative disables it entirely.
	//
	// Zero is the default rather than "off" because zero is what a caller who has not
	// thought about it supplies, and a server with no idle timeout leaks a session for
	// every client that goes away without closing.  Turning it off has to be asked for.
	IdleTimeout time.Duration

	// MaxPayloadBytes caps a single message.  Optional.
	MaxPayloadBytes int

	// MaxInflight bounds concurrently executing requests per session.  Optional.
	MaxInflight int

	// AuthRateWindow is the minimum gap between authentication attempts from a single
	// address.  A client that reconnects faster is answered with an HTTP status and hung
	// up on before the websocket upgrade.  Optional, zero takes the default of one
	// second, negative disables throttling entirely.
	AuthRateWindow time.Duration

	// MaxTrackedAddrs bounds the throttle table.  Once it is full, addresses that are
	// not already tracked share a single global limiter rather than growing the table,
	// see throttle.  Optional.
	MaxTrackedAddrs int

	// TrustedProxies lists the load balancers and reverse proxies that sit in front of
	// this handler, as bare addresses or CIDR blocks, e.g. []string{"10.0.0.0/8",
	// "2001:db8::1"}.  Forwarding headers are believed only when the request arrives
	// from one of these, see proxyTrust for why.  Optional, empty means throttle by the
	// peer address and ignore every header.
	//
	// Deployments behind a load balancer MUST set this.  Without it every client shares
	// the load balancer's address, which is one throttle entry, and they all end up
	// sharing a single attempt per window.
	TrustedProxies []string
}

// Server is the HTTP route handler.  Mount it wherever the dynamic API lives, DefaultPath
// is the expected location:
//
//	mux.Handle(rpc.DefaultPath, srv)
type Server struct {
	cfg ServerConfig
	lgr *log.Logger
	thr *throttle   // nil when throttling is disabled
	pt  *proxyTrust // which peers may speak for someone else

	// retryAfter is the Retry-After value handed to a throttled caller, precomputed
	// because it never changes
	retryAfter string
}

// NewServer builds the route handler.  It fails only on a configuration that cannot
// work, principally a missing token, because a server with no token would authenticate
// everyone.
func NewServer(cfg ServerConfig) (s *Server, err error) {
	if cfg.Token == `` {
		err = ErrEmptyToken
		return
	}
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = defaultHandshakeTimeout
	}
	if cfg.IdleTimeout < 0 {
		cfg.IdleTimeout = 0
	} else if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = defaultIdleTimeout
	}
	if cfg.MaxPayloadBytes <= 0 {
		cfg.MaxPayloadBytes = defaultMaxPayloadBytes
	}
	if cfg.AuthRateWindow == 0 {
		cfg.AuthRateWindow = defaultAuthRateWindow
	}
	if cfg.MaxTrackedAddrs <= 0 {
		cfg.MaxTrackedAddrs = defaultMaxTrackedAddrs
	}
	lgr := cfg.Logger
	if lgr == nil {
		lgr = log.NewDiscardLogger()
	}
	var pt *proxyTrust
	if pt, err = newProxyTrust(cfg.TrustedProxies); err != nil {
		s = nil
		return
	}
	s = &Server{cfg: cfg, lgr: lgr, pt: pt}
	if cfg.AuthRateWindow > 0 {
		s.thr = newThrottle(cfg.AuthRateWindow, cfg.MaxTrackedAddrs)
		s.retryAfter = strconv.Itoa(max(1, int(math.Ceil(cfg.AuthRateWindow.Seconds()))))
	}
	return
}

// ServeHTTP upgrades the request and runs a session on it.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// throttle before the upgrade.  Once this is a websocket there is no status code
	// left to send, so a client that reconnects too fast has to be turned away here,
	// while it is still an ordinary HTTP request.
	if s.thr != nil {
		addr := s.pt.clientAddr(r, s.lgr)
		// IPv6 is held responsible by prefix rather than by address, see throttleKey
		key := throttleKey(addr)
		if ok, degraded := s.thr.allow(key, time.Now()); !ok {
			// the same answer either way.  Telling a caller that it pushed us into the
			// global limiter would confirm that its address rolling is working.
			w.Header().Set(`Retry-After`, s.retryAfter)
			w.Header().Set(`Connection`, `close`) // hang up rather than keep it alive
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			if degraded {
				s.lgr.Warn("dynamic rpc authentication throttled by the global limiter",
					log.KV("remote", addr), log.KV("key", key),
					log.KV("tracked", s.thr.tracked()), log.KV("degraded", s.thr.degradedCount()))
			} else {
				s.lgr.Warn("dynamic rpc authentication throttled",
					log.KV("remote", addr), log.KV("key", key))
			}
			return
		}
	}

	ws := websocket.Server{
		// Origin is not the security boundary here and non browser clients do not send
		// one, so accept whatever arrives and let the token handshake decide.
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler:   websocket.Handler(s.handle),
	}
	ws.ServeHTTP(w, r)
}

// handle owns one connection from upgrade to teardown.
func (s *Server) handle(conn *websocket.Conn) {
	defer conn.Close()
	conn.MaxPayloadBytes = s.cfg.MaxPayloadBytes

	var remote string
	if r := conn.Request(); r != nil {
		remote = r.RemoteAddr
	}

	hello, err := s.authenticate(conn)
	if err != nil {
		// log at warn, a failed handshake is routine noise on an exposed port, but it is
		// the only record of someone trying
		s.lgr.Warn("dynamic rpc authentication failed",
			log.KV("remote", remote), log.KVErr(err))
		return
	}
	s.lgr.Info("dynamic rpc client authenticated",
		log.KV("remote", remote), log.KV("id", hello.ID), log.KV("class", hello.Class))

	sess := newSession(conn, s.cfg.Handlers, s.lgr, hello.ID, hello.Class,
		s.cfg.MaxInflight, s.cfg.IdleTimeout)
	if s.cfg.OnSession != nil {
		go s.cfg.OnSession(sess)
	}
	sess.serve() // blocks for the life of the connection, which is what holds ServeHTTP open

	s.lgr.Info("dynamic rpc client disconnected",
		log.KV("remote", remote), log.KV("id", hello.ID), log.KVErr(sess.Err()))
}

// authenticate runs the server half of the challenge/response.
//
// The server proves first.  That means an unauthenticated peer can collect a server proof
// just by connecting, which is the accepted tradeoff: it keeps a client that has been
// pointed at an impostor from ever handing over a proof of its own, and the client is the
// side that accepts configuration from whatever it is talking to.
func (s *Server) authenticate(conn *websocket.Conn) (hello clientHello, err error) {
	// one deadline covers the whole exchange, so a peer cannot stall between steps
	if err = conn.SetDeadline(time.Now().Add(s.cfg.HandshakeTimeout)); err != nil {
		return
	}
	defer conn.SetDeadline(time.Time{})

	if err = websocket.JSON.Receive(conn, &hello); err != nil {
		return
	}
	if hello.Version != ProtocolVersion {
		s.reject(conn)
		err = ErrBadVersion
		return
	} else if !validNonce(hello.Nonce) {
		s.reject(conn)
		err = ErrBadNonce
		return
	}

	var snonce []byte
	if snonce, err = newNonce(); err != nil {
		s.reject(conn)
		return
	}
	sc := serverChallenge{
		Version: ProtocolVersion,
		Nonce:   snonce,
		Proof:   proof(s.cfg.Token, serverProofLabel, hello.Nonce, snonce, hello.ID, hello.Class),
	}
	if err = websocket.JSON.Send(conn, sc); err != nil {
		return
	}

	var cr clientResponse
	if err = websocket.JSON.Receive(conn, &cr); err != nil {
		return
	}
	want := proof(s.cfg.Token, clientProofLabel, hello.Nonce, snonce, hello.ID, hello.Class)
	if !hmac.Equal(want, cr.Proof) {
		s.reject(conn)
		err = ErrAuthFailed
		return
	}
	err = websocket.JSON.Send(conn, authResult{OK: true})
	return
}

// reject tells a peer it did not get in without telling it which step it got wrong.
// A best effort, the connection is going away regardless.
func (s *Server) reject(conn *websocket.Conn) {
	websocket.JSON.Send(conn, authResult{Error: ErrAuthFailed.Error()})
}
