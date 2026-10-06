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
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest/log"
	"golang.org/x/net/websocket"
)

const (
	// defaultMaxPayloadBytes caps a single message.  A config blob is small, anything
	// this large is a bug or an attack rather than a legitimate request.
	defaultMaxPayloadBytes int = 4 * 1024 * 1024

	// defaultMaxInflight bounds how many requests from a peer are executed at once, so
	// that one connection cannot spawn unbounded goroutines.
	defaultMaxInflight int = 32
)

// Session is an authenticated connection.  Both ends get one and they are symmetric,
// either may call methods on the other.  A Session is safe for concurrent use.
type Session struct {
	conn *websocket.Conn
	mux  *Mux
	lgr  *log.Logger

	id     uuid.UUID
	class  string
	remote string

	idleTimeout time.Duration

	wmtx sync.Mutex // serializes writes, the codec is not safe for concurrent senders

	pmtx    sync.Mutex
	pending map[uint64]chan *message
	nextID  atomic.Uint64

	sem chan struct{} // bounds concurrent inbound handlers

	ctx    context.Context
	cancel context.CancelFunc

	closeOnce sync.Once
	done      chan struct{}
	// served is closed once the read loop has stopped and every handler it started has
	// returned.  done says the session is finished with, this says nothing of its is
	// still running, and the difference matters to anything that has to clean up after
	// the work those handlers were doing.
	served chan struct{}
	errMtx sync.Mutex
	err    error
}

