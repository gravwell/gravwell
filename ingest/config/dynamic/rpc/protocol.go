/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package rpc implements the websocket transport that a dynamic ingester uses to talk to
// a Gravwell webserver.  A client dials the route handler, both ends authenticate against
// a shared token, and the connection is then used for RPC in either direction.
//
// # Authentication
//
// The shared token is never transmitted, in any form, in either direction.  It is used
// only as an HMAC-SHA256 key over nonces that both sides contribute to:
//
//	C -> S  clientHello     version, client nonce, ingester UUID and class
//	S -> C  serverChallenge server nonce and the server's proof
//	C -> S  clientResponse  the client's proof
//	S -> C  authResult      accepted or not
//
// Authentication is mutual.  The server proves knowledge of the token first, so a client
// that has been pointed at an impostor never hands over a proof of its own, which is the
// direction that matters here: the client accepts configuration from whatever it connects
// to.  The two proofs are computed over distinct labels so neither can be reflected back
// at its sender, and both cover the whole handshake, so a client cannot claim an identity
// other than the one it authenticated with.  Both nonces are fresh per connection, so a
// captured proof is worthless on any other connection.
//
// # What this does not protect against
//
// A challenge/response over a shared secret cannot hide a weak secret.  Anyone who can
// reach the route can collect one (nonce, nonce, proof) triple and grind it offline
// against a dictionary, and the same is true of a rogue server collecting a client proof.
// So the token must be high entropy, treated as a credential rather than a password, and
// the connection should be TLS.  This handshake authenticates the peer, it does not
// encrypt the session, and everything after it, including the configurations themselves,
// is plaintext on a ws:// connection.  Use wss:// anywhere the network is not trusted.
//
// If tokens chosen by humans ever have to be supported, the fix is a password
// authenticated key exchange rather than a bigger hash here.
package rpc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"uuid"
)

const (
	// ProtocolVersion is the wire version.  A mismatch is refused at the hello rather
	// than risking a subtly different handshake on either side.
	ProtocolVersion int = 1

	// DefaultPath is the route the handler is expected to be mounted on.
	DefaultPath string = `/api/dynamic/rpc` // TODO FIXME - figure out where this should actually live

	// MethodPing is answered by both ends automatically and is how liveness is checked.
	// The websocket library in use here does not expose ping control frames, so this
	// stands in for them.
	MethodPing string = `ping`

	// nonceLen is the size of each side's nonce.  Both are required to be exactly this
	// long, a short nonce from a peer is a protocol violation rather than something to
	// accommodate.
	nonceLen int = 32

	// the two proof labels keep the directions distinct.  Without them the server's
	// proof and the client's proof would be the same value and either side could simply
	// reflect what it was given.
	serverProofLabel string = `gravwell dynamic rpc server proof v1`
	clientProofLabel string = `gravwell dynamic rpc client proof v1`
)

var (
	ErrBadVersion      = errors.New("unsupported protocol version")
	ErrBadNonce        = errors.New("invalid nonce")
	ErrAuthFailed      = errors.New("authentication failed")
	ErrEmptyToken      = errors.New("empty authentication token")
	ErrEmptyMethod     = errors.New("empty method name")
	ErrUnknownMethod   = errors.New("unknown method")
	ErrSessionClosed   = errors.New("session is closed")
	ErrDuplicateMethod = errors.New("method already registered")
)

// clientHello opens the handshake.  The identity it carries is unauthenticated at this
// point, it is bound into both proofs so that by the end of the handshake it is not.
type clientHello struct {
	Version int
	Nonce   []byte
	ID      uuid.UUID `json:",omitzero"`
	Class   string    `json:",omitempty"`
}

// serverChallenge answers a hello with the server's own nonce and its proof.
type serverChallenge struct {
	Version int
	Nonce   []byte
	Proof   []byte
}

// clientResponse closes the loop with the client's proof.
type clientResponse struct {
	Proof []byte
}

// authResult is the server's verdict.  A rejected client is told only that it failed,
// never which part of the handshake was wrong.
type authResult struct {
	OK    bool
	Error string `json:",omitempty"`
}

// proof computes one side's challenge response.
//
// The token is the HMAC key, so it is never on the wire and never recoverable from what
// is.  Every variable length input is length prefixed, so no two different handshakes can
// be made to hash the same bytes by shuffling a class name into a nonce.
func proof(token, label string, clientNonce, serverNonce []byte, id uuid.UUID, class string) []byte {
	mac := hmac.New(sha256.New, []byte(token))
	writeChunk(mac, []byte(label))
	writeChunk(mac, clientNonce)
	writeChunk(mac, serverNonce)
	writeChunk(mac, id[:])
	writeChunk(mac, []byte(class))
	return mac.Sum(nil)
}

// writeChunk writes a length prefixed chunk into the MAC.  hash.Hash never returns an
// error from Write, so there is nothing to check.
func writeChunk(h hash.Hash, b []byte) {
	var l [8]byte
	binary.BigEndian.PutUint64(l[:], uint64(len(b)))
	h.Write(l[:])
	h.Write(b)
}

// newNonce returns a fresh nonce.  A failure here means the system entropy source is
// broken, which is fatal to the handshake rather than something to work around.
func newNonce() (b []byte, err error) {
	b = make([]byte, nonceLen)
	if _, err = rand.Read(b); err != nil {
		err = fmt.Errorf("failed to generate nonce %w", err)
		b = nil
	}
	return
}

// validNonce reports whether a peer supplied nonce is the right size.
func validNonce(b []byte) bool {
	return len(b) == nonceLen
}

// msgType distinguishes the two things that travel over an authenticated session.
type msgType uint8

const (
	msgRequest  msgType = 1
	msgResponse msgType = 2
)

// message is the RPC envelope.  Requests and responses share one envelope because the
// session is symmetric, either end may call the other.
type message struct {
	Type   msgType
	ID     uint64
	Method string          `json:",omitempty"`
	Params json.RawMessage `json:",omitempty"`
	Error  string          `json:",omitempty"`
	Result json.RawMessage `json:",omitempty"`
}

// RemoteError is returned by Call when the peer ran the method and the method failed.
// It is distinct from a transport error so that a caller can tell "the other end said no"
// apart from "the other end is gone".
type RemoteError struct {
	Method  string
	Message string
}

func (re RemoteError) Error() string {
	return fmt.Sprintf("remote method %s failed: %s", re.Method, re.Message)
}
