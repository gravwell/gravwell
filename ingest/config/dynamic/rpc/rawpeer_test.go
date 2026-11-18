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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
	"uuid"
)

// These tests drive one end of the handshake by hand.  Running the real client against
// the real server only ever proves that the two agree with each other: with a wrong
// token the client rejects the server's proof and hangs up before the server gets to
// check anything, so a server that accepted everything would still look correct.  Each
// side has to be tested against a peer that does not play along.

// rawDial opens a websocket to a test server without doing any authentication.
func rawDial(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	uri := `ws` + strings.TrimPrefix(ts.URL, `http`) + DefaultPath
	conn, err := websocket.Dial(uri, ``, ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	return conn
}

// TestServerRejectsBadClientProof drives the server with a client that does not check the
// server's proof and simply asserts whatever it likes.  This is the only test that
// actually exercises the server's verification.
func TestServerRejectsBadClientProof(t *testing.T) {
	h := newHarness(t, nil)

	for _, tc := range []struct {
		name  string
		proof func(cnonce, snonce []byte, id uuid.UUID) []byte
	}{
		{`garbage proof`, func(_, _ []byte, _ uuid.UUID) []byte {
			return make([]byte, 32)
		}},
		{`proof under the wrong token`, func(cn, sn []byte, id uuid.UUID) []byte {
			return proof(`the-wrong-token`, clientProofLabel, cn, sn, id, ``)
		}},
		{`the server's own proof reflected back`, func(cn, sn []byte, id uuid.UUID) []byte {
			return proof(testToken, serverProofLabel, cn, sn, id, ``)
		}},
		{`a proof for a different identity`, func(cn, sn []byte, _ uuid.UUID) []byte {
			return proof(testToken, clientProofLabel, cn, sn, uuid.New(), ``)
		}},
		{`a proof over a nonce we chose instead of the server's`, func(cn, _ []byte, id uuid.UUID) []byte {
			other, _ := newNonce()
			return proof(testToken, clientProofLabel, cn, other, id, ``)
		}},
	} {
		conn := rawDial(t, h.srv)
		id := uuid.New()
		cnonce, err := newNonce()
		if err != nil {
			t.Fatal(err)
		}
		if err = websocket.JSON.Send(conn, clientHello{Version: ProtocolVersion, Nonce: cnonce, ID: id}); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var sc serverChallenge
		if err = websocket.JSON.Receive(conn, &sc); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if err = websocket.JSON.Send(conn, clientResponse{Proof: tc.proof(cnonce, sc.Nonce, id)}); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var res authResult
		err = websocket.JSON.Receive(conn, &res)
		if err == nil && res.OK {
			t.Errorf("%s: the server accepted it", tc.name)
		}
		conn.Close()
	}
}

// TestServerRejectsMalformedHello covers the guards ahead of the proof check.
func TestServerRejectsMalformedHello(t *testing.T) {
	h := newHarness(t, nil)
	short, _ := newNonce()
	for _, tc := range []struct {
		name  string
		hello clientHello
	}{
		{`wrong version`, clientHello{Version: ProtocolVersion + 1, Nonce: short}},
		{`zero version`, clientHello{Nonce: short}},
		{`no nonce`, clientHello{Version: ProtocolVersion}},
		{`short nonce`, clientHello{Version: ProtocolVersion, Nonce: short[:8]}},
		{`long nonce`, clientHello{Version: ProtocolVersion, Nonce: append(short, short...)}},
	} {
		conn := rawDial(t, h.srv)
		if err := websocket.JSON.Send(conn, tc.hello); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		// the server must refuse rather than issue a challenge it would then accept
		var sc serverChallenge
		if err := websocket.JSON.Receive(conn, &sc); err == nil && len(sc.Proof) > 0 {
			t.Errorf("%s: the server issued a challenge to a malformed hello", tc.name)
		}
		conn.Close()
	}
}

// TestServerRejectsReplayedProof captures a proof from a real handshake and replays it on
// a fresh connection.  This is what the server's per connection nonce is for.
func TestServerRejectsReplayedProof(t *testing.T) {
	h := newHarness(t, nil)
	id := uuid.New()

	// a complete, legitimate handshake, holding on to everything that crossed the wire
	conn := rawDial(t, h.srv)
	cnonce, err := newNonce()
	if err != nil {
		t.Fatal(err)
	}
	if err = websocket.JSON.Send(conn, clientHello{Version: ProtocolVersion, Nonce: cnonce, ID: id}); err != nil {
		t.Fatal(err)
	}
	var sc serverChallenge
	if err = websocket.JSON.Receive(conn, &sc); err != nil {
		t.Fatal(err)
	}
	captured := proof(testToken, clientProofLabel, cnonce, sc.Nonce, id, ``)
	if err = websocket.JSON.Send(conn, clientResponse{Proof: captured}); err != nil {
		t.Fatal(err)
	}
	var res authResult
	if err = websocket.JSON.Receive(conn, &res); err != nil || !res.OK {
		t.Fatalf("the legitimate handshake did not succeed, the replay below proves nothing: %v %+v", err, res)
	}
	conn.Close()

	// now replay it verbatim, same client nonce, same proof, on a new connection
	replay := rawDial(t, h.srv)
	if err = websocket.JSON.Send(replay, clientHello{Version: ProtocolVersion, Nonce: cnonce, ID: id}); err != nil {
		t.Fatal(err)
	}
	var sc2 serverChallenge
	if err = websocket.JSON.Receive(replay, &sc2); err != nil {
		t.Fatal(err)
	}
	if string(sc2.Nonce) == string(sc.Nonce) {
		t.Fatal(`the server reused its nonce, every captured proof is replayable`)
	}
	if err = websocket.JSON.Send(replay, clientResponse{Proof: captured}); err != nil {
		t.Fatal(err)
	}
	var res2 authResult
	err = websocket.JSON.Receive(replay, &res2)
	if err == nil && res2.OK {
		t.Error(`the server accepted a replayed proof`)
	}
	replay.Close()
}

// fakeServer is a route handler that speaks the protocol by hand so that the client can
// be tested against a server that is not the real one.
type fakeServer struct {
	// challengeProof produces whatever proof the fake wants to present
	challengeProof func(hello clientHello, snonce []byte) []byte
	// gotResponse is signalled if the client ever sends a clientResponse
	gotResponse chan clientResponse
}

func (f *fakeServer) handler(conn *websocket.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	var hello clientHello
	if err := websocket.JSON.Receive(conn, &hello); err != nil {
		return
	}
	snonce, err := newNonce()
	if err != nil {
		return
	}
	sc := serverChallenge{Version: ProtocolVersion, Nonce: snonce}
	if f.challengeProof != nil {
		sc.Proof = f.challengeProof(hello, snonce)
	}
	if err = websocket.JSON.Send(conn, sc); err != nil {
		return
	}
	// give the client ample time to send a proof it should never send
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var cr clientResponse
	if err = websocket.JSON.Receive(conn, &cr); err == nil {
		select {
		case f.gotResponse <- cr:
		default:
		}
	}
}

func newFakeServer(t *testing.T, f *fakeServer) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler:   websocket.Handler(f.handler),
	})
	t.Cleanup(ts.Close)
	return ts
}