// newSession wraps an already authenticated connection.
func newSession(conn *websocket.Conn, mux *Mux, lgr *log.Logger, id uuid.UUID, class string, maxInflight int, idle time.Duration) *Session {
	if lgr == nil {
		lgr = log.NewDiscardLogger()
	}
	if maxInflight <= 0 {
		maxInflight = defaultMaxInflight
	}
	var remote string
	if r := conn.Request(); r != nil {
		remote = r.RemoteAddr
	} else if ra := conn.RemoteAddr(); ra != nil {
		remote = ra.String()
	}
	s := &Session{
		conn:        conn,
		mux:         mux,
		lgr:         lgr,
		id:          id,
		class:       class,
		remote:      remote,
		idleTimeout: idle,
		pending:     map[uint64]chan *message{},
		sem:         make(chan struct{}, maxInflight),
		done:        make(chan struct{}),
		served:      make(chan struct{}),
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	// handlers are given a context derived from this one, so carrying the session in it
	// is how a handler learns who is calling.  It has to come from here rather than from
	// the request body: the handshake is what proves the peer's identity, a body can
	// claim anything.
	s.ctx = context.WithValue(s.ctx, sessionKey{}, s)
	return s
}

// sessionKey is the private context key the session is carried under, so nothing outside
// this package can plant a forged one.
type sessionKey struct{}

// SessionFrom returns the session a handler is running for.  Use it to find out which
// authenticated peer is making a call:
//
//	if s, ok := rpc.SessionFrom(ctx); ok {
//		id := s.ID()
//	}
//
// The identity it reports was established by the handshake, so it can be trusted in a way
// that anything in the request body cannot.
func SessionFrom(ctx context.Context) (s *Session, ok bool) {
	if ctx == nil {
		return
	}
	s, ok = ctx.Value(sessionKey{}).(*Session)
	return
}

// ID is the UUID the peer authenticated with.  On a client session this is the identity
// it presented, on a server session it is the identity the handshake bound.
func (s *Session) ID() uuid.UUID { return s.id }

// Class is the free form class the peer authenticated with.
func (s *Session) Class() string { return s.class }

// RemoteAddr is the peer address, for logging.
func (s *Session) RemoteAddr() string { return s.remote }

// Done is closed when the session ends, for whatever reason.
func (s *Session) Done() <-chan struct{} { return s.done }

// Wait blocks until the session has stopped reading and every handler it was running has
// returned.
//
// Close only starts that: it hangs up and unblocks callers, but a handler already part way
// through its work keeps going, and that work can outlive the session.  Anything that
// tears down state a handler touches has to wait for this rather than for Close, or it
// pulls the state out from under work that is still happening.
//
// It is only meaningful on a session whose read loop was started, which is every session
// Dial or the route handler produces.
func (s *Session) Wait() { <-s.served }

// WaitContext is Wait with a way out.
//
// Draining is not guaranteed to finish: a handler blocked on a filesystem that has gone
// away stays blocked, and an unbounded wait on one turns an orderly shutdown into a
// process that cannot be stopped without a kill.  A caller that has to make progress
// bounds the wait and accepts that the work it was waiting for is still running.
func (s *Session) WaitContext(ctx context.Context) error {
	select {
	case <-s.served:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Err reports why the session ended, nil while it is still running.
func (s *Session) Err() (err error) {
	s.errMtx.Lock()
	err = s.err
	s.errMtx.Unlock()
	return
}

// Context is cancelled when the session ends.  Handlers are given a context derived from
// this one so that work stops when nobody is left to receive the answer.
func (s *Session) Context() context.Context { return s.ctx }

// Close shuts the session down.  It is safe to call more than once and from any
// goroutine, and it is what unblocks anything waiting in Call.
func (s *Session) Close() error {
	s.closeWith(ErrSessionClosed)
	return nil
}

// closeWith records the first reason the session ended and tears it down.  Only the first
// caller sets the error, later ones are consequences of it.
func (s *Session) closeWith(err error) {
	s.closeOnce.Do(func() {
		s.errMtx.Lock()
		if s.err == nil {
			s.err = err
		}
		s.errMtx.Unlock()
		s.conn.Close()
		s.cancel()
		close(s.done)

		// fail everything that is still waiting on an answer rather than let it sit
		// until its own context expires
		s.pmtx.Lock()
		pending := s.pending
		s.pending = nil
		s.pmtx.Unlock()
		for _, ch := range pending {
			close(ch)
		}
	})
}

// Call invokes a method on the peer and waits for the answer.
//
// params is encoded as the request parameters, nil sends none.  result, when not nil, is
// decoded from the response.  A method that ran and failed comes back as a RemoteError, a
// connection that died comes back as the session error, so the two are distinguishable.
func (s *Session) Call(ctx context.Context, method string, params, result any) (err error) {
	if method == `` {
		return ErrEmptyMethod
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var raw json.RawMessage
	if params != nil {
		if raw, err = json.Marshal(params); err != nil {
			return fmt.Errorf("failed to encode params for %s %w", method, err)
		}
	}

	id := s.nextID.Add(1)
	ch := make(chan *message, 1)
	s.pmtx.Lock()
	if s.pending == nil {
		s.pmtx.Unlock()
		return s.Err()
	}
	s.pending[id] = ch
	s.pmtx.Unlock()
	defer func() {
		s.pmtx.Lock()
		delete(s.pending, id)
		s.pmtx.Unlock()
	}()

	if err = s.send(&message{Type: msgRequest, ID: id, Method: method, Params: raw}); err != nil {
		return
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return s.Err()
	case resp, ok := <-ch:
		if !ok {
			return s.Err() // the session died while we were waiting
		}
		if resp.Error != `` {
			return RemoteError{Method: method, Message: resp.Error}
		}
		if result != nil && len(resp.Result) > 0 {
			if err = json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("failed to decode result of %s %w", method, err)
			}
		}
	}
	return
}

// Ping calls the built in ping method, which every session answers.  It is how either end
// checks that the other is still there, the websocket library here does not give us
// control frames.
func (s *Session) Ping(ctx context.Context) error {
	return s.Call(ctx, MethodPing, nil, nil)
}

// send writes one message.  Writes are serialized, the JSON codec has no internal
// locking and two concurrent senders would interleave frames.
func (s *Session) send(m *message) (err error) {
	select {
	case <-s.done:
		return s.Err()
	default:
	}
	s.wmtx.Lock()
	err = websocket.JSON.Send(s.conn, m)
	s.wmtx.Unlock()
	if err != nil {
		err = fmt.Errorf("failed to send %w", err)
		s.closeWith(err)
	}
	return
}

// serve runs the read loop until the connection dies.  It is the only reader.
func (s *Session) serve() {
	var wg sync.WaitGroup
	defer func() {
		// stop accepting, then let the handlers that are already running finish before
		// declaring the session over
		s.closeWith(ErrSessionClosed)
		wg.Wait()
		close(s.served)
	}()

	for {
		if s.idleTimeout > 0 {
			if err := s.conn.SetReadDeadline(time.Now().Add(s.idleTimeout)); err != nil {
				s.closeWith(err)
				return
			}
		}
		var m message
		if err := websocket.JSON.Receive(s.conn, &m); err != nil {
			s.closeWith(err)
			return
		}
		switch m.Type {
		case msgResponse:
			s.deliver(&m)
		case msgRequest:
			// bound the work one peer can have in flight, and stop if the session dies
			// while we are waiting for a slot
			select {
			case s.sem <- struct{}{}:
			case <-s.done:
				return
			}
			wg.Add(1)
			go func(m message) {
				defer func() {
					<-s.sem
					wg.Done()
				}()
				s.dispatch(&m)
			}(m)
		default:
			s.lgr.Warn("dynamic rpc received an unknown message type",
				log.KV("type", m.Type), log.KV("remote", s.remote))
		}
	}
}

// deliver hands a response to whoever is waiting for it.  A response with no waiter is
// either a duplicate or an answer to a call that already gave up, neither is fatal.
func (s *Session) deliver(m *message) {
	s.pmtx.Lock()
	ch, ok := s.pending[m.ID]
	if ok {
		delete(s.pending, m.ID)
	}
	s.pmtx.Unlock()
	if !ok {
		s.lgr.Warn("dynamic rpc response with no caller",
			log.KV("id", m.ID), log.KV("remote", s.remote))
		return
	}
	ch <- m
}

// dispatch runs one inbound request and answers it.  Every request gets exactly one
// response, including the ones that fail, so the caller never waits forever.
func (s *Session) dispatch(m *message) {
	resp := message{Type: msgResponse, ID: m.ID}
	if m.Method == `` {
		resp.Error = ErrEmptyMethod.Error()
	} else if m.Method == MethodPing {
		// answered here rather than in the Mux so that liveness works on a session that
		// serves nothing at all
	} else if h, ok := s.mux.lookup(m.Method); !ok {
		resp.Error = fmt.Sprintf("%s %s", ErrUnknownMethod.Error(), m.Method)
	} else {
		result, err := s.safeCall(h, m.Params)
		if err != nil {
			resp.Error = err.Error()
		} else if result != nil {
			var raw []byte
			if raw, err = json.Marshal(result); err != nil {
				resp.Error = fmt.Sprintf("failed to encode result: %v", err)
			} else {
				resp.Result = raw
			}
		}
	}
	if err := s.send(&resp); err != nil {
		s.lgr.Error("dynamic rpc failed to answer",
			log.KV("method", m.Method), log.KV("remote", s.remote), log.KVErr(err))
	}
}

// safeCall runs a handler and turns a panic into an error.  A misbehaving handler must
// not take the whole connection, and more importantly must not leave the caller on the
// other end waiting for a response that a dead goroutine was going to send.
func (s *Session) safeCall(h Handler, params json.RawMessage) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panicked: %v", r)
			result = nil
		}
	}()
	return h(s.ctx, params)
}
