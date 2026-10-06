/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package rpc

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"net/http/httptest"
	"sync"
	"testing"
)

// recorder captures every byte that crosses the socket, in each direction separately.
//
// The capture has to be decoded rather than grepped.  RFC 6455 requires a client to mask
// every frame it sends, so a client to server payload is XORed with a per frame key and a
// plaintext search of the raw stream would find nothing no matter what was in it.  A test
// that searched the raw bytes would pass even if the token were sent in the clear, so the
// frames are parsed and unmasked here and the assertions run against the payloads.
type recorder struct {
	mtx sync.Mutex
	c2s bytes.Buffer // read by the server, masked by the client
	s2c bytes.Buffer // written by the server, never masked
}

func (r *recorder) recordC2S(b []byte) {
	r.mtx.Lock()
	r.c2s.Write(b)
	r.mtx.Unlock()
}

func (r *recorder) recordS2C(b []byte) {
	r.mtx.Lock()
	r.s2c.Write(b)
	r.mtx.Unlock()
}

// Raw is everything that crossed the wire, frames still encoded.  Useful only for
// checking the HTTP upgrade headers, see the note on recorder.
func (r *recorder) Raw() string {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	return r.c2s.String() + r.s2c.String()
}

// Payloads returns the HTTP upgrade headers plus every decoded, unmasked websocket
// message payload from both directions.  This is what a peer actually learns.
func (r *recorder) Payloads() (out []string) {
	r.mtx.Lock()
	c2s, s2c := r.c2s.Bytes(), r.s2c.Bytes()
	r.mtx.Unlock()
	for _, raw := range [][]byte{c2s, s2c} {
		head, frames := splitUpgrade(raw)
		if len(head) > 0 {
			out = append(out, string(head))
		}
		out = append(out, decodeFrames(frames)...)
	}
	return
}

// Joined is every payload as one string, for substring assertions.
func (r *recorder) Joined() (s string) {
	for _, p := range r.Payloads() {
		s += p + "\n"
	}
	return
}

// splitUpgrade peels the HTTP request or response that precedes the frames.
func splitUpgrade(b []byte) (head, rest []byte) {
	if i := bytes.Index(b, []byte("\r\n\r\n")); i >= 0 {
		return b[:i+4], b[i+4:]
	}
	return nil, b
}

// decodeFrames walks a websocket byte stream and returns each frame's payload, unmasking
// the ones that carry a masking key.  It stops at the first frame it cannot parse, a
// truncated tail is expected when a connection is torn down mid frame.
func decodeFrames(b []byte) (out []string) {
	for len(b) >= 2 {
		masked := b[1]&0x80 != 0
		size := uint64(b[1] & 0x7f)
		off := 2
		switch size {
		case 126:
			if len(b) < off+2 {
				return
			}
			size = uint64(binary.BigEndian.Uint16(b[off : off+2]))
			off += 2
		case 127:
			if len(b) < off+8 {
				return
			}
			size = binary.BigEndian.Uint64(b[off : off+8])
			off += 8
		}
		var key []byte
		if masked {
			if len(b) < off+4 {
				return
			}
			key = b[off : off+4]
			off += 4
		}
		if uint64(len(b)) < uint64(off)+size {
			return // truncated, nothing more to read
		}
		payload := make([]byte, size)
		copy(payload, b[off:uint64(off)+size])
		if masked {
			for i := range payload {
				payload[i] ^= key[i%4]
			}
		}
		out = append(out, string(payload))
		b = b[uint64(off)+size:]
	}
	return
}

// recordingListener wraps every accepted connection in a recordingConn.
type recordingListener struct {
	net.Listener
	rec *recorder
}

func (rl *recordingListener) Accept() (net.Conn, error) {
	c, err := rl.Listener.Accept()
	if err != nil {
		return c, err
	}
	return &recordingConn{Conn: c, rec: rl.rec}, nil
}

// recordingConn is the server side of the socket, so a Read is client to server and a
// Write is server to client.
type recordingConn struct {
	net.Conn
	rec *recorder
}

func (rc *recordingConn) Read(b []byte) (n int, err error) {
	if n, err = rc.Conn.Read(b); n > 0 {
		rc.rec.recordC2S(b[:n])
	}
	return
}

func (rc *recordingConn) Write(b []byte) (n int, err error) {
	if n, err = rc.Conn.Write(b); n > 0 {
		rc.rec.recordS2C(b[:n])
	}
	return
}

// newRecordingServer starts a route handler whose traffic is captured.
func newRecordingServer(t *testing.T, cfg ServerConfig) (*httptest.Server, *recorder) {
	t.Helper()
	if cfg.AuthRateWindow == 0 {
		cfg.AuthRateWindow = -1 // capture tests reconnect immediately, see newHarness
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	ts := httptest.NewUnstartedServer(srv)
	ts.Listener = &recordingListener{Listener: ts.Listener, rec: rec}
	ts.Start()
	t.Cleanup(ts.Close)
	return ts, rec
}

// TestRecorderDecodesMaskedFrames guards the guard.  If frame decoding silently stopped
// working, every assertion built on it would pass against an empty capture, so prove that
// a known client to server payload comes back out of the recorder in the clear.
func TestRecorderDecodesMaskedFrames(t *testing.T) {
	const canary = `CANARY-VALUE-IN-A-CLIENT-TO-SERVER-FRAME`
	ts, rec := newRecordingServer(t, ServerConfig{Token: testToken, Handlers: echoMux(t)})
	sess, err := Dial(context.TODO(), ClientConfig{
		Webserver: ts.URL, Token: testToken, Class: canary, PingInterval: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err = sess.Ping(context.TODO()); err != nil {
		t.Fatal(err)
	}

	// the class travels in the hello, which is a client to server frame and therefore
	// masked.  It must be invisible raw and visible decoded, otherwise the recorder is
	// not proving anything about that direction.
	if bytes.Contains([]byte(rec.Raw()), []byte(canary)) {
		t.Error(`the canary was readable in the raw stream, client frames are not masked?`)
	}
	if !bytes.Contains([]byte(rec.Joined()), []byte(canary)) {
		t.Fatal(`the recorder cannot decode client to server frames, every capture based test is vacuous`)
	}
}
