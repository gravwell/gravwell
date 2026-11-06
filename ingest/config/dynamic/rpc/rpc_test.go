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
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"uuid"
)

const testToken = `d0a1ec1f9f2d4a3e8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a`

type echoArgs struct {
	Value string
}

// testHarness is a running server plus whatever the test needs to dial it.
type testHarness struct {
	srv      *httptest.Server
	sessions chan *Session // sessions the server accepted
}

func newHarness(t *testing.T, srvMux *Mux) *testHarness {
	t.Helper()
	h := &testHarness{sessions: make(chan *Session, 8)}
	s, err := NewServer(ServerConfig{
		Token:    testToken,
		Handlers: srvMux,
		// the throttle is exercised by its own tests, everything else here reconnects
		// from 127.0.0.1 far faster than a real client ever would
		AuthRateWindow: -1,
		OnSession: func(sess *Session) {
			select {
			case h.sessions <- sess:
			default:
			}
		},
		HandshakeTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.srv = httptest.NewServer(s)
	t.Cleanup(h.srv.Close)
	return h
}

func (h *testHarness) dial(t *testing.T, cfg ClientConfig) (*Session, error) {
	t.Helper()
	if cfg.Webserver == `` {
		cfg.Webserver = h.srv.URL
	}
	if cfg.Token == `` {
		cfg.Token = testToken
	}
	cfg.PingInterval = -1 // no keepalive unless a test wants one
	ctx, cf := context.WithTimeout(context.Background(), 10*time.Second)
	defer cf()
	return Dial(ctx, cfg)
}

func echoMux(t *testing.T) *Mux {
	t.Helper()
	m := NewMux()
	if err := m.Register(`echo`, func(_ context.Context, params json.RawMessage) (any, error) {
		var a echoArgs
		if len(params) > 0 {
			if err := json.Unmarshal(params, &a); err != nil {
				return nil, err
			}
		}
		return echoArgs{Value: a.Value}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(`boom`, func(context.Context, json.RawMessage) (any, error) {
		return nil, errors.New("this method always fails")
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(`panic`, func(context.Context, json.RawMessage) (any, error) {
		panic(`a handler should not be able to take the session down`)
	}); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestAuthRoundTrip is the happy path: dial, authenticate, call.
func TestAuthRoundTrip(t *testing.T) {
	h := newHarness(t, echoMux(t))
	id := uuid.New()
	sess, err := h.dial(t, ClientConfig{ID: id, Class: `edge`})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	var reply echoArgs
	if err = sess.Call(context.Background(), `echo`, echoArgs{Value: `hello`}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Value != `hello` {
		t.Errorf("echo returned %q", reply.Value)
	}

	// the identity the client presented is bound into the handshake, so the server ends
	// up holding the authenticated identity rather than an asserted one
	select {
	case ss := <-h.sessions:
		if ss.ID() != id {
			t.Errorf("server session ID = %v, want %v", ss.ID(), id)
		}
		if ss.Class() != `edge` {
			t.Errorf("server session Class = %q, want edge", ss.Class())
		}
	case <-time.After(5 * time.Second):
		t.Fatal(`the server never surfaced the session`)
	}
}

// TestTokenNeverTransmitted is the whole point of the design.  Every byte either end
// writes during a full handshake is captured and searched for the token.
func TestTokenNeverTransmitted(t *testing.T) {
	// a token made of a distinctive byte pattern so it cannot hide in the noise
	token := `SUPERSECRETTOKENVALUE0123456789abcdefABCDEF`
	ts, rec := newRecordingServer(t, ServerConfig{Token: token, Handlers: echoMux(t)})

	sess, err := Dial(context.Background(), ClientConfig{
		Webserver:    ts.URL,
		Token:        token,
		ID:           uuid.New(),
		Class:        `edge`,
		PingInterval: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = sess.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	sess.Close()

	// decoded and unmasked, this is what each peer actually got to see
	got := rec.Joined()
	if got == `` {
		t.Fatal(`captured no traffic, the test proves nothing`)
	}
	// the capture really does contain the handshake, otherwise the search below is
	// looking at nothing
	if !strings.Contains(got, `Proof`) || !strings.Contains(got, `Nonce`) {
		t.Fatalf("the capture does not look like a handshake: %q", got)
	}
	for _, probe := range []string{token, strings.ToLower(token), strings.ToUpper(token)} {
		if strings.Contains(got, probe) {
			t.Errorf("the shared token appeared on the wire: %q", got)
		}
	}
	// and not hidden in an encoding either, the proofs are base64 in the JSON so a
	// base64 of the token would not stand out to the check above
	for _, probe := range []string{
		base64.StdEncoding.EncodeToString([]byte(token)),
		base64.RawURLEncoding.EncodeToString([]byte(token)),
		hex.EncodeToString([]byte(token)),
	} {
		if strings.Contains(got, probe) {
			t.Errorf("an encoding of the shared token appeared on the wire")
		}
	}
}

// TestWrongTokenRejected covers a client that does not hold the secret.
//
// Note what this does and does not prove.  The real client checks the server's proof
// first, so with a wrong token it hangs up before the server ever looks at anything, and
// a server that accepted every proof would still pass here.  The server's own
// verification is covered by TestServerRejectsBadClientProof, which drives it with a
// client that does not play along.
func TestWrongTokenRejected(t *testing.T) {
	h := newHarness(t, nil)
	for _, tc := range []struct {
		name  string
		token string
	}{
		{`wrong token`, `not-the-right-token`},
		{`token off by one byte`, testToken[:len(testToken)-1] + `b`},
		{`prefix of the token`, testToken[:len(testToken)-1]},
		{`token with trailing space`, testToken + ` `},
	} {
		// an empty token never reaches the wire, Dial refuses it, that is covered by
		// TestServerConfig
		sess, err := h.dial(t, ClientConfig{Token: tc.token, ID: uuid.New()})
		if err == nil {
			sess.Close()
			t.Errorf("%s: a client with the wrong token authenticated", tc.name)
		} else if !errors.Is(err, ErrAuthFailed) {
			t.Errorf("%s: got %v, want ErrAuthFailed", tc.name, err)
		}
	}
}

// TestNoncesAreFresh checks that a captured handshake cannot be replayed, which rests
// entirely on the server never reusing a nonce.
func TestNoncesAreFresh(t *testing.T) {
	seen := map[string]bool{}
	for range 32 {
		n, err := newNonce()
		if err != nil {
			t.Fatal(err)
		}
		if !validNonce(n) {
			t.Fatalf("nonce is %d bytes, want %d", len(n), nonceLen)
		}
		k := fmt.Sprintf("%x", n)
		if seen[k] {
			t.Fatal(`a nonce repeated`)
		}
		seen[k] = true
	}
}

// TestProofBinding checks that a proof is good for exactly one direction of one handshake
// by one identity, which is what stops reflection and identity swapping.
func TestProofBinding(t *testing.T) {
	cn, _ := newNonce()
	sn, _ := newNonce()
	id := uuid.New()
	base := proof(testToken, clientProofLabel, cn, sn, id, `edge`)

	other, _ := newNonce()
	for _, tc := range []struct {
		name string
		got  []byte
	}{
		{`other direction`, proof(testToken, serverProofLabel, cn, sn, id, `edge`)},
		{`other client nonce`, proof(testToken, clientProofLabel, other, sn, id, `edge`)},
		{`other server nonce`, proof(testToken, clientProofLabel, cn, other, id, `edge`)},
		{`other identity`, proof(testToken, clientProofLabel, cn, sn, uuid.New(), `edge`)},
		{`other class`, proof(testToken, clientProofLabel, cn, sn, id, `core`)},
		{`other token`, proof(testToken+`x`, clientProofLabel, cn, sn, id, `edge`)},
	} {
		if string(tc.got) == string(base) {
			t.Errorf("%s produced the same proof, it is not bound", tc.name)
		}
	}
	// and it is stable for the same inputs, otherwise nothing would ever authenticate
	if string(proof(testToken, clientProofLabel, cn, sn, id, `edge`)) != string(base) {
		t.Error(`the same inputs produced a different proof`)
	}
	// length prefixing: two handshakes that differ only in where a boundary falls must
	// not hash the same bytes.  Without the prefixes these two would be identical.
	shifted, _ := newNonce()
	a := proof(testToken, clientProofLabel, cn, shifted, id, `ab`)
	b := proof(testToken, clientProofLabel, cn, shifted, id, `a`)
	if string(a) == string(b) {
		t.Error(`fields are not unambiguously separated`)
	}
}

// TestRPCErrors covers what comes back when a method fails, is unknown, or panics.
func TestRPCErrors(t *testing.T) {
	h := newHarness(t, echoMux(t))
	sess, err := h.dial(t, ClientConfig{ID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	ctx := context.Background()

	// a method that ran and failed is a RemoteError, distinct from the transport dying
	if err = sess.Call(ctx, `boom`, nil, nil); err == nil {
		t.Error(`a failing method should return an error`)
	} else if re, ok := errors.AsType[RemoteError](err); !ok {
		t.Errorf("got %T %v, want a RemoteError", err, err)
	} else if !strings.Contains(re.Message, `always fails`) {
		t.Errorf("RemoteError did not carry the reason: %v", re)
	}

	if err = sess.Call(ctx, `nope`, nil, nil); err == nil {
		t.Error(`an unknown method should return an error`)
	} else if !strings.Contains(err.Error(), ErrUnknownMethod.Error()) {
		t.Errorf("got %v, want an unknown method error", err)
	}

	if err = sess.Call(ctx, ``, nil, nil); !errors.Is(err, ErrEmptyMethod) {
		t.Errorf("got %v, want ErrEmptyMethod", err)
	}

	// a panicking handler answers with an error rather than hanging the caller or
	// taking the connection down
	if err = sess.Call(ctx, `panic`, nil, nil); err == nil {
		t.Error(`a panicking handler should return an error`)
	}
	// and the session still works afterwards
	var reply echoArgs
	if err = sess.Call(ctx, `echo`, echoArgs{Value: `still here`}, &reply); err != nil {
		t.Fatalf("the session did not survive a panicking handler: %v", err)
	} else if reply.Value != `still here` {
		t.Errorf("echo returned %q", reply.Value)
	}
}

// TestBidirectional covers the server calling the client, which is how a config push
// reaches an ingester that is behind a firewall.
func TestBidirectional(t *testing.T) {
	clientMux := NewMux()
	var got string
	var mtx sync.Mutex
	if err := clientMux.Register(`applyConfig`, func(_ context.Context, params json.RawMessage) (any, error) {
		var a echoArgs
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		mtx.Lock()
		got = a.Value
		mtx.Unlock()
		return echoArgs{Value: `applied`}, nil
	}); err != nil {
		t.Fatal(err)
	}

	h := newHarness(t, nil) // the server serves nothing, it only calls
	sess, err := h.dial(t, ClientConfig{ID: uuid.New(), Handlers: clientMux})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	var srvSess *Session
	select {
	case srvSess = <-h.sessions:
	case <-time.After(5 * time.Second):
		t.Fatal(`no server session`)
	}

	var reply echoArgs
	if err = srvSess.Call(context.Background(), `applyConfig`, echoArgs{Value: `new config`}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Value != `applied` {
		t.Errorf("client replied %q", reply.Value)
	}
	mtx.Lock()
	defer mtx.Unlock()
	if got != `new config` {
		t.Errorf("the client handler saw %q", got)
	}
}

// TestConcurrentCalls checks that responses are matched to their own callers.
func TestConcurrentCalls(t *testing.T) {
	h := newHarness(t, echoMux(t))
	sess, err := h.dial(t, ClientConfig{ID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	const n = 64
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("call-%d", i)
			var reply echoArgs
			if err := sess.Call(context.Background(), `echo`, echoArgs{Value: want}, &reply); err != nil {
				errs <- err
			} else if reply.Value != want {
				errs <- fmt.Errorf("call %d got %q, want %q", i, reply.Value, want)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestSessionClose checks that a closed session fails its callers rather than hanging.
func TestSessionClose(t *testing.T) {
	h := newHarness(t, echoMux(t))
	sess, err := h.dial(t, ClientConfig{ID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	if err = sess.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = sess.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Done():
	case <-time.After(5 * time.Second):
		t.Fatal(`Done was never closed`)
	}
	if sess.Err() == nil {
		t.Error(`a closed session should report why`)
	}
	if err = sess.Call(context.Background(), `echo`, echoArgs{}, nil); err == nil {
		t.Error(`a call on a closed session should fail`)
	}
	if err = sess.Close(); err != nil {
		t.Errorf("Close should be idempotent, got %v", err)
	}

	// a caller whose own context expires gives up without disturbing the session
	live, err := h.dial(t, ClientConfig{ID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	ctx, cf := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cf()
	if err = live.Call(ctx, `echo`, echoArgs{}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want a deadline error", err)
	}
	if err = live.Ping(context.Background()); err != nil {
		t.Errorf("the session should still be usable: %v", err)
	}
}

// TestServerConfig covers the configuration guards.
func TestServerConfig(t *testing.T) {
	if _, err := NewServer(ServerConfig{}); !errors.Is(err, ErrEmptyToken) {
		t.Errorf("a server with no token should be refused, got %v", err)
	}
	if _, err := NewServer(ServerConfig{Token: testToken}); err != nil {
		t.Error(err)
	}
	if _, err := Dial(context.Background(), ClientConfig{Webserver: `http://127.0.0.1:1`}); !errors.Is(err, ErrEmptyToken) {
		t.Errorf("a client with no token should be refused, got %v", err)
	}
}

// TestWSURL covers the endpoint translation, the client is handed the same normalized
// form Config.Verify produces.
func TestWSURL(t *testing.T) {
	for _, tc := range []struct {
		in   string
		path string
		want string
	}{
		{`http://10.0.0.1:8080`, ``, `ws://10.0.0.1:8080` + DefaultPath},
		{`https://gw.example.com`, ``, `wss://gw.example.com` + DefaultPath},
		{`10.0.0.1:8080`, ``, `ws://10.0.0.1:8080` + DefaultPath},
		{`http://10.0.0.1:8080`, `/custom`, `ws://10.0.0.1:8080/custom`},
		{`ws://10.0.0.1:8080`, ``, `ws://10.0.0.1:8080` + DefaultPath},
		{`wss://gw.example.com`, ``, `wss://gw.example.com` + DefaultPath},
	} {
		got, err := ClientConfig{Webserver: tc.in, Path: tc.path}.wsURL()
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
		} else if got != tc.want {
			t.Errorf("%q -> %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{``, `ftp://foo`, `http://`} {
		if got, err := (ClientConfig{Webserver: bad}).wsURL(); err == nil {
			t.Errorf("%q should not translate, got %q", bad, got)
		}
	}
}

// TestMux covers the method registry.
func TestMux(t *testing.T) {
	m := NewMux()
	h := func(context.Context, json.RawMessage) (any, error) { return nil, nil }
	if err := m.Register(`a`, h); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(`a`, h); !errors.Is(err, ErrDuplicateMethod) {
		t.Errorf("got %v, want ErrDuplicateMethod", err)
	}
	if err := m.Register(``, h); !errors.Is(err, ErrEmptyMethod) {
		t.Errorf("got %v, want ErrEmptyMethod", err)
	}
	if err := m.Register(`b`, nil); err == nil {
		t.Error(`a nil handler should be refused`)
	}
	if names := m.Methods(); len(names) != 1 || names[0] != `a` {
		t.Errorf("Methods = %v", names)
	}
	// a nil Mux serves nothing rather than panicking, an end that only calls out is a
	// legitimate configuration
	var nilMux *Mux
	if _, ok := nilMux.lookup(`a`); ok {
		t.Error(`a nil mux should serve nothing`)
	}
	if names := nilMux.Methods(); len(names) != 0 {
		t.Errorf("a nil mux listed %v", names)
	}
}

// TestPingAlwaysAnswered checks that liveness works even against an end that registered
// no methods at all.
func TestPingAlwaysAnswered(t *testing.T) {
	h := newHarness(t, nil)
	sess, err := h.dial(t, ClientConfig{ID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	for i := range 3 {
		if err = sess.Ping(context.Background()); err != nil {
			t.Fatalf("ping %d: %v", i, err)
		}
	}
	// and in the other direction
	select {
	case srvSess := <-h.sessions:
		if err = srvSess.Ping(context.Background()); err != nil {
			t.Errorf("server to client ping: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal(`no server session`)
	}
}

// TestIdleTimeoutAndKeepalive covers how a dead peer is noticed.  The websocket library
// here gives us no ping control frames, so liveness rests entirely on the built in ping
// method and the server's read deadline.
func TestIdleTimeoutAndKeepalive(t *testing.T) {
	srv, err := NewServer(ServerConfig{
		Token:          testToken,
		IdleTimeout:    300 * time.Millisecond,
		AuthRateWindow: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// a client that keeps pinging well inside the idle timeout stays connected
	live, err := Dial(context.Background(), ClientConfig{
		Webserver:    ts.URL,
		Token:        testToken,
		ID:           uuid.New(),
		PingInterval: 40 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	if live.RemoteAddr() == `` {
		t.Error(`a session should know its peer address`)
	}
	if live.Context() == nil {
		t.Error(`a session should expose a context`)
	}
	time.Sleep(time.Second) // several idle timeouts worth
	select {
	case <-live.Done():
		t.Fatalf("a session with a keepalive was dropped: %v", live.Err())
	default:
	}
	if err = live.Ping(context.Background()); err != nil {
		t.Errorf("the session should still work: %v", err)
	}

	// a client that says nothing is dropped by the server
	quiet, err := Dial(context.Background(), ClientConfig{
		Webserver:    ts.URL,
		Token:        testToken,
		ID:           uuid.New(),
		PingInterval: -1, // no keepalive
	})
	if err != nil {
		t.Fatal(err)
	}
	defer quiet.Close()
	select {
	case <-quiet.Done():
	case <-time.After(5 * time.Second):
		t.Error(`an idle session was never dropped, a dead ingester would hold the socket forever`)
	}
}
