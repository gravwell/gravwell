/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

const (
	maxDataDrain = 1024 * 1024 * 4
)

var (
	ErrMaxBodyDrained = errors.New("too many response bytes in body, closing")
)

var (
	adminParams = []urlParam{
		urlParam{key: `admin`, value: `true`},
	}
)

// drainResponse will drain up to 4MB of data then close the response Body.
// We do this so that http requests can re-use connects as per docs.
func drainResponse(resp *http.Response) {
	if resp == nil {
		return
	}
	if resp.Body == nil {
		return
	}
	var nw nilWriter
	io.Copy(&nw, resp.Body)
	resp.Body.Close()
}

type nilWriter struct {
	n int
}

func (nw *nilWriter) Write(b []byte) (r int, err error) {
	if nw.n > maxDataDrain {
		r = -1
		err = ErrMaxBodyDrained
		return
	}
	nw.n += len(b)
	r = len(b)
	return
}

type jwtState struct {
	UID     int       `json:"uid"`
	Expires time.Time `json:"expires"`
}

// simple wrapper that decodes the JWT expire timestamp
// a zero time is returned on any decode failure
func decodeJWTExpires(jwt string) (r time.Time) {
	var st jwtState
	if bits := strings.Split(jwt, "."); len(bits) == 3 {
		if stateBts, err := hex.DecodeString(bits[1]); err == nil {
			if err = json.Unmarshal(stateBts, &st); err == nil {
				r = st.Expires
			}
		}
	}
	return
}

// aliasResponseError returns the error that corresponds to resp.StatusCode.
// If a non-200 status code is given, the response's body will be automatically drained.
//
// Returns nil iff status code == 200.
func aliasResponseError(c *Client, resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	defer drainResponse(resp)

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		c.state = STATE_LOGGED_OFF
		return ErrNotAuthed
	case http.StatusNotFound:
		return ErrNotFound
	default: // unhandled code
		return &ClientError{resp.Status, resp.StatusCode, getBodyErr(resp.Body)}
	}
}

// WebsocketConn implements a minimal websocket interface for sending objects as JSON encoded messages
// each Write call flushes the socket
type WebsocketConn interface {
	ReadJSON(v interface{}) error
	WriteJSON(v interface{}) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	Close() error
}

// wsJSONConn wraps a golang.org/x/net/websocket.Conn so that it satisfies
// the JSONConn interface using the package's JSON codec.
type wsJSONConn struct {
	conn *websocket.Conn
}

func (w *wsJSONConn) ReadJSON(v interface{}) error {
	return websocket.JSON.Receive(w.conn, v)
}

func (w *wsJSONConn) WriteJSON(v interface{}) error {
	return websocket.JSON.Send(w.conn, v)
}

func (w *wsJSONConn) SetReadDeadline(t time.Time) error {
	return w.conn.SetReadDeadline(t)
}

func (w *wsJSONConn) SetWriteDeadline(t time.Time) error {
	return w.conn.SetWriteDeadline(t)
}

func (w *wsJSONConn) Close() error {
	if err := w.conn.Close(); err != nil {
		// The underlying TLS connection may fail to send its closeNotify
		// alert if the peer already tore down the connection after the
		// websocket close handshake completed. The connection is closed
		// either way, so this specific error is not actionable.
		if strings.Contains(err.Error(), "failed to send closeNotify alert") {
			return nil
		}
		return err
	}
	return nil
}
