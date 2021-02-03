/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package websocketRouter

import (
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"time"

	"github.com/gravwell/gravwell/v3/client/objlog"
)

var (
	ErrInvalidTimeout = errors.New("Invalid timeout")
	ErrTimeout        = errors.New("Read timed out")
)

// SubProtoConn implements a subprotcol over the main routing websocket.
// A subprotocol connection can be treated as an independent connection with asynchronous read and write.
type SubProtoConn struct {
	subproto string
	ch       chan json.RawMessage
	sr       subParentRouter
	active   int32
	objLog   objlog.ObjLog
	timeout  time.Duration
}

// ReadJSON will read a message off of a SubProtoConn and attempt to unmarshal it into the provided object.
// If the object is nil or the message cannot be unmarshalled an error is returned.
func (sc *SubProtoConn) ReadJSON(obj interface{}) error {
	var msg json.RawMessage
	var ok bool
	if sc.timeout <= 0 {
		msg, ok = <-sc.ch
	} else {
		select {
		case <-time.After(sc.timeout):
			return ErrTimeout
		case msg, ok = <-sc.ch:
		}
	}
	if !ok {
		//channel closed, so it better not be active
		sc.active = 0
		return io.EOF
	}
	if err := json.Unmarshal(msg, obj); err != nil {
		return err
	}

	sc.objLog.Log("SUBPROTO GET", sc.subproto, obj)

	return nil
}

// WriteJSON will attempt to marshal the given object and write it to the subprotocol connection.
// If the object cannot be marshalled or if the subprotocol connection is down, an error is returned.
func (sc SubProtoConn) WriteJSON(obj interface{}) error {
	if sc.active == 0 {
		return io.EOF
	}
	if sc.sr == nil {
		//this should NEVER happen, but I am paranoid
		return errors.New("Parent Router NIL")
	}
	if err := sc.sr.writeProtoJSON(sc.subproto, obj); err != nil {
		return err
	}
	sc.objLog.Log("SUBPROTO PUT", sc.subproto, obj)
	return nil
}

// AddMessage is a convienence wrapper that allows for droping a raw JSON object onto the subprotocol connection.
// This API is primarily used for testing.
func (sc SubProtoConn) AddMessage(data json.RawMessage) error {
	if sc.active == 0 {
		return io.EOF
	}
	sc.ch <- data
	return nil
}

// Close terminates the subprotocol connection and cleans up.
// Any outstanding messages on the subprotocol connection will still be written.
// Any unread messages will be discarded.
func (sc *SubProtoConn) Close() error {
	if !atomic.CompareAndSwapInt32(&sc.active, 1, 0) {
		return io.EOF
	}
	// Attempt to drain the channel in case there were pending reads
drainLoop:
	for {
		select {
		case <-sc.ch:
		default:
			// nothing left on the channel
			break drainLoop
		}
	}
	close(sc.ch)
	return nil
}

// String implements the Stringer interface, essentially handing back the Subprotocol ID.
func (sc *SubProtoConn) String() string {
	return sc.subproto
}

// SetTimeout sets the per message timeout on the Subprotocol connection.
// The timeout is adhered to for reads and writes and only ensures that a message can be
// placed on the message queue.  A completed write does not mean the message made it all the way to the wire.
func (sc *SubProtoConn) SetTimeout(to time.Duration) error {
	if sc.active == 0 {
		return io.EOF
	}
	if to < 0 {
		return ErrInvalidTimeout
	}
	sc.timeout = to
	return nil
}