// TestClientNeverProvesToAnImpostor is the property that matters most on the client side.
// The client accepts configuration from whatever it connects to, so it must not hand a
// proof to an endpoint that could not prove itself first, or a rogue endpoint collects
// material to grind offline.
func TestClientNeverProvesToAnImpostor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proof func(clientHello, []byte) []byte
	}{
		{`no proof at all`, nil},
		{`garbage proof`, func(clientHello, []byte) []byte { return make([]byte, 32) }},
		{`proof under the wrong token`, func(h clientHello, sn []byte) []byte {
			return proof(`the-wrong-token`, serverProofLabel, h.Nonce, sn, h.ID, h.Class)
		}},
		{`the client's own label reflected`, func(h clientHello, sn []byte) []byte {
			return proof(testToken, clientProofLabel, h.Nonce, sn, h.ID, h.Class)
		}},
	} {
		f := &fakeServer{challengeProof: tc.proof, gotResponse: make(chan clientResponse, 1)}
		ts := newFakeServer(t, f)

		ctx, cf := context.WithTimeout(context.Background(), 10*time.Second)
		sess, err := Dial(ctx, ClientConfig{
			Webserver: ts.URL, Token: testToken, ID: uuid.New(), PingInterval: -1,
		})
		cf()
		if err == nil {
			sess.Close()
			t.Errorf("%s: the client authenticated against an impostor", tc.name)
		} else if !errors.Is(err, ErrAuthFailed) {
			t.Errorf("%s: got %v, want ErrAuthFailed", tc.name, err)
		}

		// the fake waited two seconds for a proof, it must not have received one
		select {
		case cr := <-f.gotResponse:
			t.Errorf("%s: the client sent a proof to an impostor: %x", tc.name, cr.Proof)
		default:
		}
	}
}

// TestClientAcceptsAGenuineServer is the control for the test above.  Without it a client
// that never sent a proof under any circumstances would pass.
func TestClientAcceptsAGenuineServer(t *testing.T) {
	f := &fakeServer{
		challengeProof: func(h clientHello, sn []byte) []byte {
			return proof(testToken, serverProofLabel, h.Nonce, sn, h.ID, h.Class)
		},
		gotResponse: make(chan clientResponse, 1),
	}
	ts := newFakeServer(t, f)

	ctx, cf := context.WithTimeout(context.Background(), 10*time.Second)
	defer cf()
	// the fake never sends an authResult so the dial fails, that is fine, what matters is
	// that the client got far enough to prove itself
	if sess, err := Dial(ctx, ClientConfig{
		Webserver: ts.URL, Token: testToken, ID: uuid.New(), PingInterval: -1,
	}); err == nil {
		sess.Close()
	}
	select {
	case <-f.gotResponse:
	case <-time.After(5 * time.Second):
		t.Fatal(`the client never proved itself to a server that proved itself first`)
	}
}
